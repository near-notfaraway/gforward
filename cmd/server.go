package cmd

import (
	"fmt"
	"github.com/near-notfaraway/gtunnel/diagnosis"
	"github.com/near-notfaraway/gtunnel/server"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
	"log"
)

var serverCmd *cobra.Command
var serverPort int
var serverMulticore bool

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
	serverCmd.Flags().BoolVar(&clientMulticore, "multicore",
		true, "run with multicore")
}

func serverRun(cmd *cobra.Command, args []string) {
	if err := diagnosis.InitLogger(&diagnosis.LogConfig{
		Level:   "debug",
		Verbose: false,
		Path:    diagnosis.StandOutPutPath,
	}); err != nil {
		panic(fmt.Sprintf("init logger failed: %s", err))
	}

	srv := server.NewListenHandler()
	log.Fatal(gnet.Run(srv, fmt.Sprintf("tcp://:%d", serverPort), gnet.WithMulticore(serverMulticore)))
}
