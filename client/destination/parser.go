package destination

import (
	"fmt"

	"github.com/panjf2000/gnet/v2"
)

type ParserProto string

const (
	ParserProtoHTTP      ParserProto = "http"
	ParserProtoHTTPS     ParserProto = "https"
	ParserProtoHTTPProxy ParserProto = "http_proxy"
	ParserProtoSocks5    ParserProto = "socks5"
)

type ParseStatus uint8

const (
	ParseNeedMoreData ParseStatus = iota
	ParseDone
	ParseRejected
)

type ParseResult struct {
	Status      ParseStatus // 本次解析所处的状态
	Destination string      // 解析出的目标主机和端口
	PerRequest  bool        // 是否需要为连接中的每个请求重新解析目标
	PayloadLen  int         // 当前请求可转发的负载长度，0 表示全部缓冲区
}

type Parser interface {
	Parse(conn gnet.Conn) (ParseResult, error)
}

type ConnStateCleaner interface {
	Clear(conn gnet.Conn)
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
		panic(fmt.Sprintf("invalid parser proto %s", proto))
	}
}
