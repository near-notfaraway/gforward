package destination

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"net/http"
)

type HTTPParser struct{}

func NewHTTPParser() *HTTPParser {
	return &HTTPParser{}
}

// Parse 根据 HTTP Host 来获取目的地
func (p *HTTPParser) Parse(conn gnet.Conn) (string, error) {
	buf, err := conn.Peek(-1)
	if err != nil {
		return "", fmt.Errorf("parser read conn failed: %w", err)
	}
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", fmt.Errorf("invalid http request: %w", err)
	}

	return httpReq.Host, nil
}
