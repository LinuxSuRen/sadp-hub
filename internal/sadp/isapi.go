package sadp

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// IsapiOptions 通过设备 HTTP ISAPI 接口（Digest 认证）修改网络参数。
// 适用新型号固件（SADP Encrypt=true），需要知道设备密码。
type IsapiOptions struct {
	DeviceIP string
	HTTPPort int
	Username string
	Password string
	NewIP    string
	NetMask  string
	Gateway  string
	DHCP     bool // true：切换为 DHCP 自动获取（忽略 NewIP/NetMask/Gateway）
	Timeout  time.Duration
}

// isapiResponseStatus ISAPI 应答状态。
type isapiResponseStatus struct {
	XMLName       xml.Name `xml:"ResponseStatus"`
	StatusValue   int      `xml:"statusCode"`
	StatusStr     string   `xml:"statusString"`
	SubStatusCode string   `xml:"subStatusCode"`
}

// isapiNetworkInterface ISAPI 网络接口配置。
type isapiNetworkInterface struct {
	XMLName   xml.Name `xml:"NetworkInterface"`
	ID        string   `xml:"id"`
	IPAddress struct {
		IPVersion      string `xml:"ipVersion"`
		AddressingType string `xml:"addressingType"`
		IPAddress      string `xml:"ipAddress"`
		SubnetMask     string `xml:"subnetMask"`
		DefaultGateway struct {
			IPv4Address string `xml:"ipv4Address"`
		} `xml:"defaultGateway"`
	} `xml:"IPAddress"`
}

// IsapiModify 修改设备网络参数（read-before-write）。
// DHCP=false 写静态地址（新 IP/掩码/网关）；DHCP=true 仅切换寻址方式为 DHCP。
func IsapiModify(opts IsapiOptions) (reply string, ok bool, err error) {
	if opts.DeviceIP == "" {
		return "", false, errors.New("DeviceIP 必填")
	}
	if !opts.DHCP && opts.NewIP == "" {
		return "", false, errors.New("NewIP 必填（静态模式）")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Username == "" {
		opts.Username = "admin"
	}
	if opts.HTTPPort == 0 {
		opts.HTTPPort = 80
	}
	base := fmt.Sprintf("http://%s:%d", wrapIP6(opts.DeviceIP), opts.HTTPPort)

	// 直连设备：Proxy 必须是零值 nil（直连）。
	// 注意：Go 1.26 中自定义 Proxy 函数返回 nil 会回退到系统代理，导致内网请求被
	// HTTP_PROXY(127.0.0.1:7890) 劫持返回 502；零值 nil 才是真正直连。
	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: &http.Transport{Proxy: nil},
	}

	// 1. GET 当前接口配置原文（同时完成认证验证）；2. 文本打补丁；3. 原文回写
	// （不重新序列化：设备 v2.0 schema 字段众多，重排/缺字段会被拒 badXmlContent）
	raw, err := isapiGetRaw(client, base, opts)
	if err != nil {
		return "", false, err
	}

	patched, err := patchNetworkXML(raw, opts)
	if err != nil {
		return "", false, err
	}

	// 3. PUT 回写（新接口 /ISAPI/System/Network/interfaces/1，回落旧接口）
	uri := "/ISAPI/System/Network/interfaces/1"
	status, reply, err := isapiDo(client, http.MethodPut, base+uri, patched, opts)
	if err != nil && strings.Contains(strings.ToLower(reply), "not support") {
		uri = "/ISAPI/System/Network/IPAddress"
		status, reply, err = isapiDo(client, http.MethodPut, base+uri, string(body), opts)
	}
	if err != nil {
		return reply, false, err
	}
	if status.StatusValue != 1 {
		return reply, false, fmt.Errorf("设备返回 %d %s（%s）",
			status.StatusValue, status.StatusStr, status.SubStatusCode)
	}
	return reply, true, nil
}

