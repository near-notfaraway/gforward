package protocol

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestForwardPacketUnmarshalState(t *testing.T) {
	Convey("ForwardPacket should report its parse state", t, func() {
		packet := &ForwardPacket{}

		Convey("Incomplete data should need more data", func() {
			n, state, err := packet.Unmarshal([]byte{0})

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseNeedMoreData)
			So(err, ShouldBeNil)
		})

		Convey("A complete frame should be done", func() {
			expected := &ForwardPacket{
				destination: "example.com:80",
				payload:     []byte("payload"),
			}
			buf, err := expected.Marshal()
			So(err, ShouldBeNil)
			buf = append(buf, []byte("next frame")...)

			n, state, err := packet.Unmarshal(buf)

			So(err, ShouldBeNil)
			So(state, ShouldEqual, ParseDone)
			So(n, ShouldEqual, len(buf)-len("next frame"))
			So(packet.destination, ShouldEqual, expected.destination)
			So(packet.payload, ShouldResemble, expected.payload)
		})

		Convey("An empty destination should be rejected", func() {
			n, state, err := packet.Unmarshal([]byte{0, 0, 0, 0})

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseRejected)
			So(err, ShouldNotBeNil)
		})
	})
}
