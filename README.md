# gforward

`gforward` 是一个使用 Go 和 gnet 实现的 TCP 正向代理/转发隧道。它由 client 和 server 两部分组成：

- **client** 接收本地 HTTP、HTTPS、HTTP Proxy、SOCKS5 或 Shadowsocks 流量，并识别目标地址。
- **server** 接收 client 发来的目标地址和负载，连接真实目标站点并双向转发数据。

```text
应用 / 浏览器
    |
    | HTTP Proxy / SOCKS5 / DNS 透明代理 / Shadowsocks
    v
gforward client
    |
    | ForwardPacket
    v
gforward server
    |
    v
目标站点
```

> [!WARNING]
> client 与 server 之间的私有协议目前没有认证、加密或访问控制。不要将 server 端口直接暴露到公网；生产部署应使用防火墙和 VPN、SSH Tunnel、TLS Tunnel 等安全通道。

## 功能

| client 模式 | 默认端口 | 用途 |
| --- | ---: | --- |
| `http_proxy` | 8080 | 标准 HTTP 代理，支持普通 HTTP 请求和 HTTPS CONNECT |
| `socks5` | 1080 | 无认证 SOCKS5 CONNECT 代理 |
| `http_dns` | 80 | 配合 DNS 劫持的透明 HTTP 代理 |
| `https_dns` | 443 | 配合 DNS 劫持的透明 HTTPS 代理 |
| `shadowsocks` | 8388 | SIP004 AEAD Shadowsocks 入站 |

其他特性：

- 基于 gnet 的事件驱动网络模型。
- 支持 TCP 半包、粘包和单次读取中的多帧解析。
- client 与 server 均支持多核事件循环。
- 异步目标拨号，单次拨号超时 2 秒，并发拨号上限 256。
- Shadowsocks 支持 `aes-256-gcm` 和 `chacha20-ietf-poly1305`。

## 快速开始

### 环境要求

- Go 1.22+
- client 能访问 server 的监听地址
- server 能访问目标站点

### 1. 启动 server

```bash
go run . server --listen 0.0.0.0:9989
```

### 2. 启动 HTTP Proxy client

```bash
go run . client \
  --mode http_proxy \
  --listen 127.0.0.1:8080 \
  --server 127.0.0.1:9989
```

通过代理访问 HTTP 或 HTTPS：

```bash
curl -x http://127.0.0.1:8080 http://example.com/
curl -x http://127.0.0.1:8080 https://example.com/
```

## 通过 Agent Skill 使用

仓库提供了 [`gforward-user-guide`](.agents/skills/gforward-user-guide/SKILL.md)，可让支持 Agent Skills 的编码助手完成环境检查、安装、模式选择、启动、验证和故障排查。

### 仓库级使用

克隆仓库后，在仓库目录中启动支持 `.agents/skills` 的 Agent。Agent 会发现项目内的 Skill，无需额外安装：

```bash
git clone https://github.com/near-notfaraway/gforward.git
cd gforward
```

### 使用 npx skills 安装

