// Package httpserver 提供 REST API 与内嵌前端静态资源。
package httpserver

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/linuxsuren/sadp-hub/internal/sadp"
	"github.com/linuxsuren/sadp-hub/internal/version"
	"github.com/linuxsuren/sadp-hub/web"
)

// Server 汇聚 API 路由。
type Server struct{}

// NewServer 创建服务。
func NewServer() (*Server, error) {
	return &Server{}, nil
}

// Handler 返回完整 HTTP 路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": version.Version, "platform": version.Platform()})
	})

	// 发现扫描
	mux.HandleFunc("POST /api/scan", s.scan)

	// 修改设备网络参数
	mux.HandleFunc("POST /api/modify", s.modify)
	// 读取设备当前网络配置（寻址方式/IP/掩码/网关）
	mux.HandleFunc("POST /api/devicenet", s.deviceNet)

	// 静态前端（SPA 兜底到 index.html）
	dist, err := fs.Sub(web.Dist(), "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(dist))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path != "" {
				if f, err := dist.Open(path); err == nil {
					_ = f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
			}
			// SPA 路由兜底
			index, err := dist.Open("index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer func() { _ = index.Close() }()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			buf := make([]byte, 0, 4096)
			tmp := make([]byte, 4096)
			for {
				n, err := index.Read(tmp)
				buf = append(buf, tmp[:n]...)
				if err != nil {
					break
				}
			}
			_, _ = w.Write(buf)
		})
	}
	return mux
}

// scan POST /api/scan：一次组播发现。
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TimeoutMs int    `json:"timeoutMs"`
		Types     string `json:"types"`
		Keyword   string `json:"keyword"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	devs, err := sadp.Discover(r.Context(), sadp.Options{
		Timeout: timeout, Types: req.Types, Keyword: req.Keyword,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"devices": devs, "count": len(devs)})
}

// modify POST /api/modify：向设备下发网络参数修改。
// via=isapi（默认，新型号固件，Digest 认证）或 via=sadp（老固件 SADP 修改方言）。
func (s *Server) modify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceIP string `json:"deviceIp"`
		MAC      string `json:"mac"`
		NewIP    string `json:"newIp"`
		NetMask  string `json:"netMask"`
		Gateway  string `json:"gateway"`
		HTTPPort int    `json:"httpPort"`
		SdkPort  int    `json:"sdkPort"`
		Username string `json:"username"`
		Password string `json:"password"`
		Via      string `json:"via"`
		DHCP     bool   `json:"dhcp"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	var reply string
	var err error
	if req.Via == "sadp" {
		reply, _, err = sadp.Modify(sadp.ModifyOptions{
			DeviceIP: req.DeviceIP, MAC: req.MAC,
			NewIP: req.NewIP, NetMask: req.NetMask, Gateway: req.Gateway,
			HTTPPort: req.HTTPPort, SdkPort: req.SdkPort,
			Username: req.Username, Password: req.Password,
			Timeout: 5 * time.Second,
		})
	} else {
		reply, _, err = sadp.IsapiModify(sadp.IsapiOptions{
			DeviceIP: req.DeviceIP, MAC: req.MAC, HTTPPort: req.HTTPPort,
			Username: req.Username, Password: req.Password,
			NewIP: req.NewIP, NetMask: req.NetMask, Gateway: req.Gateway,
			DHCP:    req.DHCP,
			Timeout: 10 * time.Second,
		})
	}
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "reply": reply})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "reply": reply})
}

// deviceNet POST /api/devicenet：读取设备当前网络配置（寻址方式/IP/掩码/网关）。
func (s *Server) deviceNet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceIP string `json:"deviceIp"`
		MAC      string `json:"mac"`
		HTTPPort int    `json:"httpPort"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ifc, err := sadp.IsapiGetNetwork(sadp.IsapiOptions{
		DeviceIP: req.DeviceIP, MAC: req.MAC, HTTPPort: req.HTTPPort,
		Username: req.Username, Password: req.Password,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":             true,
		"addressingType": ifc.IPAddress.AddressingType,
		"ip":             ifc.IPAddress.IPAddress,
		"netMask":        ifc.IPAddress.SubnetMask,
		"gateway":        ifc.IPAddress.DefaultGateway.IPv4Address,
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decodeBody(r *http.Request, v interface{}) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}
