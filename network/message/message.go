package message

import (
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

// RecvEvent 标识 RecvMsg 承载的事件类型。
type RecvEvent uint8

const (
	RecvEventData      RecvEvent = iota // 连接收到并解析出数据包
	RecvEventClose                      // 连接关闭
	RecvEventOpen                       // 连接就绪
	RecvEventDialError                  // 异步拨号失败
)

// RecvMsg 承载 dialer/dispatcher 向调用方投递的连接事件、数据包与日志上下文。
type RecvMsg struct {
	Event  RecvEvent                 // 当前消息的事件类型
	Conn   gnet.Conn                 // 产生本次数据的连接
	Pkts   []protocol.InternalPacket // RecvEventData：本次读取解析出的内部协议包
	Err    error                     // RecvEventDialError：拨号错误
	Token  any                       // RecvEventOpen / RecvEventDialError：调用方发起拨号时透传的关联标识
	Logger *logrus.Entry             // 携带连接上下文的日志实例
}

// ParseAvailable 循环解析缓冲区中的完整包，直至数据不足或读干缓冲区，
// 将本次解析出的所有包合并为一个 RecvMsg 返回。
// rejected 为 true 表示协议违例，调用方应关闭连接并丢弃本次结果。
func ParseAvailable(conn gnet.Conn, proto protocol.InternalPacket, logger *logrus.Entry) (msg *RecvMsg, rejected bool) {
	var pkts []protocol.InternalPacket
	for {
		buf, _ := conn.Peek(-1)
		logger.Debugf("read buffer: len %d", len(buf))
		pkt := proto.New()
		n, state, err := pkt.Unmarshal(buf)
		switch state {
		case protocol.ParseNeedMoreData:
			// 未解析出完整包，返回已解析的包
			logger.Debug("wait complete packet")
			return &RecvMsg{Event: RecvEventData, Conn: conn, Pkts: pkts, Logger: logger}, false
		case protocol.ParseRejected:
			// 协议违例，调用方负责关闭连接，已解析的包一并丢弃
			logger.Errorf("reject invalid packet: %v", err)
			return nil, true
		case protocol.ParseDone:
			// 完整解析出一个包，消费其字节并追加
			_, _ = conn.Discard(n)
			logger.Debugf("unmarshal packet: len %d", n)
			pkts = append(pkts, pkt)
			// 缓冲区读干，返回已解析的包
			if conn.InboundBuffered() == 0 {
				return &RecvMsg{Event: RecvEventData, Conn: conn, Pkts: pkts, Logger: logger}, false
			}
		}
	}
}
