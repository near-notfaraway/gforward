package dialer

import (
	"net"
	"testing"

	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

func TestDialEnrollsConnectionWithGNet(t *testing.T) {
	Convey("Dial should establish and enroll a TCP connection", t, func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		So(err, ShouldBeNil)
		defer listener.Close()

		accepted := make(chan net.Conn, 1)
		go func() {
			conn, acceptErr := listener.Accept()
			if acceptErr == nil {
				accepted <- conn
			}
		}()

		dialer := NewDialer("plain", logrus.New().WithField("test", "dialer"))
		defer dialer.client.Stop()

		conn, err := dialer.Dial("tcp", listener.Addr().String())
		So(err, ShouldBeNil)
		So(conn, ShouldNotBeNil)

		serverConn := <-accepted
		_ = serverConn.Close()
		_ = conn.Close()
	})
}
