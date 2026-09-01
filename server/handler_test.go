package server

import (
	"errors"
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

// TestRemoveByDestOnlyRemovesMatchingClientRoute 验证迟到的关闭事件不会误删新路由。
func TestRemoveByDestOnlyRemovesMatchingClientRoute(t *testing.T) {
	PatchConvey("Test sessionTable.removeByDest", t, func() {
		clientConn := &stubConn{}
		oldDestConn := &stubConn{}
		currentDestConn := &stubConn{}
		sessions := &sessionTable{
			byClient: map[gnet.Conn]*session{
				clientConn: {
					dest:     "example.com:80",
					destConn: currentDestConn,
				},
			},
			byDest: map[gnet.Conn]gnet.Conn{
				oldDestConn:     clientConn,
				currentDestConn: clientConn,
			},
		}

		PatchConvey("Closing an old destination should preserve the current client route", func() {
			sessions.removeByDest(oldDestConn)

			_, oldRouteExists := sessions.byDest[oldDestConn]
			So(oldRouteExists, ShouldBeFalse)
			So(sessions.byClient[clientConn], ShouldNotBeNil)
			So(sessions.byClient[clientConn].destConn, ShouldEqual, currentDestConn)
		})

		PatchConvey("Closing the current destination should remove both routes", func() {
			sessions.removeByDest(currentDestConn)

			_, reverseRouteExists := sessions.byDest[currentDestConn]
			_, clientRouteExists := sessions.byClient[clientConn]
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
		handler := &msgHandler{
			sessions: &sessionTable{
				byClient: map[gnet.Conn]*session{
					clientConn: {
						dest:     packet.GetDestination(),
						destConn: destConn,
					},
				},
				byDest: map[gnet.Conn]gnet.Conn{
					destConn: clientConn,
				},
			},
		}
		logger := logrus.New().WithField("test", "server")

		PatchConvey("Client payload should be written asynchronously to destination", func() {
			handler.handleClientMsg(&message.RecvMsg{
				Conn:   clientConn,
				Pkts:   []protocol.InternalPacket{packet},
				Logger: logger,
			})

			So(destConn.writeCount, ShouldEqual, 0)
			So(destConn.asyncWriteCount, ShouldEqual, 1)
			So(destConn.written, ShouldResemble, packet.GetPayload())
		})

		PatchConvey("Batched packets should each be forwarded in arrival order", func() {
			second := &protocol.ForwardPacket{}
			second.SetDestination(packet.GetDestination())
			second.SetPayload([]byte("payload2"))

			handler.handleClientMsg(&message.RecvMsg{
				Conn:   clientConn,
				Pkts:   []protocol.InternalPacket{packet, second},
				Logger: logger,
			})

			So(destConn.asyncWriteCount, ShouldEqual, 2)
			So(destConn.written, ShouldResemble, second.GetPayload())
		})

		PatchConvey("Empty pkts should close the client route", func() {
			handler.handleClientMsg(&message.RecvMsg{
				Conn:   clientConn,
				Logger: logger,
			})

			_, clientRouteExists := handler.sessions.byClient[clientConn]
			So(clientRouteExists, ShouldBeFalse)
			So(destConn.closeCount, ShouldEqual, 1)
		})

		PatchConvey("Destination payload should be written asynchronously to client", func() {
			handler.handleDestMsg(&message.RecvMsg{
				Conn:   destConn,
				Pkts:   []protocol.InternalPacket{packet},
				Logger: logger,
			})

			So(clientConn.writeCount, ShouldEqual, 0)
			So(clientConn.asyncWriteCount, ShouldEqual, 1)
			So(clientConn.written, ShouldResemble, packet.GetPayload())
		})

		PatchConvey("Destination write failure should remove and close its route", func() {
			destConn.asyncWriteErr = errors.New("write failed")

			handler.handleClientMsg(&message.RecvMsg{
				Conn:   clientConn,
				Pkts:   []protocol.InternalPacket{packet},
				Logger: logger,
			})

			_, clientRouteExists := handler.sessions.byClient[clientConn]
			_, destRouteExists := handler.sessions.byDest[destConn]
			So(clientRouteExists, ShouldBeFalse)
			So(destRouteExists, ShouldBeFalse)
			So(destConn.closeCount, ShouldEqual, 1)
		})
	})
}

func TestMsgHandlerForwardUpstream(t *testing.T) {
	PatchConvey("forwardUpstream should act on the admitUpstream decision", t, func() {
		clientConn := &stubConn{}
		logger := logrus.New().WithField("test", "server")

		PatchConvey("Dialing session should queue payload without any I/O", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			handler.sessions.byClient[clientConn] = &session{
				dest:    "example.com:80",
				dialing: true,
				pending: [][]byte{[]byte("first")},
			}

			pkt := &protocol.ForwardPacket{}
			pkt.SetDestination("example.com:80")
			pkt.SetPayload([]byte("second"))
			handler.forwardUpstream(clientConn, pkt, logger)

			So(handler.sessions.byClient[clientConn].pending, ShouldResemble, [][]byte{[]byte("first"), []byte("second")})
		})

		PatchConvey("New destination should close the old conn and start an async dial", func() {
			var dialedNetwork, dialedAddr string
			var dialedToken *dialToken
			Mock((*dialer.Dialer).AsyncDial).To(func(_ *dialer.Dialer, network, address string, token any) {
				dialedNetwork = network
				dialedAddr = address
				dialedToken = token.(*dialToken)
			}).Build()

			oldDestConn := &stubConn{}
			handler := &msgHandler{sessions: newSessionTable(), dialer: &dialer.Dialer{}}
			handler.sessions.byClient[clientConn] = &session{dest: "old.com:80", destConn: oldDestConn}
			handler.sessions.byDest[oldDestConn] = clientConn

			pkt := &protocol.ForwardPacket{}
			pkt.SetDestination("new.com:80")
			pkt.SetPayload([]byte("payload"))
			handler.forwardUpstream(clientConn, pkt, logger)

			So(oldDestConn.closeCount, ShouldEqual, 1)
			So(dialedNetwork, ShouldEqual, "tcp")
			So(dialedAddr, ShouldEqual, "new.com:80")
			So(dialedToken, ShouldNotBeNil)
			So(dialedToken.clientConn, ShouldEqual, clientConn)
			So(dialedToken.session, ShouldEqual, handler.sessions.byClient[clientConn])
		})
	})
}

func TestMsgHandlerHandleDialResult(t *testing.T) {
	PatchConvey("handleDialResult should validate the session then flush pending", t, func() {
		clientConn := &stubConn{}
		destConn := &stubConn{}
		logger := logrus.New().WithField("test", "server")

		PatchConvey("Stale result should be dropped and the new conn closed", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			sess := &session{dest: "example.com:80", dialing: true}

			handler.handleDialResult(&dialer.DialResult{
				Conn:  destConn,
				Token: &dialToken{clientConn: clientConn, session: sess, logger: logger},
			})

			So(destConn.closeCount, ShouldEqual, 1)
		})

		PatchConvey("Dial error on a matched session should not write anything", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			sess := &session{dest: "example.com:80", dialing: true, pending: [][]byte{[]byte("x")}}
			handler.sessions.byClient[clientConn] = sess

			handler.handleDialResult(&dialer.DialResult{
				Err:   errors.New("dial failed"),
				Token: &dialToken{clientConn: clientConn, session: sess, logger: logger},
			})

			_, exists := handler.sessions.byClient[clientConn]
			So(exists, ShouldBeFalse)
			So(destConn.asyncWriteCount, ShouldEqual, 0)
		})

		PatchConvey("Successful dial should flush pending payloads in order", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			sess := &session{
				dest:    "example.com:80",
				dialing: true,
				pending: [][]byte{[]byte("a"), []byte("b")},
			}
			handler.sessions.byClient[clientConn] = sess

			handler.handleDialResult(&dialer.DialResult{
				Conn:  destConn,
				Token: &dialToken{clientConn: clientConn, session: sess, logger: logger},
			})

			So(destConn.asyncWriteCount, ShouldEqual, 2)
			So(destConn.written, ShouldResemble, []byte("b"))
			So(sess.dialing, ShouldBeFalse)
		})
	})
}

