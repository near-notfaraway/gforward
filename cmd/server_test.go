package cmd

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/diagnosis"
	"github.com/near-notfaraway/gforward/server"
	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
)

func TestServerCommand(t *testing.T) {
	PatchConvey("Test serverCmd initialization", t, func() {
		So(serverCmd.Use, ShouldEqual, "server")
		So(serverCmd.Flags().Lookup("listen").DefValue, ShouldEqual, "0.0.0.0:9989")
		So(serverCmd.Flags().Lookup("multicore").DefValue, ShouldEqual, "true")
		So(serverCmd.Flags().Lookup("verbose").DefValue, ShouldEqual, "false")

		PatchConvey("Run should delegate to serverRun", func() {
			var called bool
			Mock(serverRun).To(func(_ *cobra.Command, _ []string) {
				called = true
			}).Build()

			serverCmd.Run(serverCmd, nil)

			So(called, ShouldBeTrue)
		})
	})
}

func TestServerRun(t *testing.T) {
	PatchConvey("Test serverRun", t, func() {
		oldListenerAddr := serverListenerAddr
		oldMulticore := serverMulticore
		oldVerbose := serverVerbose
		defer func() {
			serverListenerAddr = oldListenerAddr
			serverMulticore = oldMulticore
			serverVerbose = oldVerbose
		}()

		serverListenerAddr = "127.0.0.1:9989"
		serverMulticore = false
		serverVerbose = false

		var loggerLevel string
		var loggerPath string
		var runAddr string
		var fatalMessage string
		Mock(diagnosis.InitLogger).To(func(config *diagnosis.LogConfig) error {
			loggerLevel = string([]byte(config.Level))
			loggerPath = string([]byte(config.Path))
			return nil
		}).Build()
		Mock(gnet.Run).To(func(_ gnet.EventHandler, addr string, _ ...gnet.Option) error {
			runAddr = string([]byte(addr))
			return nil
		}).Build()
		Mock(log.Fatal).To(func(args ...any) {
			fatalMessage = fmt.Sprint(args...)
		}).Build()

		PatchConvey("Single-core mode should initialize and run the server", func() {
			dispatcherMock := Mock(server.NewDispatcher).
				When(func(workerNum int) bool { return workerNum == 1 }).
				Return(nil).
				Build()

			serverRun(&cobra.Command{}, nil)

			So(loggerLevel, ShouldEqual, "warn")
			So(loggerPath, ShouldEqual, diagnosis.StandOutPutPath)
			So(dispatcherMock.Times(), ShouldEqual, 1)
			So(runAddr, ShouldEqual, "tcp://127.0.0.1:9989")
		})

		PatchConvey("Multicore verbose mode should use CPU count and debug logging", func() {
			dispatcherMock := Mock(server.NewDispatcher).
				When(func(workerNum int) bool { return workerNum == runtime.NumCPU() }).
				Return(nil).
				Build()
			serverMulticore = true
			serverVerbose = true

			serverRun(&cobra.Command{}, nil)

			So(loggerLevel, ShouldEqual, "debug")
			So(dispatcherMock.Times(), ShouldEqual, 1)
		})

		PatchConvey("Invalid listener address should terminate the command", func() {
			Mock(log.Fatalf).To(func(_ string, _ ...any) {
				panic("fatal")
			}).Build()
			serverListenerAddr = "localhost:9989"

			So(func() {
				serverRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "fatal")
		})

		PatchConvey("Logger initialization errors should panic", func() {
			Mock(diagnosis.InitLogger).Return(errors.New("logger failed")).Build()

			So(func() {
				serverRun(&cobra.Command{}, nil)
			}, ShouldPanicWith, "init logger failed: logger failed")
		})

		PatchConvey("gnet errors should terminate the command", func() {
			runErr := errors.New("gnet failed")
			Mock(server.NewDispatcher).Return(nil).Build()
			Mock(gnet.Run).Return(runErr).Build()

			serverRun(&cobra.Command{}, nil)

			So(fatalMessage, ShouldEqual, runErr.Error())
		})

		PatchConvey("[defect-probing] A normal gnet stop should not terminate the command", func() {
			Mock(server.NewDispatcher).Return(nil).Build()

			serverRun(&cobra.Command{}, nil)

			So(fatalMessage, ShouldBeEmpty)
		})
	})
}