// IsapiGetNetwork 读取设备当前网络配置（寻址方式/IP/掩码/网关）。
func IsapiGetNetwork(opts IsapiOptions) (*isapiNetworkInterface, error) {
	if opts.DeviceIP == "" {
		return nil, errors.New("DeviceIP 必填")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Username == "" {
		opts.Username = "admin"
	}
	if opts.HTTPPort == 0 {
		opts.HTTPPort = 80
	}
	// 直连设备：Proxy 零值 nil（直连），禁用系统代理（内网请求走代理会被 502 劫持）
	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: &http.Transport{Proxy: nil},
	}
	return isapiGet(client, fmt.Sprintf("http://%s:%d", wrapIP6(opts.DeviceIP), opts.HTTPPort), opts)
}

// wrapIP6 IPv6 地址作为 URL host 时需要方括号包裹，链路本地地址的 zone（%iface）需转义为 %25。
func wrapIP6(ip string) string {
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		ip = "[" + ip + "]"
	}
	if strings.HasPrefix(ip, "[") && strings.Contains(ip, "%") {
		ip = strings.Replace(ip, "%", "%25", 1)
	}
	return ip
}

// isapiGetRaw 读取接口配置 XML 原文。
func isapiGetRaw(client *http.Client, base string, opts IsapiOptions) (string, error) {
	_, body, err := isapiDo(client, http.MethodGet, base+"/ISAPI/System/Network/interfaces/1", "", opts)
	return body, err
}

// patchNetworkXML 在设备返回的原文上做最小替换：只改寻址方式与（静态时的）地址字段。
func patchNetworkXML(raw string, opts IsapiOptions) (string, error) {
	set := func(re, val, name string) (string, error) {
		r, err := regexp.Compile(re)
		if err != nil {
			return "", err
		}
		if !r.MatchString(raw) {
			return "", fmt.Errorf("设备应答中未找到 <%s> 字段", name)
		}
		return r.ReplaceAllString(raw, "${1}"+val+"${3}"), nil
	}

	if opts.DHCP {
		out, err := set(`(<addressingType>)([^<]*)(</addressingType>)`, "dhcp", "addressingType")
		if err != nil {
			return "", err
		}
		return out, nil
	}

	out := raw
	var err error
	if opts.NewIP != "" {
		if out, err = set(`(<ipAddress>)([^<]*)(</ipAddress>)`, opts.NewIP, "ipAddress"); err != nil {
			return "", err
		}
	}
	if opts.NetMask != "" {
		if out, err = set(`(<subnetMask>)([^<]*)(</subnetMask>)`, opts.NetMask, "subnetMask"); err != nil {
			return "", err
		}
	}
	if opts.Gateway != "" {
		if out, err = set(`(<ipv4Address>)([^<]*)(</ipv4Address>)`, opts.Gateway, "ipv4Address"); err != nil {
			return "", err
		}
	}
	if out, err = set(`(<addressingType>)([^<]*)(</addressingType>)`, "static", "addressingType"); err != nil {
		return "", err
	}
	return out, nil
}

func isapiGet(client *http.Client, base string, opts IsapiOptions) (*isapiNetworkInterface, error) {
	raw, err := isapiGetRaw(client, base, opts)
	if err != nil {
		return nil, err
	}
	var ifc isapiNetworkInterface
	if err := xml.Unmarshal([]byte(raw), &ifc); err != nil || ifc.ID == "" {
		return &isapiNetworkInterface{ID: "1"}, nil // 老固件接口结构不同，用默认骨架
	}
	return &ifc, nil
}

