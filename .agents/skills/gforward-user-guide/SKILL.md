---
name: "gforward-user-guide"
description: "Installs, configures, runs, verifies, and troubleshoots gforward. Invoke when users ask to install gforward, start its client/server, configure a proxy mode, or diagnose usage issues."
---

# gforward User Guide

帮助用户安全地安装、配置、启动、验证和排查 gforward。执行命令时以当前仓库的 `README.md`、`AGENTS.md` 和 `--help` 输出为事实来源，不臆造尚未实现的参数或能力。

## 适用场景

在以下请求中使用本 Skill：

- 安装、升级或查找 gforward。
- 启动 gforward client 或 server。
- 配置 HTTP Proxy、SOCKS5、DNS 透明代理或 Shadowsocks 入站。
- 验证代理是否正常工作。
- 排查启动失败、连接失败、DNS 劫持失败或代理不可用。
- 将 gforward 配置为 systemd、launchd 等后台服务。

不用于修改 gforward 源码、协议或功能实现；此类请求按项目开发流程处理。

## 核心安全规则

必须先判断 client 到 server 的链路是否经过公网。

- gforward 私有协议没有认证、加密、完整性校验或目标 ACL。
- 不得建议把 server 的 `9989` 端口直接开放给公网。
- 私网部署也应使用防火墙或安全组限制可访问 server 的 client IP。
- 公网部署必须优先建议 WireGuard、SSH Tunnel 或 TLS Tunnel。
- Shadowsocks 密码只保护用户到 gforward client 的接入段，不保护 client 到 server 的隧道。
- 未经用户确认，不执行 `sudo`、修改防火墙、修改 DNS、写入 shell 配置或创建系统服务。
- 不在回复、日志或生成文件中回显用户的真实密码。

## 工作流程

按顺序执行以下步骤。能从本机环境确定的信息直接检测，不重复询问用户。

### 1. 确认部署目标

确定：

- 本机要运行 client、server，还是两者。
- client 与 server 是否同机、同一私网或跨公网。
- 用户需要的接入模式。
- client 监听地址和 server 地址。

模式不明确时按以下优先级推荐：

1. 应用支持显式 HTTP 代理：`http_proxy`。
2. 应用需要代理普通 TCP 或支持 SOCKS5：`socks5`。
3. 无法配置应用代理但能控制局域网 DNS：`http_dns` 或 `https_dns`。
4. 已有 Shadowsocks 客户端：`shadowsocks`。

### 2. 安装前检查

检查 Go：

```bash
go version
```

要求 Go 1.22 或更高版本。检查已安装命令：

```bash
command -v gforward
gforward --help
```

如果命令已存在，先报告路径和安装来源，不重复安装，除非用户要求升级。

### 3. 安装

#### 当前位于源码仓库

当当前目录的 `go.mod` module 为 `github.com/near-notfaraway/gforward` 时，优先安装当前工作区代码：

```bash
go install .
```

确定二进制位置：

```bash
GFORWARD_BIN="$(go env GOBIN)"
if [ -z "$GFORWARD_BIN" ]; then
  GFORWARD_BIN="$(go env GOPATH)/bin"
fi
printf '%s\n' "$GFORWARD_BIN/gforward"
```

如果目录不在 `PATH` 中，当前会话直接使用绝对路径。只有用户明确要求时才修改 shell 配置。

#### 未检出源码仓库

使用 Go 安装公开模块：

```bash
go install github.com/near-notfaraway/gforward@latest
```

安装后必须运行：

```bash
"$GFORWARD_BIN/gforward" --help
```

不要假设存在 Homebrew、APT、Docker 镜像或预编译 Release，除非实际检查后确认。

### 4. 启动 server

可信私网：

```bash
gforward server --listen 0.0.0.0:9989
```

SSH Tunnel 场景下，远端 server 只监听回环地址：

```bash
gforward server --listen 127.0.0.1:9989
```

不要在未确认网络边界和防火墙策略时使用公网监听地址。

### 5. 启动 client

#### HTTP Proxy

默认推荐只监听本机：

```bash
gforward client \
  --mode http_proxy \
  --listen 127.0.0.1:8080 \
  --server SERVER_IPV4:9989
```

验证：

```bash
curl -x http://127.0.0.1:8080 http://example.com/
curl -x http://127.0.0.1:8080 https://example.com/
```

#### SOCKS5

```bash
gforward client \
  --mode socks5 \
  --listen 127.0.0.1:1080 \
  --server SERVER_IPV4:9989
```

验证：

