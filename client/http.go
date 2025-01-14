package client

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
)

type HTTPDestinationParser struct{}

func (p *HTTPDestinationParser) ParseAndAck(buf []byte) (dest string, ack []byte, error error) {
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", nil, fmt.Errorf("invalid http request: %w", err)
	}

	return extractDestWithPort(httpReq.Host, 80), nil, nil
}
