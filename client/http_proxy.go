package client

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
)

const (
	httpProxyAckFormat = "%s 200 OK\r\n\r\n"
)

type HTTPProxyDestinationParser struct{}

func (p *HTTPProxyDestinationParser) ParseAndAck(buf []byte) (dest string, ack []byte, error error) {
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		// 非 HTTP 协议则尝试走 HTTPS 透明代理
		dest2, ack2, err2 := (&HTTPSDestinationParser{}).ParseAndAck(buf)
		if err2 != nil {
			return "", nil, fmt.Errorf("invalid http request: %w and invalid https request: %w", err, err2)
		}
		return dest2, ack2, nil
	}

	// 非 Connect 方法则相当于走 HTTP 透明代理，不需要返回 ACK
	dest = extractDestinationFromHTTPRequest(httpReq, 443)
	if httpReq.Method != http.MethodConnect {
		return dest, nil, nil
	}

	ack = []byte(fmt.Sprintf(httpProxyAckFormat, httpReq.Proto))
	return dest, ack, nil
}
