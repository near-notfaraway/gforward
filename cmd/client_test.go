package cmd

import (
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
)

func TestParseIPv4Addr(t *testing.T) {
	PatchConvey("Test parseIPv4Addr", t, func() {
		PatchConvey("Valid IPv4 and port should be parsed", func() {
			host, port, err := parseIPv4Addr("192.0.2.10:8080")

			So(err, ShouldBeNil)
			So(host, ShouldEqual, "192.0.2.10")
			So(port, ShouldEqual, "8080")
		})

		PatchConvey("Address without port should be rejected", func() {
			_, _, err := parseIPv4Addr("127.0.0.1")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "must be IPv4:port")
		})

		PatchConvey("Non-IPv4 host should be rejected", func() {
			_, _, err := parseIPv4Addr("example.com:8080")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "not a valid IPv4 address")
		})

		PatchConvey("Out-of-range port should be rejected", func() {
			_, _, err := parseIPv4Addr("127.0.0.1:65536")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "port")
		})
	})
}

func TestDefaultClientListenerAddr(t *testing.T) {
	PatchConvey("Test defaultClientListenerAddr", t, func() {
		tests := []struct {
			name     string
			mode     string
			wantAddr string
		}{
			{name: "HTTP DNS", mode: ClientModeHTTPDNS, wantAddr: "0.0.0.0:80"},
			{name: "HTTPS DNS", mode: ClientModeHTTPSDNS, wantAddr: "0.0.0.0:443"},
			{name: "HTTP proxy", mode: ClientModeHTTPProxy, wantAddr: "0.0.0.0:8080"},
			{name: "SOCKS5", mode: ClientModeHTTPSocks5, wantAddr: "0.0.0.0:1080"},
		}
		for _, tt := range tests {
			PatchConvey(tt.name+" should use its default port", func() {
				addr, err := defaultClientListenerAddr(tt.mode)

				So(err, ShouldBeNil)
				So(addr, ShouldEqual, tt.wantAddr)
			})
		}

		PatchConvey("Unknown mode should be rejected", func() {
			addr, err := defaultClientListenerAddr("unknown")

			So(addr, ShouldBeEmpty)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid client mode")
		})
	})
}

func TestResolveClientMode(t *testing.T) {
	PatchConvey("Test resolveClientMode", t, func() {
		PatchConvey("HTTP DNS should use the listener IP for DNS", func() {
			dnsIP, enableDNS, err := resolveClientMode(ClientModeHTTPDNS, "192.0.2.10:80")

			So(err, ShouldBeNil)
			So(dnsIP, ShouldEqual, "192.0.2.10")
			So(enableDNS, ShouldBeTrue)
		})

		PatchConvey("HTTPS DNS should accept a wildcard listener IP", func() {
			dnsIP, enableDNS, err := resolveClientMode(ClientModeHTTPSDNS, "0.0.0.0:443")

			So(err, ShouldBeNil)
			So(dnsIP, ShouldEqual, "0.0.0.0")
			So(enableDNS, ShouldBeTrue)
		})

		PatchConvey("Proxy modes should not enable DNS", func() {
			for _, mode := range []string{ClientModeHTTPProxy, ClientModeHTTPSocks5} {
				dnsIP, enableDNS, err := resolveClientMode(mode, "127.0.0.1:8080")

				So(err, ShouldBeNil)
				So(dnsIP, ShouldBeEmpty)
				So(enableDNS, ShouldBeFalse)
			}
		})

		PatchConvey("HTTP DNS should reject a non-80 listener port", func() {
			_, _, err := resolveClientMode(ClientModeHTTPDNS, "192.0.2.10:8080")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "must be 80")
		})

		PatchConvey("Invalid listener address should be rejected", func() {
			_, _, err := resolveClientMode(ClientModeHTTPProxy, "127.0.0.1")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "must be IPv4:port")
		})

		PatchConvey("Unknown mode should be rejected", func() {
			_, _, err := resolveClientMode("unknown", "127.0.0.1:8080")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid client mode")
		})
	})
}
