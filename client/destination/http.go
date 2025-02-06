package destination

import (
	"bufio"
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
	bufReader := bufio.NewReader(conn)
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		return "", fmt.Errorf("invalid http request: %w", err)
	}

	return extractDestWithPort(httpReq.Host, 80), nil
}
