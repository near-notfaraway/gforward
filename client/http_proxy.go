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
		return "", nil, fmt.Errorf("invalid http request: %w", err)
	}

	// 非 Connect 方法则相当于走 HTTP 透明代理，不需要返回 ACK
	dest = extractDestinationFromHTTPRequest(httpReq, 443)
	if httpReq.Method != http.MethodConnect {
		return dest, nil, nil
	}

	ack = []byte(fmt.Sprintf(httpProxyAckFormat, httpReq.Proto))
	return dest, ack, nil
}
