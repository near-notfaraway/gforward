package utils

import (
	"errors"
	"net"
	"testing"

	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

// addrStubConn 提供可控的本地/远端地址，用于验证连接格式化。
type addrStubConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	local  net.Addr
	remote net.Addr
}

func (c *addrStubConn) LocalAddr() net.Addr  { return c.local }
func (c *addrStubConn) RemoteAddr() net.Addr { return c.remote }

func TestFormatGNetConn(t *testing.T) {
	Convey("FormatGNetConn should render local->remote and tolerate nil addrs", t, func() {
		Convey("Both addresses present should be joined by an arrow", func() {
			conn := &addrStubConn{
				local:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5000},
				remote: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 80},
			}

			So(FormatGNetConn(conn), ShouldEqual, "127.0.0.1:5000->10.0.0.1:80")
		})

		Convey("Nil addresses should render as <nil>", func() {
			conn := &addrStubConn{}

			So(FormatGNetConn(conn), ShouldEqual, "<nil>-><nil>")
		})
	})
}

// writeStubConn 记录 AsyncWrite 调用并可模拟立即错误与回调错误。
type writeStubConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	asyncWriteCount int    // AsyncWrite 调用次数
	immediateErr    error  // AsyncWrite 立即返回的错误
	callbackErr     error  // 传给回调的错误
	invokeCallback  bool   // 是否触发回调
	written         []byte // 最后一次写入的数据
}

func (c *writeStubConn) AsyncWrite(buf []byte, callback gnet.AsyncCallback) error {
	c.asyncWriteCount++
	c.written = append([]byte(nil), buf...)
	if c.invokeCallback && callback != nil {
		_ = callback(c, c.callbackErr)
	}
	return c.immediateErr
}

func TestAsyncWrite(t *testing.T) {
	Convey("AsyncWrite should submit the write and surface errors exactly once", t, func() {
		logger := logrus.New().WithField("test", "utils")

		Convey("Successful write should not invoke onError", func() {
			called := 0
			conn := &writeStubConn{invokeCallback: true}

			err := AsyncWrite(conn, []byte("payload"), logger, func() { called++ })

			So(err, ShouldBeNil)
			So(conn.asyncWriteCount, ShouldEqual, 1)
			So(conn.written, ShouldResemble, []byte("payload"))
			So(called, ShouldEqual, 0)
		})

		Convey("Immediate submit error should trigger onError and be returned", func() {
			called := 0
			conn := &writeStubConn{immediateErr: errors.New("submit failed")}

			err := AsyncWrite(conn, []byte("payload"), logger, func() { called++ })

			So(err, ShouldNotBeNil)
			So(called, ShouldEqual, 1)
		})

		Convey("Callback error should trigger onError only once even with immediate error", func() {
			called := 0
			conn := &writeStubConn{
				invokeCallback: true,
				callbackErr:    errors.New("write failed"),
				immediateErr:   errors.New("write failed"),
			}

			err := AsyncWrite(conn, []byte("payload"), logger, func() { called++ })

			So(err, ShouldNotBeNil)
			So(called, ShouldEqual, 1)
		})

		Convey("Nil onError should be safe on failure", func() {
			conn := &writeStubConn{invokeCallback: true, callbackErr: errors.New("write failed")}

			So(func() { _ = AsyncWrite(conn, []byte("x"), logger, nil) }, ShouldNotPanic)
		})
	})
}
