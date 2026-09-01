package cmd

import (
	"fmt"
	"log"
	"net"
	"strconv"

	"github.com/near-notfaraway/gforward/client"
	"github.com/near-notfaraway/gforward/diagnosis"
	"github.com/panjf2000/gnet/v2"
	"github.com/spf13/cobra"
)

const (
	ClientModeHTTPDNS    = "http_dns"
	ClientModeHTTPSDNS   = "https_dns"
	ClientModeHTTPProxy  = "http_proxy"
	ClientModeHTTPSocks5 = "socks5"

	clientDNSListenAddr = ":53"
)

var clientCmd *cobra.Command
var clientMode string
var clientListenerAddr string
var clientMulticore bool
var clientServerAddr string
var clientVerbose bool

// init 注册 client 子命令及其运行参数。
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
	clientCmd.Flags().StringVarP(&clientListenerAddr, "listen", "l",
		"", "IPv4 address and port listened on by client, defaults depend on mode")
	clientCmd.Flags().StringVarP(&clientServerAddr, "server", "s",
		"127.0.0.1:9989", "IPv4 address and port of server")
	clientCmd.Flags().BoolVar(&clientMulticore, "multicore",
		true, "run with multicore")
	clientCmd.Flags().BoolVarP(&clientVerbose, "verbose", "v",
		false, "log more information")
}

func parseIPv4Addr(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("address %q must be IPv4:port: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return "", "", fmt.Errorf("address host %q is not a valid IPv4 address", host)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", fmt.Errorf("address port %q is not valid", port)
	}
	return ip.To4().String(), port, nil
}

func defaultClientListenerAddr(mode string) (string, error) {
	switch mode {
	case ClientModeHTTPDNS:
		return "0.0.0.0:80", nil
	case ClientModeHTTPSDNS:
		return "0.0.0.0:443", nil
	case ClientModeHTTPProxy:
		return "0.0.0.0:8080", nil
	case ClientModeHTTPSocks5:
		return "0.0.0.0:1080", nil
	default:
		return "", fmt.Errorf("invalid client mode %s", mode)
	}
}

// resolveClientMode validates the listener and returns the DNS hijack IP when required.
func resolveClientMode(mode, listenerAddr string) (string, bool, error) {
	listenerIP, listenerPort, err := parseIPv4Addr(listenerAddr)
	if err != nil {
		return "", false, err
	}
	switch mode {
	case ClientModeHTTPDNS:
		if listenerPort != "80" {
			return "", false, fmt.Errorf("http_dns listener port must be 80")
		}
	case ClientModeHTTPSDNS:
		if listenerPort != "443" {
			return "", false, fmt.Errorf("https_dns listener port must be 443")
		}
	case ClientModeHTTPProxy, ClientModeHTTPSocks5:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("invalid client mode %s", mode)
	}
	return listenerIP, true, nil
}

// clientRun 校验客户端模式、初始化日志，并按模式启动 DNS 与流量监听服务。
func clientRun(_ *cobra.Command, _ []string) {
	listenerAddr := clientListenerAddr
	if listenerAddr == "" {
		var err error
		listenerAddr, err = defaultClientListenerAddr(clientMode)
		if err != nil {
			log.Fatal(err)
		}
	}
	dnsHijackIP, enableDNS, err := resolveClientMode(clientMode, listenerAddr)
	if err != nil {
		log.Fatal(err)
	}
	if _, _, err := parseIPv4Addr(clientServerAddr); err != nil {
		log.Fatalf("invalid client server address: %s", err)
	}
	logLevel := "warn"
	if clientVerbose {
		logLevel = "debug"
	}
	if err := diagnosis.InitLogger(&diagnosis.LogConfig{
		Level:   logLevel,
		Verbose: false,
		Path:    diagnosis.StandOutPutPath,
	}); err != nil {
		panic(fmt.Sprintf("init logger failed: %s", err))
	}

	if enableDNS {
		if _, err := client.StartDNSServer(clientDNSListenAddr, dnsHijackIP); err != nil {
			log.Fatalf("start DNS server failed: %s", err)
		}
		log.Printf("DNS server listens on %s and hijacks A records to %s",
			clientDNSListenAddr, dnsHijackIP)
	}

	cli := client.NewListenHandler(clientMode, clientServerAddr)
	log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://%s", listenerAddr), gnet.WithMulticore(clientMulticore)))
}
