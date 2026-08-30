package server

import (
	"errors"
	"net"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/near-notfaraway/gtunnel/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

type stubConn struct {
	gnet.Conn // 提供测试无需调用的连接方法

	writeCount      int    // 记录同步写入调用次数
	asyncWriteCount int    // 记录异步写入调用次数
	asyncWriteErr   error  // 模拟异步写入失败
	closeCount      int    // 记录连接关闭次数
	written         []byte // 记录最后一次写入的数据
}

func (c *stubConn) Write(buf []byte) (int, error) {
	c.writeCount++
	c.written = append([]byte(nil), buf...)
	return len(buf), nil
}

func (c *stubConn) AsyncWrite(buf []byte, callback gnet.AsyncCallback) error {
	c.asyncWriteCount++
	c.written = append([]byte(nil), buf...)
	if callback != nil {
		_ = callback(c, c.asyncWriteErr)
	}
	return c.asyncWriteErr
}

func (c *stubConn) Close() error {
	c.closeCount++
	return nil
}

func (c *stubConn) LocalAddr() net.Addr {
	return nil
}

func (c *stubConn) RemoteAddr() net.Addr {
	return nil
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

func TestMsgHandlerUsesAsyncWrite(t *testing.T) {
	PatchConvey("MsgHandler should use concurrency-safe writes", t, func() {
		clientConn := &stubConn{}
		destConn := &stubConn{}
		packet := &protocol.ForwardPacket{}
		packet.SetDestination("example.com:80")
		packet.SetPayload([]byte("payload"))
		handler := &MsgHandler{
			clientConnMapDestRoute: map[gnet.Conn]*destRoute{
				clientConn: {
					destination: packet.GetDestination(),
					conn:        destConn,
				},
			},
			destConnMapClientConn: map[gnet.Conn]gnet.Conn{
				destConn: clientConn,
			},
		}
		logger := logrus.New().WithField("test", "server")

		PatchConvey("Client payload should be written asynchronously to destination", func() {
			handler.handleClientMsg(&DispatchMsg{
				conn:   clientConn,
				pkt:    packet,
				logger: logger,
			})

			So(destConn.writeCount, ShouldEqual, 0)
			So(destConn.asyncWriteCount, ShouldEqual, 1)
			So(destConn.written, ShouldResemble, packet.GetPayload())
		})

		PatchConvey("Destination payload should be written asynchronously to client", func() {
			handler.handleDestPkt(&dialer.RecvPkt{
				Conn:   destConn,
				Pkt:    packet,
				Logger: logger,
			})

			So(clientConn.writeCount, ShouldEqual, 0)
			So(clientConn.asyncWriteCount, ShouldEqual, 1)
			So(clientConn.written, ShouldResemble, packet.GetPayload())
		})

		PatchConvey("Destination write failure should remove and close its route", func() {
			destConn.asyncWriteErr = errors.New("write failed")

			handler.handleClientMsg(&DispatchMsg{
				conn:   clientConn,
				pkt:    packet,
				logger: logger,
			})

			_, clientRouteExists := handler.clientConnMapDestRoute[clientConn]
			_, destRouteExists := handler.destConnMapClientConn[destConn]
			So(clientRouteExists, ShouldBeFalse)
			So(destRouteExists, ShouldBeFalse)
			So(destConn.closeCount, ShouldEqual, 1)
		})
	})
}
