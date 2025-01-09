package client

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"strings"
)

type HTTPDestinationParser struct{}

func (p *HTTPDestinationParser) ParseAndAck(buf []byte) (dest string, ack []byte, error error) {
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", nil, fmt.Errorf("invalid http request: %w", err)
	}

	return extractDestinationFromHTTPRequest(httpReq, 80), nil, nil
}

// extractDestinationFromHTTPRequest 从 HTTP 请求中提取转发目的地
// 使用请求中的 Host 作为目的地，若 Host 不包含端口则为其补充默认端口
func extractDestinationFromHTTPRequest(req *http.Request, defaultPort int) string {
	host := req.Host
	portColonIdx := strings.LastIndexByte(host, ':')
	if portColonIdx == -1 {
		return host + ":80"
	}
	if strings.Index(host[portColonIdx+1:], "]") >= 0 {
		return fmt.Sprintf("%s:%d", host, defaultPort)
	}
	return host
}
