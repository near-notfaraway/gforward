package destination

import (
	"testing"

	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

// stubConn 内嵌 gnet.Conn，仅以内存缓冲覆盖解析器用到的 Peek/Discard/Write：
// Peek 返回剩余缓冲，Discard 消费，Write 记录回写数据。
type stubConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	buf      []byte // 尚未消费的入站缓冲
	written  []byte // 解析器回写的数据（ACK、SOCKS5 响应等）
	peekErr  error  // 模拟 Peek 失败
	writeErr error  // 模拟 Write 失败
}

func (c *stubConn) Peek(n int) ([]byte, error) {
	if c.peekErr != nil {
		return nil, c.peekErr
	}
	if n < 0 || n > len(c.buf) {
		return c.buf, nil
	}
	return c.buf[:n], nil
}

func (c *stubConn) Discard(n int) (int, error) {
	if n > len(c.buf) {
		n = len(c.buf)
	}
	c.buf = c.buf[n:]
	return n, nil
}

func (c *stubConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	c.written = append(c.written, p...)
	return len(p), nil
}

func TestNewParser(t *testing.T) {
	Convey("NewParser should build the parser matching the proto", t, func() {
		Convey("Known protos should build their concrete parsers", func() {
			_, isHTTP := NewParser(ParserProtoHTTP, nil).(*HTTPParser)
			So(isHTTP, ShouldBeTrue)

			_, isHTTPS := NewParser(ParserProtoHTTPS, nil).(*HTTPSParser)
			So(isHTTPS, ShouldBeTrue)

			_, isProxy := NewParser(ParserProtoHTTPProxy, nil).(*HTTPProxyParser)
			So(isProxy, ShouldBeTrue)

			_, isSocks5 := NewParser(ParserProtoSocks5, nil).(*Socks5Parser)
			So(isSocks5, ShouldBeTrue)

			_, isShadowsocks := NewParser(ParserProtoShadowsocks, &ParseConfig{Method: "aes-256-gcm", Password: "pw"}).(*ShadowsocksParser)
			So(isShadowsocks, ShouldBeTrue)
		})

		Convey("An unknown proto should panic", func() {
			So(func() { NewParser("unknown", nil) }, ShouldPanic)
		})
	})
}
