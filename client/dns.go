package client

import (
	"fmt"
	"net"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

const dnsRecordTTL = 60

type dnsHijackHandler struct {
	hijackIP net.IP // DNS A 查询统一返回的客户端 IPv4 地址。
}

func newDNSHandler(hijackIP string) (*dnsHijackHandler, error) {
	ip := net.ParseIP(hijackIP)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("DNS hijack IP %q is not a valid IPv4 address", hijackIP)
	}
	return &dnsHijackHandler{
		hijackIP: ip.To4(),
	}, nil
}

func localIPv4ForRemote(remoteAddr net.Addr) (net.IP, error) {
	remoteUDPAddr, ok := remoteAddr.(*net.UDPAddr)
	if !ok || remoteUDPAddr.IP.To4() == nil {
		return nil, fmt.Errorf("DNS remote address %q is not IPv4 UDP", remoteAddr)
	}
	conn, err := net.DialUDP("udp4", nil, remoteUDPAddr)
	if err != nil {
		return nil, fmt.Errorf("select local IP for DNS client %s failed: %w", remoteAddr, err)
	}
	defer conn.Close()
	localUDPAddr := conn.LocalAddr().(*net.UDPAddr)
	localIP := localUDPAddr.IP.To4()
	if localIP == nil {
		return nil, fmt.Errorf("selected local address %s is not IPv4", localUDPAddr)
	}
	return localIP, nil
}

// ServeDNS returns the configured IP, or the request-facing local IP for a wildcard listener.
func (h *dnsHijackHandler) ServeDNS(w dns.ResponseWriter, request *dns.Msg) {
	response := new(dns.Msg)
	response.SetReply(request)
	response.Authoritative = true

	for _, question := range request.Question {
		if question.Qclass != dns.ClassINET || question.Qtype != dns.TypeA {
			continue
		}
		hijackIP := h.hijackIP
		if hijackIP.IsUnspecified() {
			var err error
			hijackIP, err = localIPv4ForRemote(w.RemoteAddr())
			if err != nil {
				response.Rcode = dns.RcodeServerFailure
				logrus.WithError(err).Error("resolve DNS hijack IP failed")
				break
			}
		}
		response.Answer = append(response.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   question.Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    dnsRecordTTL,
			},
			A: append(net.IP(nil), hijackIP...),
		})
	}

	if err := w.WriteMsg(response); err != nil {
		logrus.WithError(err).Error("write DNS response failed")
	}
}

// StartDNSServer synchronously binds a UDP socket and serves hijacked A records in the background.
func StartDNSServer(listenAddr, hijackIP string) (*dns.Server, error) {
	handler, err := newDNSHandler(hijackIP)
	if err != nil {
		return nil, err
	}
	packetConn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen DNS server on %s failed: %w", listenAddr, err)
	}
	server := &dns.Server{
		PacketConn: packetConn,
		Handler:    handler,
	}
	go func() {
		if err := server.ActivateAndServe(); err != nil {
			logrus.WithError(err).Error("DNS server stopped")
		}
	}()
	return server, nil
}
