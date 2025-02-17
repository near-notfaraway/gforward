package server

import (
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type DispatchMsg struct {
	conn   gnet.Conn
	pkt    protocol.InternalPacket
	logger *logrus.Entry
}

type Dispatcher struct {
	gnet.BuiltinEventEngine

	channels         []chan *DispatchMsg
	handlers         []*MsgHandler
	internalProtocol protocol.InternalPacket

	uploadLogger   *logrus.Entry
	downloadLogger *logrus.Entry
}

func NewDispatcher(handlerNum int) *Dispatcher {
	downloadLogger := logrus.WithFields(logrus.Fields{
		"role":      "server",
		"direction": "dest->client",
	})
	uploadLogger := logrus.WithFields(logrus.Fields{
		"role":      "server",
		"direction": "client->dest",
	})

	channels := make([]chan *DispatchMsg, handlerNum)
	handlers := make([]*MsgHandler, handlerNum)
	for i := range channels {
		ch := make(chan *DispatchMsg, 20)
		dl := dialer.NewDialer(protocol.PacketTypePlain, downloadLogger)
		hdl := NewMsgHandler(dl, ch)
		channels[i] = ch
		handlers[i] = hdl
		hdl.Start()
	}

	return &Dispatcher{
		channels:         channels,
		handlers:         handlers,
		internalProtocol: &protocol.ForwardPacket{},
		uploadLogger:     uploadLogger,
		downloadLogger:   downloadLogger,
	}
}

func (d *Dispatcher) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	connStr := utils.FormatGNetConn(conn)
	logger := d.uploadLogger.WithField("fromConn", connStr)
	logger.Debugf("closed conn by err: %s", err)

	chanIdx := int(utils.StringHash(connStr) % uint64(len(d.channels)))
	msg := &DispatchMsg{
		conn:   conn,
		logger: d.uploadLogger,
	}
	d.channels[chanIdx] <- msg
	logger.Debugf("dispatch close to chan %d", chanIdx)
	return gnet.None
}

func (d *Dispatcher) OnTraffic(conn gnet.Conn) gnet.Action {
	connStr := utils.FormatGNetConn(conn)
	logger := d.uploadLogger.WithField("fromConn", connStr)

	// 读取 pkt
	buf, _ := conn.Peek(-1)
	logger.Debugf("read buffer: len %d", len(buf))
	pkt := d.internalProtocol.New()
	n, err := pkt.Unmarshal(buf)
	if err != nil {
		logger.Errorf("unmarshal packet failed: %s", err)
		return gnet.None
	}
	_, _ = conn.Discard(n)
	logger.Debugf("unmarshal packet: len %d", n)

	// 分发 pkt
	chanIdx := int(utils.StringHash(connStr) % uint64(len(d.channels)))
	msg := &DispatchMsg{
		conn:   conn,
		pkt:    pkt,
		logger: logger,
	}
	d.channels[chanIdx] <- msg
	logger.Debugf("dispatch packet to chan %d", chanIdx)

	return gnet.None
}
