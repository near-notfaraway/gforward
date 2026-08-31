package server

import (
	"sync"

	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type destRoute struct {
	destination string    // 当前路由指向的目标主机和端口
	conn        gnet.Conn // 与目标站点建立的连接，拨号完成前为 nil
	dialing     bool      // 是否正在异步拨号
	pending     [][]byte  // 拨号完成前按到达顺序缓存的上行 payload
}

// dialToken 关联一次异步拨号与其上行上下文，作为不透明标识传给 dialer 并原样带回。
type dialToken struct {
	clientConn gnet.Conn     // 发起本次拨号的客户端连接
	route      *destRoute    // 本次拨号对应的路由，用于校验结果是否仍然有效
	logger     *logrus.Entry // 携带连接上下文的日志实例
}

type MsgHandler struct {
	connMapMu              sync.RWMutex             // 保护双向连接映射的并发访问
	clientConnMapDestRoute map[gnet.Conn]*destRoute // 维护客户端连接到当前目标路由的映射
	destConnMapClientConn  map[gnet.Conn]gnet.Conn  // 维护目标连接到客户端连接的反向映射
	dialer                 *dialer.Dialer           // 建立并管理目标站点连接
	channel                <-chan *DispatchMsg      // 接收 Dispatcher 固定分配的消息
}

func NewMsgHandler(dl *dialer.Dialer, ch <-chan *DispatchMsg) *MsgHandler {
	return &MsgHandler{
		clientConnMapDestRoute: make(map[gnet.Conn]*destRoute),
		destConnMapClientConn:  make(map[gnet.Conn]gnet.Conn),
		dialer:                 dl,
		channel:                ch,
	}
}

func (h *MsgHandler) removeDestRoute(destConn gnet.Conn) {
	h.connMapMu.Lock()
	defer h.connMapMu.Unlock()

	clientConn, ok := h.destConnMapClientConn[destConn]
	if !ok {
		return
	}
	delete(h.destConnMapClientConn, destConn)

	route, ok := h.clientConnMapDestRoute[clientConn]
	if ok && route.conn == destConn {
		delete(h.clientConnMapDestRoute, clientConn)
	}
}

// writeToDest 异步转发上行 payload，并在写入失败时清理并关闭该目标路由。
func (h *MsgHandler) writeToDest(destConn gnet.Conn, payload []byte, logger *logrus.Entry) {
	logger.Debugf("write payload: %d", len(payload))
	utils.AsyncWrite(destConn, payload, logger, func() {
		h.removeDestRoute(destConn)
		_ = destConn.Close()
	})
}

// startDial 交由 dialer 异步拨号，避免慢拨号阻塞上行处理循环，结果经 DialResultChan 回投。
func (h *MsgHandler) startDial(clientConn gnet.Conn, route *destRoute, dest string, logger *logrus.Entry) {
	h.dialer.AsyncDial("tcp", dest, &dialToken{
		clientConn: clientConn,
		route:      route,
		logger:     logger,
	})
}

// handleClientMsg 处理客户端关闭、目标切换和上行数据转发。
func (h *MsgHandler) handleClientMsg(msg *DispatchMsg) {
	pkt := msg.pkt
	conn := msg.conn
	logger := msg.logger

	// 客户端连接关闭：清理路由并关闭已建立的目标连接
	if pkt == nil {
		h.connMapMu.Lock()
		route, ok := h.clientConnMapDestRoute[conn]
		if ok {
			delete(h.clientConnMapDestRoute, conn)
			if route.conn != nil {
				delete(h.destConnMapClientConn, route.conn)
			}
		}
		h.connMapMu.Unlock()
		if ok && route.conn != nil {
			_ = route.conn.Close()
		}
		return
	}

	dest := pkt.GetDestination()
	logger.Debugf("packet dest: %s", dest)
	payload := pkt.GetPayload()

	h.connMapMu.Lock()
	route, ok := h.clientConnMapDestRoute[conn]

	// 命中同目标的现有路由：拨号中缓存 payload，已就绪则直接转发
	if ok && route.destination == dest {
		if route.dialing {
			route.pending = append(route.pending, payload)
			h.connMapMu.Unlock()
			logger.Debugf("queue payload while dialing: %d", len(payload))
			return
		}
		destConn := route.conn
		h.connMapMu.Unlock()
		h.writeToDest(destConn, payload, logger.WithField("toConn", utils.FormatGNetConn(destConn)))
		return
	}

	// 目标切换：拆除旧路由，其在途拨号结果会因 route 指针不匹配而被丢弃
	var oldConn gnet.Conn
	if ok {
		delete(h.clientConnMapDestRoute, conn)
		if route.conn != nil {
			delete(h.destConnMapClientConn, route.conn)
			oldConn = route.conn
		}
	}

	// 新建拨号中的路由并缓存首个 payload，交由独立 goroutine 拨号
	newRoute := &destRoute{
		destination: dest,
		dialing:     true,
		pending:     [][]byte{payload},
	}
	h.clientConnMapDestRoute[conn] = newRoute
	h.connMapMu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}
	logger.Debugf("start async dial dest: %s", dest)
	h.startDial(conn, newRoute, dest, logger)
}

