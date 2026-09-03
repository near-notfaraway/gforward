package client

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/near-notfaraway/gforward/client/destination"
	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
	"golang.org/x/crypto/hkdf"
)

type trackingConn struct {
	gnet.Conn // 提供测试无需调用的其余连接方法

	closeCount      atomic.Int32 // 记录连接被关闭的次数
	writeCount      atomic.Int32 // 记录同步写入调用次数
	asyncWriteCount atomic.Int32 // 记录异步写入调用次数
	written         []byte       // 记录最后一次写入的数据
	writes          [][]byte     // 按顺序记录每次异步写入的数据

	buf []byte // OnTraffic 测试用的入站缓冲
}

func (c *trackingConn) Close() error {
	c.closeCount.Add(1)
	return nil
}

func (c *trackingConn) Write(buf []byte) (int, error) {
	c.writeCount.Add(1)
	c.written = append([]byte(nil), buf...)
	return len(buf), nil
}

func (c *trackingConn) AsyncWrite(buf []byte, callback gnet.AsyncCallback) error {
	c.asyncWriteCount.Add(1)
	c.written = append([]byte(nil), buf...)
	c.writes = append(c.writes, append([]byte(nil), buf...))
	if callback != nil {
		_ = callback(c, nil)
	}
	return nil
}

func (c *trackingConn) Peek(n int) ([]byte, error) {
	if n <= 0 || n > len(c.buf) {
		return c.buf, nil
	}
	return c.buf[:n], nil
}

func (c *trackingConn) Discard(n int) (int, error) {
	if n > len(c.buf) {
		n = len(c.buf)
	}
	c.buf = c.buf[n:]
	return n, nil
}

func (c *trackingConn) InboundBuffered() int { return len(c.buf) }

func (c *trackingConn) LocalAddr() net.Addr {
	return nil
}

func (c *trackingConn) RemoteAddr() net.Addr {
	return nil
}

// fakeParser 以固定结果实现 destination.Parser，供 OnTraffic 测试脱离真实协议解析。
type fakeParser struct {
	result destination.ParseResult
	err    error
}

func (p *fakeParser) Parse(_ gnet.Conn) (destination.ParseResult, error) {
	return p.result, p.err
}

