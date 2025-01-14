package client

import (
	"fmt"
	"github.com/miekg/dns"
	"log"
	"net"
)

func handleDnsRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	log.Printf("received %s dns request", r.Question[0].Name)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		if r.Question[0].Name == "www.google.com.hk" {
			rr, _ := dns.NewRR(fmt.Sprintf("%s A 10.74.54.120", r.Question[0].Name))
			m.Answer = append(m.Answer, rr)
			log.Printf("send %s dns resp: %s", r.Question[0].Name)
		} else {
			addrs, err := net.LookupHost("www.google.com.hk")
			if err != nil {
				panic(err)
			}
			if len(addrs) > 0 {
				rr, _ := dns.NewRR(fmt.Sprintf("%s A %s", addrs[0], r.Question[0].Name))
				m.Answer = append(m.Answer, rr)
				log.Printf("send %s dns resp: %s", r.Question[0].Name)
			}
		}
	default:
	}

	wBuf, _ := m.Pack()
	w.Write(wBuf)
}
