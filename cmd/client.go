package cmd

import (
	"fmt"
	"github.com/near-notfaraway/gtunnel/client"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
	"log"
)

const (
	ClientModeHTTPDNS   = "http_dns"
	ClientModeHTTPSDNS  = "https_dns"
	ClientModeHTTPProxy = "http_proxy"
)

var clientCmd *cobra.Command
var clientMode string
var clientPort int
var clientMulticore bool

func init() {
	clientCmd = &cobra.Command{
		Use:   "client",
		Short: "A client which accesses user traffic",
		Long:  `A client which accesses user traffic and forward it to gforward server through the private protocol`,
		Run: func(cmd *cobra.Command, args []string) {
			clientRun(cmd, args)
		},
	}
	clientCmd.Flags().StringVarP(&clientMode, "mode", "m",
		"http_proxy", "one of http_dns,https_dns,http_proxy")
	clientCmd.Flags().IntVarP(&clientPort, "port", "p",
		8989, "listen port")
	clientCmd.Flags().BoolVar(&clientMulticore, "multicore",
		true, "run with multicore")
}

func clientRun(cmd *cobra.Command, args []string) {
	cli := client.NewListenHandler()
	switch clientMode {
	case ClientModeHTTPDNS:
		log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://:%d", 80), gnet.WithMulticore(clientMulticore)))
	case ClientModeHTTPSDNS:
		log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://:%d", 443), gnet.WithMulticore(clientMulticore)))
	case ClientModeHTTPProxy:
		log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://:%d", clientPort), gnet.WithMulticore(clientMulticore)))
	default:
		log.Fatalf("invalid client mode %s", clientMode)
	}
}

func main() {
	//dns.HandleFunc(".", handleDnsRequest)
	//server := &dns.Server{
	//	Addr:    "10.74.54.120:53",
	//	Net:     "udp",
	//	Handler: nil,
	//}
	//go func() {
	//	err := server.ListenAndServe()
	//	if err != nil {
	//		log.Fatalf("启动DNS服务器失败: %v", err)
	//	}
	//}()
}
