package dialer

import (
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/panjf2000/gnet/v2"
	"log"
)

type RecvPkt struct {
	Conn gnet.Conn
	Pkt  protocol.InternalPacket
}

type DialHandler struct {
	gnet.BuiltinEventEngine

	caller       string
	recvProtocol protocol.InternalPacket
	recvChan     chan *RecvPkt
}

func NewDialHandler(caller, proto string) *DialHandler {
	return &DialHandler{
		caller:       caller,
		recvProtocol: protocol.NewInternalPacket(proto),
		recvChan:     make(chan *RecvPkt),
	}
}

func (dh *DialHandler) OnTraffic(c gnet.Conn) gnet.Action {
	// 预读全部 buf，消耗单个 pkt 反序列化所需的字节数
	buf, _ := c.Peek(-1)
	pkt := dh.recvProtocol.New()
	n, err := pkt.Unmarshal(buf)
	if err != nil {
		log.Printf("[%s] dialer unmarshal buf from conn %p failed: %s", dh.caller, c, err)
		return gnet.None
	}
	log.Printf("[%s] dialer read pkt from conn %p: len %d", dh.caller, c, n)
	_, _ = c.Discard(n)

	// 组装 RecvPkt 并且通过 recvChan 传至调用方
	dh.recvChan <- &RecvPkt{
		Conn: c,
		Pkt:  pkt,
	}
	return gnet.None
}
