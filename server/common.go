package server

import (
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/panjf2000/gnet/v2"
	"log"
)

type ListenHandler struct {
	gnet.BuiltinEventEngine

	userIdentMapDestConn  map[string]gnet.Conn
	destConnMapClientConn map[gnet.Conn]gnet.Conn
	internalProtocol      protocol.InternalPacket
	dialer                *dialer.Dialer
}

func NewListenHandler() *ListenHandler {
	return &ListenHandler{}
}

func (lh *ListenHandler) OnBoot(e gnet.Engine) (action gnet.Action) {
	lh.userIdentMapDestConn = make(map[string]gnet.Conn)
	lh.destConnMapClientConn = make(map[gnet.Conn]gnet.Conn)
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer("server", protocol.PacketTypePlain)
	lh.RecvFromDialer()
	return gnet.None
}

func (lh *ListenHandler) OnTraffic(c gnet.Conn) gnet.Action {
	buf, _ := c.Peek(-1)
	pkt := lh.internalProtocol.New()
	n, err := pkt.Unmarshal(buf)
	if err != nil {
		return gnet.None
	}
	_, _ = c.Discard(n)
	log.Printf("[server] recv from client %p len: %d", c, n)
	dest := pkt.GetDestination()
	log.Printf("[server] recv destination from client conn %p : %s", c, dest)
	userIdent := c.RemoteAddr().String() + dest
	var dc gnet.Conn
	if _dc, ok := lh.userIdentMapDestConn[userIdent]; ok {
		dc = _dc
	} else {
		dl, err := lh.dialer.Dial("tcp", dest)
		if err != nil {
			log.Printf("[server] dail destination failed: %s", err)
			return gnet.None
		}
		log.Printf("[server] new dial destination: %s -> %s", dl.LocalAddr().String(), dl.RemoteAddr().String())
		lh.userIdentMapDestConn[userIdent] = dl
		lh.destConnMapClientConn[dl] = c
		dc = dl
	}

	wn, err := dc.Write(pkt.GetPayload())
	if err != nil {
		log.Printf("[server] send to dest conn %p failed: %s", dc, err)
		return gnet.None
	}
	log.Printf("[server] send to dest conn %p: len %d", dc, wn)

	return gnet.None
}

func (lh *ListenHandler) RecvFromDialer() {
	go func() {
		for {
			select {
			case pkt := <-lh.dialer.RecvChan():
				cc, ok := lh.destConnMapClientConn[pkt.Conn]
				if !ok {
					log.Printf("failed to lookup client conn by dest conn %p", pkt.Conn)
					continue
				}
				log.Printf("success lookup client conn %p by dest conn %p", cc, pkt.Conn)
				n, err := cc.Write(pkt.Pkt.GetPayload())
				if err != nil || n != len(pkt.Pkt.GetPayload()) {
					log.Printf("[server] send to client conn %p failed: %s", cc, err)
					continue
				}
				log.Printf("[server] send to client: len %d", len(pkt.Pkt.GetPayload()))
			}
		}
	}()
}
