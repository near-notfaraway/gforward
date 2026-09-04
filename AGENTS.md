# gforward 项目协作指南

## 1. 项目概览

`gforward` 是一个 Go 实现的 TCP 正向代理/转发隧道，包含 client 和 server 两个角色：

- client 接收 HTTP、HTTPS、HTTP Proxy、SOCKS5 或 Shadowsocks 流量，识别目标地址。
- client 使用自定义 `ForwardPacket` 协议把目标地址和明文负载发送给 server。
- server 连接真实目标站点，并在 client 与目标站点之间双向转发 TCP 字节流。

基本信息：

- Go module：`github.com/near-notfaraway/gforward`
- 命令及构建产物：`gforward`
- Go 版本：1.22
- 许可证：GPL-3.0

## 2. 开发硬约束

### 2.1 改动范围与代码组织

- 只修改需求直接相关的代码，不顺带重构无关模块。
- 结构体定义后连续放置其构造函数和方法组，不在中间穿插其他结构体。
- 每个结构体字段必须有简短注释，说明用途或维护的状态。
- 函数体超过 20 行时，在函数定义前添加一行职责说明。
- 优先使用现有类型、命名和并发模型；新增抽象必须能实际降低复杂度或重复。

### 2.2 单元测试

- UT 统一使用 GoConvey 和 Mockey。
- 一个生产代码文件对应一个 `*_test.go`。
- 每个被测函数尽量由一个顶层 `Test*` 覆盖，不同场景使用嵌套 `Convey` 或 `PatchConvey`。
- Mockey 测试必须关闭内联：

```bash
go test -gcflags="all=-l -N" ./...
```

### 2.3 命名对称性

- 跨包、跨模块承担相同语义的类型、函数、方法、枚举和常量必须保持命名一致。
- 命名应准确表达职责和行为，不用相近但含义模糊的词表达同一概念。
- 实现结构不要求表面对称；只有语义相同时才要求同名，职责不同的流程应使用能体现差异的名称。

### 2.4 协议与连接修改检查项

- 新增接入协议：在 `client/destination` 实现 `Parser`，并在 `NewParser` 和 `cmd/client.go` 注册。
- 短包返回 `ParseNeedMoreData`，协议错误返回 `ParseRejected`，目标完整后才返回 `ParseDone`。
- 握手解析只能消费已完整解析的字节；必须保留半包以及粘在握手后的首段业务数据。
- 需要逐请求处理或持续解码的协议设置 `ParseResult.PerRequest=true`，每次流量重新进入 `Parse`。
- 解码后的首段负载通过 `ParseResult.Payload` 返回，不能丢失。
- 需要编码下行数据的解析器实现 `PayloadEncoder`；连接关闭时通过 `ConnStateCleaner` 清理状态。
- 修改 `ForwardPacket` 时同步修改 Marshal/Unmarshal，并测试半包、粘包、非法帧和最大长度。
- 修改连接生命周期时同时检查正向映射、反向映射、在途拨号 token、pending 数据及双向关闭。
- 任何网络 I/O 都应在 sessionTable 锁外执行。

## 3. 核心架构

```text
应用 / 浏览器 / SS 客户端
        |
        | HTTP / TLS / HTTP Proxy / SOCKS5 / Shadowsocks AEAD
        v
client.forwarder (gnet server)
        |
        | destination.Parser
        | sessionTable: userConn <-> serverConn
        | ForwardPacket(destination + payload)
        v
network/dialer.Dialer (gnet client)
        |
        v
server.dispatcher (gnet server)
        |
        | 按入站连接 FNV-1a 哈希固定分配 worker
        v
server.msgHandler
        |
        | sessionTable: clientConn <-> destConn
        v
network/dialer.Dialer (gnet client) ---> 目标站点

响应沿反方向返回；隧道下行使用 PlainPacket，直接承载原始字节。
```

一条用户连接对应一条 client 到 server 的连接，并在 server 侧对应至多一条当前目标连接。HTTP Proxy 可在同一连接上按请求切换目标；CONNECT、SOCKS5、DNS 透明代理和 Shadowsocks 使用固定目标。

## 4. 并发与顺序保证

client 和 server 的 `sessionTable` 命名对称，但拨号事件模型有意不同，禁止互相照搬。

| 维度 | client | server |
| --- | --- | --- |
| 部署取舍 | 家用网关，优先节省 CPU、内存和 goroutine | 云服务器，优先吞吐和上下行并行 |
| 上行执行体 | gnet `OnTraffic` | dispatcher worker goroutine |
| 拨号结果 | `RecvChan` 单 goroutine 处理 Open、Data、DialError | 上行 goroutine 处理 `DialResultChan` |
| 下行数据 | 同一个 `RecvChan` goroutine | 独立 goroutine处理 `RecvChan` |
| 路由注册 | `completeDial` 一次完成 | `bindDial` 在 OnOpen 注册，`completeDial` 再翻转 ready |
| pending 顺序 | `session.sendMu` 保证回放先于后续转发 | 回放与后续转发同属上行 goroutine，无需 `sendMu` |

必须保持以下不变量：

1. 同一 server 入站连接固定落到同一个 worker，保持连接事件的局部性和处理顺序。
2. 反向映射必须早于对应连接的任何下行 Data/Close 事件可见。
3. pending 回放必须早于同会话后续 ready 数据写入。
4. 在途拨号结果必须校验 session 指针 token，不能误绑定已关闭或已切换的会话。
5. client 的 `sendMu`、server 的 `bindDial` 都是顺序保证的一部分，不得为追求表面对称而删除。

