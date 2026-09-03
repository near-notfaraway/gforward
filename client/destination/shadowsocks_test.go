package destination

import (
	"crypto/rand"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

// ssATYPDomain 为 SOCKS5 域名地址类型常量，测试构造地址头时使用。
const ssATYPDomain = 0x03

// buildSSStream 将「域名地址头 + 负载」编码为 SS AEAD 上行密文（含随机 salt），供解析测试使用。
func buildSSStream(t *testing.T, method, password, domain string, port int, payload []byte) []byte {
	t.Helper()
	c, err := newSSAEAD(method, password)
	So(err, ShouldBeNil)

	header := []byte{ssATYPDomain, byte(len(domain))}
	header = append(header, []byte(domain)...)
	header = append(header, byte(port>>8), byte(port))

	enc := newSSEncrypter(c)
	stream, err := enc.encrypt(append(header, payload...))
	So(err, ShouldBeNil)
	return stream
}

// ssStubConn 内嵌 gnet.Conn，仅以内存缓冲覆盖解析器用到的 Peek/Discard。
type ssStubConn struct {
	gnet.Conn

	buf     []byte // 尚未消费的入站缓冲
	peekErr error  // 模拟 Peek 失败
}

func (c *ssStubConn) Peek(n int) ([]byte, error) {
	if c.peekErr != nil {
		return nil, c.peekErr
	}
	if n < 0 || n > len(c.buf) {
		return c.buf, nil
	}
	return c.buf[:n], nil
}

func (c *ssStubConn) Discard(n int) (int, error) {
	if n > len(c.buf) {
		n = len(c.buf)
	}
	c.buf = c.buf[n:]
	return n, nil
}

func TestShadowsocksRoundTrip(t *testing.T) {
	PatchConvey("Encrypt then decrypt should recover plaintext for both AEAD methods", t, func() {
		for _, method := range []string{"aes-256-gcm", "chacha20-ietf-poly1305"} {
			c, err := newSSAEAD(method, "secret-pass")
			So(err, ShouldBeNil)

			enc := newSSEncrypter(c)
			dec := newSSDecrypter(c)

			// 跨越单 chunk 上限，验证多 chunk 拆分与拼接
			plaintext := make([]byte, ssMaxChunkSize+1234)
			_, err = rand.Read(plaintext)
			So(err, ShouldBeNil)

			cipherText, err := enc.encrypt(plaintext)
			So(err, ShouldBeNil)

			got, consumed, err := dec.decrypt(cipherText, ssMaxDecodedPayload)
			So(err, ShouldBeNil)
			So(consumed, ShouldEqual, len(cipherText))
			So(got, ShouldResemble, plaintext)
		}
	})
}

func TestShadowsocksDecryptHandlesPartialData(t *testing.T) {
	PatchConvey("Decrypt should tolerate incomplete salt and chunk boundaries", t, func() {
		c, err := newSSAEAD("aes-256-gcm", "secret-pass")
		So(err, ShouldBeNil)

		plaintext := []byte("hello shadowsocks payload split across feeds")
		cipherText, err := newSSEncrypter(c).encrypt(plaintext)
		So(err, ShouldBeNil)

		PatchConvey("Partial salt yields nothing consumed", func() {
			got, consumed, err := newSSDecrypter(c).decrypt(cipherText[:c.saltLen-1], ssMaxDecodedPayload)
			So(err, ShouldBeNil)
			So(consumed, ShouldEqual, 0)
			So(got, ShouldBeNil)
		})

		PatchConvey("Feeding one byte at a time still recovers the full plaintext", func() {
			dec := newSSDecrypter(c)
			var pending, recovered []byte
			for _, b := range cipherText {
				pending = append(pending, b)
				got, consumed, err := dec.decrypt(pending, ssMaxDecodedPayload)
				So(err, ShouldBeNil)
				recovered = append(recovered, got...)
				pending = pending[consumed:]
			}
			So(recovered, ShouldResemble, plaintext)
		})
	})
}

func TestShadowsocksParserParsesDestination(t *testing.T) {
	PatchConvey("Parse should decrypt the address header and expose the destination", t, func() {
		stream := buildSSStream(t, "chacha20-ietf-poly1305", "pw", "example.com", 443, []byte("GET / HTTP/1.1\r\n"))
		parser := NewShadowsocksParser(&ParseConfig{Method: "chacha20-ietf-poly1305", Password: "pw"})
		conn := &ssStubConn{buf: stream}

		result, err := parser.Parse(conn)

		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseDone)
		So(result.Destination, ShouldEqual, "example.com:443")
		So(result.PerRequest, ShouldBeTrue)
		So(string(result.Payload), ShouldEqual, "GET / HTTP/1.1\r\n")
	})
}

