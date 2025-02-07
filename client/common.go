package client

import (
	"github.com/near-notfaraway/gtunnel/client/destination"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	"log"
	"strings"
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
	uploadLogger     *logrus.Entry
	downloadLogger   *logrus.Entry
}

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
	lh.userConnMapDestination = make(map[gnet.Conn]string)
	lh.userConnMapServerConn = make(map[gnet.Conn]gnet.Conn)
	lh.serverConnMapUserConn = make(map[gnet.Conn]gnet.Conn)
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer(protocol.PacketTypePlain, lh.downloadLogger)
	lh.RecvFromDialer()
	return gnet.None
}

func (lh *ListenHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := lh.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))

	// 根据 user conn 获取 dest
	dest, ok := lh.userConnMapDestination[conn]
	if !ok {
		newDest, err := lh.destinationParser.Parse(conn)
		if err != nil {
			logger.Errorf("parse dest failed: %s", err)
			return gnet.Close
		}
		if newDest == "" {
			logger.Warnf("skip blank dest")
			return gnet.None
		}
		logger.Debugf("parse new dest: %s", newDest)
		lh.userConnMapDestination[conn] = newDest
		dest = newDest
	}
	logger.Debugf("the dest: %s", dest)

	// 根据 user conn 获取 server conn
	serverConn, ok := lh.userConnMapServerConn[conn]
	if !ok {
		newServerConn, err := lh.dialer.Dial("tcp", lh.serverAddr)
		if err != nil {
			logger.Errorf("dial server failed: %s", err)
			return gnet.Close
		}
		logger.Debugf("new server conn: %s", utils.FormatGNetConn(newServerConn))
		lh.userConnMapServerConn[conn] = newServerConn
		lh.serverConnMapUserConn[newServerConn] = conn
		serverConn = newServerConn
	}
	logger = logger.WithField("fromConn", utils.FormatGNetConn(serverConn))

	// 组装用于发送的 packet
	pkt := lh.internalProtocol.New()
	buf, err := conn.Next(-1)
	if err != nil {
		logger.Errorf("read buffer failed: %s", err)
		return gnet.Close
	}
	if len(buf) == 0 {
		logger.Warnf("read empty buffer")
		return gnet.None
	}
	logger.Debugf("read buffer: len %d", len(buf))
	pkt.SetPayload(buf)
	pkt.SetDestination(dest)
	pktBuf, err := pkt.Marshal()
	if err != nil {
		logger.Errorf("marshal packet failed: %s", err)
		return gnet.None
	}
	logger.Debugf("marshal packet: len %d", len(pktBuf))

	// 发送 packet 到 server conn
	wn, err := serverConn.Write(pktBuf)
	if err != nil {
		logger.Errorf("write packet failed: %s", err)
		return gnet.None
	}
	if wn != len(pktBuf) {
		logger.Errorf("write packet not complete: actural len %d", wn)
	}
	logger.Debugf("write packet success: len %d", wn)

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
				logger := lh.downloadLogger.WithField("fromConn", utils.FormatGNetConn(pkt.Conn))
				// 根据 server conn 查找 user conn
				userConn, ok := lh.serverConnMapUserConn[pkt.Conn]
				if !ok {
					logger.Errorf("lookup user conn by server conn failed")
					continue
				}
				logger = logger.WithField("toConn", utils.FormatGNetConn(userConn))

				// 发送 packet payload 到 user conn
				logger.Debugf("packet payload len: %d", len(pkt.Pkt.GetPayload()))
				wn, err := userConn.Write(pkt.Pkt.GetPayload())
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