已安装 Node.js/npm 时，可使用 [skills CLI](https://github.com/vercel-labs/skills) 从 GitHub 安装 Skill：

```bash
# 安装到当前项目，交互式选择目标 Agent
npx skills add near-notfaraway/gforward --skill gforward-user-guide

# 安装到用户级目录，在所有项目中使用
npx skills add near-notfaraway/gforward \
  --skill gforward-user-guide \
  --global
```

查看安装结果：

```bash
npx skills list
npx skills list --global
```

在 CI 或其他非交互环境中，可添加 `--yes` 跳过确认。安装完成后重新打开 Agent 会话，使其重新扫描 Skill。

### 手动安装为用户级 Skill

没有 Node.js/npm 时，可手动安装到用户级通用目录：

```bash
install -d "$HOME/.agents/skills/gforward-user-guide"
install -m 0644 \
  ".agents/skills/gforward-user-guide/SKILL.md" \
  "$HOME/.agents/skills/gforward-user-guide/SKILL.md"
```

手动安装后同样需要重新打开 Agent 会话。

### 使用示例

正常情况下直接描述 gforward 需求即可，Agent 会根据 Skill 的触发描述自动使用 `gforward-user-guide`。

安装当前源码：

```text
检查本机环境并从当前源码安装 gforward，安装后告诉我二进制路径。
```

启动并验证本机 HTTP Proxy：

```text
在本机启动 gforward server 和 HTTP Proxy client，并用 curl 验证 HTTP、HTTPS 代理。
```

通过 SSH Tunnel 安全部署：

```text
通过 SSH Tunnel 部署 gforward。远端是 user@server.example.com，
server 只监听回环地址，本地 HTTP Proxy 监听 127.0.0.1:8080。
```

配置其他接入模式：

```text
帮我配置 gforward SOCKS5。
帮我在局域网配置 gforward HTTPS DNS 透明代理。
帮我配置 gforward Shadowsocks 入站，不要在输出中显示密码。
```

排查问题：

```text
排查 gforward client 无法连接 server 的问题；先检查监听端口和网络连通性，
不要通过开放公网防火墙来绕过问题。
```

如果所用 Agent 没有自动触发，可再显式指定“使用 `gforward-user-guide`”。

Skill 会优先采用用户级安装和回环地址监听；涉及 `sudo`、防火墙、DNS、系统服务或公网暴露时，会先检查风险和必要条件。

## 使用方式

### HTTP Proxy

适合浏览器、命令行工具或应用显式配置代理的场景。

```bash
go run . client \
  --mode http_proxy \
  --listen 127.0.0.1:8080 \
  --server 10.0.0.2:9989
```

- 普通 HTTP 请求根据 `Host` 逐请求选择目标。
- HTTPS 使用 CONNECT 建立 TCP 隧道。
- 同一连接跨 Host 请求应串行发送，不支持跨 Host HTTP/1.1 pipelining。

### SOCKS5

```bash
go run . client \
  --mode socks5 \
  --listen 127.0.0.1:1080 \
  --server 10.0.0.2:9989

curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

当前仅支持无认证 CONNECT，不支持 BIND、UDP ASSOCIATE 或用户名密码认证。

### DNS 透明代理

HTTP：

```bash
sudo go run . client \
  --mode http_dns \
  --listen 192.168.1.10:80 \
  --server 10.0.0.2:9989
```

HTTPS：

```bash
sudo go run . client \
  --mode https_dns \
  --listen 192.168.1.10:443 \
  --server 10.0.0.2:9989
```

将终端设备的 DNS 服务器设置为 client 所在主机。gforward 会监听 UDP 53，把 IN A 查询响应到 client 的 IPv4 地址，再从 HTTP Host 或 TLS SNI 恢复真实目标。

注意：

- `http_dns` 必须监听 TCP 80，`https_dns` 必须监听 TCP 443。
- DNS 只响应 IPv4 A 查询，不处理 AAAA。
- UDP 53、TCP 80 和 TCP 443 通常需要管理员权限。
- 两个 DNS 模式都会绑定 UDP 53，默认不能在同一主机同时启动。
- HTTPS 流量必须携带 SNI。

### Shadowsocks 入站

```bash
go run . client \
  --mode shadowsocks \
  --listen 0.0.0.0:8388 \
  --server 10.0.0.2:9989 \
  --ss-method aes-256-gcm \
  --ss-password 'change-me'
```

将标准 Shadowsocks 客户端连接到该监听地址，并使用相同的 method 和 password。

Shadowsocks 加解密只发生在用户到 gforward client 的接入段。gforward client 到 server 的隧道仍是明文协议，因此仍需使用可信内网或额外的安全隧道。

## 命令参数

查看完整参数：

```bash
go run . client --help
go run . server --help
```

常用参数：

| 参数 | 适用命令 | 说明 |
| --- | --- | --- |
| `--listen`, `-l` | client/server | 监听的原生 `IPv4:port` |
| `--server`, `-s` | client | gforward server 的 `IPv4:port` |
| `--mode`, `-m` | client | client 接入模式 |
| `--multicore` | client/server | 是否启用 gnet 多核事件循环，默认开启 |
| `--verbose`, `-v` | client/server | 启用 debug 日志 |
| `--ss-method` | client | Shadowsocks AEAD 算法 |
| `--ss-password` | client | Shadowsocks 密码 |

## 最佳实践

### 安全部署

1. server 优先监听私网或 VPN 地址，不直接监听公网接口。
2. 使用主机防火墙或安全组，仅允许可信 client IP 访问 server 端口。
3. 跨公网部署时，在 gforward 外层使用 WireGuard、SSH Tunnel 或 TLS Tunnel。
4. 不要把 Shadowsocks 入站密码当作 client-server 隧道认证。
5. server 当前没有目标白名单，应避免让不可信用户接入。

通过 SSH Tunnel 连接远端 server 的最小示例：

```bash
# 远端机器：仅监听回环地址
go run . server --listen 127.0.0.1:9989

# client 所在机器：建立到远端的本地端口转发
ssh -N -L 19989:127.0.0.1:9989 user@server.example.com

# client 通过 SSH Tunnel 连接 server
go run . client --mode http_proxy \
  --listen 127.0.0.1:8080 --server 127.0.0.1:19989
```

### 模式选择

- 应用支持显式 HTTP 代理：优先使用 `http_proxy`。
- 应用支持 SOCKS5 或需要代理非 HTTP TCP：使用 `socks5`。
- 无法配置应用代理但能控制局域网 DNS：使用 `http_dns` 或 `https_dns`。
- 已有 Shadowsocks 客户端：使用 `shadowsocks` 作为接入方式。

### 运行建议

- 显式指定 `--listen` 和 `--server`，避免部署时依赖默认地址。
- client 只供本机使用时绑定 `127.0.0.1`，不要使用 `0.0.0.0`。
- 排障时临时启用 `--verbose`，稳定运行后关闭以减少日志量。
- 修改协议、连接生命周期或并发逻辑后运行完整测试。

## 构建与测试

```bash
# 全量测试；Mockey 要求关闭内联
go test -gcflags="all=-l -N" ./...

# 本机构建并打包
bash build.sh

# darwin/amd64 或 linux/amd64
SYSTEM=darwin bash build.sh
SYSTEM=linux bash build.sh
```

构建产物位于：

```text
output/
├── gforward/
│   ├── bin/gforward
│   ├── config/
│   └── logs/
└── gforward.tgz
```

`build.sh` 会删除并重建整个 `output/` 目录。

## 已知限制

- client-server 协议没有版本号、认证、加密、压缩或完整性校验。
- server 没有访问控制和目标地址白名单。
- HTTP Proxy 不支持跨 Host HTTP/1.1 pipelining。
- SOCKS5 仅支持无认证 CONNECT。
- DNS 模式仅处理 IPv4 A 查询。
- TLS 透明代理依赖 SNI。
- 所有配置来自命令行，没有配置文件。

## License

[GPL-3.0](LICENSE)
