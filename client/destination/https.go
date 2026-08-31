package destination

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

// HTTPSParser 适配 DNS 劫持 HTTPS 的透明代理场景
type HTTPSParser struct{}

func NewHTTPSParser() *HTTPSParser {
	return &HTTPSParser{}
}

// Parse 根据 TLS 识别 SNI 来获取目的地
// https://datatracker.ietf.org/doc/html/rfc8446 [The Transport Layer Security (TLS) Protocol Version 1.3]
// https://datatracker.ietf.org/doc/html/rfc6066 [Transport Layer Security (TLS) Extensions: Extension Definitions]
func (p *HTTPSParser) Parse(conn ParserConn) (ParseResult, error) {
	rejected := func(format string, args ...any) (ParseResult, error) {
		return ParseResult{Status: ParseRejected}, fmt.Errorf(format, args...)
	}

	// 校验是否为 TLS Handshake 的 Client Hello
	buf, err := conn.Peek(-1)
	if err != nil {
		return rejected("parser read conn failed: %v", err)
	}
	if len(buf) == 0 {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}
	if buf[0] != tlsRecordTypeHandshake {
		return rejected("not TLS handshake")
	}
	if len(buf) < tlsRecordLen {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}
	if buf[1] != tlsMainVersionV3 {
		return rejected("TLS major version is not 3")
	}
	if len(buf) < 6 {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}
	if buf[5] != tlsHandshakeTypeClientHello {
		return rejected("not TLS ClientHello")
	}
	if len(buf) < 9 {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}

	// 读取 TLS Handshake 的 buf
	hsLengthBuf := append([]byte{0}, buf[6:9]...)
	hsLength := int(binary.BigEndian.Uint32(hsLengthBuf))
	if len(buf) < 9+hsLength {
		return ParseResult{Status: ParseNeedMoreData}, nil
	}
	hsBuf := buf[9 : 9+hsLength]

	// 找到 extensions 的起始位置
	if len(hsBuf) < 35 {
		return rejected("TLS ClientHello is too short")
	}
	sessionIdLen := int(hsBuf[34])
	if len(hsBuf) < 37+sessionIdLen {
		return rejected("TLS ClientHello session id is truncated")
	}
	cipherSuitListLen := int(hsBuf[35+sessionIdLen])<<8 + int(hsBuf[36+sessionIdLen])
	if len(hsBuf) < 38+sessionIdLen+cipherSuitListLen {
		return rejected("TLS ClientHello cipher suites are truncated")
	}
	compressionMethodLen := int(hsBuf[37+sessionIdLen+cipherSuitListLen])
	extensionsLenPos := 38 + sessionIdLen + cipherSuitListLen + compressionMethodLen
	if len(hsBuf) < extensionsLenPos+2 {
		return rejected("TLS ClientHello compression methods are truncated")
	}
	extensionsLen := int(binary.BigEndian.Uint16(hsBuf[extensionsLenPos : extensionsLenPos+2]))
	extensionsPos := extensionsLenPos + 2
	if len(hsBuf)-extensionsPos != extensionsLen {
		return rejected("TLS ClientHello extensions length is invalid")
	}

	// 迭代所有的 extensions 找到 sni
	for pos := extensionsPos; pos < len(hsBuf); {
		if pos+4 > len(hsBuf) {
			return rejected("TLS extension header is truncated")
		}
		extensionType := binary.BigEndian.Uint16(hsBuf[pos : pos+2])
		extensionLen := int(binary.BigEndian.Uint16(hsBuf[pos+2 : pos+4]))
		extensionEnd := pos + 4 + extensionLen
		if extensionEnd > len(hsBuf) {
			return rejected("TLS extension is truncated")
		}
		if extensionType == tlsExtensionTypeServerName {
			if extensionLen < 5 {
				return rejected("TLS server name extension is too short")
			}
			if hsBuf[pos+6] == tlsServerNameTypeHost {
				nameLen := binary.BigEndian.Uint16(hsBuf[pos+7 : pos+9])
				nameEnd := pos + 9 + int(nameLen)
				if nameEnd > extensionEnd {
					return rejected("TLS server name is truncated")
				}
				host := string(hsBuf[pos+9 : nameEnd])
				return ParseResult{
					Status:      ParseDone,
					Destination: extractDestWithPort(host, 443),
				}, nil
			}
		}
		pos = extensionEnd
	}

	return rejected("SNI not found in TLS ClientHello")
}

// extractDestWithPort 若 host 不包含端口则为其补充默认端口
// host 必须合法，即为这几种格式: domain/ipv4/ipv6/domain:port/ipv4:port/ipv6:port
func extractDestWithPort(host string, defaultPort int) string {
	portColonIdx := strings.LastIndexByte(host, ':')
	if portColonIdx == -1 {
		return fmt.Sprintf("%s:%d", host, defaultPort)
	}
	if strings.Contains(host[portColonIdx+1:], "]") {
		return fmt.Sprintf("%s:%d", host, defaultPort)
	}
	return host
}
