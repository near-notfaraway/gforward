package protocol

type PlainPacket struct {
	payload []byte
}

func (p *PlainPacket) New() InternalPacket {
	return &PlainPacket{}
}

func (p *PlainPacket) Marshal() ([]byte, error) {
	buf := make([]byte, len(p.payload))
	copy(buf, p.payload)
	return buf, nil
}

func (p *PlainPacket) Unmarshal(buf []byte) (int, error) {
	p.payload = make([]byte, len(buf))
	copy(p.payload, buf)
	return len(buf), nil
}

func (p *PlainPacket) GetPayload() []byte {
	return p.payload
}

func (p *PlainPacket) SetPayload(payload []byte) {
	p.payload = payload
}

func (p *PlainPacket) GetDestination() string {
	return ""
}

func (p *PlainPacket) SetDestination(destination string) {}
