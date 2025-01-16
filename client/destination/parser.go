package destination

import (
	"github.com/panjf2000/gnet/v2"
)

type ParserProto string

const (
	ParserProtoHTTP      ParserProto = "http"
	ParserProtoHTTPS     ParserProto = "https"
	ParserProtoHTTPProxy ParserProto = "http_proxy"
	ParserProtoSocks5    ParserProto = "socks5"
)

type Parser interface {
	Parse(conn gnet.Conn) (string, error)
}

func NewParser(proto ParserProto) Parser {
	switch proto {
	case ParserProtoHTTP:
		return NewHTTPParser()
	case ParserProtoHTTPS:
		return NewHTTPSParser()
	case ParserProtoHTTPProxy:
		return NewHTTPProxyParser()
	case ParserProtoSocks5:
		return NewSocks5Parser()
	default:
		return nil
	}
}
