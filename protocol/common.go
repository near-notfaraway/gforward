package protocol

const (
	PacketTypePlain   = "plain"
	PacketTypeForward = "forward"
)

type InternalPacket interface {
	New() InternalPacket
	Marshal() ([]byte, error)
	Unmarshal([]byte) (int, error)

	GetPayload() []byte
	SetPayload(payload []byte)
	GetDestination() string
	SetDestination(destination string)
}

func NewInternalPacket(proto string) InternalPacket {
	switch proto {
	case PacketTypePlain:
		return &PlainPacket{}
	case PacketTypeForward:
		return &ForwardPacket{}
	default:
		return &PlainPacket{}
	}
}
