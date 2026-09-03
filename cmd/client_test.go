package cmd

import (
	"errors"
	"fmt"
	"log"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/miekg/dns"
	"github.com/near-notfaraway/gforward/client"
	"github.com/near-notfaraway/gforward/diagnosis"
	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
)

func TestClientCommand(t *testing.T) {
	PatchConvey("Test clientCmd initialization", t, func() {
		So(clientCmd.Use, ShouldEqual, "client")
		So(clientCmd.Flags().Lookup("mode").DefValue, ShouldEqual, "http_proxy")
		So(clientCmd.Flags().Lookup("listen").DefValue, ShouldBeEmpty)
		So(clientCmd.Flags().Lookup("server").DefValue, ShouldEqual, "127.0.0.1:9989")
		So(clientCmd.Flags().Lookup("multicore").DefValue, ShouldEqual, "true")
		So(clientCmd.Flags().Lookup("verbose").DefValue, ShouldEqual, "false")
		So(clientCmd.Flags().Lookup("ss-method").DefValue, ShouldBeEmpty)
		So(clientCmd.Flags().Lookup("ss-password").DefValue, ShouldBeEmpty)

		PatchConvey("Run should delegate to clientRun", func() {
			var called bool
			Mock(clientRun).To(func(_ *cobra.Command, _ []string) {
				called = true
			}).Build()

			clientCmd.Run(clientCmd, nil)

			So(called, ShouldBeTrue)
		})
	})
}

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

		PatchConvey("Non-numeric and zero ports should be rejected", func() {
			for _, addr := range []string{"127.0.0.1:http", "127.0.0.1:0"} {
				_, _, err := parseIPv4Addr(addr)

				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "port")
			}
		})

		PatchConvey("[defect-probing] IPv4-mapped IPv6 syntax should be rejected", func() {
			_, _, err := parseIPv4Addr("[::ffff:127.0.0.1]:8080")

			So(err, ShouldNotBeNil)
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
			{name: "Shadowsocks", mode: ClientModeShadowsocks, wantAddr: "0.0.0.0:8388"},
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
			for _, mode := range []string{ClientModeHTTPProxy, ClientModeHTTPSocks5, ClientModeShadowsocks} {
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

		PatchConvey("HTTPS DNS should reject a non-443 listener port", func() {
			_, _, err := resolveClientMode(ClientModeHTTPSDNS, "192.0.2.10:8443")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "must be 443")
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

func TestClientRun(t *testing.T) {
	PatchConvey("Test clientRun", t, func() {
		oldMode := clientMode
		oldListenerAddr := clientListenerAddr
		oldMulticore := clientMulticore
		oldServerAddr := clientServerAddr
		oldVerbose := clientVerbose
		oldSSMethod := clientSSMethod
		oldSSPassword := clientSSPassword
		defer func() {
			clientMode = oldMode
			clientListenerAddr = oldListenerAddr
			clientMulticore = oldMulticore
			clientServerAddr = oldServerAddr
			clientVerbose = oldVerbose
			clientSSMethod = oldSSMethod
			clientSSPassword = oldSSPassword
		}()

		clientMode = ClientModeHTTPProxy
		clientListenerAddr = "127.0.0.1:8080"
		clientMulticore = false
		clientServerAddr = "127.0.0.1:9989"
		clientVerbose = false
		clientSSMethod = ""
		clientSSPassword = ""

		var loggerLevel string
		var loggerPath string
		var runAddr string
		var fatalMessage string
		Mock(diagnosis.InitLogger).To(func(config *diagnosis.LogConfig) error {
			loggerLevel = string([]byte(config.Level))
			loggerPath = string([]byte(config.Path))
			return nil
		}).Build()
		Mock(client.NewForwarder).Return(nil).Build()
		Mock(gnet.Run).To(func(_ gnet.EventHandler, addr string, _ ...gnet.Option) error {
			runAddr = string([]byte(addr))
			return nil
		}).Build()
		Mock(log.Fatal).To(func(args ...any) {
			fatalMessage = fmt.Sprint(args...)
		}).Build()
		Mock(log.Printf).Return().Build()

		PatchConvey("Proxy mode should initialize and run with the configured listener", func() {
			clientRun(&cobra.Command{}, nil)

			So(loggerLevel, ShouldEqual, "warn")
			So(loggerPath, ShouldEqual, diagnosis.StandOutPutPath)
			So(runAddr, ShouldEqual, "tcp://127.0.0.1:8080")
		})

		PatchConvey("Shadowsocks mode should use its default listener and pass parser config", func() {
			clientMode = ClientModeShadowsocks
			clientListenerAddr = ""
			clientVerbose = true
			clientSSMethod = "aes-256-gcm"
			clientSSPassword = "secret"

			clientRun(&cobra.Command{}, nil)

			So(loggerLevel, ShouldEqual, "debug")
			So(runAddr, ShouldEqual, "tcp://0.0.0.0:8388")
		})

		PatchConvey("DNS mode should start DNS with the listener IP", func() {
			var dnsAddr string
			var hijackIP string
			Mock(client.StartDNSServer).To(func(addr, ip string) (*dns.Server, error) {
				dnsAddr = addr
				hijackIP = ip
				return nil, nil
			}).Build()
			clientMode = ClientModeHTTPDNS
			clientListenerAddr = "192.0.2.10:80"

			clientRun(&cobra.Command{}, nil)

			So(dnsAddr, ShouldEqual, clientDNSListenAddr)
			So(hijackIP, ShouldEqual, "192.0.2.10")
		})

		PatchConvey("Logger initialization errors should panic", func() {
			Mock(diagnosis.InitLogger).Return(errors.New("logger failed")).Build()

			So(func() {
				clientRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "init logger failed: logger failed")
		})

		PatchConvey("DNS startup errors should terminate the command", func() {
			Mock(client.StartDNSServer).Return(nil, errors.New("dns failed")).Build()
			Mock(log.Fatalf).To(func(_ string, _ ...any) {
				panic("fatal")
			}).Build()
			clientMode = ClientModeHTTPDNS
			clientListenerAddr = "192.0.2.10:80"

			So(func() {
				clientRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "fatal")
		})

		PatchConvey("Invalid mode should terminate the command", func() {
			Mock(log.Fatal).To(func(_ ...any) {
				panic("fatal")
			}).Build()
			clientMode = "invalid"
			clientListenerAddr = ""

			So(func() {
				clientRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "fatal")
		})

		PatchConvey("Invalid server address should terminate the command", func() {
			Mock(log.Fatalf).To(func(_ string, _ ...any) {
				panic("fatal")
			}).Build()
			clientServerAddr = "localhost:9989"

			So(func() {
				clientRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "fatal")
		})

		PatchConvey("gnet errors should terminate the command", func() {
			runErr := errors.New("gnet failed")
			Mock(gnet.Run).Return(runErr).Build()

			clientRun(&cobra.Command{}, nil)

			So(fatalMessage, ShouldEqual, runErr.Error())
		})

		PatchConvey("[defect-probing] A normal gnet stop should not terminate the command", func() {
			clientRun(&cobra.Command{}, nil)

			So(fatalMessage, ShouldBeEmpty)
		})
	})
}

func TestResolveShadowsocksConfig(t *testing.T) {
	PatchConvey("Test resolveShadowsocksConfig", t, func() {
		PatchConvey("Non-shadowsocks modes should return no config", func() {
			cfg, err := resolveShadowsocksConfig(ClientModeHTTPProxy, "", "")

			So(err, ShouldBeNil)
			So(cfg, ShouldBeNil)
		})

		PatchConvey("Supported AEAD methods with a password should build a config", func() {
			for _, method := range []string{"aes-256-gcm", "chacha20-ietf-poly1305"} {
				cfg, err := resolveShadowsocksConfig(ClientModeShadowsocks, method, "secret")

				So(err, ShouldBeNil)
				So(cfg, ShouldNotBeNil)
				So(cfg.Method, ShouldEqual, method)
				So(cfg.Password, ShouldEqual, "secret")
			}
		})

		PatchConvey("A missing method should be rejected", func() {
			_, err := resolveShadowsocksConfig(ClientModeShadowsocks, "", "secret")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "requires --ss-method")
		})

		PatchConvey("An unsupported method should be rejected", func() {
			_, err := resolveShadowsocksConfig(ClientModeShadowsocks, "rc4-md5", "secret")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unsupported shadowsocks method")
		})

		PatchConvey("A missing password should be rejected", func() {
			_, err := resolveShadowsocksConfig(ClientModeShadowsocks, "aes-256-gcm", "")

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "requires --ss-password")
		})
	})
}