func TestShadowsocksParserNeedMoreData(t *testing.T) {
	PatchConvey("Parse should wait when the address header is incomplete", t, func() {
		stream := buildSSStream(t, "aes-256-gcm", "pw", "example.com", 80, []byte("x"))
		parser := NewShadowsocksParser(&ParseConfig{Method: "aes-256-gcm", Password: "pw"})

		// 只喂入不足以解出任何 chunk 的前缀（缺少末尾若干字节）
		conn := &ssStubConn{buf: stream[:len(stream)-5]}

		result, err := parser.Parse(conn)

		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseNeedMoreData)
	})
}

func TestShadowsocksParserWaitsForCompletePayloadAfterAddress(t *testing.T) {
	PatchConvey("Parse should not emit an empty payload for an incomplete chunk after the address", t, func() {
		c, err := newSSAEAD("aes-256-gcm", "pw")
		So(err, ShouldBeNil)
		enc := newSSEncrypter(c)

		address := []byte{ssATYPDomain, byte(len("example.com"))}
		address = append(address, []byte("example.com")...)
		address = append(address, 0, 80)
		first, err := enc.encrypt(address)
		So(err, ShouldBeNil)
		second, err := enc.encrypt([]byte("later payload"))
		So(err, ShouldBeNil)

		parser := NewShadowsocksParser(&ParseConfig{Method: "aes-256-gcm", Password: "pw"})
		conn := &ssStubConn{buf: first}
		result, err := parser.Parse(conn)
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseDone)
		So(result.Payload, ShouldNotBeNil)
		So(result.Payload, ShouldBeEmpty)

		conn.buf = append(conn.buf, second[:len(second)-1]...)
		result, err = parser.Parse(conn)
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseNeedMoreData)
		So(result.Payload, ShouldBeNil)

		conn.buf = append(conn.buf, second[len(second)-1])
		result, err = parser.Parse(conn)
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseDone)
		So(result.PerRequest, ShouldBeTrue)
		So(string(result.Payload), ShouldEqual, "later payload")
	})
}

func TestShadowsocksParserRejectsBadCipherText(t *testing.T) {
	PatchConvey("Parse should reject a stream that fails authentication", t, func() {
		stream := buildSSStream(t, "aes-256-gcm", "pw", "example.com", 80, []byte("payload"))
		// 篡改 length chunk 首字节（salt 长度为 32），触发 AEAD 校验失败
		tampered := append([]byte(nil), stream...)
		tampered[32] ^= 0xFF
		parser := NewShadowsocksParser(&ParseConfig{Method: "aes-256-gcm", Password: "pw"})
		conn := &ssStubConn{buf: tampered}

		result, err := parser.Parse(conn)

		So(err, ShouldNotBeNil)
		So(result.Status, ShouldEqual, ParseRejected)
	})
}

func TestShadowsocksParserWrongPassword(t *testing.T) {
	PatchConvey("Parse should reject a stream encrypted with a different password", t, func() {
		stream := buildSSStream(t, "aes-256-gcm", "right-pw", "example.com", 80, []byte("payload"))
		parser := NewShadowsocksParser(&ParseConfig{Method: "aes-256-gcm", Password: "wrong-pw"})
		conn := &ssStubConn{buf: stream}

		result, err := parser.Parse(conn)

		So(err, ShouldNotBeNil)
		So(result.Status, ShouldEqual, ParseRejected)
	})
}

func TestNewShadowsocksParserRequiresConfig(t *testing.T) {
	PatchConvey("NewShadowsocksParser should panic without a config or with a bad method", t, func() {
		So(func() { NewShadowsocksParser(nil) }, ShouldPanic)
		So(func() { NewShadowsocksParser(&ParseConfig{Method: "rc4", Password: "x"}) }, ShouldPanic)
	})
}
