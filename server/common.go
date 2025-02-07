package server

import (
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type ListenHandler struct {
	gnet.BuiltinEventEngine

	destConnIdxMapDestConn map[string]gnet.Conn
	destConnMapClientConn  map[gnet.Conn]gnet.Conn
	internalProtocol       protocol.InternalPacket
	dialer                 *dialer.Dialer
	uploadLogger           *logrus.Entry
	downloadLogger         *logrus.Entry
}

func NewListenHandler() *ListenHandler {
	return &ListenHandler{
		uploadLogger: logrus.WithFields(logrus.Fields{
			"role":      "server",
			"direction": "client->dest",
		}),
		downloadLogger: logrus.WithFields(logrus.Fields{
			"role":      "server",
			"direction": "dest->client",
		}),
	}
}

func (lh *ListenHandler) OnBoot(e gnet.Engine) (action gnet.Action) {
	lh.destConnIdxMapDestConn = make(map[string]gnet.Conn)
	lh.destConnMapClientConn = make(map[gnet.Conn]gnet.Conn)
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer(protocol.PacketTypePlain, lh.downloadLogger)
	lh.RecvFromDialer()
	return gnet.None
}

func (lh *ListenHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	connStr := utils.FormatGNetConn(conn)
	logger := lh.uploadLogger.WithField("fromConn", connStr)

	// 读取 pkt
	buf, _ := conn.Peek(-1)
	logger.Debugf("read buffer: len %d", len(buf))
	pkt := lh.internalProtocol.New()
	n, err := pkt.Unmarshal(buf)
	if err != nil {
		logger.Errorf("unmarshal packet failed: %s", err)
		return gnet.None
	}
	_, _ = conn.Discard(n)
	logger.Debugf("unmarshal packet: len %d", n)

	// 根据 client conn 和 dest 查找或新建 dest conn
	dest := pkt.GetDestination()
	logger.Debugf("packet dest: %s", dest)
	destConnIdx := connStr + dest
	destConn, ok := lh.destConnIdxMapDestConn[destConnIdx]
	if !ok {
		newDestConn, err := lh.dialer.Dial("tcp", dest)
		if err != nil {
			logger.Errorf("dail dest failed: %s", err)
			return gnet.None
		}
		logger.Debugf("new dest conn: %s", utils.FormatGNetConn(newDestConn))
		lh.destConnIdxMapDestConn[destConnIdx] = newDestConn
		lh.destConnMapClientConn[newDestConn] = conn
		destConn = newDestConn
	}
	logger = logger.WithField("toConn", utils.FormatGNetConn(destConn))

	// 发送 pkt payload 到 dest
	logger.Debugf("packet payload: len %d", len(pkt.GetPayload()))
	wn, err := destConn.Write(pkt.GetPayload())
	if err != nil {
		logger.Errorf("write payload failed: %s", err)
		return gnet.None
	}
	if wn != len(pkt.GetPayload()) {
		logger.Errorf("write payload not complete: actural len %d", wn)
	}
	logger.Debugf("write payload success: len %d", wn)

	return gnet.None
}

func (lh *ListenHandler) RecvFromDialer() {
	go func() {
		for {
			select {
			case pkt := <-lh.dialer.RecvChan():
				// 根据 dest conn 查找 client conn
				logger := lh.downloadLogger.WithField("fromConn", utils.FormatGNetConn(pkt.Conn))
				clientConn, ok := lh.destConnMapClientConn[pkt.Conn]
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
	}()
}
