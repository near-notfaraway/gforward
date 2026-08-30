package client

import (
	"strings"
	"sync"

	"github.com/near-notfaraway/gtunnel/client/destination"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type ListenHandler struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	destinationParser      destination.Parser // 从用户流量中解析目标地址
	userConnMapDestination sync.Map           // 缓存用户连接对应的固定目标地址
	userConnMapServerConn  sync.Map           // 维护用户连接到服务端连接的映射
	serverConnMapUserConn  sync.Map           // 维护服务端连接到用户连接的反向映射

	internalProtocol protocol.InternalPacket // 编码发往服务端的内部协议包
	dialer           *dialer.Dialer          // 建立并管理到服务端的连接
	serverAddr       string                  // gforward 服务端地址
	uploadLogger     *logrus.Entry           // 用户到服务端方向的日志
	downloadLogger   *logrus.Entry           // 服务端到用户方向的日志
}

// NewListenHandler 根据客户端模式创建目标解析器并初始化双向流量日志。
func NewListenHandler(mode, serverAddr string) *ListenHandler {
	var proto destination.ParserProto
	if strings.HasSuffix(mode, "_dns") {
		proto = destination.ParserProto(strings.TrimSuffix(mode, "_dns"))
	} else {
		proto = destination.ParserProto(mode)
	}
	return &ListenHandler{
		destinationParser: destination.NewParser(proto),
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

func (lh *ListenHandler) OnBoot(e gnet.Engine) (action gnet.Action) {
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer(protocol.PacketTypePlain, lh.downloadLogger)
	lh.RecvFromDialer()
	return gnet.None
}

// OnTraffic 解析当前请求目标，按请求边界封包并将用户流量转发到服务端。
func (lh *ListenHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := lh.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))

	for {
		payloadLen := 0

		// 根据 user conn 获取或解析 dest
		destVal, ok := lh.userConnMapDestination.Load(conn)
		if !ok {
			result, err := lh.destinationParser.Parse(conn)
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
			if !result.PerRequest {
				lh.userConnMapDestination.Store(conn, result.Destination)
			}
			destVal = result.Destination
			payloadLen = result.PayloadLen
		}
		dest := destVal.(string)
		logger.Debugf("the dest: %s", dest)

		// 根据 user conn 获取 server conn
		serverConnVal, ok := lh.userConnMapServerConn.Load(conn)
		if !ok {
			newServerConn, err := lh.dialer.Dial("tcp", lh.serverAddr)
			if err != nil {
				logger.Errorf("dial server failed: %s", err)
				return gnet.Close
			}
			logger.Debugf("new server conn: %s", utils.FormatGNetConn(newServerConn))
			lh.userConnMapServerConn.Store(conn, newServerConn)
			lh.serverConnMapUserConn.Store(newServerConn, conn)
			serverConnVal = newServerConn
		}
		serverConn := serverConnVal.(gnet.Conn)
		packetLogger := logger.WithField("toConn", utils.FormatGNetConn(serverConn))

		// 组装用于发送的 packet
		pkt := lh.internalProtocol.New()
		buf, err := conn.Peek(payloadLen)
		if err != nil {
			packetLogger.Errorf("read buffer failed: %s", err)
			return gnet.Close
		}
		if len(buf) == 0 {
			return gnet.None
		}
		if len(buf) > 65535 {
			buf = buf[:65535]
		}
		_, _ = conn.Discard(len(buf))
		packetLogger.Debugf("read buffer: len %d", len(buf))
		pkt.SetPayload(buf)
		pkt.SetDestination(dest)
		pktBuf, err := pkt.Marshal()
		if err != nil {
			packetLogger.Errorf("marshal packet failed: %s", err)
			return gnet.None
		}
		packetLogger.Debugf("marshal packet: len %d", len(pktBuf))

		// 发送 packet 到 server conn
		wn, err := serverConn.Write(pktBuf)
		if err != nil {
			packetLogger.Errorf("write packet failed: %s", err)
			return gnet.None
		}
		if wn != len(pktBuf) {
			packetLogger.Errorf("write packet not complete: actural len %d", wn)
		}
		packetLogger.Debugf("write packet success: len %d", wn)

		if conn.InboundBuffered() == 0 {
			return gnet.None
		}
	}
}

func (lh *ListenHandler) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	connStr := utils.FormatGNetConn(conn)
	logger := lh.uploadLogger.WithField("fromConn", connStr)
	logger.Debugf("closed conn by err: %s", err)

	if cleaner, ok := lh.destinationParser.(destination.ConnStateCleaner); ok {
		cleaner.Clear(conn)
	}
	lh.userConnMapDestination.Delete(conn)
	scVal, ok := lh.userConnMapServerConn.LoadAndDelete(conn)
	if ok {
		sc := scVal.(gnet.Conn)
		lh.serverConnMapUserConn.CompareAndDelete(sc, conn)
		_ = sc.Close()
	}
	return
}

// handleServerPacket 处理服务端数据或关闭事件，并维护双向连接映射。
func (lh *ListenHandler) handleServerPacket(pkt *dialer.RecvPkt) {
	logger := lh.downloadLogger.WithField("fromConn", utils.FormatGNetConn(pkt.Conn))
	if pkt.Pkt == nil {
		userConnVal, ok := lh.serverConnMapUserConn.LoadAndDelete(pkt.Conn)
		if !ok {
			logger.Debug("server conn route already removed")
			return
		}
		userConn := userConnVal.(gnet.Conn)
		if !lh.userConnMapServerConn.CompareAndDelete(userConn, pkt.Conn) {
			logger.Debug("server conn route has been replaced")
			return
		}
		_ = userConn.Close()
		logger.Debug("removed route for closed server connection")
		return
	}

	userConnVal, ok := lh.serverConnMapUserConn.Load(pkt.Conn)
	if !ok {
		logger.Debug("drop packet for closed user connection")
		return
	}
	userConn := userConnVal.(gnet.Conn)
	logger = logger.WithField("toConn", utils.FormatGNetConn(userConn))

	logger.Debugf("packet payload len: %d", len(pkt.Pkt.GetPayload()))
	wn, err := userConn.Write(pkt.Pkt.GetPayload())
	if err != nil {
		logger.Errorf("write payload failed: %s", err)
		return
	}
	if wn != len(pkt.Pkt.GetPayload()) {
		logger.Errorf("write payload not complete: actural len %d", wn)
	}
	logger.Debugf("write payload success: len %d", wn)
}

// RecvFromDialer 持续接收服务端响应，并根据连接映射写回对应用户连接。
func (lh *ListenHandler) RecvFromDialer() {
	go func() {
		for pkt := range lh.dialer.RecvChan() {
			lh.handleServerPacket(pkt)
		}
	}()
}
