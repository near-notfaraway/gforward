package client

import (
	"strings"

	"github.com/near-notfaraway/gforward/client/destination"
	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/near-notfaraway/gforward/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

// dialToken 关联一次异步拨号与其上行上下文，作为不透明标识传给 dialer 并原样带回。
type dialToken struct {
	userConn gnet.Conn     // 发起本次拨号的用户连接
	session  *session      // 本次拨号对应的会话，用于校验结果是否仍然有效
	logger   *logrus.Entry // 携带连接上下文的日志实例
}

// forwarder 客户端转发器：作为监听侧的 gnet 事件引擎，将本地用户流量封包并转发到 gforward 服务端。
type forwarder struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	destinationParser destination.Parser      // 从用户流量解析目标地址的解析器
	internalProtocol  protocol.InternalPacket // 编码发往服务端的内部协议包
	sessions          *sessionTable           // 维护用户连接的目标缓存、双向连接映射及拨号状态

	dialer         *dialer.Dialer // 建立并管理到服务端的连接
	serverAddr     string         // gforward 服务端地址
	uploadLogger   *logrus.Entry  // 用户到服务端方向的日志
	downloadLogger *logrus.Entry  // 服务端到用户方向的日志
}

// NewForwarder 根据客户端模式创建目标解析器并初始化双向流量日志。
func NewForwarder(mode, serverAddr string) *forwarder {
	var proto destination.ParserProto
	if strings.HasSuffix(mode, "_dns") {
		proto = destination.ParserProto(strings.TrimSuffix(mode, "_dns"))
	} else {
		proto = destination.ParserProto(mode)
	}
	return &forwarder{
		destinationParser: destination.NewParser(proto),
		sessions:          newSessionTable(),
		serverAddr:        serverAddr,
		uploadLogger: logrus.WithFields(logrus.Fields{
			"role":      "client",
			"direction": "user->server",
		}),
		downloadLogger: logrus.WithFields(logrus.Fields{
			"role":      "client",
			"direction": "server->user",
		}),
	}
}

func (f *forwarder) OnBoot(e gnet.Engine) (action gnet.Action) {
	f.internalProtocol = &protocol.ForwardPacket{}
	f.dialer = dialer.NewDialer(protocol.PacketTypePlain, f.downloadLogger)
	f.dialer.SetDialErrorToRecvChan(true)
	f.start()
	return gnet.None
}

// OnTraffic 解析当前请求目标，按请求边界封包并将用户流量转发到服务端。
// 首个 packet 会触发异步拨号，后续 packet 在拨号完成前入队缓存。
func (f *forwarder) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := f.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))

	for {
		payloadLen := 0

		// 从会话缓存或解析器获取目标地址；PerRequest 模式不入缓存，每个包重新解析
		dest, ok := f.sessions.getDest(conn)
		var cachedDest string
		if !ok {
			result, err := f.destinationParser.Parse(conn)
			if err != nil {
				logger.Errorf("parse dest failed: %s", err)
				return gnet.Close
			}
			switch result.Status {
			case destination.ParseNeedMoreData:
				logger.Debug("wait for more destination data")
				return gnet.None
			case destination.ParseDone:
				if result.Destination == "" {
					logger.Error("parsed blank destination")
					return gnet.Close
				}
			case destination.ParseRejected:
				logger.Error("destination parser rejected connection without error")
				return gnet.Close
			default:
				logger.Errorf("invalid destination parse status: %d", result.Status)
				return gnet.Close
			}
			logger.Debugf("parse new dest: %s", result.Destination)
			dest = result.Destination
			payloadLen = result.PayloadLen
			if !result.PerRequest {
				cachedDest = result.Destination
			}
		}
		logger.Debugf("the dest: %s", dest)

		// 组装用于发送的 packet
		pkt := f.internalProtocol.New()
		buf, err := conn.Peek(payloadLen)
		if err != nil {
			logger.Errorf("read buffer failed: %s", err)
			return gnet.Close
		}
		// 无负载可发时通常无需处理；但新建缓存路由（如 HTTP CONNECT、SOCKS5 隧道建立）时，
		// 握手字节已被解析器消费，首个 packet 只携带目标、负载为空，仍需照常发包以登记会话并触发拨号，
		// 否则后续隧道数据会因无会话而被重新按握手协议解析（表现为把 TLS ClientHello 当成 HTTP 方法）。
		// 该空包同时是 server 拨号目标的唯一信号：server 端仅在收到带 destination 的包时才拨号目标站点，
		// 故对 SMTP/SSH/MySQL 等 server-first（服务端先发 banner）协议，必须在隧道建立时即发此空包触发上游拨号，
		// 否则 app 等 server banner、server 等 app 数据将互相死锁。此处不能省包只建 session。
		if len(buf) == 0 && cachedDest == "" {
			return gnet.None
		}
		if len(buf) > 65535 {
			buf = buf[:65535]
		}
		_, _ = conn.Discard(len(buf))
		logger.Debugf("read buffer: len %d", len(buf))
		pkt.SetPayload(buf)
		pkt.SetDestination(dest)
		pktBuf, err := pkt.Marshal()
		if err != nil {
			logger.Errorf("marshal packet failed: %s", err)
			return gnet.None
		}
		logger.Debugf("marshal packet: len %d", len(pktBuf))

		// 按会话状态决定：命中就绪路由则转发，拨号中则入队，无会话则发起异步拨号
		act := f.sessions.admitUpstream(conn, cachedDest, pktBuf)
		switch act.action {
		case upstreamActionQueued:
			logger.Debugf("queue packet while dialing: %d", len(pktBuf))
		case upstreamActionForward:
			packetLogger := logger.WithField("toConn", utils.FormatGNetConn(act.serverConn))
			packetLogger.Debugf("write packet: len %d", len(pktBuf))
			// 写失败不即时收敛（onError 传 nil）：serverConn 断开由 dialer 的 OnClose 触发，
			// 经 RecvEventClose 走 handleDialClose -> purgeByServer 统一回收路由并关闭用户连接。
			// 不同于 server 的 dest 侧即时清理——client 重拨号仍指向同一 server，链路故障时重拨大概率也失败，
			// 即时收敛收益有限，且可避免用户连接事件循环跨连接改 sessionTable，保持 client「一律靠 OnClose」的简洁规则。
			act.session.sendMu.Lock()
			_ = utils.AsyncWrite(act.serverConn, pktBuf, packetLogger, nil)
			act.session.sendMu.Unlock()
		case upstreamActionDial:
			logger.Debugf("start async dial server: %s", f.serverAddr)
			f.dialer.AsyncDial("tcp", f.serverAddr, &dialToken{
				userConn: conn,
				session:  act.session,
				logger:   logger,
			})
		}

		if conn.InboundBuffered() == 0 {
			return gnet.None
		}
	}
}

