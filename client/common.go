package client

import (
	"fmt"
	"github.com/near-notfaraway/gtunnel/client/destination"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/panjf2000/gnet/v2"
	"log"
)

type ListenHandler struct {
	gnet.BuiltinEventEngine

	destinationParser      destination.Parser
	userConnMapDestination map[gnet.Conn]string
	userConnMapServerConn  map[gnet.Conn]gnet.Conn
	serverConnMapUserConn  map[gnet.Conn]gnet.Conn

	internalProtocol protocol.InternalPacket
	dialer           *dialer.Dialer
	serverAddr       string
}

func NewListenHandler(mode, serverAddr string) *ListenHandler {
	return &ListenHandler{
		destinationParser: destination.NewParser(destination.ParserProto(mode)),
		serverAddr:        serverAddr,
	}
}

func (lh *ListenHandler) OnBoot(e gnet.Engine) (action gnet.Action) {
	lh.destinationParser = destination.NewParser(destination.ParserProtoSocks5)
	lh.userConnMapDestination = make(map[gnet.Conn]string)
	lh.userConnMapServerConn = make(map[gnet.Conn]gnet.Conn)
	lh.serverConnMapUserConn = make(map[gnet.Conn]gnet.Conn)
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer("client", protocol.PacketTypePlain)
	lh.RecvFromDialer()
	return gnet.None
}

func (lh *ListenHandler) OnTraffic(c gnet.Conn) gnet.Action {
	dest, ok := lh.userConnMapDestination[c]
	if !ok {
		dest_, err := lh.destinationParser.Parse(c)
		if err != nil {
			log.Printf("[client] destination parse failed: %s", err.Error())
			return gnet.Close
		}
		if dest_ == "" {
			return gnet.None
		}
		log.Printf("[client] destination is: %s", dest_)
		dest = dest_
		lh.userConnMapDestination[c] = dest
	}

	var serverConn gnet.Conn
	if sc, ok := lh.userConnMapServerConn[c]; ok {
		serverConn = sc
	} else {
		sc_, err := lh.dialer.Dial("tcp", lh.serverAddr)
		if err != nil {
			panic(err)
		}
		log.Printf("new server conn %s -> %s", sc_.LocalAddr().String(), sc_.RemoteAddr().String())
		lh.userConnMapServerConn[c] = sc_
		lh.serverConnMapUserConn[sc_] = c
		serverConn = sc_
	}

	// need forward though server conn
	pkt := lh.internalProtocol.New()
	buf, err := c.Peek(-1)
	if err != nil {
		log.Printf(fmt.Sprintf("[client] recv from user conn failed: %s", err))
		return gnet.Close
	}
	if len(buf) == 0 {
		log.Printf(fmt.Sprintf("[client] recv from user conn no data"))
		return gnet.None
	}
	log.Printf(fmt.Sprintf("[client] recv from user conn %p: len %d", c, len(buf)))
	pkt.SetPayload(buf)
	pkt.SetDestination(dest)
	outBuf, err := pkt.Marshal()
	if err != nil {
		panic(err)
	}
	if _, err := serverConn.Write(outBuf); err != nil {
		log.Printf("[client] forward to server failed: %s", err.Error())
		log.Printf("[client] forward to server len: %d", len(outBuf))
		return gnet.None
	}
	log.Printf(fmt.Sprintf("[client] forward to server conn %p: len %d", serverConn, len(buf)))

	return gnet.None
}

func (lh *ListenHandler) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	log.Printf("close user conn %p, err: %s", c, err)
	delete(lh.userConnMapDestination, c)
	sc, ok := lh.userConnMapServerConn[c]
	if ok {
		delete(lh.userConnMapServerConn, c)
		delete(lh.serverConnMapUserConn, sc)
	}
	return
}

func (lh *ListenHandler) RecvFromDialer() {
	go func() {
		for {
			select {
			case pkt := <-lh.dialer.RecvChan():
				uc, ok := lh.serverConnMapUserConn[pkt.Conn]
				if !ok {
					log.Printf("[client] failed to lookup user conn by server conn %p", pkt.Conn)
					continue
				}
				log.Printf("[client] success lookup user conn by server conn %p: %p", pkt.Conn, uc)
				n, err := uc.Write(pkt.Pkt.GetPayload())
				if err != nil || n != len(pkt.Pkt.GetPayload()) {
					log.Printf("[client] send to user conn %p failed: %s", uc, err)
					continue
				}
				log.Printf("[client] send to user: len %d", len(pkt.Pkt.GetPayload()))
			}
		}
	}()
}
