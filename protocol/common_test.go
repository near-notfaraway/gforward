package protocol

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewInternalPacket(t *testing.T) {
	Convey("NewInternalPacket should build a packet matching the requested proto", t, func() {
		Convey("Plain proto should build a PlainPacket", func() {
			pkt := NewInternalPacket(PacketTypePlain)

			_, ok := pkt.(*PlainPacket)
			So(ok, ShouldBeTrue)
		})

		Convey("Forward proto should build a ForwardPacket", func() {
			pkt := NewInternalPacket(PacketTypeForward)

			_, ok := pkt.(*ForwardPacket)
			So(ok, ShouldBeTrue)
		})

		Convey("Unknown proto should default to a PlainPacket", func() {
			pkt := NewInternalPacket("unknown")

			_, ok := pkt.(*PlainPacket)
			So(ok, ShouldBeTrue)
		})
	})
}
