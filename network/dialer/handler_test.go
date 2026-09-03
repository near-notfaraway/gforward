package dialer

import (
	"errors"
	"net"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

type stubConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	context any // 保存测试用连接上下文
}

func (c *stubConn) LocalAddr() net.Addr {
	return nil
}

func (c *stubConn) RemoteAddr() net.Addr {
	return nil
}

func (c *stubConn) Context() any {
	return c.context
}

// bufferConn 用内存缓冲模拟 gnet 连接的 Peek/Discard/InboundBuffered，供 OnTraffic 解析测试。
type bufferConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	buf     []byte // 尚未消费的入站缓冲
	context any    // 连接上下文
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

func (c *bufferConn) InboundBuffered() int { return len(c.buf) }
func (c *bufferConn) LocalAddr() net.Addr  { return nil }
func (c *bufferConn) RemoteAddr() net.Addr { return nil }
func (c *bufferConn) Context() any         { return c.context }

func TestOnOpenPublishesOpenEventWithoutHook(t *testing.T) {
	PatchConvey("OnOpen should publish an open event when no hook is registered", t, func() {
		handler := NewDialHandler("plain", logrus.New().WithField("test", "dialer"))
		token := "dial-token"
		conn := &stubConn{context: token}

		out, action := handler.OnOpen(conn)

		So(out, ShouldBeNil)
		So(action, ShouldEqual, gnet.None)
		var msg *message.RecvMsg
		select {
		case msg = <-handler.recvChan:
		default:
		}
		So(msg, ShouldNotBeNil)
		if msg == nil {
			return
		}
		So(msg.Event, ShouldEqual, message.RecvEventOpen)
		So(msg.Conn, ShouldEqual, conn)
		So(msg.Token, ShouldEqual, token)
	})
}

// TestOnClosePublishesConnectionEvent 验证目标连接关闭后会向消费方发送关闭消息。
func TestOnClosePublishesConnectionEvent(t *testing.T) {
	PatchConvey("Test DialHandler.OnClose", t, func() {
		handler := NewDialHandler("plain", logrus.New().WithField("test", "dialer"))
		conn := &stubConn{}
		closeErr := errors.New("peer closed")

		action := handler.OnClose(conn, closeErr)

		So(action, ShouldEqual, gnet.None)
		var msg *message.RecvMsg
		select {
		case msg = <-handler.recvChan:
		default:
		}
		So(msg, ShouldNotBeNil)
		if msg == nil {
			return
		}
		So(msg.Conn, ShouldEqual, conn)
		So(msg.Event, ShouldEqual, message.RecvEventClose)
		So(msg.Pkts, ShouldBeEmpty)
	})
}

// TestOnOpenInvokesRegisteredHook 验证注册 onOpen 回调后，OnOpen 直接回调而不投递 RecvChan。
func TestOnOpenInvokesRegisteredHook(t *testing.T) {
	PatchConvey("OnOpen should invoke the registered hook instead of publishing", t, func() {
		handler := NewDialHandler("plain", logrus.New().WithField("test", "dialer"))
		var gotConn gnet.Conn
		handler.onOpen = func(conn gnet.Conn) gnet.Action {
			gotConn = conn
			return gnet.Close
		}
		conn := &stubConn{}

		out, action := handler.OnOpen(conn)

		So(out, ShouldBeNil)
		So(action, ShouldEqual, gnet.Close)
		So(gotConn, ShouldEqual, conn)
		So(len(handler.recvChan), ShouldEqual, 0)
	})
}

func TestDialHandlerOnTraffic(t *testing.T) {
	PatchConvey("OnTraffic should parse packets and reject protocol violations", t, func() {
		handler := NewDialHandler("plain", logrus.New().WithField("test", "dialer"))

		PatchConvey("Parsed data should be published to the recv chan", func() {
			conn := &bufferConn{buf: []byte("payload")}

			action := handler.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			msg := <-handler.recvChan
			So(msg.Event, ShouldEqual, message.RecvEventData)
			So(msg.Pkts, ShouldHaveLength, 1)
			So(msg.Pkts[0].GetPayload(), ShouldResemble, []byte("payload"))
		})

		PatchConvey("A rejected parse should close the connection", func() {
			conn := &bufferConn{buf: []byte("bad")}
			Mock(message.ParseAvailable).Return(nil, true).Build()

			action := handler.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
			So(len(handler.recvChan), ShouldEqual, 0)
		})

		PatchConvey("Empty parse result should publish nothing", func() {
			conn := &bufferConn{}

			action := handler.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(handler.recvChan), ShouldEqual, 0)
		})
	})
}
