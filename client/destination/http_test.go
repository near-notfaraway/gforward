package destination

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHTTPParserParse(t *testing.T) {
	Convey("HTTPParser.Parse should extract the destination from the HTTP Host", t, func() {
		parser := NewHTTPParser()

		Convey("A complete request should return its Host", func() {
			conn := &stubConn{buf: []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseDone)
			So(result.Destination, ShouldEqual, "example.com")
		})

		Convey("An incomplete request should need more data", func() {
			conn := &stubConn{buf: []byte("GET / HTTP/1.1\r\nHost: exam")}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})

		Convey("A malformed request line should be rejected", func() {
			conn := &stubConn{buf: []byte("not-a-valid-request-line\r\n\r\n")}

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})
	})
}
