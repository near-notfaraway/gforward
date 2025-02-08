package server

import (
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
)

type MsgHandler struct {
	clientConnIdxMapDestConn map[gnet.Conn]gnet.Conn
	destConnMapClientConn    map[gnet.Conn]gnet.Conn
	dialer                   *dialer.Dialer
	channel                  <-chan *DispatchMsg
}

func NewMsgHandler(dl *dialer.Dialer, ch <-chan *DispatchMsg) *MsgHandler {
	return &MsgHandler{
		clientConnIdxMapDestConn: make(map[gnet.Conn]gnet.Conn),
		destConnMapClientConn:    make(map[gnet.Conn]gnet.Conn),
		dialer:                   dl,
		channel:                  ch,
	}
}

func (h *MsgHandler) Start() {
	for {
		select {
		case msg := <-h.channel:
			pkt := msg.pkt
			conn := msg.conn
			logger := msg.logger

			if pkt == nil {
				destConn := h.clientConnIdxMapDestConn[conn]
				_, ok := h.destConnMapClientConn[destConn]
				if ok {
					delete(h.destConnMapClientConn, destConn)
				}
				continue
			}

			// 根据 client conn 和 dest 查找或新建 dest conn
			dest := pkt.GetDestination()
			logger.Debugf("packet dest: %s", dest)
			destConn, ok := h.clientConnIdxMapDestConn[conn]
			if !ok {
				newDestConn, err := h.dialer.Dial("tcp", dest)
				if err != nil {
					logger.Errorf("dail dest failed: %s", err)
					continue
				}
				logger.Debugf("new dest conn: %s", utils.FormatGNetConn(newDestConn))
				h.clientConnIdxMapDestConn[conn] = newDestConn
				h.destConnMapClientConn[newDestConn] = conn
				destConn = newDestConn
			}
			logger = logger.WithField("toConn", utils.FormatGNetConn(destConn))

			// 发送 pkt payload 到 dest
			logger.Debugf("packet payload: len %d", len(pkt.GetPayload()))
			wn, err := destConn.Write(pkt.GetPayload())
			if err != nil {
				logger.Errorf("write payload failed: %s", err)
				continue
			}
			if wn != len(pkt.GetPayload()) {
				logger.Errorf("write payload not complete: actural len %d", wn)
			}
			logger.Debugf("write payload success: len %d", wn)

		case pkt := <-h.dialer.RecvChan():
			logger := pkt.Logger
			// 根据 dest conn 查找 client conn
			clientConn, ok := h.destConnMapClientConn[pkt.Conn]
			if !ok {
				logger.Errorf("lookup client conn by dest conn failed")
				continue
			}
			logger = logger.WithField("toConn", utils.FormatGNetConn(clientConn))

			// 发送 packet payload 到 client conn
			logger.Debugf("packet payload len: %d", len(pkt.Pkt.GetPayload()))
			wn, err := clientConn.Write(pkt.Pkt.GetPayload())
			if err != nil {
				logger.Errorf("write payload failed: %s", err)
				continue
			}
			if wn != len(pkt.Pkt.GetPayload()) {
				logger.Errorf("write payload not complete: actural len %d", wn)
			}
			logger.Debugf("write payload success: len %d", wn)
		}
	}
}