func (f *forwarder) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	logger := f.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))
	logger.Debugf("closed conn by err: %s", err)

	if cleaner, ok := f.destinationParser.(destination.ConnStateCleaner); ok {
		cleaner.Clear(conn)
	}
	if serverConn := f.sessions.purgeByUser(conn); serverConn != nil {
		_ = serverConn.Close()
	}
	return
}

// handleDialClose 处理服务端连接关闭事件：回收双向映射并级联关闭对应的用户连接。
func (f *forwarder) handleDialClose(msg *message.RecvMsg) {
	logger := f.downloadLogger.WithField("fromConn", utils.FormatGNetConn(msg.Conn))
	if userConn := f.sessions.purgeByServer(msg.Conn); userConn != nil {
		_ = userConn.Close()
		logger.Debug("removed route for closed server connection")
	} else {
		logger.Debug("server conn route already removed")
	}
}

// handleServerMsg 处理服务端下行数据，按到达顺序将 payload 写回用户连接。
func (f *forwarder) handleServerMsg(msg *message.RecvMsg) {
	logger := f.downloadLogger.WithField("fromConn", utils.FormatGNetConn(msg.Conn))

	userConn, ok := f.sessions.getByServer(msg.Conn)
	if !ok {
		logger.Debug("drop packet for closed user connection")
		return
	}
	logger = logger.WithField("toConn", utils.FormatGNetConn(userConn))

	// 按到达顺序逐个将下行 payload 写回用户连接
	// 写失败无需在此清理：用户连接的断开由 gnet 的 OnClose 触发，
	// 经 purgeByUser 统一回收路由并关闭服务端连接。
	for _, pkt := range msg.Pkts {
		payload := pkt.GetPayload()
		logger.Debugf("write payload: %d", len(payload))
		_ = utils.AsyncWrite(userConn, payload, logger, nil)
	}
}

// handleDialOpen 处理服务端连接就绪事件（拨号成功）：注册反向映射、翻转就绪并按序回放缓存 packet。
func (f *forwarder) handleDialOpen(msg *message.RecvMsg) {
	token := msg.Token.(*dialToken)
	logger := token.logger

	token.session.sendMu.Lock()
	defer token.session.sendMu.Unlock()

	pending, matched := f.sessions.completeDial(token.userConn, token.session, msg.Conn)
	if !matched {
		_ = msg.Conn.Close()
		logger.Debug("drop dial open for stale session")
		return
	}

	logger = logger.WithField("toConn", utils.FormatGNetConn(msg.Conn))
	logger.Debugf("dial open, flush pending: %d", len(pending))
	// 回放同样写 serverConn，写失败不即时收敛（onError 传 nil），理由同 OnTraffic 的 forward 分支。
	for _, packet := range pending {
		_ = utils.AsyncWrite(msg.Conn, packet, logger, nil)
	}
}

// handleDialError 处理异步拨号失败事件，校验会话有效后移除拨号中会话。
func (f *forwarder) handleDialError(msg *message.RecvMsg) {
	token := msg.Token.(*dialToken)
	logger := token.logger

	token.session.sendMu.Lock()
	defer token.session.sendMu.Unlock()

	if !f.sessions.abortDial(token.userConn, token.session) {
		logger.Debug("drop stale dial error")
		return
	}

	logger.Errorf("dial server failed: %s", msg.Err)
}

// start 启动 dialer 事件处理循环；client 将服务端下行、连接就绪与拨号失败统一收敛到 RecvChan。
func (f *forwarder) start() {
	// dialer 事件循环：转发服务端下行数据、处理服务端连接关闭，并完成异步拨号
	go func() {
		for msg := range f.dialer.RecvChan() {
			switch msg.Event {
			case message.RecvEventData:
				f.handleServerMsg(msg)
			case message.RecvEventClose:
				f.handleDialClose(msg)
			case message.RecvEventOpen:
				f.handleDialOpen(msg)
			case message.RecvEventDialError:
				f.handleDialError(msg)
			default:
				f.downloadLogger.Errorf("invalid dialer event: %d", msg.Event)
			}
		}
	}()
}
