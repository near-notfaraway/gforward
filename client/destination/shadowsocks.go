package destination

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/panjf2000/gnet/v2"
	"github.com/txthinking/socks5"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// shadowsocks AEAD 单个 payload chunk 的最大明文长度，见 SIP004。
const (
	ssMaxChunkSize      = 0x3FFF
	ssMaxDecodedPayload = 65535
)

// evpBytesToKey 复刻 OpenSSL EVP_BytesToKey（MD5、无 salt、单轮），由密码派生 SS 主密钥。
func evpBytesToKey(password string, keyLen int) []byte {
	var prev, out []byte
	for len(out) < keyLen {
		h := md5.New()
		h.Write(prev)
		h.Write([]byte(password))
		prev = h.Sum(nil)
		out = append(out, prev...)
	}
	return out[:keyLen]
}

// increment 将 nonce 视为小端计数器自增 1，用于逐 chunk 推进 AEAD nonce。
func increment(b []byte) {
	for i := range b {
		b[i]++
		if b[i] != 0 {
			return
		}
	}
}

// ssAEAD 承载 shadowsocks 的加密方式与主密钥，并按连接 salt 派生会话子密钥构造 AEAD。
type ssAEAD struct {
	method    string // AEAD 加密方式，如 aes-256-gcm、chacha20-ietf-poly1305
	masterKey []byte // 由密码经 EVP_BytesToKey 派生的主密钥
	keyLen    int    // 子密钥长度（字节）
	saltLen   int    // 每连接 salt 长度，SIP004 中等于 keyLen
}

// newSSAEAD 校验加密方式并由密码派生主密钥；仅支持 SIP004 AEAD 的两种主流方式。
func newSSAEAD(method, password string) (*ssAEAD, error) {
	var keyLen int
	switch method {
	case "aes-256-gcm", "chacha20-ietf-poly1305":
		keyLen = 32
	default:
		return nil, fmt.Errorf("unsupported shadowsocks method %q", method)
	}
	return &ssAEAD{
		method:    method,
		masterKey: evpBytesToKey(password, keyLen),
		keyLen:    keyLen,
		saltLen:   keyLen,
	}, nil
}

// newAEAD 依据连接 salt 用 HKDF-SHA1 派生会话子密钥，并构造对应方式的 AEAD 实例。
func (c *ssAEAD) newAEAD(salt []byte) (cipher.AEAD, error) {
	subkey := make([]byte, c.keyLen)
	r := hkdf.New(sha1.New, c.masterKey, salt, []byte("ss-subkey"))
	if _, err := io.ReadFull(r, subkey); err != nil {
		return nil, fmt.Errorf("derive ss subkey failed: %w", err)
	}
	switch c.method {
	case "aes-256-gcm":
		block, err := aes.NewCipher(subkey)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case "chacha20-ietf-poly1305":
		return chacha20poly1305.New(subkey)
	default:
		return nil, fmt.Errorf("unsupported shadowsocks method %q", c.method)
	}
}

// ssDecrypter 维护一条连接的上行解密状态：首个 salt、会话 AEAD 与逐 chunk 推进的 nonce。
type ssDecrypter struct {
	cipher *ssAEAD     // 加密方式与主密钥来源
	aead   cipher.AEAD // 读到 salt 后构造的会话 AEAD，nil 表示尚未消费 salt
	nonce  []byte      // 当前读方向 nonce，随每个 chunk 自增
}

func newSSDecrypter(c *ssAEAD) *ssDecrypter {
	return &ssDecrypter{cipher: c}
}

// decrypt 从 src 尽可能多地解出完整 chunk，返回拼接后的明文与已消费的 src 字节数。
// 首次调用先消费 salt 并构造 AEAD；length chunk 与 payload chunk 必须成对完整才推进 nonce，
// 否则保留半包等待下次调用，保证 nonce 与 chunk 边界严格对齐。
func (d *ssDecrypter) decrypt(src []byte, maxPlaintext int) ([]byte, int, error) {
	consumed := 0
	if d.aead == nil {
		if len(src) < d.cipher.saltLen {
			return nil, 0, nil
		}
		aead, err := d.cipher.newAEAD(src[:d.cipher.saltLen])
		if err != nil {
			return nil, 0, err
		}
		d.aead = aead
		d.nonce = make([]byte, aead.NonceSize())
		consumed = d.cipher.saltLen
	}

	tag := d.aead.Overhead()
	rem := src[consumed:]
	var out []byte
	for {
		// length chunk 用当前 nonce 试解，但在确认 payload chunk 完整前不推进状态
		if len(rem) < 2+tag {
			break
		}
		lenNonce := append([]byte(nil), d.nonce...)
		lenPlain, err := d.aead.Open(nil, lenNonce, rem[:2+tag], nil)
		if err != nil {
			return nil, 0, fmt.Errorf("decrypt ss length chunk failed: %w", err)
		}
		payLen := int(lenPlain[0])<<8 | int(lenPlain[1])
		if payLen == 0 || payLen > ssMaxChunkSize {
			return nil, 0, fmt.Errorf("invalid ss chunk length %d", payLen)
		}
		need := 2 + tag + payLen + tag
		if len(rem) < need {
			break
		}
		if len(out) > 0 && len(out)+payLen > maxPlaintext {
			break
		}
		payNonce := append([]byte(nil), d.nonce...)
		increment(payNonce)
		payPlain, err := d.aead.Open(nil, payNonce, rem[2+tag:need], nil)
		if err != nil {
			return nil, 0, fmt.Errorf("decrypt ss payload chunk failed: %w", err)
		}
		increment(d.nonce)
		increment(d.nonce)
		out = append(out, payPlain...)
		consumed += need
		rem = rem[need:]
	}
	return out, consumed, nil
}