func newTestSSAEAD(password string, salt []byte) (cipher.AEAD, error) {
	var masterKey []byte
	var prev []byte
	for len(masterKey) < 32 {
		h := md5.New()
		_, _ = h.Write(prev)
		_, _ = h.Write([]byte(password))
		prev = h.Sum(nil)
		masterKey = append(masterKey, prev...)
	}
	subkey := make([]byte, 32)
	_, err := io.ReadFull(hkdf.New(sha1.New, masterKey[:32], salt, []byte("ss-subkey")), subkey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(subkey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func incrementTestSSNonce(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

func buildSSRequest(t *testing.T, domain string, port int, payload []byte) []byte {
	t.Helper()

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	aead, err := newTestSSAEAD("secret", salt)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte{0x03, byte(len(domain))}
	plaintext = append(plaintext, domain...)
	plaintext = append(plaintext, byte(port>>8), byte(port))
	plaintext = append(plaintext, payload...)

	nonce := make([]byte, aead.NonceSize())
	length := []byte{byte(len(plaintext) >> 8), byte(len(plaintext))}
	out := append([]byte(nil), salt...)
	out = aead.Seal(out, nonce, length, nil)
	incrementTestSSNonce(nonce)
	out = aead.Seal(out, nonce, plaintext, nil)
	return out
}

func buildSSPayload(t *testing.T, payload []byte, nonceValue byte) []byte {
	t.Helper()

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	aead, err := newTestSSAEAD("secret", salt)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	nonce[0] = nonceValue
	length := []byte{byte(len(payload) >> 8), byte(len(payload))}
	out := aead.Seal(nil, nonce, length, nil)
	incrementTestSSNonce(nonce)
	return aead.Seal(out, nonce, payload, nil)
}

func decodeSSResponse(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 32 {
		return nil, fmt.Errorf("missing shadowsocks response salt")
	}
	aead, err := newTestSSAEAD("secret", ciphertext[:32])
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	pos := 32
	var plaintext []byte
	for pos < len(ciphertext) {
		lengthEnd := pos + 2 + aead.Overhead()
		if lengthEnd > len(ciphertext) {
			return nil, fmt.Errorf("truncated shadowsocks length chunk")
		}
		length, err := aead.Open(nil, nonce, ciphertext[pos:lengthEnd], nil)
		if err != nil {
			return nil, err
		}
		incrementTestSSNonce(nonce)
		payloadLen := int(length[0])<<8 | int(length[1])
		payloadEnd := lengthEnd + payloadLen + aead.Overhead()
		if payloadEnd > len(ciphertext) {
			return nil, fmt.Errorf("truncated shadowsocks payload chunk")
		}
		payload, err := aead.Open(nil, nonce, ciphertext[lengthEnd:payloadEnd], nil)
		if err != nil {
			return nil, err
		}
		incrementTestSSNonce(nonce)
		plaintext = append(plaintext, payload...)
		pos = payloadEnd
	}
	return plaintext, nil
}

func decodeForwardPacket(t *testing.T, buf []byte) *protocol.ForwardPacket {
	t.Helper()
	pkt := &protocol.ForwardPacket{}
	_, state, err := pkt.Unmarshal(buf)
	if err != nil {
		t.Fatal(err)
	}
	if state != protocol.ParseDone {
		t.Fatalf("unexpected packet parse state: %d", state)
	}
	return pkt
}

func TestNewForwarder(t *testing.T) {
	PatchConvey("NewForwarder should pick the parser proto from the mode", t, func() {
		PatchConvey("A _dns mode should strip the suffix for the parser proto", func() {
			var gotProto destination.ParserProto
			Mock(destination.NewParser).To(func(proto destination.ParserProto, _ *destination.ParseConfig) destination.Parser {
				gotProto = proto
				return &fakeParser{}
			}).Build()

			f := NewForwarder("https_dns", "127.0.0.1:9989", nil)

			So(gotProto, ShouldEqual, destination.ParserProto("https"))
			So(f.serverAddr, ShouldEqual, "127.0.0.1:9989")
			So(f.sessions, ShouldNotBeNil)
			So(f.uploadLogger, ShouldNotBeNil)
			So(f.downloadLogger, ShouldNotBeNil)
		})

		PatchConvey("A plain mode should be used as the parser proto verbatim", func() {
			var gotProto destination.ParserProto
			Mock(destination.NewParser).To(func(proto destination.ParserProto, _ *destination.ParseConfig) destination.Parser {
				gotProto = proto
				return &fakeParser{}
			}).Build()

			NewForwarder("socks5", "127.0.0.1:9989", nil)

			So(gotProto, ShouldEqual, destination.ParserProto("socks5"))
		})
	})
}

func TestForwarderOnTraffic(t *testing.T) {
	PatchConvey("OnTraffic should parse, frame and route user traffic", t, func() {
		logger := logrus.New().WithField("test", "client")

		PatchConvey("Need-more-data from the parser should keep the connection open", func() {
			conn := &trackingConn{buf: []byte("partial")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseNeedMoreData}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(f.sessions.byUser), ShouldEqual, 0)
		})

		PatchConvey("A rejected parse should close the connection", func() {
			conn := &trackingConn{buf: []byte("bad")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseRejected}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
		})

		PatchConvey("A parser error should close the connection", func() {
			conn := &trackingConn{buf: []byte("data")}
			f := &forwarder{
				destinationParser: &fakeParser{err: errors.New("parse failed")},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.Close)
		})

		PatchConvey("A first packet should register a session and start an async dial", func() {
			var dialedAddr string
			var dialedToken *dialToken
			Mock((*dialer.Dialer).AsyncDial).To(func(_ *dialer.Dialer, _, address string, token any) {
				dialedAddr = address
				dialedToken = token.(*dialToken)
			}).Build()

			conn := &trackingConn{buf: []byte("hello")}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseDone, Destination: "example.com:80"}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				dialer:            &dialer.Dialer{},
				serverAddr:        "127.0.0.1:9989",
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(dialedAddr, ShouldEqual, "127.0.0.1:9989")
			So(dialedToken, ShouldNotBeNil)
			So(dialedToken.userConn, ShouldEqual, conn)
			So(f.sessions.byUser[conn], ShouldNotBeNil)
			So(f.sessions.byUser[conn].dialing, ShouldBeTrue)
		})

		PatchConvey("A ready session should forward the packet to the server conn", func() {
			conn := &trackingConn{buf: []byte("hello")}
			serverConn := &trackingConn{}
			f := &forwarder{
				internalProtocol: &protocol.ForwardPacket{},
				sessions:         newSessionTable(),
				uploadLogger:     logger,
			}
			f.sessions.byUser[conn] = &session{dest: "example.com:80", serverConn: serverConn}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(serverConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		})

		PatchConvey("A Shadowsocks parser should reuse its parsed destination for subsequent traffic", func() {
			plaintext := []byte("hello")
			conn := &trackingConn{buf: buildSSRequest(t, "example.com", 443, nil)}
			serverConn := &trackingConn{}
			parser := destination.NewShadowsocksParser(&destination.ParseConfig{
				Method:   "aes-256-gcm",
				Password: "secret",
			})
			result, err := parser.Parse(conn)
			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, destination.ParseDone)
			conn.buf = buildSSPayload(t, plaintext, 2)

			f := &forwarder{
				destinationParser: parser,
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				uploadLogger:      logger,
			}
			f.sessions.byUser[conn] = &session{serverConn: serverConn}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(serverConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
			pkt := decodeForwardPacket(t, serverConn.written)
			So(pkt.GetDestination(), ShouldEqual, "example.com:443")
			So(pkt.GetPayload(), ShouldResemble, plaintext)
		})

		PatchConvey("A Shadowsocks address-only request should cache the destination and start dialing", func() {
			var dialedToken *dialToken
			Mock((*dialer.Dialer).AsyncDial).To(func(_ *dialer.Dialer, _, _ string, token any) {
				dialedToken = token.(*dialToken)
			}).Build()

			conn := &trackingConn{buf: buildSSRequest(t, "example.com", 443, nil)}
			f := &forwarder{
				destinationParser: destination.NewShadowsocksParser(&destination.ParseConfig{
					Method:   "aes-256-gcm",
					Password: "secret",
				}),
				internalProtocol: &protocol.ForwardPacket{},
				sessions:         newSessionTable(),
				dialer:           &dialer.Dialer{},
				serverAddr:       "127.0.0.1:9989",
				uploadLogger:     logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(dialedToken, ShouldNotBeNil)
			So(f.sessions.byUser[conn].dest, ShouldBeEmpty)
			pkt := decodeForwardPacket(t, f.sessions.byUser[conn].pending[0])
			So(pkt.GetDestination(), ShouldEqual, "example.com:443")
			So(pkt.GetPayload(), ShouldBeEmpty)
		})

		PatchConvey("A dialing session should queue the packet without writing", func() {
			conn := &trackingConn{buf: []byte("hello")}
			f := &forwarder{
				internalProtocol: &protocol.ForwardPacket{},
				sessions:         newSessionTable(),
				uploadLogger:     logger,
			}
			f.sessions.byUser[conn] = &session{dest: "example.com:80", dialing: true, pending: [][]byte{[]byte("first")}}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(len(f.sessions.byUser[conn].pending), ShouldEqual, 2)
		})

		PatchConvey("A tunnel setup with a cached dest but empty payload should still register a session and dial", func() {
			// 模拟 HTTP CONNECT / SOCKS5 隧道建立：握手字节已被解析器消费，首个 packet 目标非空、负载为空
			var dialedToken *dialToken
			Mock((*dialer.Dialer).AsyncDial).To(func(_ *dialer.Dialer, _, _ string, token any) {
				dialedToken = token.(*dialToken)
			}).Build()

			conn := &trackingConn{buf: nil}
			f := &forwarder{
				destinationParser: &fakeParser{result: destination.ParseResult{Status: destination.ParseDone, Destination: "example.com:443"}},
				internalProtocol:  &protocol.ForwardPacket{},
				sessions:          newSessionTable(),
				dialer:            &dialer.Dialer{},
				serverAddr:        "127.0.0.1:9989",
				uploadLogger:      logger,
			}

			action := f.OnTraffic(conn)

			So(action, ShouldEqual, gnet.None)
			So(dialedToken, ShouldNotBeNil)
			So(f.sessions.byUser[conn], ShouldNotBeNil)
			So(f.sessions.byUser[conn].dest, ShouldEqual, "example.com:443")
			So(f.sessions.byUser[conn].dialing, ShouldBeTrue)
		})
	})
}

// TestUserConnCloseClosesServerConn 验证用户连接关闭时会同步释放服务端连接和双向映射。
func TestUserConnCloseClosesServerConn(t *testing.T) {
	PatchConvey("Test forwarder.OnClose", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &forwarder{
			destinationParser: &fakeParser{},
			sessions:          newSessionTable(),
			uploadLogger:      logrus.New().WithField("test", "client"),
		}
		handler.sessions.byUser[userConn] = &session{serverConn: serverConn}
		handler.sessions.byServer[serverConn] = userConn

		handler.OnClose(userConn, nil)

		_, userRouteExists := handler.sessions.byUser[userConn]
		_, serverRouteExists := handler.sessions.byServer[serverConn]
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(serverConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}

func TestServerPayloadUsesAsyncWrite(t *testing.T) {
	PatchConvey("Server payload should be written asynchronously to user", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		packet := &protocol.PlainPacket{}
		packet.SetPayload([]byte("payload"))
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logrus.New().WithField("test", "client"),
		}
		handler.sessions.byServer[serverConn] = userConn

		handler.handleServerMsg(&message.RecvMsg{
			Conn:   serverConn,
			Pkts:   []protocol.InternalPacket{packet},
			Logger: logrus.New().WithField("test", "dialer"),
		})

		So(userConn.writeCount.Load(), ShouldEqual, int32(0))
		So(userConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		So(userConn.written, ShouldResemble, packet.GetPayload())
	})
}

func TestServerPayloadIsEncryptedForShadowsocks(t *testing.T) {
	PatchConvey("Server payload should use one continuous Shadowsocks response stream", t, func() {
		userConn := &trackingConn{buf: buildSSRequest(t, "example.com", 443, nil)}
		serverConn := &trackingConn{}
		parser := destination.NewShadowsocksParser(&destination.ParseConfig{
			Method:   "aes-256-gcm",
			Password: "secret",
		})
		result, err := parser.Parse(userConn)
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, destination.ParseDone)

		first := &protocol.PlainPacket{}
		first.SetPayload([]byte("hello "))
		second := &protocol.PlainPacket{}
		second.SetPayload([]byte("world"))
		handler := &forwarder{
			destinationParser: parser,
			sessions:          newSessionTable(),
			downloadLogger:    logrus.New().WithField("test", "client"),
		}
		handler.sessions.byServer[serverConn] = userConn

		handler.handleServerMsg(&message.RecvMsg{
			Conn: serverConn,
			Pkts: []protocol.InternalPacket{
				first,
				second,
			},
		})

		So(userConn.asyncWriteCount.Load(), ShouldEqual, int32(2))
		ciphertext := append([]byte(nil), userConn.writes[0]...)
		ciphertext = append(ciphertext, userConn.writes[1]...)
		plaintext, err := decodeSSResponse(ciphertext)
		So(err, ShouldBeNil)
		So(string(plaintext), ShouldEqual, "hello world")
	})
}

func TestOpenEventCompletesDialAndFlushesPending(t *testing.T) {
	PatchConvey("Open event should complete session and flush pending packets", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		sess := &session{
			dialing: true,
			pending: [][]byte{[]byte("pending")},
		}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		handler.sessions.byUser[userConn] = sess

		handler.handleDialOpen(&message.RecvMsg{
			Event:  message.RecvEventOpen,
			Conn:   serverConn,
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		boundUserConn, ok := handler.sessions.byServer[serverConn]
		So(ok, ShouldBeTrue)
		So(boundUserConn, ShouldEqual, userConn)
		So(sess.serverConn, ShouldEqual, serverConn)
		So(sess.dialing, ShouldBeFalse)
		So(sess.pending, ShouldBeNil)
		So(serverConn.asyncWriteCount.Load(), ShouldEqual, int32(1))
		So(serverConn.written, ShouldResemble, []byte("pending"))
	})
}

// TestOpenEventForStaleSessionClosesServerConn 验证会话失效时拨号就绪事件会关闭新连接。
func TestOpenEventForStaleSessionClosesServerConn(t *testing.T) {
	PatchConvey("Open event for a stale session should close the server conn", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		sess := &session{dialing: true}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		// byUser 中没有该会话，视为已失效

		handler.handleDialOpen(&message.RecvMsg{
			Event:  message.RecvEventOpen,
			Conn:   serverConn,
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		So(serverConn.closeCount.Load(), ShouldEqual, int32(1))
		_, exists := handler.sessions.byServer[serverConn]
		So(exists, ShouldBeFalse)
	})
}

func TestDialErrorEventClearsDialingSession(t *testing.T) {
	PatchConvey("Dial error event should clear dialing session", t, func() {
		userConn := &trackingConn{}
		sess := &session{
			dialing: true,
			pending: [][]byte{[]byte("pending")},
		}
		logger := logrus.New().WithField("test", "client")
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logger,
		}
		handler.sessions.byUser[userConn] = sess

		handler.handleDialError(&message.RecvMsg{
			Event:  message.RecvEventDialError,
			Err:    errors.New("dial failed"),
			Token:  &dialToken{userConn: userConn, session: sess, logger: logger},
			Logger: logger,
		})

		_, ok := handler.sessions.byUser[userConn]
		So(ok, ShouldBeFalse)
	})
}

// TestServerConnCloseClosesUserConn 验证服务端连接关闭事件会清理映射并关闭用户连接。
func TestServerConnCloseClosesUserConn(t *testing.T) {
	PatchConvey("Test forwarder.handleDialClose with close event", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		handler := &forwarder{
			sessions:       newSessionTable(),
			downloadLogger: logrus.New().WithField("test", "client"),
		}
		handler.sessions.byUser[userConn] = &session{serverConn: serverConn}
		handler.sessions.byServer[serverConn] = userConn

		handler.handleDialClose(&message.RecvMsg{Event: message.RecvEventClose, Conn: serverConn})

		_, userRouteExists := handler.sessions.byUser[userConn]
		_, serverRouteExists := handler.sessions.byServer[serverConn]
		So(userRouteExists, ShouldBeFalse)
		So(serverRouteExists, ShouldBeFalse)
		So(userConn.closeCount.Load(), ShouldEqual, int32(1))
	})
}
