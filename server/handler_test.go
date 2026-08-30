package server

import (
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

type stubConn struct {
	gnet.Conn // 提供测试无需调用的连接方法
}

// TestRemoveDestRouteOnlyRemovesMatchingClientRoute 验证迟到的关闭事件不会误删新路由。
func TestRemoveDestRouteOnlyRemovesMatchingClientRoute(t *testing.T) {
	PatchConvey("Test MsgHandler.removeDestRoute", t, func() {
		clientConn := &stubConn{}
		oldDestConn := &stubConn{}
		currentDestConn := &stubConn{}
		handler := &MsgHandler{
			clientConnMapDestRoute: map[gnet.Conn]*destRoute{
				clientConn: {
					destination: "example.com:80",
					conn:        currentDestConn,
				},
			},
			destConnMapClientConn: map[gnet.Conn]gnet.Conn{
				oldDestConn:     clientConn,
				currentDestConn: clientConn,
			},
		}

		PatchConvey("Closing an old destination should preserve the current client route", func() {
			handler.removeDestRoute(oldDestConn)

			_, oldRouteExists := handler.destConnMapClientConn[oldDestConn]
			So(oldRouteExists, ShouldBeFalse)
			So(handler.clientConnMapDestRoute[clientConn], ShouldNotBeNil)
			So(handler.clientConnMapDestRoute[clientConn].conn, ShouldEqual, currentDestConn)
		})

		PatchConvey("Closing the current destination should remove both routes", func() {
			handler.removeDestRoute(currentDestConn)

			_, reverseRouteExists := handler.destConnMapClientConn[currentDestConn]
			_, clientRouteExists := handler.clientConnMapDestRoute[clientConn]
			So(reverseRouteExists, ShouldBeFalse)
			So(clientRouteExists, ShouldBeFalse)
		})
	})
}