// handleDialResult 处理异步拨号完成事件，校验路由有效后就绪并按序回放缓存数据。
func (h *MsgHandler) handleDialResult(res *dialer.DialResult) {
	token := res.Token.(*dialToken)
	logger := token.logger

	h.connMapMu.Lock()
	route, ok := h.clientConnMapDestRoute[token.clientConn]

	// 路由已被关闭或切换：结果失效，丢弃并关闭新建的连接
	if !ok || route != token.route {
		h.connMapMu.Unlock()
		if res.Conn != nil {
			_ = res.Conn.Close()
		}
		logger.Debug("drop stale dial result")
		return
	}

	// 拨号失败：移除路由并丢弃缓存数据
	if res.Err != nil {
		delete(h.clientConnMapDestRoute, token.clientConn)
		h.connMapMu.Unlock()
		logger.Errorf("dial dest failed: %s", res.Err)
		return
	}

	// 拨号成功：填充目标连接、建立反向映射并取出缓存数据
	route.conn = res.Conn
	route.dialing = false
	pending := route.pending
	route.pending = nil
	h.destConnMapClientConn[res.Conn] = token.clientConn
	h.connMapMu.Unlock()

	logger = logger.WithField("toConn", utils.FormatGNetConn(res.Conn))
	logger.Debugf("dial done, flush pending: %d", len(pending))
	for _, payload := range pending {
		h.writeToDest(res.Conn, payload, logger)
	}
}

// handleDestPkt 处理目标连接关闭和下行数据转发。
func (h *MsgHandler) handleDestPkt(pkt *dialer.RecvPkt) {
	logger := pkt.Logger
	if pkt.Pkt == nil {
		h.removeDestRoute(pkt.Conn)
		logger.Debug("removed route for closed destination connection")
		return
	}

	// 根据 dest conn 查找 client conn
	h.connMapMu.RLock()
	clientConn, ok := h.destConnMapClientConn[pkt.Conn]
	h.connMapMu.RUnlock()
	if !ok {
		logger.Debug("lookup client conn by dest conn failed")
		return
	}
	logger = logger.WithField("toConn", utils.FormatGNetConn(clientConn))

	// 发送 packet payload 到 client conn
	payload := pkt.Pkt.GetPayload()
	logger.Debugf("write payload: %d", len(payload))
	utils.AsyncWrite(clientConn, payload, logger, nil)
}

// Start 启动上行和下行处理循环。上行循环串行处理客户端消息与拨号结果以保证发送顺序。
func (h *MsgHandler) Start() {
	go func() {
		for {
			select {
			case msg, ok := <-h.channel:
				if !ok {
					return
				}
				h.handleClientMsg(msg)
			case res := <-h.dialer.DialResultChan():
				h.handleDialResult(res)
			}
		}
	}()

	go func() {
		for pkt := range h.dialer.RecvChan() {
			h.handleDestPkt(pkt)
		}
	}()
}
