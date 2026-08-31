package dialer

import (
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/near-notfaraway/gtunnel/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

const recvChanSize = 20 // 出站连接接收结果通道的缓冲大小

type RecvPkt struct {
	Conn   gnet.Conn               // 接收到数据的出站连接
	Pkt    protocol.InternalPacket // 从连接数据解析出的内部协议包，nil 表示连接关闭
	Logger *logrus.Entry           // 携带连接上下文的日志实例
}

type DialHandler struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	logger       *logrus.Entry           // 出站连接流量日志
	recvProtocol protocol.InternalPacket // 解码接收流量的协议原型
	recvChan     chan *RecvPkt           // 向 Dialer 调用方投递解析结果
}

func NewDialHandler(proto string, logger *logrus.Entry) *DialHandler {
	return &DialHandler{
		logger:       logger,
		recvProtocol: protocol.NewInternalPacket(proto),
		recvChan:     make(chan *RecvPkt, recvChanSize),
	}
}

func (dh *DialHandler) OnClose(conn gnet.Conn, err error) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))
	logger.Debugf("closed conn by err: %v", err)
	dh.recvChan <- &RecvPkt{
		Conn:   conn,
		Logger: logger,
	}
	return gnet.None
}

// OnTraffic 将连接缓冲区反序列化为内部包，并投递给 Dialer 调用方。
func (dh *DialHandler) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := dh.logger.WithField("fromConn", utils.FormatGNetConn(conn))

	// 预读全部 buf，消耗单个 pkt 反序列化所需的字节数
	buf, _ := conn.Peek(-1)
	logger.Debugf("read buffer: len %d", len(buf))
	pkt := dh.recvProtocol.New()
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

	// 组装 RecvPkt 并且通过 recvChan 传至调用方
	dh.recvChan <- &RecvPkt{
		Conn:   conn,
		Pkt:    pkt,
		Logger: logger,
	}

	return gnet.None
}
