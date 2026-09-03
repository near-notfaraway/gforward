package cmd

import (
	"errors"
	"log"
	"os"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/miekg/dns"
	"github.com/near-notfaraway/gforward/client"
	"github.com/near-notfaraway/gforward/diagnosis"
	"github.com/near-notfaraway/gforward/server"
	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
)

func TestExecute(t *testing.T) {
	PatchConvey("Execute should register subcommands and run the root command", t, func() {
		PatchConvey("A successful run should not exit", func() {
			var exited bool
			Mock((*cobra.Command).Execute).Return(nil).Build()
			Mock(os.Exit).To(func(_ int) { exited = true }).Build()

			Execute()

			So(exited, ShouldBeFalse)
		})

		PatchConvey("A failing run should exit with code 1", func() {
			var exitCode int
			Mock((*cobra.Command).Execute).Return(errors.New("boom")).Build()
			Mock(os.Exit).To(func(code int) { exitCode = code }).Build()

			Execute()

			So(exitCode, ShouldEqual, 1)
		})
	})
}

func TestServerRun(t *testing.T) {
	PatchConvey("serverRun should init logging then run the dispatcher via gnet", t, func() {
		var initCalled, gnetCalled bool
		Mock(diagnosis.InitLogger).To(func(_ *diagnosis.LogConfig) error {
			initCalled = true
			return nil
		}).Build()
		// NewDispatcher 返回未导出类型，用 Return(nil) 避免真实创建 gnet 客户端
		Mock(server.NewDispatcher).Return(nil).Build()
		Mock(gnet.Run).To(func(_ gnet.EventHandler, _ string, _ ...gnet.Option) error {
			gnetCalled = true
			return nil
		}).Build()
		Mock(log.Fatal).Return().Build()

		PatchConvey("Multicore mode should init logging and start the server", func() {
			serverMulticore = true
			serverVerbose = true

			serverRun(&cobra.Command{}, nil)

			So(initCalled, ShouldBeTrue)
			So(gnetCalled, ShouldBeTrue)
		})

		PatchConvey("A logger init failure should panic", func() {
			Mock(diagnosis.InitLogger).Return(errors.New("init failed")).Build()

			So(func() { serverRun(&cobra.Command{}, nil) }, ShouldPanic)
		})
	})
}

func TestClientRun(t *testing.T) {
	PatchConvey("clientRun should resolve the mode then run the forwarder via gnet", t, func() {
		var gnetCalled bool
		Mock(diagnosis.InitLogger).Return(nil).Build()
		// NewForwarder 返回未导出类型，用 Return(nil) 避免真实创建
		Mock(client.NewForwarder).Return(nil).Build()
		Mock(gnet.Run).To(func(_ gnet.EventHandler, _ string, _ ...gnet.Option) error {
			gnetCalled = true
			return nil
		}).Build()
		Mock(log.Fatal).Return().Build()
		Mock(log.Fatalf).Return().Build()
		Mock(log.Printf).Return().Build()

		PatchConvey("Proxy mode should not start DNS and should run the forwarder", func() {
			var dnsStarted bool
			Mock(client.StartDNSServer).To(func(_, _ string) (*dns.Server, error) {
				dnsStarted = true
				return nil, nil
			}).Build()
			clientMode = ClientModeHTTPProxy
			clientListenerAddr = ""
			clientServerAddr = "127.0.0.1:9989"
			clientVerbose = false

			clientRun(&cobra.Command{}, nil)

			So(dnsStarted, ShouldBeFalse)
			So(gnetCalled, ShouldBeTrue)
		})

		PatchConvey("DNS mode should start the DNS server before running", func() {
			var dnsStarted bool
			Mock(client.StartDNSServer).To(func(_, _ string) (*dns.Server, error) {
				dnsStarted = true
				return nil, nil
			}).Build()
			clientMode = ClientModeHTTPDNS
			clientListenerAddr = "192.0.2.10:80"
			clientServerAddr = "127.0.0.1:9989"

			clientRun(&cobra.Command{}, nil)

			So(dnsStarted, ShouldBeTrue)
			So(gnetCalled, ShouldBeTrue)
		})
	})
}
