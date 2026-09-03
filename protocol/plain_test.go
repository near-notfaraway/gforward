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

func TestPlainPacketAccessors(t *testing.T) {
	Convey("PlainPacket accessors should carry only the raw payload", t, func() {
		packet := (&PlainPacket{}).New()
		_, isPlain := packet.(*PlainPacket)
		So(isPlain, ShouldBeTrue)

		packet.SetPayload([]byte("raw"))
		So(packet.GetPayload(), ShouldResemble, []byte("raw"))

		// PlainPacket carries no destination frame; SetDestination is a no-op.
		packet.SetDestination("ignored")
		So(packet.GetDestination(), ShouldEqual, "")

		buf, err := packet.Marshal()
		So(err, ShouldBeNil)
		So(buf, ShouldResemble, []byte("raw"))
	})
}
