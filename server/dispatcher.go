package server

import (
	"sync/atomic"

	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type dispatchContext struct {
	handlerIndex int // 当前连接固定分配的消息处理器下标
}

type DispatchMsg struct {
	conn   gnet.Conn               // 客户端到服务端的入站连接
	pkt    protocol.InternalPacket // 已解析的内部协议包，nil 表示连接关闭
	logger *logrus.Entry           // 携带当前流量上下文的日志实例
}

type Dispatcher struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	channels         []chan *DispatchMsg     // 按连接固定分发消息的处理通道
	handlers         []*MsgHandler           // 与分发通道一一对应的消息处理器
	internalProtocol protocol.InternalPacket // 解码客户端上行流量的协议原型

	nextHandlerIndex atomic.Uint64 // 轮询分配新连接时使用的处理器序号
	uploadLogger     *logrus.Entry // 客户端到目标站点方向的日志
	downloadLogger   *logrus.Entry // 目标站点到客户端方向的日志
}

// NewDispatcher 创建指定数量的消息处理器及其固定分发通道。
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

func (d *Dispatcher) OnOpen(conn gnet.Conn) ([]byte, gnet.Action) {
	handlerIndex := int((d.nextHandlerIndex.Add(1) - 1) % uint64(len(d.channels)))
	conn.SetContext(&dispatchContext{handlerIndex: handlerIndex})
	return nil, gnet.None
}

func (d *Dispatcher) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	connStr := utils.FormatGNetConn(conn)
	logger := d.uploadLogger.WithField("fromConn", connStr)
	logger.Debugf("closed conn by err: %s", err)

	chanIdx := conn.Context().(*dispatchContext).handlerIndex
	msg := &DispatchMsg{
		conn:   conn,
		logger: d.uploadLogger,
	}
	d.channels[chanIdx] <- msg
	logger.Debugf("dispatch close to chan %d", chanIdx)
	return gnet.None
}

// OnTraffic 循环解析当前缓冲区中的完整 ForwardPacket，并按连接固定分发。
func (d *Dispatcher) OnTraffic(conn gnet.Conn) gnet.Action {
	connStr := utils.FormatGNetConn(conn)
	logger := d.uploadLogger.WithField("fromConn", connStr)
	chanIdx := conn.Context().(*dispatchContext).handlerIndex

	for {
		// 读取 pkt
		buf, _ := conn.Peek(-1)
		logger.Debugf("read buffer: len %d", len(buf))
		pkt := d.internalProtocol.New()
		n, state, err := pkt.Unmarshal(buf)
		switch state {
		case protocol.ParseNeedMoreData:
			logger.Debug("wait complete packet")
			return gnet.None
		case protocol.ParseRejected:
			logger.Errorf("reject invalid packet: %v", err)
			return gnet.Close
		case protocol.ParseDone:
		}
		_, _ = conn.Discard(n)
		logger.Debugf("unmarshal packet: len %d", n)

		// 分发 pkt
		msg := &DispatchMsg{
			conn:   conn,
			pkt:    pkt,
			logger: logger,
		}
		d.channels[chanIdx] <- msg
		logger.Debugf("dispatch packet to chan %d", chanIdx)

		if conn.InboundBuffered() == 0 {
			return gnet.None
		}
	}
}
