package protocol

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
)

type ForwardPacket struct {
	addr    string // 目标主机地址
	port    uint16 // 目标主机端口
	payload []byte // 需要转发的原始负载
}

func (p *ForwardPacket) New() InternalPacket {
	return &ForwardPacket{}
}

// Marshal 将目标地址、二进制端口和负载编码为一帧转发数据。
func (p *ForwardPacket) Marshal() ([]byte, error) {
	if len(p.addr) == 0 {
		return nil, fmt.Errorf("address is empty")
	}
	if len(p.addr) > 255 {
		return nil, fmt.Errorf("address is too long: %d", len(p.addr))
	}
	if p.port == 0 {
		return nil, fmt.Errorf("port is zero")
	}
	if len(p.payload) > 65535 {
		return nil, fmt.Errorf("payload is too long: %d", len(p.payload))
	}

	buf := make([]byte, 5+len(p.addr)+len(p.payload))
	pos := 0
	buf[pos] = byte(len(p.addr))
	pos++
	copy(buf[pos:pos+len(p.addr)], p.addr)
	pos += len(p.addr)
	binary.BigEndian.PutUint16(buf[pos:pos+2], p.port)
	pos += 2
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(p.payload)))
	pos += 2
	copy(buf[pos:pos+len(p.payload)], p.payload)
	return buf, nil
}

// Unmarshal 从缓冲区解析一帧目标地址和负载，并返回实际消费的字节数。
func (p *ForwardPacket) Unmarshal(buf []byte) (int, ParseState, error) {
	if len(buf) < 1 {
		return 0, ParseNeedMoreData, nil
	}
	pos := 0
	addrLen := int(buf[pos])
	pos++
	if addrLen == 0 {
		return 0, ParseRejected, fmt.Errorf("address is empty")
	}
	if 5+addrLen > len(buf) {
		return 0, ParseNeedMoreData, nil
	}
	addr := string(buf[pos : pos+addrLen])
	pos += addrLen
	port := binary.BigEndian.Uint16(buf[pos : pos+2])
	pos += 2
	if port == 0 {
		return 0, ParseRejected, fmt.Errorf("port is zero")
	}
	payloadLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
	pos += 2
	if pos+payloadLen > len(buf) {
		return 0, ParseNeedMoreData, nil
	}
	payload := make([]byte, payloadLen)
	copy(payload, buf[pos:pos+payloadLen])
	pos += payloadLen

	p.addr = addr
	p.port = port
	p.payload = payload
	return pos, ParseDone, nil
}

func (p *ForwardPacket) GetPayload() []byte {
	return p.payload
}

func (p *ForwardPacket) SetPayload(payload []byte) {
	p.payload = payload
}

func (p *ForwardPacket) GetDestination() string {
	if p.addr == "" || p.port == 0 {
		return ""
	}
	return net.JoinHostPort(p.addr, strconv.Itoa(int(p.port)))
}

func (p *ForwardPacket) SetDestination(destination string) {
	addr, portText, err := net.SplitHostPort(destination)
	if err != nil {
		p.addr = ""
		p.port = 0
		return
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		p.addr = ""
		p.port = 0
		return
	}
	p.addr = addr
	p.port = uint16(port)
}
