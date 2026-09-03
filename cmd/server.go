package cmd

import (
	"fmt"
	"log"
	"runtime"

	"github.com/near-notfaraway/gforward/diagnosis"
	"github.com/near-notfaraway/gforward/server"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
)

var serverCmd *cobra.Command
var serverListenerAddr string
var serverMulticore bool
var serverVerbose bool

func init() {
	serverCmd = &cobra.Command{
		Use:   "server",
		Short: "A server which receives traffic sent by the client",
		Long:  `A server which receives the private protocol traffic sent by the client, forwards it to other server or back-to-source`,
		Run: func(cmd *cobra.Command, args []string) {
			serverRun(cmd, args)
		},
	}

	serverCmd.Flags().StringVarP(&serverListenerAddr, "listen", "l",
		"0.0.0.0:9989", "IPv4 address and port listened on by server")
	serverCmd.Flags().BoolVar(&serverMulticore, "multicore",
		true, "run with multicore")
	serverCmd.Flags().BoolVarP(&serverVerbose, "verbose", "v",
		false, "log more information")
}

func serverRun(cmd *cobra.Command, args []string) {
	if _, _, err := parseIPv4Addr(serverListenerAddr); err != nil {
		log.Fatalf("invalid server listener address: %s", err)
	}
	logLevel := "warn"
	if serverVerbose {
		logLevel = "debug"
	}
	if err := diagnosis.InitLogger(&diagnosis.LogConfig{
		Level:   logLevel,
		Verbose: false,
		Path:    diagnosis.StandOutPutPath,
	}); err != nil {
		panic(fmt.Sprintf("init logger failed: %s", err))
	}
	handlerNum := 1
	if serverMulticore {
		handlerNum = runtime.NumCPU()
	}

	srv := server.NewDispatcher(handlerNum)
	if err := gnet.Run(srv, fmt.Sprintf("tcp://%s", serverListenerAddr), gnet.WithMulticore(serverMulticore)); err != nil {
		log.Fatal(err)
	}
}
