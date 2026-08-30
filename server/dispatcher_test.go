package server

import (
	"testing"

	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

type contextStubConn struct {
	gnet.Conn
	context any // 保存 Dispatcher 分配的连接上下文
}

func (c *contextStubConn) Context() any {
	return c.context
}

func (c *contextStubConn) SetContext(ctx any) {
	c.context = ctx
}

func TestDispatcherAssignsHandlerOnOpen(t *testing.T) {
	Convey("Dispatcher should assign handlers round-robin on open", t, func() {
		dispatcher := &Dispatcher{
			channels: make([]chan *DispatchMsg, 2),
		}
		conns := []*contextStubConn{{}, {}, {}}

		for _, conn := range conns {
			out, action := dispatcher.OnOpen(conn)
			So(out, ShouldBeNil)
			So(action, ShouldEqual, gnet.None)
		}

		So(conns[0].context.(*dispatchContext).handlerIndex, ShouldEqual, 0)
		So(conns[1].context.(*dispatchContext).handlerIndex, ShouldEqual, 1)
		So(conns[2].context.(*dispatchContext).handlerIndex, ShouldEqual, 0)
	})
}
