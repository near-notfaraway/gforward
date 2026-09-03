package server

import (
	"net"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

// newUploadLogger 提供测试用的日志实例，避免直接依赖包内部的 logger 构造。
func newUploadLogger() *logrus.Entry {
	return logrus.New().WithField("test", "dispatcher")
}

type contextStubConn struct {
	gnet.Conn
	context any // 保存 Dispatcher 分配的连接上下文
}

func (c *contextStubConn) LocalAddr() net.Addr {
	return nil
}

func (c *contextStubConn) RemoteAddr() net.Addr {
	return nil
}

func (c *contextStubConn) Context() any {
	return c.context
}

func (c *contextStubConn) SetContext(ctx any) {
	c.context = ctx
}

func TestDispatcherAssignsHandlerOnOpen(t *testing.T) {
	Convey("Dispatcher should assign handlers round-robin on open", t, func() {
		dispatcher := &dispatcher{
			workers: make([]*dispatchWorker, 2),
		}
		conns := []*contextStubConn{{}, {}, {}}

		for _, conn := range conns {
			out, action := dispatcher.OnOpen(conn)
			So(out, ShouldBeNil)
			So(action, ShouldEqual, gnet.None)
		}

		So(conns[0].context.(*dispatchContext).workerIndex, ShouldEqual, 0)
		So(conns[1].context.(*dispatchContext).workerIndex, ShouldEqual, 1)
		So(conns[2].context.(*dispatchContext).workerIndex, ShouldEqual, 0)
	})
}

func TestNewDispatcher(t *testing.T) {
	PatchConvey("NewDispatcher should create the requested number of workers", t, func() {
		// 避免创建真实 gnet 客户端与后台循环
		Mock(dialer.NewDialer).Return(&dialer.Dialer{}).Build()
		// newMsgHandler 会在裸 Dialer 上注册 OnDialOpen 回调，桩掉以免空指针
		Mock((*dialer.Dialer).SetOnDialOpen).Return().Build()
		Mock((*msgHandler).start).Return().Build()

		d := NewDispatcher(3)

		So(len(d.workers), ShouldEqual, 3)
		for _, w := range d.workers {
			So(w.channel, ShouldNotBeNil)
			So(w.handler, ShouldNotBeNil)
		}
		So(d.internalProtocol, ShouldNotBeNil)
		So(d.uploadLogger, ShouldNotBeNil)
	})
}

func TestDispatcherOnClose(t *testing.T) {
	PatchConvey("OnClose should dispatch a close event", t, func() {
		ch := make(chan *message.RecvMsg, 1)
		d := &dispatcher{
			workers:      []*dispatchWorker{{channel: ch}},
			uploadLogger: newUploadLogger(),
		}
		conn := &contextStubConn{context: &dispatchContext{workerIndex: 0}}

		action := d.OnClose(conn, nil)

		So(action, ShouldEqual, gnet.None)
		msg := <-ch
		So(msg.Conn, ShouldEqual, conn)
		So(msg.Event, ShouldEqual, message.RecvEventClose)
		So(len(msg.Pkts), ShouldEqual, 0)
	})
}

func TestDispatcherOnTraffic(t *testing.T) {
	PatchConvey("OnTraffic should parse and dispatch, closing on rejection", t, func() {
		ch := make(chan *message.RecvMsg, 1)
		d := &dispatcher{
			workers:      []*dispatchWorker{{channel: ch}},
			uploadLogger: newUploadLogger(),
		}
		conn := &contextStubConn{context: &dispatchContext{workerIndex: 0}}

		PatchConvey("Rejected parse should close the connection", func() {
			Mock(message.ParseAvailable).Return(&message.RecvMsg{Conn: conn}, true).Build()

			action := d.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
			So(len(ch), ShouldEqual, 0)
		})

		PatchConvey("Parsed packets should be dispatched to the worker", func() {
			parsed := &message.RecvMsg{
				Conn:   conn,
				Pkts:   []protocol.InternalPacket{&protocol.ForwardPacket{}},
				Logger: newUploadLogger(),
			}
			Mock(message.ParseAvailable).Return(parsed, false).Build()

			action := d.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(<-ch, ShouldEqual, parsed)
		})

		PatchConvey("Empty parse result should not dispatch anything", func() {
			Mock(message.ParseAvailable).Return(&message.RecvMsg{Conn: conn, Logger: newUploadLogger()}, false).Build()

			action := d.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(ch), ShouldEqual, 0)
		})
	})
}

func TestDispatcherDispatch(t *testing.T) {
	Convey("dispatch should route the message to the worker chosen by connection context", t, func() {
		ch0 := make(chan *message.RecvMsg, 1)
		ch1 := make(chan *message.RecvMsg, 1)
		d := &dispatcher{
			workers: []*dispatchWorker{{channel: ch0}, {channel: ch1}},
		}
		conn := &contextStubConn{context: &dispatchContext{workerIndex: 1}}
		msg := &message.RecvMsg{Conn: conn, Logger: newUploadLogger()}

		d.dispatch(msg)

		So(len(ch0), ShouldEqual, 0)
		So(<-ch1, ShouldEqual, msg)
	})
}
