package message

import (
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

// RecvMsg 承载一次读取解析出的包及其连接与日志上下文，供收发两端统一使用。
type RecvMsg struct {
	Conn   gnet.Conn                 // 产生本次数据的连接
	Pkts   []protocol.InternalPacket // 本次读取解析出的内部协议包，为空表示连接关闭
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
			return &RecvMsg{Conn: conn, Pkts: pkts, Logger: logger}, false
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
				return &RecvMsg{Conn: conn, Pkts: pkts, Logger: logger}, false
			}
		}
	}
}
