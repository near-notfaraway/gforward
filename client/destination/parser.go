package destination

import (
	"fmt"

	"github.com/panjf2000/gnet/v2"
)

type ParserProto string

const (
	ParserProtoHTTP        ParserProto = "http"
	ParserProtoHTTPS       ParserProto = "https"
	ParserProtoHTTPProxy   ParserProto = "http_proxy"
	ParserProtoSocks5      ParserProto = "socks5"
	ParserProtoShadowsocks ParserProto = "shadowsocks"
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
	Payload     []byte      // 解码后的负载；非 nil 时替代连接缓冲区中的原始字节
}

type Parser interface {
	Parse(conn gnet.Conn) (ParseResult, error)
}

// PayloadEncoder 标识解析器需要在下行写回用户连接前编码负载。
type PayloadEncoder interface {
	EncodePayload(conn gnet.Conn, payload []byte) ([]byte, error)
}

type ConnStateCleaner interface {
	Clear(conn gnet.Conn)
}

// ParseConfig 承载与接入模式相关的解析器构建参数，目前仅 shadowsocks 模式使用；
// 其余模式无需额外配置，可传入 nil。
type ParseConfig struct {
	Method   string // shadowsocks AEAD 加密方式，如 aes-256-gcm、chacha20-ietf-poly1305
	Password string // shadowsocks 预共享密码，用于派生 AEAD 主密钥
}

// NewParser 根据接入协议构造对应的目标解析器；cfg 仅供需要额外配置的模式（当前为 shadowsocks）使用。
func NewParser(proto ParserProto, cfg *ParseConfig) Parser {
	switch proto {
	case ParserProtoHTTP:
		return NewHTTPParser()
	case ParserProtoHTTPS:
		return NewHTTPSParser()
	case ParserProtoHTTPProxy:
		return NewHTTPProxyParser()
	case ParserProtoSocks5:
		return NewSocks5Parser()
	case ParserProtoShadowsocks:
		return NewShadowsocksParser(cfg)
	default:
		panic(fmt.Sprintf("invalid parser proto %s", proto))
	}
}