// isapiDo 执行一次 ISAPI 请求，401 时自动做 Digest 认证重试。
func isapiDo(client *http.Client, method, url, body string, opts IsapiOptions) (*isapiResponseStatus, string, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	log.Printf("[isapi] %s %s -> %s", method, url, resp.Status)

	// 401 → Digest 认证重试一次
	if resp.StatusCode == http.StatusUnauthorized {
		chal := resp.Header.Get("WWW-Authenticate")
		auth, err := digestAuth(method, req.URL.RequestURI(), chal, opts.Username, opts.Password)
		if err != nil {
			return nil, string(data), fmt.Errorf("设备要求认证但挑战头无法解析：%w", err)
		}
		req2, _ := http.NewRequest(method, url, strings.NewReader(body))
		req2.Header.Set("Authorization", auth)
		resp2, err := client.Do(req2)
		if err != nil {
			return nil, "", err
		}
		data, _ = io.ReadAll(resp2.Body)
		_ = resp2.Body.Close()
		resp = resp2
	}

	var status isapiResponseStatus
	_ = xml.Unmarshal(data, &status)

	// 从应答体提取更具体的错误描述（设备返回的错误通常在 ResponseStatus 里）
	detail := ""
	if status.StatusValue != 0 || status.StatusStr != "" {
		detail = fmt.Sprintf("%d %s（%s）", status.StatusValue, status.StatusStr, status.SubStatusCode)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// GET 的 NetworkInterface 应答没有 statusCode；PUT 的 ResponseStatus statusCode=1 表示成功
		if status.StatusValue != 0 && status.StatusValue != 1 {
			return &status, string(data), fmt.Errorf("设备返回 %d %s（%s）",
				status.StatusValue, status.StatusStr, status.SubStatusCode)
		}
		return &status, string(data), nil
	case resp.StatusCode == http.StatusUnauthorized:
		return &status, string(data), errors.New("用户名或密码错误（设备 401 拒绝）")
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
		return &status, string(data), fmt.Errorf("设备不支持该接口（HTTP %d）%s", resp.StatusCode, detail)
	case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable:
		return &status, string(data), fmt.Errorf(
			"HTTP %d：请求未到达设备或设备正忙（网络重启窗口期/代理劫持/固件重启中），稍后重试%s",
			resp.StatusCode, detailSnippet(data))
	default:
		return &status, string(data), fmt.Errorf("设备返回 HTTP %d %s %s",
			resp.StatusCode, detail, detailSnippet(data))
	}
}

// detailSnippet 从应答体截取有用的错误片段（ResponseStatus 之外的原始文本）。
func detailSnippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	// 已解析出 ResponseStatus 时正文不再重复展示
	if strings.Contains(s, "ResponseStatus") && (strings.Contains(s, "statusCode") || strings.Contains(s, "statusString")) {
		return ""
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return "｜应答：" + s
}

// digestAuth 按 RFC 2617 计算 Digest Authorization 头（无 qop 与 qop=auth 双兼容）。
func digestAuth(method, uri, challenge, user, pass string) (string, error) {
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(challenge, "Digest "), ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			fields[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	realm, nonce := fields["realm"], fields["nonce"]
	if realm == "" || nonce == "" {
		return "", errors.New("challenge 缺少 realm/nonce")
	}
	md5hex := func(s string) string {
		sum := md5.Sum([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	ha1 := md5hex(user + ":" + realm + ":" + pass)
	ha2 := md5hex(method + ":" + uri)

	parts := []string{
		`username="` + user + `"`,
		`realm="` + realm + `"`,
		`nonce="` + nonce + `"`,
		`uri="` + uri + `"`,
	}
	qop := fields["qop"]
	var response string
	if qop != "" {
		cnonce := newUUID()
		nc := "00000001"
		response = md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
		parts = append(parts, `cnonce="`+cnonce+`"`, `nc=`+nc, `qop=`+qop)
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	parts = append(parts, `response="`+response+`"`)
	if opaque := fields["opaque"]; opaque != "" {
		parts = append(parts, `opaque="`+opaque+`"`)
	}
	if algo := fields["algorithm"]; algo != "" {
		parts = append(parts, `algorithm=`+algo)
	}
	return "Digest " + strings.Join(parts, ", "), nil
}
