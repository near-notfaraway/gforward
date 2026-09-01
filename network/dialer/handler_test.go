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
}

func (c *stubConn) LocalAddr() net.Addr {
	return nil
}

func (c *stubConn) RemoteAddr() net.Addr {
	return nil
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
		So(msg.Pkts, ShouldBeEmpty)
	})
}
