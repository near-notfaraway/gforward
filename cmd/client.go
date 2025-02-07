package cmd

import (
	"fmt"
	"github.com/near-notfaraway/gtunnel/client"
	"github.com/near-notfaraway/gtunnel/diagnosis"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
	"log"
)

const (
	ClientModeHTTPDNS    = "http_dns"
	ClientModeHTTPSDNS   = "https_dns"
	ClientModeHTTPProxy  = "http_proxy"
	ClientModeHTTPSocks5 = "socks5"
)

var clientCmd *cobra.Command
var clientMode string
var clientPort int
var clientMulticore bool
var clientServerAddr string

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
		"http_proxy", "one of http_dns,https_dns,http_proxy,socks5")
	clientCmd.Flags().IntVarP(&clientPort, "port", "p",
		0, "depend on mode, must http_dns:80, https_dns:443, default http_proxy:8080, socks5:1080")
	clientCmd.Flags().StringVarP(&clientServerAddr, "server", "s",
		"127.0.0.1:9989", "addr of server")
	clientCmd.Flags().BoolVar(&clientMulticore, "multicore",
		true, "run with multicore")
}

func clientRun(cmd *cobra.Command, args []string) {
	switch clientMode {
	case ClientModeHTTPDNS:
		clientPort = 80
	case ClientModeHTTPSDNS:
		clientPort = 443
	case ClientModeHTTPProxy:
		if clientPort == 0 {
			clientPort = 8080
		}
	case ClientModeHTTPSocks5:
		if clientPort == 0 {
			clientPort = 1080
		}
	default:
		log.Fatalf("invalid client mode %s", clientMode)
	}

	if err := diagnosis.InitLogger(&diagnosis.LogConfig{
		Level:   "debug",
		Verbose: false,
		Path:    diagnosis.StandOutPutPath,
	}); err != nil {
		panic(fmt.Sprintf("init logger failed: %s", err))
	}

	cli := client.NewListenHandler(clientMode, clientServerAddr)
	log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://:%d", clientPort), gnet.WithMulticore(clientMulticore)))
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
