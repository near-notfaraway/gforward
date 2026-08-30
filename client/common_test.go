package client

import (
	"net"
	"sync/atomic"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gtunnel/dialer"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

type trackingConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	closeCount atomic.Int32 // 记录连接被关闭的次数
}

func (c *trackingConn) Close() error {
	c.closeCount.Add(1)
	return nil
}

func (c *trackingConn) LocalAddr() net.Addr {
	return nil
}

func (c *trackingConn) RemoteAddr() net.Addr {
	return nil
}

// TestUserConnCloseClosesServerConn 验证用户连接关闭时会同步释放服务端连接和双向映射。
func TestUserConnCloseClosesServerConn(t *testing.T) {
	PatchConvey("Test ListenHandler.OnClose", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &ListenHandler{
			uploadLogger: logrus.New().WithField("test", "client"),
		}
		handler.userConnMapServerConn.Store(userConn, serverConn)
		handler.serverConnMapUserConn.Store(serverConn, userConn)

		handler.OnClose(userConn, nil)

		_, userRouteExists := handler.userConnMapServerConn.Load(userConn)
		_, serverRouteExists := handler.serverConnMapUserConn.Load(serverConn)
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(serverConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}

// TestServerConnCloseClosesUserConn 验证服务端连接关闭事件会清理映射并关闭用户连接。
func TestServerConnCloseClosesUserConn(t *testing.T) {
	PatchConvey("Test ListenHandler.handleServerPacket with close event", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &ListenHandler{
			downloadLogger: logrus.New().WithField("test", "client"),
		}
		handler.userConnMapServerConn.Store(userConn, serverConn)
		handler.serverConnMapUserConn.Store(serverConn, userConn)

		handler.handleServerPacket(&dialer.RecvPkt{Conn: serverConn})

		_, userRouteExists := handler.userConnMapServerConn.Load(userConn)
		_, serverRouteExists := handler.serverConnMapUserConn.Load(serverConn)
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(userConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}