// ssEncrypter 维护一条连接的下行加密状态：随首个 chunk 生成并前置的 salt、会话 AEAD 与逐 chunk 推进的 nonce。
type ssEncrypter struct {
	cipher *ssAEAD     // 加密方式与主密钥来源
	aead   cipher.AEAD // 生成 salt 后构造的会话 AEAD，nil 表示尚未发送 salt
	nonce  []byte      // 当前写方向 nonce，随每个 chunk 自增
}

func newSSEncrypter(c *ssAEAD) *ssEncrypter {
	return &ssEncrypter{cipher: c}
}

// encrypt 将 plaintext 编码为 SS AEAD 密文：首次调用生成随机 salt 并前置，
// 超过单 chunk 上限的明文拆分为多个 length+payload chunk。
func (e *ssEncrypter) encrypt(plaintext []byte) ([]byte, error) {
	var out []byte
	if e.aead == nil {
		salt := make([]byte, e.cipher.saltLen)
		if _, err := rand.Read(salt); err != nil {
			return nil, fmt.Errorf("generate ss salt failed: %w", err)
		}
		aead, err := e.cipher.newAEAD(salt)
		if err != nil {
			return nil, err
		}
		e.aead = aead
		e.nonce = make([]byte, aead.NonceSize())
		out = append(out, salt...)
	}

	for len(plaintext) > 0 {
		n := len(plaintext)
		if n > ssMaxChunkSize {
			n = ssMaxChunkSize
		}
		lenBuf := []byte{byte(n >> 8), byte(n)}
		out = e.aead.Seal(out, e.nonce, lenBuf, nil)
		increment(e.nonce)
		out = e.aead.Seal(out, e.nonce, plaintext[:n], nil)
		increment(e.nonce)
		plaintext = plaintext[n:]
	}
	return out, nil
}

// parseShadowsocksAddress 从明文前缀解析 SOCKS5 风格的目标地址头，返回 host、port 及头部字节数。
// ok 为 false 且 err 为 nil 表示明文不足以容纳完整地址头，调用方应等待更多数据。
func parseShadowsocksAddress(b []byte) (host string, port, headerLen int, ok bool, err error) {
	if len(b) < 1 {
		return "", 0, 0, false, nil
	}
	switch b[0] {
	case socks5.ATYPIPv4:
		if len(b) < 1+net.IPv4len+2 {
			return "", 0, 0, false, nil
		}
		host = net.IP(b[1 : 1+net.IPv4len]).String()
		portOff := 1 + net.IPv4len
		port = int(b[portOff])<<8 | int(b[portOff+1])
		return host, port, portOff + 2, true, nil
	case socks5.ATYPDomain:
		if len(b) < 2 {
			return "", 0, 0, false, nil
		}
		domainLen := int(b[1])
		if domainLen == 0 {
			return "", 0, 0, false, fmt.Errorf("shadowsocks address has empty domain")
		}
		need := 2 + domainLen + 2
		if len(b) < need {
			return "", 0, 0, false, nil
		}
		host = string(b[2 : 2+domainLen])
		port = int(b[2+domainLen])<<8 | int(b[2+domainLen+1])
		return host, port, need, true, nil
	case socks5.ATYPIPv6:
		if len(b) < 1+net.IPv6len+2 {
			return "", 0, 0, false, nil
		}
		host = net.IP(b[1 : 1+net.IPv6len]).String()
		portOff := 1 + net.IPv6len
		port = int(b[portOff])<<8 | int(b[portOff+1])
		return host, port, portOff + 2, true, nil
	default:
		return "", 0, 0, false, fmt.Errorf("invalid shadowsocks address type: %d", b[0])
	}
}

