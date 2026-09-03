package client

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/client/destination"
	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

type trackingConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	closeCount      atomic.Int32 // 记录连接被关闭的次数
	writeCount      atomic.Int32 // 记录同步写入调用次数
	asyncWriteCount atomic.Int32 // 记录异步写入调用次数
	written         []byte       // 记录最后一次写入的数据

	buf []byte // OnTraffic 测试用的入站缓冲
}

func (c *trackingConn) Close() error {
	c.closeCount.Add(1)
	return nil
}

func (c *trackingConn) Write(buf []byte) (int, error) {
	c.writeCount.Add(1)
	c.written = append([]byte(nil), buf...)
	return len(buf), nil
}

func (c *trackingConn) AsyncWrite(buf []byte, callback gnet.AsyncCallback) error {
	c.asyncWriteCount.Add(1)
	c.written = append([]byte(nil), buf...)
	if callback != nil {
		_ = callback(c, nil)
	}
	return nil
}

func (c *trackingConn) Peek(n int) ([]byte, error) {
	if n <= 0 || n > len(c.buf) {
		return c.buf, nil
	}
	return c.buf[:n], nil
}

func (c *trackingConn) Discard(n int) (int, error) {
	if n > len(c.buf) {
		n = len(c.buf)
	}
	c.buf = c.buf[n:]
	return n, nil
}

func (c *trackingConn) InboundBuffered() int { return len(c.buf) }

func (c *trackingConn) LocalAddr() net.Addr {
	return nil
}

func (c *trackingConn) RemoteAddr() net.Addr {
	return nil
}

// fakeParser 以固定结果实现 destination.Parser，供 OnTraffic 测试脱离真实协议解析。
type fakeParser struct {
	result destination.ParseResult
	err    error
}

func (p *fakeParser) Parse(_ gnet.Conn) (destination.ParseResult, error) {
	return p.result, p.err
}

func TestNewForwarder(t *testing.T) {
	PatchConvey("NewForwarder should pick the parser proto from the mode", t, func() {
		PatchConvey("A _dns mode should strip the suffix for the parser proto", func() {
			var gotProto destination.ParserProto
			Mock(destination.NewParser).To(func(proto destination.ParserProto) destination.Parser {
				gotProto = proto
				return &fakeParser{}
			}).Build()

			f := NewForwarder("https_dns", "127.0.0.1:9989")

			So(gotProto, ShouldEqual, destination.ParserProto("https"))
			So(f.serverAddr, ShouldEqual, "127.0.0.1:9989")
			So(f.sessions, ShouldNotBeNil)
			So(f.uploadLogger, ShouldNotBeNil)
			So(f.downloadLogger, ShouldNotBeNil)
		})

		PatchConvey("A plain mode should be used as the parser proto verbatim", func() {
			var gotProto destination.ParserProto
			Mock(destination.NewParser).To(func(proto destination.ParserProto) destination.Parser {
				gotProto = proto
				return &fakeParser{}
			}).Build()

			NewForwarder("socks5", "127.0.0.1:9989")

			So(gotProto, ShouldEqual, destination.ParserProto("socks5"))
		})
	})
}

func TestForwarderOnTraffic(t *testing.T) {
	PatchConvey("OnTraffic should parse, frame and route user traffic", t, func() {
		logger := logrus.New().WithField("test", "client")

		PatchConvey("Need-more-data from the parser should keep the connection open", func() {
			conn := &trackingConn{buf: []byte("partial")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseNeedMoreData}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(f.sessions.byUser), ShouldEqual, 0)
		})

		PatchConvey("A rejected parse should close the connection", func() {
			conn := &trackingConn{buf: []byte("bad")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseRejected}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
		})

		PatchConvey("A parser error should close the connection", func() {
			conn := &trackingConn{buf: []byte("data")}
			f := &forwarder{
				destinationParser: &fakeParser{err: errors.New("parse failed")},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
		})

		PatchConvey("A first packet should register a session and start an async dial", func() {
			var dialedAddr string
			var dialedToken *dialToken
			Mock((*dialer.Dialer).AsyncDial).To(func(_ *dialer.Dialer, _, address string, token any) {
				dialedAddr = address
				dialedToken = token.(*dialToken)
			}).Build()

			conn := &trackingConn{buf: []byte("hello")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseDone, Destination: "example.com:80"}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				dialer:            &dialer.Dialer{},
				serverAddr:        "127.0.0.1:9989",
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(dialedAddr, ShouldEqual, "127.0.0.1:9989")
			So(dialedToken, ShouldNotBeNil)
			So(dialedToken.userConn, ShouldEqual, conn)
			So(f.sessions.byUser[conn], ShouldNotBeNil)
			So(f.sessions.byUser[conn].dialing, ShouldBeTrue)
		})

		PatchConvey("A ready session should forward the packet to the server conn", func() {
			conn := &trackingConn{buf: []byte("hello")}
			serverConn := &trackingConn{}
			f := &forwarder{
				internalProtocol: &protocol.ForwardPacket{},
				sessions:         newSessionTable(),
				uploadLogger:     logger,
			}
			f.sessions.byUser[conn] = &session{dest: "example.com:80", serverConn: serverConn}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(serverConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		})

		PatchConvey("A dialing session should queue the packet without writing", func() {
			conn := &trackingConn{buf: []byte("hello")}
			f := &forwarder{
				internalProtocol: &protocol.ForwardPacket{},
				sessions:         newSessionTable(),
				uploadLogger:     logger,
			}
			f.sessions.byUser[conn] = &session{dest: "example.com:80", dialing: true, pending: [][]byte{[]byte("first")}}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(f.sessions.byUser[conn].pending), ShouldEqual, 2)
		})
	})
}

