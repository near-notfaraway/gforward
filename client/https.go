package client

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	tlsRecordLen                = 5
	tlsMainVersionV3            = 3
	tlsRecordTypeHandshake      = 22
	tlsHandshakeTypeClientHello = 1
	tlsExtensionTypeServerName  = 0
	tlsServerNameTypeHost       = 0
)

type HTTPSDestinationParser struct{}

// ParseAndAck 根据 TLS 识别 SNI 来获取目的地
// https://datatracker.ietf.org/doc/html/rfc8446 [The Transport Layer Security (TLS) Protocol Version 1.3]
// https://datatracker.ietf.org/doc/html/rfc6066 [Transport Layer Security (TLS) Extensions: Extension Definitions]
func (p *HTTPSDestinationParser) ParseAndAck(buf []byte) (dest string, ack []byte, error error) {
	// 校验是否为 TLS Handshake 的 Client Hello
	if len(buf) < tlsRecordLen || buf[0] != tlsRecordTypeHandshake {
		return "", nil, fmt.Errorf("not TLS Handshark")
	}
	if buf[1] != tlsMainVersionV3 {
		return "", nil, fmt.Errorf("TLS version less than 3")
	}
	if buf[5] != tlsHandshakeTypeClientHello {
		return "", nil, fmt.Errorf("not TLS Handshark Client Hello")
	}
	hsLengthBuf := append([]byte{0}, buf[6:9]...)
	hsLength := int(binary.BigEndian.Uint32(hsLengthBuf))
	if len(buf[9:]) != hsLength {
		return "", nil, fmt.Errorf("TLS Handshark Client Hello length invalid")
	}

	// 找到 extensions 的起始位置
	sessionIdLen := int(buf[43])
	cipherSuitListLen := int(buf[44+sessionIdLen])<<8 + int(buf[45+sessionIdLen])
	compressionMethodLen := int(buf[46+sessionIdLen+cipherSuitListLen])
	extensionsLenPos := 47 + sessionIdLen + cipherSuitListLen + compressionMethodLen
	extensionsLen := int(binary.BigEndian.Uint16(buf[extensionsLenPos : extensionsLenPos+2]))
	extensionsPos := extensionsLenPos + 2
	if len(buf[extensionsPos:]) != extensionsLen {
		return "", nil, fmt.Errorf("TLS Handshark extensions length invalid in message")
	}

	// 迭代所有的 extensions 找到 sni
	for pos := extensionsPos; pos < len(buf); {
		extensionType := binary.BigEndian.Uint16(buf[pos : pos+2])
		extensionLen := binary.BigEndian.Uint16(buf[pos+2 : pos+4])
		if extensionType == tlsExtensionTypeServerName {
			if buf[pos+6] == tlsServerNameTypeHost {
				nameLen := binary.BigEndian.Uint16(buf[pos+7 : pos+9])
				host := string(buf[pos+9 : pos+9+int(nameLen)])
				return extractDestWithPort(host, 443), nil, nil
			}
		}
		pos += 4 + int(extensionLen)
	}

	return "", nil, fmt.Errorf("not found sni in TLS Handshark extensions")
}

// extractDestWithPort 若 host 不包含端口则为其补充默认端口
// host 必须合法，即为这几种格式: domain/ipv4/ipv6/domain:port/ipv4:port/ipv6:port
func extractDestWithPort(host string, defaultPort int) string {
	portColonIdx := strings.LastIndexByte(host, ':')
	if portColonIdx == -1 {
		return fmt.Sprintf("%s:%d", host, defaultPort)
	}
	if strings.Index(host[portColonIdx+1:], "]") >= 0 {
		return fmt.Sprintf("%s:%d", host, defaultPort)
	}
	return host
}