```bash
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

仅支持无认证 CONNECT，不支持 BIND、UDP ASSOCIATE 或用户名密码认证。

#### HTTP DNS

```bash
sudo gforward client \
  --mode http_dns \
  --listen CLIENT_LAN_IPV4:80 \
  --server SERVER_IPV4:9989
```

将终端设备的 DNS 设置为 `CLIENT_LAN_IPV4`。该模式绑定 UDP 53 和 TCP 80，通常需要管理员权限。

#### HTTPS DNS

```bash
sudo gforward client \
  --mode https_dns \
  --listen CLIENT_LAN_IPV4:443 \
  --server SERVER_IPV4:9989
```

该模式绑定 UDP 53 和 TCP 443，依赖 TLS ClientHello SNI。DNS 仅响应 IPv4 A 查询。

`http_dns` 和 `https_dns` 都会绑定 UDP 53，默认不能在同一主机同时运行。

#### Shadowsocks

不要生成或回显真实密码。让用户在 shell 中安全读入：

```bash
read -s GF_SS_PASSWORD
gforward client \
  --mode shadowsocks \
  --listen 127.0.0.1:8388 \
  --server SERVER_IPV4:9989 \
  --ss-method aes-256-gcm \
  --ss-password "$GF_SS_PASSWORD"
unset GF_SS_PASSWORD
```

可用算法：

- `aes-256-gcm`
- `chacha20-ietf-poly1305`

### 6. 公网安全连接

当 client 与 server 跨公网且用户没有 VPN 时，推荐 SSH Tunnel：

```bash
# 远端：server 仅监听回环地址
gforward server --listen 127.0.0.1:9989

# 本地：把本地 19989 转发到远端 server
ssh -N -L 19989:127.0.0.1:9989 user@server.example.com

# 本地：gforward client 连接 SSH Tunnel
gforward client \
  --mode http_proxy \
  --listen 127.0.0.1:8080 \
  --server 127.0.0.1:19989
```

不要同时再把远端 `9989` 开放到公网。

### 7. 验证运行状态

验证顺序：

1. 命令能启动且没有立即退出。
2. server 监听地址可从 client 到达。
3. client 本地端口已监听。
4. 使用对应模式的真实请求验证。
5. 请求失败时添加 `--verbose` 重试并读取首个明确错误。

macOS 可检查监听端口：

```bash
lsof -nP -iTCP -sTCP:LISTEN | grep gforward
```

Linux 可检查监听端口：

```bash
ss -lntp | grep gforward
```

### 8. 故障排查

| 现象 | 优先检查 |
| --- | --- |
| `address already in use` | 监听端口是否被其他进程占用；两个 DNS 模式是否同时绑定 UDP 53 |
| `permission denied` | 是否在绑定 53、80、443；确认后使用管理员权限或 capability |
| `invalid ... address` | `--listen` 和 `--server` 只接受原生 `IPv4:port`，不接受域名或 IPv6 |
| client 无法连接 server | server 监听地址、防火墙、安全组、SSH/VPN 隧道 |
| HTTP Proxy 的 HTTP 可用但 HTTPS 不可用 | CONNECT 请求、目标 443 连通性和 server 日志 |
| DNS 模式无响应 | 终端 DNS 设置、UDP 53、防火墙、A/AAAA 查询类型 |
| HTTPS DNS 部分站点失败 | 目标是否发送 SNI |
| Shadowsocks 无法连接 | method/password 是否一致；仅支持两种 SIP004 AEAD 算法 |

不要通过放宽公网防火墙到 `0.0.0.0/0` 来诊断连接问题。

### 9. 后台服务

只有用户明确要求时才创建 systemd 或 launchd 配置。创建前：

- 使用绝对二进制路径。
- 明确运行用户、工作目录、监听地址和重启策略。
- 不把 Shadowsocks 密码直接写入世界可读的服务文件。
- 创建后验证服务状态、监听端口和真实代理请求。

## 完成标准

任务只有同时满足以下条件才算完成：

- `gforward --help` 可执行。
- client/server 按目标拓扑启动。
- 对应模式的验证请求成功，或已给出可操作的外部阻塞原因。
- 公网部署没有裸露未认证的 server 端口。
- 最终回复包含二进制路径、实际启动命令、监听地址和验证结果。

## 项目内参考

在 gforward 仓库中执行时，优先读取：

- `README.md`：面向用户的最新安装与使用说明。
- `AGENTS.md`：项目架构、开发约束和已知边界。
- `cmd/client.go`、`cmd/server.go`：命令参数的最终事实来源。