// TestUserConnCloseClosesServerConn 验证用户连接关闭时会同步释放服务端连接和双向映射。
func TestUserConnCloseClosesServerConn(t *testing.T) {
	PatchConvey("Test forwarder.OnClose", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &forwarder{
			destinationParser: &fakeParser{},
			sessions:          newSessionTable(),
			uploadLogger:      logrus.New().WithField("test", "client"),
		}
		handler.sessions.byUser[userConn] = &session{serverConn: serverConn}
		handler.sessions.byServer[serverConn] = userConn

		handler.OnClose(userConn, nil)

		_, userRouteExists := handler.sessions.byUser[userConn]
		_, serverRouteExists := handler.sessions.byServer[serverConn]
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(serverConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}

func TestServerPayloadUsesAsyncWrite(t *testing.T) {
	PatchConvey("Server payload should be written asynchronously to user", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		packet := &protocol.PlainPacket{}
		packet.SetPayload([]byte("payload"))
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logrus.New().WithField("test", "client"),
		}
		handler.sessions.byServer[serverConn] = userConn

		handler.handleServerMsg(&message.RecvMsg{
			Conn:   serverConn,
			Pkts:   []protocol.InternalPacket{packet},
			Logger: logrus.New().WithField("test", "dialer"),
		})

		So(userConn.writeCount.Load(), ShouldEqual, int32(0))
		So(userConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		So(userConn.written, ShouldResemble, packet.GetPayload())
	})
}

func TestOpenEventCompletesDialAndFlushesPending(t *testing.T) {
	PatchConvey("Open event should complete session and flush pending packets", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		sess := &session{
			dialing: true,
			pending: [][]byte{[]byte("pending")},
		}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		handler.sessions.byUser[userConn] = sess

		handler.handleDialOpen(&message.RecvMsg{
			Event:  message.RecvEventOpen,
			Conn:   serverConn,
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		boundUserConn, ok := handler.sessions.byServer[serverConn]
		So(ok, ShouldBeTrue)
		So(boundUserConn, ShouldEqual, userConn)
		So(sess.serverConn, ShouldEqual, serverConn)
		So(sess.dialing, ShouldBeFalse)
		So(sess.pending, ShouldBeNil)
		So(serverConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		So(serverConn.written, ShouldResemble, []byte("pending"))
	})
}

// TestOpenEventForStaleSessionClosesServerConn 验证会话失效时拨号就绪事件会关闭新连接。
func TestOpenEventForStaleSessionClosesServerConn(t *testing.T) {
	PatchConvey("Open event for a stale session should close the server conn", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		sess := &session{dialing: true}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		// byUser 中没有该会话，视为已失效

		handler.handleDialOpen(&message.RecvMsg{
			Event:  message.RecvEventOpen,
			Conn:   serverConn,
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		So(serverConn.closeCount.Load(), ShouldEqual, int32(1))
		_, exists := handler.sessions.byServer[serverConn]
		So(exists, ShouldBeFalse)
	})
}

func TestDialErrorEventClearsDialingSession(t *testing.T) {
	PatchConvey("Dial error event should clear dialing session", t, func() {
		userConn := &trackingConn{}
		sess := &session{
			dialing: true,
			pending: [][]byte{[]byte("pending")},
		}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		handler.sessions.byUser[userConn] = sess

		handler.handleDialError(&message.RecvMsg{
			Event:  message.RecvEventDialError,
			Err:    errors.New("dial failed"),
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		_, ok := handler.sessions.byUser[userConn]
		So(ok, ShouldBeFalse)
	})
}

// TestServerConnCloseClosesUserConn 验证服务端连接关闭事件会清理映射并关闭用户连接。
func TestServerConnCloseClosesUserConn(t *testing.T) {
	PatchConvey("Test forwarder.handleServerMsg with close event", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logrus.New().WithField("test", "client"),
		}
		handler.sessions.byUser[userConn] = &session{serverConn: serverConn}
		handler.sessions.byServer[serverConn] = userConn

		handler.handleServerMsg(&message.RecvMsg{Event: message.RecvEventClose, Conn: serverConn})

		_, userRouteExists := handler.sessions.byUser[userConn]
		_, serverRouteExists := handler.sessions.byServer[serverConn]
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(userConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}
