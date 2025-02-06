package destination

import (
	"encoding/binary"
	"fmt"
	"github.com/panjf2000/gnet/v2"
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

type HTTPSParser struct{}

func NewHTTPSParser() *HTTPSParser {
	return &HTTPSParser{}
}

// Parse 根据 TLS 识别 SNI 来获取目的地
// https://datatracker.ietf.org/doc/html/rfc8446 [The Transport Layer Security (TLS) Protocol Version 1.3]
// https://datatracker.ietf.org/doc/html/rfc6066 [Transport Layer Security (TLS) Extensions: Extension Definitions]
func (p *HTTPSParser) Parse(conn gnet.Conn) (string, error) {
	// 校验是否为 TLS Handshake 的 Client Hello
	buf, err := conn.Next(10)
	if err != nil {
		return "", err
	}
	if len(buf) < tlsRecordLen || buf[0] != tlsRecordTypeHandshake {
		return "", fmt.Errorf("not TLS Handshark")
	}
	if buf[1] != tlsMainVersionV3 {
		return "", fmt.Errorf("TLS version less than 3")
	}
	if buf[5] != tlsHandshakeTypeClientHello {
		return "", fmt.Errorf("not TLS Handshark Client Hello")
	}

	// 读取 TLS Handshake 的 buf
	hsLengthBuf := append([]byte{0}, buf[6:9]...)
	hsLength := int(binary.BigEndian.Uint32(hsLengthBuf))
	hsBuf, err := conn.Next(hsLength)
	if len(hsBuf) != hsLength {
		return "", fmt.Errorf("TLS Handshark Client Hello length invalid")
	}

	// 找到 extensions 的起始位置
	sessionIdLen := int(hsBuf[33])
	cipherSuitListLen := int(hsBuf[34+sessionIdLen])<<8 + int(hsBuf[35+sessionIdLen])
	compressionMethodLen := int(hsBuf[46+sessionIdLen+cipherSuitListLen])
	extensionsLenPos := 37 + sessionIdLen + cipherSuitListLen + compressionMethodLen
	extensionsLen := int(binary.BigEndian.Uint16(hsBuf[extensionsLenPos : extensionsLenPos+2]))
	extensionsPos := extensionsLenPos + 2
	if len(hsBuf[extensionsPos:]) != extensionsLen {
		return "", fmt.Errorf("TLS Handshark extensions length invalid in message")
	}

	// 迭代所有的 extensions 找到 sni
	for pos := extensionsPos; pos < len(hsBuf); {
		extensionType := binary.BigEndian.Uint16(hsBuf[pos : pos+2])
		extensionLen := binary.BigEndian.Uint16(hsBuf[pos+2 : pos+4])
		if extensionType == tlsExtensionTypeServerName {
			if hsBuf[pos+6] == tlsServerNameTypeHost {
				nameLen := binary.BigEndian.Uint16(hsBuf[pos+7 : pos+9])
				host := string(hsBuf[pos+9 : pos+9+int(nameLen)])
				return extractDestWithPort(host, 443), nil
			}
		}
		pos += 4 + int(extensionLen)
	}

	return "", fmt.Errorf("not found sni in TLS Handshark extensions")
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