func TestMsgHandlerHandleDestMsg(t *testing.T) {
	PatchConvey("handleDestMsg should route downstream data and handle closures", t, func() {
		clientConn := &stubConn{}
		destConn := &stubConn{}
		logger := logrus.New().WithField("test", "server")

		PatchConvey("Empty pkts should remove the dest route", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			handler.sessions.byDest[destConn] = clientConn
			handler.sessions.byClient[clientConn] = &session{dest: "example.com:80", destConn: destConn}

			handler.handleDestMsg(&message.RecvMsg{Conn: destConn, Logger: logger})

			_, exists := handler.sessions.byDest[destConn]
			So(exists, ShouldBeFalse)
		})

		PatchConvey("Unknown dest conn should be ignored without writes", func() {
			handler := &msgHandler{sessions: newSessionTable()}
			pkt := &protocol.ForwardPacket{}
			pkt.SetPayload([]byte("payload"))

			handler.handleDestMsg(&message.RecvMsg{
				Conn:   destConn,
				Pkts:   []protocol.InternalPacket{pkt},
				Logger: logger,
			})

			So(clientConn.asyncWriteCount, ShouldEqual, 0)
		})
	})
}

func TestNewMsgHandler(t *testing.T) {
	Convey("newMsgHandler should wire the dialer, channel and a fresh session table", t, func() {
		ch := make(chan *message.RecvMsg)
		dl := &dialer.Dialer{}

		handler := newMsgHandler(dl, ch)

		So(handler.dialer, ShouldEqual, dl)
		So(handler.channel, ShouldNotBeNil)
		So(handler.sessions, ShouldNotBeNil)
		So(handler.sessions.byClient, ShouldNotBeNil)
		So(handler.sessions.byDest, ShouldNotBeNil)
	})
}

