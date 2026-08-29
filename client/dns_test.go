package client

import (
	"errors"
	"net"
	"testing"
	"time"

	. "github.com/bytedance/mockey"
	"github.com/miekg/dns"
	. "github.com/smartystreets/goconvey/convey"
)

type dnsResponseWriter struct {
	msg *dns.Msg // 保存 DNS 处理器写回的响应。
}

func (w *dnsResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

func (w *dnsResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 53000,
	}
}

func (w *dnsResponseWriter) WriteMsg(msg *dns.Msg) error {
	w.msg = msg.Copy()
	return nil
}

func (w *dnsResponseWriter) Write(buf []byte) (int, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(buf); err != nil {
		return 0, err
	}
	w.msg = msg
	return len(buf), nil
}

func (w *dnsResponseWriter) Close() error {
	return nil
}

func (w *dnsResponseWriter) TsigStatus() error {
	return nil
}

func (w *dnsResponseWriter) TsigTimersOnly(bool) {}

func (w *dnsResponseWriter) Hijack() {}

func TestNewDNSHandler(t *testing.T) {
	PatchConvey("Test newDNSHandler", t, func() {
		PatchConvey("Valid IPv4 should create a handler", func() {
			handler, err := newDNSHandler("192.0.2.10")

			So(err, ShouldBeNil)
			So(handler, ShouldNotBeNil)
			So(handler.hijackIP.String(), ShouldEqual, "192.0.2.10")
		})

		PatchConvey("IPv6 should be rejected", func() {
			handler, err := newDNSHandler("2001:db8::1")

			So(handler, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "not a valid IPv4 address")
		})
	})
}

func TestLocalIPv4ForRemote(t *testing.T) {
	PatchConvey("Test localIPv4ForRemote", t, func() {
		PatchConvey("IPv4 UDP remote should select the route-facing local IP", func() {
			ip, err := localIPv4ForRemote(&net.UDPAddr{
				IP:   net.ParseIP("127.0.0.1"),
				Port: 53000,
			})

			So(err, ShouldBeNil)
			So(ip.String(), ShouldEqual, "127.0.0.1")
		})

		PatchConvey("Non-UDP remote address should be rejected", func() {
			ip, err := localIPv4ForRemote(&net.TCPAddr{
				IP:   net.ParseIP("127.0.0.1"),
				Port: 53000,
			})

			So(ip, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "not IPv4 UDP")
		})
	})
}

func TestDNSHijackHandlerServeDNS(t *testing.T) {
	PatchConvey("Test dnsHijackHandler.ServeDNS", t, func() {
		PatchConvey("A query should return the configured hijack IP", func() {
			handler, err := newDNSHandler("192.0.2.10")
			So(err, ShouldBeNil)
			request := new(dns.Msg)
			request.SetQuestion("example.com.", dns.TypeA)
			writer := new(dnsResponseWriter)

			handler.ServeDNS(writer, request)

			So(writer.msg, ShouldNotBeNil)
			So(writer.msg.Rcode, ShouldEqual, dns.RcodeSuccess)
			So(writer.msg.Answer, ShouldHaveLength, 1)
			answer, ok := writer.msg.Answer[0].(*dns.A)
			So(ok, ShouldBeTrue)
			So(answer.A.String(), ShouldEqual, "192.0.2.10")
		})

		PatchConvey("AAAA query should not receive a hijacked answer", func() {
			handler, err := newDNSHandler("192.0.2.10")
			So(err, ShouldBeNil)
			request := new(dns.Msg)
			request.SetQuestion("example.com.", dns.TypeAAAA)
			writer := new(dnsResponseWriter)

			handler.ServeDNS(writer, request)

			So(writer.msg, ShouldNotBeNil)
			So(writer.msg.Answer, ShouldBeEmpty)
		})

		PatchConvey("Wildcard IP should use the request-facing local IP", func() {
			Mock(localIPv4ForRemote).
				Return(net.ParseIP("192.0.2.20"), nil).
				Build()
			handler, err := newDNSHandler("0.0.0.0")
			So(err, ShouldBeNil)
			request := new(dns.Msg)
			request.SetQuestion("example.com.", dns.TypeA)
			writer := new(dnsResponseWriter)

			handler.ServeDNS(writer, request)

			So(writer.msg.Answer, ShouldHaveLength, 1)
			answer := writer.msg.Answer[0].(*dns.A)
			So(answer.A.String(), ShouldEqual, "192.0.2.20")
		})

		PatchConvey("Local IP resolution failure should return SERVFAIL", func() {
			Mock(localIPv4ForRemote).
				Return(nil, errors.New("route unavailable")).
				Build()
			handler, err := newDNSHandler("0.0.0.0")
			So(err, ShouldBeNil)
			request := new(dns.Msg)
			request.SetQuestion("example.com.", dns.TypeA)
			writer := new(dnsResponseWriter)

			handler.ServeDNS(writer, request)

			So(writer.msg, ShouldNotBeNil)
			So(writer.msg.Rcode, ShouldEqual, dns.RcodeServerFailure)
			So(writer.msg.Answer, ShouldBeEmpty)
		})
	})
}

func TestStartDNSServer(t *testing.T) {
	PatchConvey("Test StartDNSServer", t, func() {
		PatchConvey("Started server should answer UDP queries", func() {
			server, err := StartDNSServer("127.0.0.1:0", "192.0.2.10")
			So(err, ShouldBeNil)
			defer func() {
				So(server.Shutdown(), ShouldBeNil)
			}()

			request := new(dns.Msg)
			request.SetQuestion("example.com.", dns.TypeA)
			dnsClient := &dns.Client{
				Net:     "udp",
				Timeout: time.Second,
			}
			response, _, err := dnsClient.Exchange(request, server.PacketConn.LocalAddr().String())

			So(err, ShouldBeNil)
			So(response.Answer, ShouldHaveLength, 1)
		})

		PatchConvey("Invalid hijack IP should prevent startup", func() {
			server, err := StartDNSServer("127.0.0.1:0", "invalid")

			So(server, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})
}
