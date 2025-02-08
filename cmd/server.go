package cmd

import (
	"fmt"
	"github.com/near-notfaraway/gtunnel/diagnosis"
	"github.com/near-notfaraway/gtunnel/server"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
	"log"
	"runtime"
)

var serverCmd *cobra.Command
var serverPort int
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

	serverCmd.Flags().IntVarP(&serverPort, "port", "p",
		9989, "listen port")
	serverCmd.Flags().BoolVar(&serverMulticore, "multicore",
		true, "run with multicore")
	serverCmd.Flags().BoolVarP(&serverVerbose, "verbose", "v",
		false, "log more information")
}

func serverRun(cmd *cobra.Command, args []string) {
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
	log.Fatal(gnet.Run(srv, fmt.Sprintf("tcp://:%d", serverPort), gnet.WithMulticore(serverMulticore)))
}
