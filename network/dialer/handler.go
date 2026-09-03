package dialer

import (
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/near-notfaraway/gforward/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

const recvChanSize = 256 // 出站连接事件通道的缓冲大小

type DialHandler struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	logger       *logrus.Entry                    // 出站连接流量日志
	recvProtocol protocol.InternalPacket          // 解码接收流量的协议原型
	recvChan     chan *message.RecvMsg            // 向 Dialer 调用方投递连接事件与解析结果
	onOpen       func(conn gnet.Conn) gnet.Action // 可选：连接就绪时在事件循环上回调，用于在任何读/关事件前预注册路由
}

func NewDialHandler(proto string, logger *logrus.Entry) *DialHandler {
	return &DialHandler{
		logger:       logger,
		recvProtocol: protocol.NewInternalPacket(proto),
		recvChan:     make(chan *message.RecvMsg, recvChanSize),
	}
}

// OnOpen 在连接就绪时（早于该连接的任何读/关事件）优先触发已注册的 onOpen 回调，
// 供调用方原子地预注册路由；未注册回调时，将连接就绪事件投递给调用方异步处理。
func (dh *DialHandler) OnOpen(conn gnet.Conn) ([]byte, gnet.Action) {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))
	if dh.onOpen != nil {
		return nil, dh.onOpen(conn)
	}
	dh.recvChan <- &message.RecvMsg{
		Event:  message.RecvEventOpen,
		Conn:   conn,
		Token:  conn.Context(),
		Logger: logger,
	}
	return nil, gnet.None
}

func (dh *DialHandler) OnClose(conn gnet.Conn, err error) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))
	logger.Debugf("closed conn by err: %v", err)
	dh.recvChan <- &message.RecvMsg{Event: message.RecvEventClose, Conn: conn, Logger: logger}
	return gnet.None
}

// OnTraffic 解析当前缓冲区中的完整包，读干缓冲区后将本次解析出的所有包合并投递给调用方。
func (dh *DialHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))

	msg, rejected := message.ParseAvailable(conn, dh.recvProtocol, logger)
	if rejected {
		return gnet.Close
	}
	if len(msg.Pkts) > 0 {
		dh.recvChan <- msg
	}
	return gnet.None
}
