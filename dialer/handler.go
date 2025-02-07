package dialer

import (
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type RecvPkt struct {
	Conn gnet.Conn
	Pkt  protocol.InternalPacket
}

type DialHandler struct {
	gnet.BuiltinEventEngine

	logger       *logrus.Entry
	recvProtocol protocol.InternalPacket
	recvChan     chan *RecvPkt
}

func NewDialHandler(proto string, logger *logrus.Entry) *DialHandler {
	return &DialHandler{
		logger:       logger,
		recvProtocol: protocol.NewInternalPacket(proto),
		recvChan:     make(chan *RecvPkt),
	}
}

func (dh *DialHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))

	// 预读全部 buf，消耗单个 pkt 反序列化所需的字节数
	buf, _ := conn.Peek(-1)
	logger.Debugf("read buffer: len %d", len(buf))
	pkt := dh.recvProtocol.New()
	n, err := pkt.Unmarshal(buf)
	if err != nil {
		logger.Errorf("unmarshal packet failed: %s", err)
		return gnet.None
	}
	_, _ = conn.Discard(n)
	logger.Debugf("marshal packet: len %d", n)

	// 组装 RecvPkt 并且通过 recvChan 传至调用方
	dh.recvChan <- &RecvPkt{
		Conn: conn,
		Pkt:  pkt,
	}
	return gnet.None
}
