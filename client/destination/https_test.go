package destination

import (
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// buildClientHello 构造一个携带指定 SNI 的最小 TLS ClientHello 记录，供 SNI 解析测试。
func buildClientHello(host string) []byte {
	name := []byte(host)

	// server name entry: type(1)=host + name_len(2) + name
	entry := []byte{0x00}
	entry = binary.BigEndian.AppendUint16(entry, uint16(len(name)))
	entry = append(entry, name...)

	// server name list: list_len(2) + entry
	list := binary.BigEndian.AppendUint16(nil, uint16(len(entry)))
	list = append(list, entry...)

	// extension: type(2)=server_name + ext_len(2) + list
	ext := binary.BigEndian.AppendUint16(nil, tlsExtensionTypeServerName)
	ext = binary.BigEndian.AppendUint16(ext, uint16(len(list)))
	ext = append(ext, list...)

	// handshake body (ClientHello)
	hs := []byte{tlsMainVersionV3, 0x03}    // client version
	hs = append(hs, make([]byte, 32)...)    // random
	hs = append(hs, 0x00)                   // session id len = 0
	hs = append(hs, 0x00, 0x02, 0x00, 0x00) // cipher suite list len(2) + one suite(2)
	hs = append(hs, 0x01, 0x00)             // compression method len(1) + one method(1)
	hs = binary.BigEndian.AppendUint16(hs, uint16(len(ext)))
	hs = append(hs, ext...)

	// record + handshake header
	buf := []byte{tlsRecordTypeHandshake, tlsMainVersionV3, 0x03}
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(hs)+4))
	buf = append(buf, tlsHandshakeTypeClientHello)
	buf = append(buf, byte(len(hs)>>16), byte(len(hs)>>8), byte(len(hs)))
	buf = append(buf, hs...)
	return buf
}

func TestHTTPSParserParse(t *testing.T) {
	Convey("HTTPSParser.Parse should extract the SNI as the destination", t, func() {
		parser := NewHTTPSParser()

		Convey("A ClientHello with SNI should return host:443", func() {
			conn := &stubConn{buf: buildClientHello("example.com")}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseDone)
			So(result.Destination, ShouldEqual, "example.com:443")
		})

		Convey("An empty buffer should need more data", func() {
			conn := &stubConn{buf: nil}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})

		Convey("A non-handshake first byte should be rejected", func() {
			conn := &stubConn{buf: []byte{0x00, 0x03, 0x03, 0x00, 0x00}}

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})

		Convey("A truncated record header should need more data", func() {
			conn := &stubConn{buf: []byte{tlsRecordTypeHandshake, tlsMainVersionV3}}

			result, err := parser.Parse(conn)

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})

		Convey("A wrong TLS major version should be rejected", func() {
			conn := &stubConn{buf: []byte{tlsRecordTypeHandshake, 0x02, 0x03, 0x00, 0x05}}

			result, err := parser.Parse(conn)

			So(err, ShouldNotBeNil)
			So(result.Status, ShouldEqual, ParseRejected)
		})

		Convey("A truncated handshake body should need more data", func() {
			full := buildClientHello("example.com")

			result, err := parser.Parse(&stubConn{buf: full[:len(full)-5]})

			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, ParseNeedMoreData)
		})
	})
}

func TestExtractDestWithPort(t *testing.T) {
	Convey("extractDestWithPort should append the default port only when missing", t, func() {
		Convey("A bare host should gain the default port", func() {
			So(extractDestWithPort("example.com", 443), ShouldEqual, "example.com:443")
		})

		Convey("A host with an explicit port should be preserved", func() {
			So(extractDestWithPort("example.com:8443", 443), ShouldEqual, "example.com:8443")
		})

		Convey("A bracketed IPv6 without a port should gain the default port", func() {
			So(extractDestWithPort("[2001:db8::1]", 443), ShouldEqual, "[2001:db8::1]:443")
		})

		Convey("A bracketed IPv6 with a port should be preserved", func() {
			So(extractDestWithPort("[2001:db8::1]:8443", 443), ShouldEqual, "[2001:db8::1]:8443")
		})
	})
}
