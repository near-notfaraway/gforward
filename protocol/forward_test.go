package protocol

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestForwardPacketUnmarshalState(t *testing.T) {
	Convey("ForwardPacket should report its parse state", t, func() {
		packet := &ForwardPacket{}

		Convey("Incomplete data should need more data", func() {
			n, state, err := packet.Unmarshal(nil)

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseNeedMoreData)
			So(err, ShouldBeNil)
		})

		Convey("A complete frame should be done", func() {
			expected := &ForwardPacket{}
			expected.SetDestination("example.com:80")
			expected.SetPayload([]byte("payload"))
			buf, err := expected.Marshal()
			So(err, ShouldBeNil)
			buf = append(buf, []byte("next frame")...)

			n, state, err := packet.Unmarshal(buf)

			So(err, ShouldBeNil)
			So(state, ShouldEqual, ParseDone)
			So(n, ShouldEqual, len(buf)-len("next frame"))
			So(packet.GetDestination(), ShouldEqual, expected.GetDestination())
			So(packet.GetPayload(), ShouldResemble, expected.GetPayload())
		})

		Convey("An empty address should be rejected", func() {
			n, state, err := packet.Unmarshal([]byte{0, 0, 80, 0, 0})

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseRejected)
			So(err, ShouldNotBeNil)
		})

		Convey("Port zero should be rejected", func() {
			n, state, err := packet.Unmarshal([]byte{1, 'a', 0, 0, 0, 0})

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseRejected)
			So(err, ShouldNotBeNil)
		})

		Convey("A truncated payload should need more data", func() {
			complete := &ForwardPacket{}
			complete.SetDestination("a:80")
			complete.SetPayload([]byte("payload"))
			buf, err := complete.Marshal()
			So(err, ShouldBeNil)

			n, state, err := packet.Unmarshal(buf[:len(buf)-1])

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseNeedMoreData)
			So(err, ShouldBeNil)
		})
	})
}

func TestForwardPacketMarshal(t *testing.T) {
	Convey("ForwardPacket should encode address and port separately", t, func() {
		packet := &ForwardPacket{}
		packet.SetDestination("a.com:443")
		packet.SetPayload([]byte("ok"))

		buf, err := packet.Marshal()

		So(err, ShouldBeNil)
		So(buf, ShouldResemble, []byte{
			5, 'a', '.', 'c', 'o', 'm',
			0x01, 0xbb,
			0x00, 0x02, 'o', 'k',
		})
	})

	Convey("ForwardPacket should reject invalid destinations", t, func() {
		Convey("A destination without a port should be rejected", func() {
			packet := &ForwardPacket{}
			packet.SetDestination("example.com")

			_, err := packet.Marshal()

			So(err, ShouldNotBeNil)
		})

		Convey("Port zero should be rejected", func() {
			packet := &ForwardPacket{}
			packet.SetDestination("example.com:0")

			_, err := packet.Marshal()

			So(err, ShouldNotBeNil)
		})

		Convey("An address longer than 255 bytes should be rejected", func() {
			packet := &ForwardPacket{}
			packet.SetDestination(string(bytes.Repeat([]byte{'a'}, 256)) + ":80")

			_, err := packet.Marshal()

			So(err, ShouldNotBeNil)
		})

		Convey("A payload longer than 65535 bytes should be rejected", func() {
			packet := &ForwardPacket{}
			packet.SetDestination("example.com:80")
			packet.SetPayload(make([]byte, 65536))

			_, err := packet.Marshal()

			So(err, ShouldNotBeNil)
		})
	})

	Convey("ForwardPacket should accept maximum field lengths", t, func() {
		packet := &ForwardPacket{}
		packet.SetDestination(string(bytes.Repeat([]byte{'a'}, 255)) + ":65535")
		packet.SetPayload(make([]byte, 65535))

		buf, err := packet.Marshal()

		So(err, ShouldBeNil)
		So(buf, ShouldHaveLength, 5+255+65535)
	})
}

func TestForwardPacketAccessors(t *testing.T) {
	Convey("ForwardPacket accessors should round-trip through Marshal", t, func() {
		packet := (&ForwardPacket{}).New()
		_, isForward := packet.(*ForwardPacket)
		So(isForward, ShouldBeTrue)

		packet.SetDestination("example.com:443")
		packet.SetPayload([]byte("body"))
		So(packet.GetDestination(), ShouldEqual, "example.com:443")
		So(packet.GetPayload(), ShouldResemble, []byte("body"))

		buf, err := packet.Marshal()
		So(err, ShouldBeNil)

		decoded := &ForwardPacket{}
		n, state, err := decoded.Unmarshal(buf)
		So(err, ShouldBeNil)
		So(state, ShouldEqual, ParseDone)
		So(n, ShouldEqual, len(buf))
		So(decoded.GetDestination(), ShouldEqual, "example.com:443")
		So(decoded.GetPayload(), ShouldResemble, []byte("body"))
	})

	Convey("ForwardPacket should round-trip an IPv6 destination", t, func() {
		packet := &ForwardPacket{}
		packet.SetDestination("[2001:db8::1]:8443")

		buf, err := packet.Marshal()
		So(err, ShouldBeNil)

		decoded := &ForwardPacket{}
		_, state, err := decoded.Unmarshal(buf)

		So(err, ShouldBeNil)
		So(state, ShouldEqual, ParseDone)
		So(decoded.GetDestination(), ShouldEqual, "[2001:db8::1]:8443")
	})
}
