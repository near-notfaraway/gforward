package destination

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/txthinking/socks5"
)

// negotiationNoAuth 构造一个仅提供无认证方式的 SOCKS5 协商请求。
func negotiationNoAuth() []byte {
	return []byte{socks5.Ver, 0x01, socks5.MethodNone}
}

// connectRequestIPv4 构造一个针对 IPv4 目标的 SOCKS5 CONNECT 请求。
func connectRequestIPv4(a, b, c, d byte, port uint16) []byte {
	return []byte{
		socks5.Ver, socks5.CmdConnect, 0x00, socks5.ATYPIPv4,
		a, b, c, d,
		byte(port >> 8), byte(port),
	}
}

func TestSocks5ParserParse(t *testing.T) {
	Convey("Socks5Parser.Parse should negotiate then return the CONNECT destination", t, func() {
		parser := NewSocks5Parser()

		Convey("A no-auth negotiation and IPv4 CONNECT should succeed", func() {
			buf := append(negotiationNoAuth(), connectRequestIPv4(1, 2, 3, 4, 443)...)
			conn := &stubConn{buf: buf}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseDone)
			So(result.Destination, ShouldEqual, "1.2.3.4:443")
			// 协商响应与请求响应都应回写
			So(len(conn.written), ShouldBeGreaterThan, 0)
		})

		Convey("A short negotiation packet should need more data", func() {
			conn := &stubConn{buf: []byte{socks5.Ver}}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})

		Convey("An unsupported auth method should be rejected", func() {
			// 只提供用户名密码认证，不含无认证
			conn := &stubConn{buf: []byte{socks5.Ver, 0x01, socks5.MethodUsernamePassword}}

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})

		Convey("A non-CONNECT command should be rejected", func() {
			udpRequest := []byte{socks5.Ver, socks5.CmdUDP, 0x00, socks5.ATYPIPv4, 1, 2, 3, 4, 0x00, 0x50}
			buf := append(negotiationNoAuth(), udpRequest...)
			conn := &stubConn{buf: buf}

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})

		Convey("An already-connected connection should be rejected", func() {
			conn := &stubConn{}
			parser.connMapState.Store(conn, connStateConnected)

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})
	})
}

func TestSocks5ParserClear(t *testing.T) {
	Convey("Socks5Parser.Clear should drop the per-connection state", t, func() {
		parser := NewSocks5Parser()
		conn := &stubConn{}
		parser.connMapState.Store(conn, connStateNegotiated)

		parser.Clear(conn)

		_, ok := parser.connMapState.Load(conn)
		So(ok, ShouldBeFalse)
	})
}

func TestNegotiationPacketLen(t *testing.T) {
	Convey("negotiationPacketLen should size the negotiation packet and detect violations", t, func() {
		Convey("Fewer than two bytes should be incomplete without error", func() {
			n, complete, err := negotiationPacketLen([]byte{socks5.Ver})

			So(err, ShouldBeNil)
			So(complete, ShouldBeFalse)
			So(n, ShouldEqual, 0)
		})

		Convey("A wrong version should error", func() {
			_, _, err := negotiationPacketLen([]byte{0x04, 0x01})

			So(err, ShouldNotBeNil)
		})

		Convey("Zero methods should error", func() {
			_, _, err := negotiationPacketLen([]byte{socks5.Ver, 0x00})

			So(err, ShouldNotBeNil)
		})

		Convey("A full packet should report its length and completeness", func() {
			n, complete, err := negotiationPacketLen([]byte{socks5.Ver, 0x01, socks5.MethodNone})

			So(err, ShouldBeNil)
			So(complete, ShouldBeTrue)
			So(n, ShouldEqual, 3)
		})
	})
}

func TestRequestPacketLen(t *testing.T) {
	Convey("requestPacketLen should size the request by address type", t, func() {
		Convey("Fewer than four bytes should be incomplete", func() {
			n, complete, err := requestPacketLen([]byte{socks5.Ver, socks5.CmdConnect})

			So(err, ShouldBeNil)
			So(complete, ShouldBeFalse)
			So(n, ShouldEqual, 0)
		})

		Convey("A wrong version should error", func() {
			_, _, err := requestPacketLen([]byte{0x04, socks5.CmdConnect, 0x00, socks5.ATYPIPv4})

			So(err, ShouldNotBeNil)
		})

		Convey("An IPv4 request should be sized to ten bytes", func() {
			n, complete, err := requestPacketLen(connectRequestIPv4(1, 2, 3, 4, 443))

			So(err, ShouldBeNil)
			So(complete, ShouldBeTrue)
			So(n, ShouldEqual, 10)
		})

		Convey("A domain request should be sized by the domain length byte", func() {
			buf := []byte{socks5.Ver, socks5.CmdConnect, 0x00, socks5.ATYPDomain, 0x03, 'a', 'b', 'c', 0x00, 0x50}

			n, complete, err := requestPacketLen(buf)

			So(err, ShouldBeNil)
			So(complete, ShouldBeTrue)
			So(n, ShouldEqual, 10)
		})

		Convey("An empty domain should error", func() {
			buf := []byte{socks5.Ver, socks5.CmdConnect, 0x00, socks5.ATYPDomain, 0x00}

			_, _, err := requestPacketLen(buf)

			So(err, ShouldNotBeNil)
		})

		Convey("An invalid address type should error", func() {
			buf := []byte{socks5.Ver, socks5.CmdConnect, 0x00, 0x09}

			_, _, err := requestPacketLen(buf)

			So(err, ShouldNotBeNil)
		})
	})
}