## 5. 客户端接入模式

| 模式 | 默认/固定端口 | 目标来源 | 主要行为 |
| --- | ---: | --- | --- |
| `http_proxy` | 8080 | HTTP Host / CONNECT 地址 | 普通代理逐请求解析；CONNECT 返回 `200 OK` 后建立隧道 |
| `socks5` | 1080 | SOCKS5 CONNECT 地址 | 仅支持无认证 CONNECT |
| `http_dns` | 固定 80 | HTTP Host | UDP 53 劫持 A 查询到本机，接收透明 HTTP |
| `https_dns` | 固定 443 | TLS ClientHello SNI | UDP 53 劫持 A 查询到本机，接收透明 HTTPS |
| `shadowsocks` | 8388 | 解密后的 SOCKS5 风格地址头 | 仅支持 SIP004 AEAD |

Shadowsocks 约束：

- 仅支持 `aes-256-gcm` 和 `chacha20-ietf-poly1305`。
- 通过 `--ss-method` 和 `--ss-password` 配置。
- 加解密只发生在 client 接入层；client 到 server 的 `ForwardPacket` 仍是明文、无认证协议。
- 每条连接独立维护 salt、AEAD 和 nonce。
- length chunk 与 payload chunk 必须成对完整后再推进 nonce，避免半包破坏 chunk 边界。
- 上行持续解密通过 `PerRequest` 重入 Parser；下行通过 `PayloadEncoder` 加密。

DNS 模式约束：

- UDP 53 仅伪造 IN A 记录，不响应 AAAA。
- `http_dns` 必须监听 TCP 80，`https_dns` 必须监听 TCP 443。
- listener 为 `0.0.0.0` 时，根据请求路由选择本机 IPv4 作为 A 记录响应。

## 6. 隧道协议

`protocol.InternalPacket` 当前有两种实现：

- `ForwardPacket`：client 到 server，携带目标地址和负载。
- `PlainPacket`：server 到 client，无额外帧头。

ForwardPacket 使用大端序：

```text
+--------------+---------------+-----------+------------------+------------------+
| addr_len: u8 | addr: N bytes | port: u16 | payload_len: u16 | payload: M bytes |
+--------------+---------------+-----------+------------------+------------------+
```

- `addr` 为不含端口的主机名或 IP 地址，最长 255 字节。
- `port` 为 1 到 65535 的二进制端口。
- 单帧 payload 最大 65535 字节。
- `network/message.ParseAvailable` 负责循环拆解粘包、保留半包，并在协议拒绝时通知调用方关闭连接。
- 协议没有版本号、认证、加密、压缩或完整性校验，不应直接暴露到公网。

## 7. 目录职责

| 路径 | 职责 |
| --- | --- |
| `main.go` | 程序入口 |
| `cmd/` | Cobra 命令、参数校验、日志及 gnet 启动配置 |
| `client/forwarder.go` | client 接入、解析、封包和双向转发 |
| `client/session.go` | userConn 与 serverConn 会话及拨号状态 |
| `client/destination/` | HTTP、TLS、HTTP Proxy、SOCKS5、Shadowsocks 解析与编解码 |
| `client/dns.go` | DNS A 记录劫持 |
| `server/dispatcher.go` | 收包并按连接哈希分发到 worker |
| `server/handler.go` | 目标拨号及双向转发 |
| `server/session.go` | clientConn 与 destConn 会话及拨号状态 |
| `network/dialer/` | gnet 出站客户端、异步拨号和连接事件 |
| `network/message/` | 连接事件模型及流式拆包 |
| `protocol/` | ForwardPacket、PlainPacket 及解析状态 |
| `diagnosis/` | logrus 初始化和滚动日志 |
| `utils/` | 连接格式化和 FNV-1a 哈希 |

## 8. 运行、测试与构建

```bash
# 服务端
go run . server --listen 0.0.0.0:9989

# HTTP Proxy
go run . client --mode http_proxy --listen 127.0.0.1:8080 --server 127.0.0.1:9989

# SOCKS5
go run . client --mode socks5 --listen 127.0.0.1:1080 --server 127.0.0.1:9989

# Shadowsocks
go run . client --mode shadowsocks --listen 0.0.0.0:8388 \
  --server 127.0.0.1:9989 --ss-method aes-256-gcm --ss-password secret

# 全量测试
go test -gcflags="all=-l -N" ./...

# 本机或 amd64 交叉构建
bash build.sh
SYSTEM=darwin bash build.sh
SYSTEM=linux bash build.sh
```

`--multicore` 默认开启，`--verbose` 将日志级别从 `warn` 调整为 `debug`。`build.sh` 会删除并重建 `output/`，交叉构建目标固定为 amd64。

## 9. 已知边界

- 所有配置来自命令行，没有配置文件。
- `--listen` 和 `--server` 只接受原生 `IPv4:port`。
- 默认 DNS 53、HTTP 80 和 HTTPS 443 端口可能需要管理员权限。
- TLS 依赖 ClientHello SNI，不支持无 SNI 流量。
- SOCKS5 不支持 BIND、UDP ASSOCIATE 或用户名密码认证。
- HTTP Proxy 支持同一 TCP 连接顺序访问不同 Host，不支持 HTTP/1.1 pipelining 并发响应排序。
- server 没有身份认证、访问控制或目标白名单。
- 拨号超时为 2 秒，并发拨号上限为 256。
