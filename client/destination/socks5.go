package destination

import (
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"github.com/txthinking/socks5"
	"log"
	"net"
	"slices"
	"sync"
)

type ConnState int

const (
	_ ConnState = iota
	connStateInit
	connStateNegotiated
	connStateConnected
)

type Socks5Parser struct {
	connMapState sync.Map // map[gnet.Conn]ConnState
}

func NewSocks5Parser() *Socks5Parser {
	return &Socks5Parser{}
}

// Parse 根据 Socks5 Request Address 来获取目的地
// https://datatracker.ietf.org/doc/html/rfc1928 [SOCKS Protocol Version 5]
func (p *Socks5Parser) Parse(conn gnet.Conn) (string, error) {
	connState := p.getConnState(conn)
	switch connState {
	case connStateInit:
		if err := p.handleNegotiationRequest(conn); err != nil {
			p.connMapState.Delete(conn)
			return "", err
		}
		p.connMapState.Store(conn, connStateNegotiated)
		log.Printf("[client] conn %p sock5 state turn to: %d", conn, connStateNegotiated)
		return "", nil

	case connStateNegotiated:
		dest, err := p.handleRequest(conn)
		if err != nil {
			p.connMapState.Delete(conn)
			return "", err
		}
		p.connMapState.Store(conn, connStateConnected)
		log.Printf("[client] conn %p sock5 state turn to: %d", conn, connStateConnected)
		return dest, nil

	case connStateConnected:
		log.Printf("[client] conn %p sock5 state aready is: %d", conn, connStateConnected)
		return "", nil

	default:
		return "", fmt.Errorf("invalid conn state in parse")
	}
}

func (p *Socks5Parser) getConnState(conn gnet.Conn) ConnState {
	if state, ok := p.connMapState.Load(conn); ok {
		return state.(ConnState)
	} else {
		p.connMapState.Store(conn, connStateInit)
		return connStateInit
	}
}

func (p *Socks5Parser) handleNegotiationRequest(conn gnet.Conn) error {
	req, err := socks5.NewNegotiationRequestFrom(conn)
	if err != nil {
		return fmt.Errorf("invalid sock5 nago request: %w", err)
	}

	// 目前仅支持无验证机制
	if !slices.Contains(req.Methods, socks5.MethodNone) {
		rp := socks5.NewNegotiationReply(socks5.MethodUnsupportAll)
		if _, err := rp.WriteTo(conn); err != nil {
			return err
		}
	}

	// 回复协商响应
	rp := socks5.NewNegotiationReply(socks5.MethodNone)
	if _, err := rp.WriteTo(conn); err != nil {
		return err
	}

	return nil
}

func (p *Socks5Parser) handleRequest(conn gnet.Conn) (string, error) {
	req, err := socks5.NewRequestFrom(conn)
	if err != nil {
		return "", fmt.Errorf("invalid sock5 request: %w", err)
	}

	// 目前仅支持连接命令
	if req.Cmd != socks5.CmdConnect {
		errMsg := fmt.Errorf("invalid sock5 request cmd: %d", req.Cmd)
		var rp *socks5.Reply
		if req.Atyp == socks5.ATYPIPv4 || req.Atyp == socks5.ATYPDomain {
			rp = socks5.NewReply(socks5.RepCommandNotSupported, socks5.ATYPIPv4, net.IPv4zero, []byte{0x00, 0x00})
		} else {
			rp = socks5.NewReply(socks5.RepCommandNotSupported, socks5.ATYPIPv6, net.IPv6zero, []byte{0x00, 0x00})
		}
		if _, err = rp.WriteTo(conn); err != nil {
			return "", fmt.Errorf("%s, %w", errMsg, err)
		}
		return "", fmt.Errorf("invalid sock5 request cmd: %d", req.Cmd)
	}

	rp := socks5.NewReply(socks5.RepSuccess, req.Atyp, net.IPv4zero, []byte{0x00, 0x00})
	if _, err := rp.WriteTo(conn); err != nil {
		return "", fmt.Errorf("write socks5 resp to conn failed: %w", err)
	}

	return req.Address(), nil
}
