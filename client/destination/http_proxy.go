package destination

import (
	"bufio"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"net/http"
)

const (
	httpProxyAckFormat = "%s 200 OK\r\n\r\n"
)

type HTTPProxyParser struct{}

func NewHTTPProxyParser() *HTTPProxyParser {
	return &HTTPProxyParser{}
}

func (p *HTTPProxyParser) Parse(conn gnet.Conn) (string, error) {
	bufReader := bufio.NewReader(conn)
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", fmt.Errorf("invalid http request: %w", err)
	}

	// 非 Connect 方法则相当于走 HTTP 透明代理，不需要返回 ACK
	dest := extractDestWithPort(httpReq.Host, 80)
	if httpReq.Method != http.MethodConnect {
		return dest, nil
	}

	// Connect 方法需要返回 ACK
	_, err = conn.Write([]byte(fmt.Sprintf(httpProxyAckFormat, httpReq.Proto)))
	if err != nil {
		return "", err
	}

	return dest, nil
}
