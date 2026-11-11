package sadp

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ModifyOptions 修改设备网络参数（SADP 修改方言）。
type ModifyOptions struct {
	DeviceIP string // 设备当前 IPv4
	MAC      string // 设备 MAC（TargetMAC）
	NewIP    string // 新 IPv4
	NetMask  string // 新子网掩码
	Gateway  string // 新网关
	HTTPPort int    // 新 HTTP 端口（0 不变）
	SdkPort  int    // 新 SDK 端口（0 不变）
	Username string // 设备用户名（默认 admin）
	Password string // 设备密码
	Timeout  time.Duration
}

// modifyResp 修改应答（方言：<SADP><Types>/<Status> 或 ProbeMatch）。
type modifyResp struct {
	Types    string `xml:"Types"`
	Status   string `xml:"Status"`
	DeviceSN string `xml:"DeviceSN"`
}

// Modify 单播向设备下发网络参数修改请求，返回设备原始应答。
// 注意：仅对未开启 SADP 加密（Encrypt=false）的设备生效；
// 现代海康固件（Encrypt=true，SADP v3.x）需要私有密钥协商，本工具会返回设备拒绝应答。
func Modify(opts ModifyOptions) (reply string, ok bool, err error) {
	if opts.DeviceIP == "" || opts.NewIP == "" || opts.MAC == "" {
		return "", false, errors.New("DeviceIP、NewIP、MAC 必填")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.Username == "" {
		opts.Username = "admin"
	}

	nonce := newUUID()
	// SADP 修改方言口令摘要：Base64(MD5( md5(密码) : md5(随机数) ))
	passwd := ""
	if opts.Password != "" {
		sum := md5.Sum([]byte(opts.Password))
		md5Pass := hex.EncodeToString(sum[:])
		sum2 := md5.Sum([]byte(nonce))
		md5Nonce := hex.EncodeToString(sum2[:])
		sum3 := md5.Sum([]byte(md5Pass + ":" + md5Nonce))
		passwd = base64.StdEncoding.EncodeToString(sum3[:])
	}

	user := ""
	if passwd != "" {
		user = fmt.Sprintf("<User Name=%q Password=%q/>", opts.Username, passwd)
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<SADP>
<Uuid>%s</Uuid>
<Types>modify</Types>
%s
<Random>%s</Random>
<TargetMAC>%s</TargetMAC>
<IpAddress>%s</IpAddress>
<SubnetMask>%s</SubnetMask>
<Gataway>%s</Gataway>
<Port>%d</Port>
<HttpPort>%d</HttpPort>
</SADP>`,
		newUUID(), user, nonce,
		normalizeMAC(opts.MAC),
		opts.NewIP, opts.NetMask, opts.Gateway,
		opts.SdkPort, opts.HTTPPort)

	// 临时端口 socket 单播发往设备 37020，收应答
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(opts.DeviceIP), Port: DefaultPort})
	if err != nil {
		return "", false, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(body)); err != nil {
		return "", false, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(opts.Timeout))

	buf := make([]byte, 16384)
	n, err := conn.Read(buf)
	if err != nil {
		return "", false, fmt.Errorf("设备无应答（超时 %.0fs）：%w", opts.Timeout.Seconds(), err)
	}
	reply = string(buf[:n])

	var resp modifyResp
	if err := parseXML(buf[:n], &resp); err == nil {
		// 成功判定：Types 含 modify 且无 Status/Deny/拒绝标记
		low := strings.ToLower(reply)
		switch {
		case strings.Contains(low, "deny") || strings.Contains(low, "lock"):
			return reply, false, errors.New("设备拒绝（密码错误或已锁定）")
		case strings.Contains(low, "encrypt"):
			return reply, false, errors.New("设备开启了 SADP 加密（Encrypt=true），需要海康官方工具的私有密钥协商，本工具不支持修改该设备")
		}
		return reply, true, nil
	}
	return reply, false, errors.New("无法解析设备应答")
}

// normalizeMAC 统一 MAC 为 xx-xx-xx-xx-xx-xx 小写（海康方言）。
func normalizeMAC(mac string) string {
	mac = strings.ToLower(strings.NewReplacer(":", "-", " ", "").Replace(mac))
	if len(mac) == 17 {
		return mac
	}
	return mac
}
