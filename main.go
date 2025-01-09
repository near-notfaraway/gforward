package main

import (
	"flag"
	"fmt"
	"github.com/near-notfaraway/gtunnel/client"
	"github.com/near-notfaraway/gtunnel/server"
	"github.com/panjf2000/gnet/v2"
	"log"
)

//func handleDnsRequest(w dns.ResponseWriter, r *dns.Msg) {
//	m := new(dns.Msg)
//	m.SetReply(r)
//	m.Compress = false
//
//	log.Printf("received %s dns request", r.Question[0].Name)
//	switch r.Question[0].Qtype {
//	case dns.TypeA:
//		if r.Question[0].Name == "www.google.com.hk" {
//			rr, _ := dns.NewRR(fmt.Sprintf("%s A 10.74.54.120", r.Question[0].Name))
//			m.Answer = append(m.Answer, rr)
//			log.Printf("send %s dns resp: %s", r.Question[0].Name)
//		} else {
//			addrs, err := net.LookupHost("www.google.com.hk")
//			if err != nil {
//				panic(err)
//			}
//			if len(addrs) > 0 {
//				rr, _ := dns.NewRR(fmt.Sprintf("%s A %s", addrs[0], r.Question[0].Name))
//				m.Answer = append(m.Answer, rr)
//				log.Printf("send %s dns resp: %s", r.Question[0].Name)
//			}
//		}
//	default:
//	}
//
//	wBuf, _ := m.Pack()
//	w.Write(wBuf)
//}

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

	var port int
	var multicore bool

	flag.IntVar(&port, "port", 9000, "--port 9000")
	flag.BoolVar(&multicore, "multicore", false, "--multicore true")
	flag.Parse()

	srv := server.NewListenHandler()
	go func() {
		log.Fatal(gnet.Run(srv, fmt.Sprintf("tcp://:%d", 9000), gnet.WithMulticore(multicore)))
	}()
	cli := client.NewListenHandler()
	log.Fatal(gnet.Run(cli, fmt.Sprintf("tcp://:%d", 8443), gnet.WithMulticore(multicore)))
}
