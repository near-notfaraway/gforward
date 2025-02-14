package client

import (
	"github.com/near-notfaraway/gtunnel/client/destination"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	"strings"
	"sync"
)

type ListenHandler struct {
	gnet.BuiltinEventEngine

	destinationParser      destination.Parser
	userConnMapDestination sync.Map
	userConnMapServerConn  sync.Map
	serverConnMapUserConn  sync.Map

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
	lh.internalProtocol = &protocol.ForwardPacket{}
	lh.dialer = dialer.NewDialer(protocol.PacketTypePlain, lh.downloadLogger)
	lh.RecvFromDialer()
	return gnet.None
}

func (lh *ListenHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := lh.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))

	// 根据 user conn 获取 dest
	destVal, ok := lh.userConnMapDestination.Load(conn)
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
		lh.userConnMapDestination.Store(conn, newDest)
		destVal = newDest
	}
	dest := destVal.(string)
	logger.Debugf("the dest: %s", dest)

	// 根据 user conn 获取 server conn
	serverConnVal, ok := lh.userConnMapServerConn.Load(conn)
	if !ok {
		newServerConn, err := lh.dialer.Dial("tcp", lh.serverAddr)
		if err != nil {
			logger.Errorf("dial server failed: %s", err)
			return gnet.Close
		}
		logger.Debugf("new server conn: %s", utils.FormatGNetConn(newServerConn))
		lh.userConnMapServerConn.Store(conn, newServerConn)
		lh.serverConnMapUserConn.Store(newServerConn, conn)
		serverConnVal = newServerConn
	}
	serverConn := serverConnVal.(gnet.Conn)
	logger = logger.WithField("fromConn", utils.FormatGNetConn(serverConn))

	// 组装用于发送的 packet
	pkt := lh.internalProtocol.New()
	buf, err := conn.Next(-1)
	if err != nil {
		logger.Errorf("read buffer failed: %s", err)
		return gnet.Close
	}
	if len(buf) == 0 {
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

func (lh *ListenHandler) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	connStr := utils.FormatGNetConn(conn)
	logger := lh.uploadLogger.WithField("fromConn", connStr)
	logger.Debugf("closed conn by err: %s", err)

	lh.userConnMapDestination.Delete(conn)
	scVal, ok := lh.userConnMapServerConn.Load(conn)
	if ok {
		sc := scVal.(gnet.Conn)
		lh.userConnMapServerConn.Delete(conn)
		lh.serverConnMapUserConn.Delete(sc)
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
				userConnVal, ok := lh.serverConnMapUserConn.Load(pkt.Conn)
				if !ok {
					logger.Errorf("lookup user conn by server conn failed")
					continue
				}
				userConn := userConnVal.(gnet.Conn)
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
