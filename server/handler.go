package server

import (
	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/near-notfaraway/gforward/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

// dialToken 关联一次异步拨号与其上行上下文，作为不透明标识传给 dialer 并原样带回。
type dialToken struct {
	clientConn gnet.Conn     // 发起本次拨号的客户端连接
	session    *session      // 本次拨号对应的会话，用于校验结果是否仍然有效
	logger     *logrus.Entry // 携带连接上下文的日志实例
}

// msgHandler 处理客户端上行数据，包括目标切换、转发负载、拨号等
type msgHandler struct {
	sessions *sessionTable           // 维护双向连接映射并保证跨映射操作的原子性
	dialer   *dialer.Dialer          // 建立并管理目标站点连接
	channel  <-chan *message.RecvMsg // 接收 Dispatcher 固定分配的消息
}

func newMsgHandler(dl *dialer.Dialer, ch <-chan *message.RecvMsg) *msgHandler {
	return &msgHandler{
		sessions: newSessionTable(),
		dialer:   dl,
		channel:  ch,
	}
}

// handleClientMsg 处理客户端关闭，以及批量转发本次读取的上行数据（含目标切换）。
func (h *msgHandler) handleClientMsg(msg *message.RecvMsg) {
	conn := msg.Conn
	logger := msg.Logger

	// 客户端连接关闭：清理会话并关闭已建立的目标连接
	if len(msg.Pkts) == 0 {
		if destConn := h.sessions.closeClient(conn); destConn != nil {
			_ = destConn.Close()
		}
		return
	}

	// 按到达顺序逐个转发本次读取解析出的上行包
	for _, pkt := range msg.Pkts {
		h.forwardUpstream(conn, pkt, logger)
	}
}

// forwardUpstream 转发单个上行包：命中就绪路由则写入，拨号中则入队，新建或切换目标则发起异步拨号。
func (h *msgHandler) forwardUpstream(conn gnet.Conn, pkt protocol.InternalPacket, logger *logrus.Entry) {
	dest := pkt.GetDestination()
	logger.Debugf("packet dest: %s", dest)
	payload := pkt.GetPayload()

	act := h.sessions.admitUpstream(conn, dest, payload)
	switch act.action {
	case upstreamActionQueued:
		logger.Debugf("queue payload while dialing: %d", len(payload))
	case upstreamActionForward:
		utils.AsyncWrite(act.destConn, payload, logger, func() {
			h.sessions.removeByDest(act.destConn)
			_ = act.destConn.Close()
		})
	case upstreamActionDial:
		if act.oldDestConn != nil {
			_ = act.oldDestConn.Close()
		}
		// 异步拨号，避免慢拨号阻塞上行处理循环，结果经 DialResultChan 回投
		logger.Debugf("start async dial dest: %s", act.dest)
		h.dialer.AsyncDial("tcp", act.dest, &dialToken{
			clientConn: conn,
			session:    act.session,
			logger:     logger,
		})
	}
}

// handleDialResult 处理异步拨号完成事件，校验会话有效后就绪并按序回放缓存数据。
func (h *msgHandler) handleDialResult(res *dialer.DialResult) {
	token := res.Token.(*dialToken)
	logger := token.logger

	pending, matched := h.sessions.completeDial(token.clientConn, token.session, res.Conn, res.Err)

	// 会话已被关闭或切换：结果失效，丢弃并关闭新建的连接
	if !matched {
		if res.Conn != nil {
			_ = res.Conn.Close()
		}
		logger.Debug("drop stale dial result")
		return
	}

	// 拨号失败：会话已被移除，丢弃缓存数据
	if res.Err != nil {
		logger.Errorf("dial dest failed: %s", res.Err)
		return
	}

	// 拨号成功：按到达顺序回放缓存数据
	logger = logger.WithField("toConn", utils.FormatGNetConn(res.Conn))
	logger.Debugf("dial done, flush pending: %d", len(pending))
	for _, payload := range pending {
		utils.AsyncWrite(res.Conn, payload, logger, func() {
			h.sessions.removeByDest(res.Conn)
			_ = res.Conn.Close()
		})
	}
}

// handleDestPkt 处理目标连接关闭和下行数据转发。
func (h *msgHandler) handleDestMsg(msg *message.RecvMsg) {
	logger := msg.Logger
	if len(msg.Pkts) == 0 {
		h.sessions.removeByDest(msg.Conn)
		logger.Debug("removed route for closed destination connection")
		return
	}

	// 根据 dest conn 查找 client conn
	clientConn, ok := h.sessions.clientByDest(msg.Conn)
	if !ok {
		logger.Debug("lookup client conn by dest conn failed")
		return
	}
	logger = logger.WithField("toConn", utils.FormatGNetConn(clientConn))

	// 按到达顺序逐个将下行 payload 发送到 client conn
	// 写失败无需在此清理：客户端连接的断开由 gnet 的 OnClose 触发，
	// 经空批次 RecvMsg 走 handleClientMsg -> closeClient 统一回收路由。
	for _, pkt := range msg.Pkts {
		payload := pkt.GetPayload()
		logger.Debugf("write payload: %d", len(payload))
		utils.AsyncWrite(clientConn, payload, logger, nil)
	}
}

// Start 启动上行和下行处理循环
func (h *msgHandler) start() {
	// 上行循环串行处理客户端消息与拨号结果以保证发送顺序。
	go func() {
		for {
			select {
			case msg, ok := <-h.channel:
				if !ok {
					return
				}
				h.handleClientMsg(msg)
			case res, ok := <-h.dialer.DialResultChan():
				if !ok {
					return
				}
				h.handleDialResult(res)
			}
		}
	}()

	// 下行循环并行处理目标连接关闭和下行数据转发
	go func() {
		for msg := range h.dialer.RecvChan() {
			h.handleDestMsg(msg)
		}
	}()
}
