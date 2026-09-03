package destination

import (
	"bytes"
	"fmt"
	"net"
	"slices"
	"sync"

	"github.com/panjf2000/gnet/v2"
	"github.com/txthinking/socks5"
)

type ConnState int

const (
	_ ConnState = iota
	connStateInit
	connStateNegotiated
	connStateConnected
)

type Socks5Parser struct {
	connMapState sync.Map // 维护每条连接的 SOCKS5 握手状态
}

func NewSocks5Parser() *Socks5Parser {
	return &Socks5Parser{}
}

func (p *Socks5Parser) Clear(conn gnet.Conn) {
	p.connMapState.Delete(conn)
}

// Parse 根据 Socks5 Request Address 来获取目的地
// https://datatracker.ietf.org/doc/html/rfc1928 [SOCKS Protocol Version 5]
func (p *Socks5Parser) Parse(conn gnet.Conn) (ParseResult, error) {
	for {
		buf, err := conn.Peek(-1)
		if err != nil {
			return p.reject(conn, fmt.Errorf("read socks5 request failed: %w", err))
		}

		switch p.getConnState(conn) {
		case connStateInit:
			packetLen, complete, err := negotiationPacketLen(buf)
			if err != nil {
				return p.reject(conn, err)
			}
			if !complete {
				return ParseResult{Status: ParseNeedMoreData}, nil
			}
			if err = p.handleNegotiationRequest(conn, buf[:packetLen]); err != nil {
				return p.reject(conn, err)
			}
			if _, err = conn.Discard(packetLen); err != nil {
				return p.reject(conn, err)
			}
			p.connMapState.Store(conn, connStateNegotiated)

		case connStateNegotiated:
			packetLen, complete, err := requestPacketLen(buf)
			if err != nil {
				return p.reject(conn, err)
			}
			if !complete {
				return ParseResult{Status: ParseNeedMoreData}, nil
			}
			dest, err := p.handleRequest(conn, buf[:packetLen])
			if err != nil {
				return p.reject(conn, err)
			}
			if _, err = conn.Discard(packetLen); err != nil {
				return p.reject(conn, err)
			}
			p.connMapState.Store(conn, connStateConnected)
			return ParseResult{
				Status:      ParseDone,
				Destination: dest,
			}, nil

		case connStateConnected:
			return p.reject(conn, fmt.Errorf("socks5 connection is already connected"))

		default:
			return p.reject(conn, fmt.Errorf("invalid conn state in parse"))
		}
	}
}

func (p *Socks5Parser) getConnState(conn gnet.Conn) ConnState {
	if state, ok := p.connMapState.Load(conn); ok {
		return state.(ConnState)
	}
	p.connMapState.Store(conn, connStateInit)
	return connStateInit
}

func (p *Socks5Parser) reject(conn gnet.Conn, err error) (ParseResult, error) {
	p.connMapState.Delete(conn)
	return ParseResult{Status: ParseRejected}, err
}

// handleNegotiationRequest 校验 SOCKS5 认证方式并返回无认证协商结果。
func (p *Socks5Parser) handleNegotiationRequest(conn gnet.Conn, buf []byte) error {
	req, err := socks5.NewNegotiationRequestFrom(bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("invalid sock5 nago request: %w", err)
	}

	// 目前仅支持无验证机制
	if !slices.Contains(req.Methods, socks5.MethodNone) {
		rp := socks5.NewNegotiationReply(socks5.MethodUnsupportAll)
		if _, err := rp.WriteTo(conn); err != nil {
			return err
		}
		return fmt.Errorf("socks5 authentication methods are not supported")
	}

	// 回复协商响应
	rp := socks5.NewNegotiationReply(socks5.MethodNone)
	if _, err := rp.WriteTo(conn); err != nil {
		return err
	}

	return nil
}

// handleRequest 校验 SOCKS5 CONNECT 请求、返回响应并提取目标地址。
func (p *Socks5Parser) handleRequest(conn gnet.Conn, buf []byte) (string, error) {
	req, err := socks5.NewRequestFrom(bytes.NewReader(buf))
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

	rp := socks5.NewReply(socks5.RepSuccess, socks5.ATYPIPv4, []byte{0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00})
	if _, err := rp.WriteTo(conn); err != nil {
		return "", fmt.Errorf("write socks5 resp to conn failed: %w", err)
	}

	return req.Address(), nil
}

func negotiationPacketLen(buf []byte) (int, bool, error) {
	if len(buf) < 2 {
		return 0, false, nil
	}
	if buf[0] != socks5.Ver {
		return 0, false, fmt.Errorf("invalid socks5 version: %d", buf[0])
	}
	if buf[1] == 0 {
		return 0, false, fmt.Errorf("socks5 negotiation has no methods")
	}
	packetLen := 2 + int(buf[1])
	return packetLen, len(buf) >= packetLen, nil
}

// requestPacketLen 根据 SOCKS5 地址类型计算完整请求长度，并区分短包与非法包。
func requestPacketLen(buf []byte) (int, bool, error) {
	if len(buf) < 4 {
		return 0, false, nil
	}
	if buf[0] != socks5.Ver {
		return 0, false, fmt.Errorf("invalid socks5 version: %d", buf[0])
	}

	var packetLen int
	switch buf[3] {
	case socks5.ATYPIPv4:
		packetLen = 4 + net.IPv4len + 2
	case socks5.ATYPIPv6:
		packetLen = 4 + net.IPv6len + 2
	case socks5.ATYPDomain:
		if len(buf) < 5 {
			return 0, false, nil
		}
		if buf[4] == 0 {
			return 0, false, fmt.Errorf("socks5 request has empty domain")
		}
		packetLen = 5 + int(buf[4]) + 2
	default:
		return 0, false, fmt.Errorf("invalid socks5 address type: %d", buf[3])
	}
	return packetLen, len(buf) >= packetLen, nil
}