// shadowsocksConn 维护一条连接双向独立的编解码状态，以及已解析出的目标地址。
type shadowsocksConn struct {
	dec       *ssDecrypter // 该连接的上行解密器
	enc       *ssEncrypter // 该连接的下行加密器
	plaintext []byte       // 已解密但尚未被上层消费的明文（地址头解析后仅剩待转发负载）
	addrDone  bool         // 是否已解析出目标地址
	dest      string       // 解析出的目标 host:port
}

// ShadowsocksParser 从 SS AEAD 上行流中解密并解析目标地址，按连接维护解密状态。
type ShadowsocksParser struct {
	cipher *ssAEAD  // 由配置构造的加密方式与主密钥
	conns  sync.Map // 维护每条连接的双向编解码状态：gnet.Conn -> *shadowsocksConn
}

// NewShadowsocksParser 依据解析配置构造 SS 解析器；配置缺失或方式非法属编程错误，直接 panic。
func NewShadowsocksParser(cfg *ParseConfig) *ShadowsocksParser {
	if cfg == nil {
		panic("shadowsocks parser requires a parse config")
	}
	c, err := newSSAEAD(cfg.Method, cfg.Password)
	if err != nil {
		panic(fmt.Sprintf("build shadowsocks cipher failed: %s", err))
	}
	return &ShadowsocksParser{cipher: c}
}

func (p *ShadowsocksParser) getConn(conn gnet.Conn) *shadowsocksConn {
	if v, ok := p.conns.Load(conn); ok {
		return v.(*shadowsocksConn)
	}
	sc := &shadowsocksConn{
		dec: newSSDecrypter(p.cipher),
		enc: newSSEncrypter(p.cipher),
	}
	p.conns.Store(conn, sc)
	return sc
}

func (p *ShadowsocksParser) reject(conn gnet.Conn, err error) (ParseResult, error) {
	p.conns.Delete(conn)
	return ParseResult{Status: ParseRejected}, err
}

func (p *ShadowsocksParser) Clear(conn gnet.Conn) {
	p.conns.Delete(conn)
}

// EncodePayload 使用连接独立的回复方向 salt、子密钥和 nonce 将服务端明文编码为 SS AEAD 流。
func (p *ShadowsocksParser) EncodePayload(conn gnet.Conn, payload []byte) ([]byte, error) {
	value, ok := p.conns.Load(conn)
	if !ok {
		return nil, fmt.Errorf("shadowsocks connection state not found")
	}
	return value.(*shadowsocksConn).enc.encrypt(payload)
}

// decodePayload 解密连接缓冲区中的完整 SS chunk，并仅消费已经完整解密的密文字节。
func (p *ShadowsocksParser) decodePayload(conn gnet.Conn, sc *shadowsocksConn) ([]byte, ParseStatus, error) {
	buf, err := conn.Peek(-1)
	if err != nil {
		p.conns.Delete(conn)
		return nil, ParseRejected, fmt.Errorf("read shadowsocks stream failed: %w", err)
	}
	plain, consumed, err := sc.dec.decrypt(buf, ssMaxDecodedPayload)
	if err != nil {
		p.conns.Delete(conn)
		return nil, ParseRejected, err
	}
	if consumed > 0 {
		if _, err = conn.Discard(consumed); err != nil {
			p.conns.Delete(conn)
			return nil, ParseRejected, err
		}
	}
	if len(plain) == 0 {
		return nil, ParseNeedMoreData, nil
	}
	return plain, ParseDone, nil
}

// Parse 解密连接上已缓冲的 SS 密文并解析目标地址。首个完整地址头就绪时返回目标，
// 并通过 ParseResult.Payload 将地址头之后的明文交给 forwarder 转发。
func (p *ShadowsocksParser) Parse(conn gnet.Conn) (ParseResult, error) {
	sc := p.getConn(conn)

	// 目标已解析后直接解密后续 chunk；PerRequest 使 forwarder 每次流量都回到此入口。
	if sc.addrDone {
		payload, status, err := p.decodePayload(conn, sc)
		return ParseResult{
			Status:      status,
			Destination: sc.dest,
			PerRequest:  true,
			Payload:     payload,
		}, err
	}

	plain, status, err := p.decodePayload(conn, sc)
	if status != ParseDone {
		return ParseResult{Status: status}, err
	}
	sc.plaintext = append(sc.plaintext, plain...)

	host, port, headerLen, ok, err := parseShadowsocksAddress(sc.plaintext)
	if err != nil {
		return p.reject(conn, err)
	}
	if !ok {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}
	sc.plaintext = sc.plaintext[headerLen:]
	sc.dest = net.JoinHostPort(host, strconv.Itoa(port))
	sc.addrDone = true
	out := append([]byte{}, sc.plaintext...)
	sc.plaintext = nil
	return ParseResult{Status: ParseDone, Destination: sc.dest, PerRequest: true, Payload: out}, nil
}
