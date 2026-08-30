package protocol

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPlainPacketUnmarshalState(t *testing.T) {
	Convey("PlainPacket should report its parse state", t, func() {
		packet := &PlainPacket{}

		Convey("An empty buffer should need more data", func() {
			n, state, err := packet.Unmarshal(nil)

			So(n, ShouldEqual, 0)
			So(state, ShouldEqual, ParseNeedMoreData)
			So(err, ShouldBeNil)
		})

		Convey("A non-empty buffer should be done", func() {
			n, state, err := packet.Unmarshal([]byte("payload"))

			So(n, ShouldEqual, len("payload"))
			So(state, ShouldEqual, ParseDone)
			So(err, ShouldBeNil)
			So(packet.payload, ShouldResemble, []byte("payload"))
		})
	})
}
