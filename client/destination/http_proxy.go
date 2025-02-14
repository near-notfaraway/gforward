package destination

import (
	"bufio"
	"bytes"
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

// Parse 根据 HTTP Connect 请求的 Host 来获取目的地
func (p *HTTPProxyParser) Parse(conn gnet.Conn) (string, error) {
	buf, err := conn.Peek(-1)
	if err != nil {
		return "", fmt.Errorf("parser read conn failed: %w", err)
	}
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", fmt.Errorf("invalid http request: %w", err)
	}

	// 非 Connect 方法则相当于走 HTTP 透明代理，不需要返回 ACK
	if httpReq.Method != http.MethodConnect {
		return extractDestWithPort(httpReq.Host, 80), nil
		//return "", fmt.Errorf("invalid http method: %s", httpReq.Method)
	}

	// Connect 方法需要返回 ACK
	_, err = conn.Write([]byte(fmt.Sprintf(httpProxyAckFormat, httpReq.Proto)))
	if err != nil {
		return "", err
	}
	_, _ = conn.Discard(len(buf))

	return extractDestWithPort(httpReq.Host, 80), nil
}