func TestMsgHandlerStart(t *testing.T) {
	PatchConvey("start should route each source to its handler", t, func() {
		clientCh := make(chan *message.RecvMsg)
		dialCh := make(chan *dialer.DialResult)
		recvCh := make(chan *message.RecvMsg)

		Mock((*dialer.Dialer).DialResultChan).Return((<-chan *dialer.DialResult)(dialCh)).Build()
		Mock((*dialer.Dialer).RecvChan).Return((<-chan *message.RecvMsg)(recvCh)).Build()

		gotClient := make(chan *message.RecvMsg, 1)
		gotDial := make(chan *dialer.DialResult, 1)
		gotDest := make(chan *message.RecvMsg, 1)
		Mock((*msgHandler).handleClientMsg).To(func(_ *msgHandler, msg *message.RecvMsg) {
			gotClient <- msg
		}).Build()
		Mock((*msgHandler).handleDialResult).To(func(_ *msgHandler, res *dialer.DialResult) {
			gotDial <- res
		}).Build()
		Mock((*msgHandler).handleDestMsg).To(func(_ *msgHandler, msg *message.RecvMsg) {
			gotDest <- msg
		}).Build()

		h := &msgHandler{channel: clientCh, dialer: &dialer.Dialer{}}
		h.start()

		clientMsg := &message.RecvMsg{}
		clientCh <- clientMsg
		So(<-gotClient, ShouldEqual, clientMsg)

		dialRes := &dialer.DialResult{}
		dialCh <- dialRes
		So(<-gotDial, ShouldEqual, dialRes)

		destMsg := &message.RecvMsg{}
		recvCh <- destMsg
		So(<-gotDest, ShouldEqual, destMsg)

		// 关闭源通道，令两个后台循环经 !ok 与 range 结束干净退出
		close(clientCh)
		close(recvCh)
	})
}
