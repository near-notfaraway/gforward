package client

import (
	"encoding/binary"
	"fmt"
	"log"
)

const (
	tlsRecordTypeHandshake      = 22
	tlsVersionV3                = 3
	tlsHandshakeTypeClientHello = 1
)

// https://datatracker.ietf.org/doc/html/rfc8446 [The Transport Layer Security (TLS) Protocol Version 1.3]
// https://datatracker.ietf.org/doc/html/rfc6066 [Transport Layer Security (TLS) Extensions: Extension Definitions]
func getServerName(buf []byte) (string, error) {
	if len(buf) < 5 || buf[0] != tlsRecordTypeHandshake {
		return "", fmt.Errorf("not TLS Handshark message")
	}
	if buf[1] != tlsVersionV3 {
		return "", fmt.Errorf("TLS version less than 3")
	}
	log.Printf("version is %d %d", buf[1], buf[2])

	if buf[5] != tlsHandshakeTypeClientHello {
		return "", fmt.Errorf("not TLS Handshark Client Hello message")
	}
	hsLengthBuf := []byte{0, buf[6], buf[7], buf[8]}
	hsLength := int(binary.BigEndian.Uint32(hsLengthBuf))
	if len(buf[9:]) != hsLength {
		return "", fmt.Errorf("TLS Handshark length invalid in message")
	}
	log.Printf("handshark len is %d", hsLength)

	log.Printf("handshark version is %d %d", buf[9], buf[10])
	// random
	log.Printf("handshark random is %v", buf[11:43])
	sessionIdLen := int(buf[43])
	log.Printf("sessionId len is %v", sessionIdLen)
	cipherSuitListLen := int(buf[43+1+sessionIdLen])<<8 + int(buf[43+1+sessionIdLen+1])
	log.Printf("cipherSuitList len is %v", cipherSuitListLen)
	compressionMethodLen := int(buf[43+1+sessionIdLen+2+cipherSuitListLen])
	log.Printf("compressionMethodLen len is %v", compressionMethodLen)
	extensionsLenPos := 43 + 1 + sessionIdLen + 2 + cipherSuitListLen + 1 + compressionMethodLen
	extensionsLen := binary.BigEndian.Uint16(buf[extensionsLenPos : extensionsLenPos+2])
	log.Printf("extensions len is %v", extensionsLen)
	extensionsPos := extensionsLenPos + 2
	for pos := extensionsPos; pos < len(buf); {
		extensionType := binary.BigEndian.Uint16(buf[pos : pos+2])
		log.Printf("extension type is %v", extensionType)
		extensionLen := binary.BigEndian.Uint16(buf[pos+2 : pos+4])
		log.Printf("extension len is %v", extensionLen)
		if extensionType == 0 {
			nameListLen := binary.BigEndian.Uint16(buf[pos+4 : pos+6])
			log.Printf("extension server name list len is %v", nameListLen)
			if buf[pos+6] == 0 {
				nameLen := binary.BigEndian.Uint16(buf[pos+7 : pos+9])
				log.Printf("extension server name len is %d", nameLen)
				return string(buf[pos+9 : pos+9+int(nameLen)]), nil
			}
		}
		log.Printf("extension content is %v", string(buf[pos+4:pos+4+int(extensionsLen)]))
		pos += 4 + int(extensionsLen)
	}

	return "", fmt.Errorf("no server name extension")
}
