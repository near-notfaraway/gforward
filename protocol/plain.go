package protocol

import (
	"encoding/binary"
	"fmt"
)

type ForwardPacket struct {
	destination string
	payload     []byte
}

func (p *ForwardPacket) New() InternalPacket {
	return &ForwardPacket{}
}

func (p *ForwardPacket) Marshal() ([]byte, error) {
	buf := make([]byte, 4+len(p.destination)+len(p.payload))
	pos := 0
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(p.destination)))
	pos += 2
	copy(buf[pos:pos+len(p.destination)], p.destination)
	pos += len(p.destination)
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(p.payload)))
	pos += 2
	copy(buf[pos:pos+len(p.payload)], p.payload)
	return buf, nil
}

func (p *ForwardPacket) Unmarshal(buf []byte) (int, error) {
	if len(buf) < 4 {
		return 0, fmt.Errorf("packet too short")
	}
	pos := 0
	destinationLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
	pos += 2
	if 4+destinationLen > len(buf) {
		return 0, fmt.Errorf("packet too short")
	}
	p.destination = string(buf[pos : pos+destinationLen])
	pos += destinationLen
	payloadLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
	pos += 2
	if 4+destinationLen+payloadLen > len(buf) {
		return 0, fmt.Errorf("packet too short")
	}
	p.payload = make([]byte, payloadLen)
	copy(p.payload, buf[pos:pos+payloadLen])
	pos += payloadLen
	return pos, nil
}

func (p *ForwardPacket) GetPayload() []byte {
	return p.payload
}

func (p *ForwardPacket) SetPayload(payload []byte) {
	p.payload = payload
}

func (p *ForwardPacket) GetDestination() string {
	return p.destination
}

func (p *ForwardPacket) SetDestination(destination string) {
	p.destination = destination
}
