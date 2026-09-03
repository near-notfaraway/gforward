package destination

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/panjf2000/gnet/v2"
)

// HTTPParser 适配 DNS 劫持 HTTP 的透明代理场景
type HTTPParser struct{}

func NewHTTPParser() *HTTPParser {
	return &HTTPParser{}
}

// Parse 根据 HTTP Host 来获取目的地
func (p *HTTPParser) Parse(conn gnet.Conn) (ParseResult, error) {
	buf, err := conn.Peek(-1)
	if err != nil {
		return ParseResult{Status: ParseRejected}, fmt.Errorf("parser read conn failed: %w", err)
	}
	bufReader := bufio.NewReader(bytes.NewReader(buf))
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ParseResult{Status: ParseNeedMoreData}, nil
		}
		return ParseResult{Status: ParseRejected}, fmt.Errorf("invalid http request: %w", err)
	}

	return ParseResult{
		Status:      ParseDone,
		Destination: httpReq.Host,
	}, nil
}
