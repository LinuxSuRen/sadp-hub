# sadp-hub

<p align="center">
  <img src="web/public/logo.svg" width="160" alt="sadp-hub logo" />
</p>

自托管的 **SADP 风格局域网设备发现工具**：基于 **UDP 组播（239.255.255.250:37020）** 的 XML 发现协议——客户端组播 Probe，设备单播回源应答序列号 / MAC / IP / 端口 / 激活状态（与海康 SADP、WS-Discovery 同类思路）。

树莓派 / 迷你主机 / NAS 一把梭：Go 单二进制内嵌 Web 管理台，无外部依赖、无持久化状态。

- **设备发现**：向所有组播网卡发 Probe，按序列号去重聚合应答，支持关键字（SN / IP / 名称 / 型号）过滤与扫描窗口调节
- **协议兼容**：解析真实海康 SADP v3.x `ProbeMatch` 报文（SN / 型号 / 掩码 / 网关 / DHCP / 固件版本 / 激活状态）
- **REST API**：`POST /api/scan`，供脚本集成
- **协议实现**：`internal/sadp`，约 250 行，可单独复用

## 快速开始

```bash
cd web && npm install && npm run build && cd ..
go build -o sadp-hub .
./sadp-hub -addr :8080
# 浏览器打开 http://localhost:8080，点「扫描」
```

## 工作原理

```
客户端（发现）                          设备（海康相机/NVR 等）
────────────────                       ────────────────
UDP 组播 Probe ──────────────────────→ 设备监听 239.255.255.250:37020
  <Probe><Uuid><Types>inquiry           ← 单播回源 ProbeMatch XML
   <ResponseMode>2</ResponseMode>         序列号/型号/MAC/IP/端口/激活状态
按序列号去重、关键字过滤、列表展示
```

与海康 SADP 的异同：

| | 海康 SADP | sadp-hub |
|---|---|---|
| 发现 | UDP 组播 239.255.255.250:37020 XML | 同款协议（Probe / ProbeMatch 方言） |
| 跨网段二层搜索 | WinPcap 原始帧按 MAC | 未实现（可扩展 gopacket） |
| 激活 / 改密 / 改 IP | RSA + AES 私有安全通道 | 不涉及，仅发现层 |

## REST API

```bash
# 一次扫描（3 秒窗口，关键字过滤）
curl -X POST http://localhost:8080/api/scan \
  -H 'Content-Type: application/json' \
  -d '{"timeoutMs": 3000, "keyword": "IPC"}'

# 返回：{"count":2,"devices":[{"serialNo":"...","model":"...","ipv4":"...","mac":"...","httpPort":80,"sdkPort":8000,"activated":true,"version":"...","source":"192.168.1.64:37020"}, ...]}
```

> 组播发现层无鉴权、仅暴露设备标识（序列号/MAC/IP），与 SADP 的发现层思路一致；敏感操作请走独立鉴权通道。

## Windows 实测陷阱（发现链路三个坑）

| 现象 | 根因与解法 |
|---|---|
| 本机进程收不到自己发的组播 | Windows 组播回环副本只送达发送 socket 自身——发送侧显式 `SetMulticastLoopback(true)` |
| 应答被同端口进程抢走 | 绑定 37020 会与其他 SADP 工具撞端口，应答按源端口回源会撞到自身——改**临时端口绑定 + 显式 `JoinGroup`** |
| 真机收不到 Probe | 未绑定网卡的 socket 组播出口由系统默认路由决定——逐网卡 `SetMulticastInterface` 显式指定出口 |

## 开发

```bash
make test    # go test ./...
make web     # 前端构建
make         # 前端 + 后端全量构建
```
