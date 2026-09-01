package dialer

import (
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/near-notfaraway/gforward/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

const recvChanSize = 20 // 出站连接接收结果通道的缓冲大小

type DialHandler struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	logger       *logrus.Entry           // 出站连接流量日志
	recvProtocol protocol.InternalPacket // 解码接收流量的协议原型
	recvChan     chan *message.RecvMsg   // 向 Dialer 调用方投递解析结果
}

func NewDialHandler(proto string, logger *logrus.Entry) *DialHandler {
	return &DialHandler{
		logger:       logger,
		recvProtocol: protocol.NewInternalPacket(proto),
		recvChan:     make(chan *message.RecvMsg, recvChanSize),
	}
}

func (dh *DialHandler) OnClose(conn gnet.Conn, err error) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))
	logger.Debugf("closed conn by err: %v", err)
	// 空批次表示连接关闭
	dh.recvChan <- &message.RecvMsg{Conn: conn, Logger: logger}
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
