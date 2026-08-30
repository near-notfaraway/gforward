package server

import (
	"sync"

	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
)

type destRoute struct {
	destination string    // 当前路由指向的目标主机和端口
	conn        gnet.Conn // 与目标站点建立的连接
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

// handleClientMsg 处理客户端关闭、目标切换和上行数据转发。
func (h *MsgHandler) handleClientMsg(msg *DispatchMsg) {
	pkt := msg.pkt
	conn := msg.conn
	logger := msg.logger

	if pkt == nil {
		h.connMapMu.Lock()
		route, ok := h.clientConnMapDestRoute[conn]
		if ok {
			delete(h.clientConnMapDestRoute, conn)
			delete(h.destConnMapClientConn, route.conn)
		}
		h.connMapMu.Unlock()
		if ok {
			_ = route.conn.Close()
		}
		return
	}

	// 根据 client conn 和 dest 查找或新建 dest conn
	dest := pkt.GetDestination()
	logger.Debugf("packet dest: %s", dest)
	h.connMapMu.Lock()
	route, ok := h.clientConnMapDestRoute[conn]
	if ok && route.destination != dest {
		delete(h.clientConnMapDestRoute, conn)
		delete(h.destConnMapClientConn, route.conn)
		ok = false
	}
	h.connMapMu.Unlock()
	if route != nil && route.destination != dest {
		_ = route.conn.Close()
	}
	if !ok {
		logger.Debugf("wait dial dest: %s", dest)
		newDestConn, err := h.dialer.Dial("tcp", dest)
		if err != nil {
			logger.Errorf("dail dest failed: %s", err)
			return
		}
		logger.Debugf("new dest conn: %s", utils.FormatGNetConn(newDestConn))
		route = &destRoute{
			destination: dest,
			conn:        newDestConn,
		}
		h.connMapMu.Lock()
		h.clientConnMapDestRoute[conn] = route
		h.destConnMapClientConn[newDestConn] = conn
		h.connMapMu.Unlock()
	}
	destConn := route.conn
	logger = logger.WithField("toConn", utils.FormatGNetConn(destConn))

	// 发送 pkt payload 到 dest
	payload := pkt.GetPayload()
	logger.Debugf("write payload: %d", len(payload))
	utils.AsyncWrite(destConn, payload, logger, func() {
		h.removeDestRoute(destConn)
		_ = destConn.Close()
	})
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

// Start 启动相互独立的上行和下行处理循环。
func (h *MsgHandler) Start() {
	go func() {
		for msg := range h.channel {
			h.handleClientMsg(msg)
		}
	}()

	go func() {
		for pkt := range h.dialer.RecvChan() {
			h.handleDestPkt(pkt)
		}
	}()
}
