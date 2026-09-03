package message

import (
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

// bufferConn 用内存缓冲模拟 gnet 连接的 Peek/Discard/InboundBuffered，供解析循环测试。
type bufferConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	buf []byte // 当前尚未消费的入站缓冲
}

func (c *bufferConn) Peek(n int) ([]byte, error) {
	if n <= 0 || n > len(c.buf) {
		return c.buf, nil
	}
	return c.buf[:n], nil
}

func (c *bufferConn) Discard(n int) (int, error) {
	if n > len(c.buf) {
		n = len(c.buf)
	}
	c.buf = c.buf[n:]
	return n, nil
}

func (c *bufferConn) InboundBuffered() int {
	return len(c.buf)
}

func newLogger() *logrus.Entry {
	return logrus.New().WithField("test", "message")
}

func mustMarshal(dest string, payload []byte) []byte {
	pkt := &protocol.ForwardPacket{}
	pkt.SetDestination(dest)
	pkt.SetPayload(payload)
	buf, err := pkt.Marshal()
	So(err, ShouldBeNil)
	return buf
}

func TestParseAvailable(t *testing.T) {
	PatchConvey("ParseAvailable should drain complete packets and report protocol violations", t, func() {
		logger := newLogger()

		PatchConvey("A single complete frame should be parsed and consumed", func() {
			conn := &bufferConn{buf: mustMarshal("example.com:80", []byte("hello"))}

			msg, rejected := ParseAvailable(conn, &protocol.ForwardPacket{}, logger)

			So(rejected, ShouldBeFalse)
			So(msg.Event, ShouldEqual, RecvEventData)
			So(msg.Pkts, ShouldHaveLength, 1)
			So(msg.Pkts[0].GetDestination(), ShouldEqual, "example.com:80")
			So(msg.Pkts[0].GetPayload(), ShouldResemble, []byte("hello"))
			So(conn.InboundBuffered(), ShouldEqual, 0)
		})

		PatchConvey("Multiple frames in one buffer should all be parsed in order", func() {
			buf := append(mustMarshal("a.com:80", []byte("one")), mustMarshal("b.com:80", []byte("two"))...)
			conn := &bufferConn{buf: buf}

			msg, rejected := ParseAvailable(conn, &protocol.ForwardPacket{}, logger)

			So(rejected, ShouldBeFalse)
			So(msg.Pkts, ShouldHaveLength, 2)
			So(msg.Pkts[0].GetPayload(), ShouldResemble, []byte("one"))
			So(msg.Pkts[1].GetPayload(), ShouldResemble, []byte("two"))
			So(conn.InboundBuffered(), ShouldEqual, 0)
		})

		PatchConvey("A trailing partial frame should stop parsing and leave it buffered", func() {
			full := mustMarshal("a.com:80", []byte("one"))
			partial := mustMarshal("b.com:80", []byte("two"))
			conn := &bufferConn{buf: append(full, partial[:len(partial)-1]...)}

			msg, rejected := ParseAvailable(conn, &protocol.ForwardPacket{}, logger)

			So(rejected, ShouldBeFalse)
			So(msg.Pkts, ShouldHaveLength, 1)
			So(msg.Pkts[0].GetPayload(), ShouldResemble, []byte("one"))
			So(conn.InboundBuffered(), ShouldEqual, len(partial)-1)
		})

		PatchConvey("Insufficient data should need more without rejecting", func() {
			conn := &bufferConn{buf: []byte{0x00}}

			msg, rejected := ParseAvailable(conn, &protocol.ForwardPacket{}, logger)

			So(rejected, ShouldBeFalse)
			So(msg.Pkts, ShouldBeEmpty)
		})

		PatchConvey("A protocol violation should reject and drop parsed packets", func() {
			// destinationLen==0 触发 ForwardPacket 的 ParseRejected
			conn := &bufferConn{buf: []byte{0x00, 0x00, 0x00, 0x00}}

			msg, rejected := ParseAvailable(conn, &protocol.ForwardPacket{}, logger)

			So(rejected, ShouldBeTrue)
			So(msg, ShouldBeNil)
		})
	})
}
