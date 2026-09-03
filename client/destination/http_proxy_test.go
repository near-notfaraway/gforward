package destination

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHTTPProxyParserConnect(t *testing.T) {
	Convey("HTTPProxyParser.Parse should ACK CONNECT and return host:443", t, func() {
		parser := NewHTTPProxyParser()
		conn := &stubConn{buf: []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")}

		result, err := parser.Parse(conn)

		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, ParseDone)
		So(result.Destination, ShouldEqual, "example.com:443")
		So(result.PerRequest, ShouldBeFalse)
		So(string(conn.written), ShouldContainSubstring, "200 OK")
		// CONNECT 握手字节应被消费，留下的缓冲为空
		So(conn.buf, ShouldBeEmpty)
	})
}

func TestHTTPProxyParserPlainRequest(t *testing.T) {
	Convey("HTTPProxyParser.Parse should treat non-CONNECT as a per-request HTTP proxy request", t, func() {
		parser := NewHTTPProxyParser()

		Convey("A bodyless GET should return host:80 as a per-request destination", func() {
			conn := &stubConn{buf: []byte("GET /path HTTP/1.1\r\nHost: example.com\r\n\r\n")}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseDone)
			So(result.Destination, ShouldEqual, "example.com:80")
			So(result.PerRequest, ShouldBeTrue)
			// 无回写 ACK
			So(conn.written, ShouldBeEmpty)
		})

		Convey("A request truncated mid-headers should need more data", func() {
			conn := &stubConn{buf: []byte("GET /path HTTP/1.1\r\nHost: exam")}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})

		Convey("A Content-Length body spanning two reads should track remaining bytes", func() {
			request := "POST /submit HTTP/1.1\r\nHost: example.com\r\nContent-Length: 10\r\n\r\n"
			// 首次仅到达前 4 个 body 字节
			conn := &stubConn{buf: []byte(request + "0123")}

			first, err := parser.Parse(conn)
			So(err, ShouldBeNil)
			So(first.Status, ShouldEqual, ParseDone)
			So(first.Destination, ShouldEqual, "example.com:80")
			So(first.PerRequest, ShouldBeTrue)

			// 消费掉本次已计入的字节，剩余 6 个 body 字节在下一次读取到达
			consumed := first.PayloadLen
			conn.buf = append([]byte(nil), []byte(request + "0123")[consumed:]...)
			conn.buf = append(conn.buf, []byte("456789")...)

			second, err := parser.Parse(conn)
			So(err, ShouldBeNil)
			So(second.Status, ShouldEqual, ParseDone)
			So(second.Destination, ShouldEqual, "example.com:80")
		})
	})
}

func TestHTTPProxyParserClear(t *testing.T) {
	Convey("HTTPProxyParser.Clear should drop the per-connection request state", t, func() {
		parser := NewHTTPProxyParser()
		request := "POST /submit HTTP/1.1\r\nHost: example.com\r\nContent-Length: 100\r\n\r\n"
		conn := &stubConn{buf: []byte(request)}

		_, err := parser.Parse(conn)
		So(err, ShouldBeNil)
		_, stored := parser.connMapRequestState.Load(conn)
		So(stored, ShouldBeTrue)

		parser.Clear(conn)

		_, stored = parser.connMapRequestState.Load(conn)
		So(stored, ShouldBeFalse)
	})
}

func TestHasChunkedTransferEncoding(t *testing.T) {
	Convey("hasChunkedTransferEncoding should detect a chunked encoding case-insensitively", t, func() {
		So(hasChunkedTransferEncoding([]string{"Chunked"}), ShouldBeTrue)
		So(hasChunkedTransferEncoding([]string{"gzip"}), ShouldBeFalse)
		So(hasChunkedTransferEncoding(nil), ShouldBeFalse)
	})
}

func TestHTTPChunkedBodyStateConsume(t *testing.T) {
	Convey("httpChunkedBodyState.consume should advance through a full chunked body", t, func() {
		Convey("A complete single-chunk body with trailer should report done", func() {
			state := &httpChunkedBodyState{}
			// "5\r\nhello\r\n0\r\n\r\n"
			body := []byte("5\r\nhello\r\n0\r\n\r\n")

			n, done, err := state.consume(body)

			So(err, ShouldBeNil)
			So(done, ShouldBeTrue)
			So(n, ShouldEqual, len(body))
		})

		Convey("An invalid chunk size should error", func() {
			state := &httpChunkedBodyState{}

			_, _, err := state.consume([]byte("zz\r\n"))

			So(err, ShouldNotBeNil)
		})

		Convey("A malformed line ending should error", func() {
			state := &httpChunkedBodyState{}

			_, _, err := state.consume([]byte("5\n"))

			So(err, ShouldNotBeNil)
		})
	})
}
