// Package sadp 实现海康 SADP 风格的 UDP 组播发现协议：
// 组播 Probe（inquiry + ResponseMode），设备以 ProbeMatch XML 单播回源应答。
package sadp

import (
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// 组播组与端口（海康 SADP：SSDP 组播地址 + 37020 端口）。
const (
	MulticastAddr = "239.255.255.250"
	DefaultPort   = 37020
)

// Device 一台被发现（或被模拟）的设备，统一真实设备与模拟设备的字段。
type Device struct {
	Uuid        string `json:"uuid"`
	DeviceType  string `json:"deviceType"` // 模拟设备的类型（IPC/NVR…）；真实设备为数字码
	Model       string `json:"model"`      // 真实设备的型号描述
	Name        string `json:"name"`       // 模拟设备名称；真实设备回落为型号
	MAC         string `json:"mac"`
	SerialNO    string `json:"serialNo"`
	IPv4Address string `json:"ipv4"`
	IPv6Address string `json:"ipv6,omitempty"`
	NetMask     string `json:"netMask,omitempty"`
	Gateway     string `json:"gateway,omitempty"`
	DHCP        bool   `json:"dhcp"`
	HttpPort    int    `json:"httpPort"`
	SdkPort     int    `json:"sdkPort"`
	Activated   bool   `json:"activated"`
	Version     string `json:"version"`
	SADPVersion string `json:"sadpVersion,omitempty"`
	Source      string `json:"source"` // 应答来源地址 ip:port
}

// probeMatch 真实海康设备应答报文（SADP v3.x 方言，字段众多，只取关心的）。
type probeMatch struct {
	XMLName           xml.Name `xml:"ProbeMatch"`
	Uuid              string   `xml:"Uuid"`
	Types             string   `xml:"Types"`
	SADPVersion       string   `xml:"SADPVersion"`
	DeviceType        string   `xml:"DeviceType"`
	DeviceDescription string   `xml:"DeviceDescription"`
	DeviceSN          string   `xml:"DeviceSN"`
	CommandPort       int      `xml:"CommandPort"`
	HttpPort          int      `xml:"HttpPort"`
	MAC               string   `xml:"MAC"`
	IPv4Address       string   `xml:"IPv4Address"`
	IPv4SubnetMask    string   `xml:"IPv4SubnetMask"`
	IPv4Gateway       string   `xml:"IPv4Gateway"`
	IPv6Address       string   `xml:"IPv6Address"`
	DHCP              string   `xml:"DHCP"`
	SoftwareVersion   string   `xml:"SoftwareVersion"`
	Activated         string   `xml:"Activated"`
}

// simDevice sadp-hub 模拟器的应答报文（自有方言，字段语义直观）。
type simDevice struct {
	XMLName     xml.Name `xml:"Device"`
	Uuid        string   `xml:"Uuid"`
	DeviceType  string   `xml:"DeviceType"`
	Name        string   `xml:"Name"`
	MAC         string   `xml:"MAC"`
	SerialNO    string   `xml:"SerialNumber"`
	IPv4Address string   `xml:"IPv4Address"`
	IPv6Address string   `xml:"IPv6Address"`
	HttpPort    int      `xml:"HttpPort"`
	SdkPort     int      `xml:"SdkPort"`
	Activated   string   `xml:"Activated"`
	Version     string   `xml:"Version"`
}

func truthy(s string) bool {
	return strings.EqualFold(s, "true") || s == "1"
}

// MarshalProbe 生成 Probe 报文（真实 SADP v3.x 方言）。
func MarshalProbe(uuid, mac, types string) []byte {
	if types == "" {
		types = "inquiry"
	}
	body := struct {
		XMLName      xml.Name `xml:"Probe"`
		Uuid         string   `xml:"Uuid"`
		MAC          string   `xml:"MAC"`
		Types        string   `xml:"Types"`
		ResponseMode int      `xml:"ResponseMode"`
	}{Uuid: uuid, MAC: mac, Types: types, ResponseMode: 2}
	buf, _ := xml.Marshal(body)
	return append([]byte(xml.Header), append(buf, '\n')...)
}

// UnmarshalProbe 解析 Probe 报文。
func UnmarshalProbe(data []byte) (*struct {
	Uuid  string
	Types string
}, error,
) {
	var p struct {
		XMLName xml.Name `xml:"Probe"`
		Uuid    string   `xml:"Uuid"`
		MAC     string   `xml:"MAC"`
		Types   string   `xml:"Types"`
	}
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &struct {
		Uuid  string
		Types string
	}{Uuid: p.Uuid, Types: p.Types}, nil
}

// MarshalSimResponse 生成模拟器应答报文。
func MarshalSimResponse(dev Device) []byte {
	activated := "false"
	if dev.Activated {
		activated = "true"
	}
	buf, _ := xml.Marshal(simDevice{
		Uuid: dev.Uuid, DeviceType: dev.DeviceType, Name: dev.Name,
		MAC: dev.MAC, SerialNO: dev.SerialNO,
		IPv4Address: dev.IPv4Address, IPv6Address: dev.IPv6Address,
		HttpPort: dev.HttpPort, SdkPort: dev.SdkPort,
		Activated: activated, Version: dev.Version,
	})
	return append([]byte(xml.Header), append(buf, '\n')...)
}

// ParseResponse 解析设备应答：优先真实 ProbeMatch 方言，回落模拟器方言。
func ParseResponse(data []byte) (*Device, error) {
	var m probeMatch
	if err := xml.Unmarshal(data, &m); err == nil && (m.DeviceSN != "" || m.SADPVersion != "") {
		return &Device{
			Uuid:        m.Uuid,
			DeviceType:  m.DeviceType,
			Model:       m.DeviceDescription,
			Name:        m.DeviceDescription,
			MAC:         m.MAC,
			SerialNO:    m.DeviceSN,
			IPv4Address: m.IPv4Address,
			IPv6Address: m.IPv6Address,
			NetMask:     m.IPv4SubnetMask,
			Gateway:     m.IPv4Gateway,
			DHCP:        truthy(m.DHCP),
			HttpPort:    m.HttpPort,
			SdkPort:     m.CommandPort,
			Activated:   m.Activated == "" || truthy(m.Activated),
			Version:     m.SoftwareVersion,
			SADPVersion: m.SADPVersion,
		}, nil
	}
	var s simDevice
	if err := xml.Unmarshal(data, &s); err != nil || s.SerialNO == "" {
		return nil, fmt.Errorf("非设备应答报文")
	}
	return &Device{
		Uuid: s.Uuid, DeviceType: s.DeviceType, Name: s.Name,
		MAC: s.MAC, SerialNO: s.SerialNO,
		IPv4Address: s.IPv4Address, IPv6Address: s.IPv6Address,
		HttpPort: s.HttpPort, SdkPort: s.SdkPort,
		Activated: s.Activated == "" || truthy(s.Activated),
		Version:   s.Version,
	}, nil
}

// Options 一次发现扫描的参数。
type Options struct {
	Timeout time.Duration // 单次扫描窗口
	Types   string        // 类型过滤，空为 inquiry
	Keyword string        // 对 SN/IP/名称/型号做子串过滤
}

// Discover 在所有启用组播的 IPv4 网卡上发 Probe 并聚合应答（按 SN 去重）。
// Windows 组播默认不回环本机：显式开 IP_MULTICAST_LOOP，让本机模拟器也能被扫到。
func Discover(ctx context.Context, opts Options) ([]Device, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	port := DefaultPort
	group := &net.UDPAddr{IP: net.ParseIP(MulticastAddr), Port: port}

	ifaces, err := UsableInterfaces()
	if err != nil {
		return nil, err
	}
	if len(ifaces) == 0 {
		return nil, fmt.Errorf("没有可用组播网卡")
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var (
		mu       sync.Mutex
		bySN     = map[string]Device{}
		wg       sync.WaitGroup
		firstErr error
		sent     int
	)
	for _, ifc := range ifaces {
		// 临时端口绑定 + 显式 JoinGroup：避免绑定 37020 与本机设备端撞端口
		// （设备应答按源端口回源，绑定 37020 时本机应答会撞到自身）
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: nil, Port: 0})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p := ipv4.NewPacketConn(conn)
		if err := p.JoinGroup(&ifc, group); err != nil {
			_ = conn.Close()
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = p.SetMulticastLoopback(true)
		// 未绑定网卡的 socket 组播出口由系统默认路由决定，必须显式指定出口网卡
		var ifcV4 net.IP
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ifcV4 = ipnet.IP.To4()
				break
			}
		}
		if ifcV4 != nil {
			if err := p.SetMulticastInterface(&ifc); err != nil {
				_ = conn.Close()
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		probe := MarshalProbe(newUUID(), "", opts.Types)
		if _, err := p.WriteTo(probe, nil, group); err != nil {
			_ = conn.Close()
			continue
		}
		sent++
		wg.Add(1)
		go func(conn *net.UDPConn) {
			defer wg.Done()
			defer func() { _ = conn.Close() }()
			buf := make([]byte, 8192)
			for {
				_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				n, src, err := conn.ReadFromUDP(buf)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue // 窗口内超时，继续等下一帧
				}
				dev, err := ParseResponse(buf[:n])
				if err != nil || dev.SerialNO == "" {
					continue
				}
				dev.Source = src.String()
				mu.Lock()
				if _, dup := bySN[dev.SerialNO]; !dup {
					bySN[dev.SerialNO] = *dev
				}
				mu.Unlock()
			}
		}(conn)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	<-done

	devs := make([]Device, 0, len(bySN))
	for _, d := range bySN {
		if opts.Keyword == "" || matchKeyword(d, opts.Keyword) {
			devs = append(devs, d)
		}
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].SerialNO < devs[j].SerialNO })
	if sent == 0 && firstErr != nil {
		return nil, firstErr
	}
	return devs, nil
}

func matchKeyword(d Device, kw string) bool {
	low := strings.ToLower(kw)
	for _, s := range []string{d.SerialNO, d.IPv4Address, d.Name, d.Model, d.DeviceType, d.MAC} {
		if strings.Contains(strings.ToLower(s), low) {
			return true
		}
	}
	return false
}

// UsableInterfaces 返回所有 Up、支持组播、有 IPv4 地址的网卡（含回环，便于单机自测）。
func UsableInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []net.Interface
	for _, ifc := range all {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 {
			continue
		}
		has4 := false
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				has4 = true
				break
			}
		}
		if has4 {
			out = append(out, ifc)
		}
	}
	return out, nil
}

// newUUID 生成随机 UUID（v4 格式）。
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
