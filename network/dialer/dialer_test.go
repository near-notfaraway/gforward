package dialer

import (
	"net"
	"testing"
	"time"

	"github.com/near-notfaraway/gforward/network/message"
	"github.com/panjf2000/gnet/v2"
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

		conn, err := dialer.Dial("tcp", listener.Addr().String(), nil)
		So(err, ShouldBeNil)
		So(conn, ShouldNotBeNil)

		serverConn := <-accepted
		_ = serverConn.Close()
		_ = conn.Close()
	})
}

func TestAsyncDialRecvChanModePublishesOpenOnlyOnSuccess(t *testing.T) {
	Convey("AsyncDial in recvChan mode should publish open event only on success", t, func() {
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
		dialer.SetDialErrorToRecvChan(true)

		token := "dial-token"
		dialer.AsyncDial("tcp", listener.Addr().String(), token)

		msg := <-dialer.RecvChan()
		So(msg.Event, ShouldEqual, message.RecvEventOpen)
		So(msg.Token, ShouldEqual, token)
		So(msg.Conn, ShouldNotBeNil)

		select {
		case extra := <-dialer.RecvChan():
			t.Fatalf("unexpected extra event after open: %d", extra.Event)
		case <-time.After(50 * time.Millisecond):
		}

		serverConn := <-accepted
		_ = serverConn.Close()
		_ = msg.Conn.Close()
	})
}

func TestAsyncDialRecvChanModePublishesDialErrorOnFailure(t *testing.T) {
	Convey("AsyncDial in recvChan mode should publish dial error event on failure", t, func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		So(err, ShouldBeNil)
		address := listener.Addr().String()
		So(listener.Close(), ShouldBeNil)

		dialer := NewDialer("plain", logrus.New().WithField("test", "dialer"))
		defer dialer.client.Stop()
		dialer.SetDialErrorToRecvChan(true)

		token := "dial-token"
		dialer.AsyncDial("tcp", address, token)

		var msg *message.RecvMsg
		select {
		case msg = <-dialer.RecvChan():
		case <-time.After(2 * time.Second):
		}

		So(msg, ShouldNotBeNil)
		if msg == nil {
			return
		}
		So(msg.Event, ShouldEqual, message.RecvEventDialError)
		So(msg.Conn, ShouldBeNil)
		So(msg.Err, ShouldNotBeNil)
		So(msg.Token, ShouldEqual, token)
	})
}

func TestDialerAccessorsAndHooks(t *testing.T) {
	Convey("Dialer accessors should expose channels and register the open hook", t, func() {
		dialer := NewDialer("plain", logrus.New().WithField("test", "dialer"))
		defer dialer.client.Stop()

		Convey("RecvChan and DialResultChan should be non-nil", func() {
			So(dialer.RecvChan(), ShouldNotBeNil)
			So(dialer.DialResultChan(), ShouldNotBeNil)
		})

		Convey("SetOnDialOpen should store the hook on the handler", func() {
			hook := func(conn gnet.Conn) gnet.Action { return gnet.None }
			dialer.SetOnDialOpen(hook)

			So(dialer.handler.onOpen, ShouldNotBeNil)
		})
	})
}

func TestAsyncDialDefaultModePublishesResultOnSuccess(t *testing.T) {
	Convey("AsyncDial in default mode should publish the dial result on success", t, func() {
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

		token := "dial-token"
		dialer.AsyncDial("tcp", listener.Addr().String(), token)

		var res *DialResult
		select {
		case res = <-dialer.DialResultChan():
		case <-time.After(2 * time.Second):
		}

		So(res, ShouldNotBeNil)
		if res == nil {
			return
		}
		So(res.Err, ShouldBeNil)
		So(res.Conn, ShouldNotBeNil)
		So(res.Token, ShouldEqual, token)

		serverConn := <-accepted
		_ = serverConn.Close()
		_ = res.Conn.Close()
	})
}

func TestAsyncDialDefaultModePublishesErrorOnFailure(t *testing.T) {
	Convey("AsyncDial in default mode should publish a failed result on error", t, func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		So(err, ShouldBeNil)
		address := listener.Addr().String()
		So(listener.Close(), ShouldBeNil)

		dialer := NewDialer("plain", logrus.New().WithField("test", "dialer"))
		defer dialer.client.Stop()

		token := "dial-token"
		dialer.AsyncDial("tcp", address, token)

		var res *DialResult
		select {
		case res = <-dialer.DialResultChan():
		case <-time.After(2 * time.Second):
		}

		So(res, ShouldNotBeNil)
		if res == nil {
			return
		}
		So(res.Err, ShouldNotBeNil)
		So(res.Conn, ShouldBeNil)
		So(res.Token, ShouldEqual, token)
	})
}
