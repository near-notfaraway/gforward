package destination

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	httpProxyAckFormat       = "%s 200 OK\r\n\r\n"
	maxHTTPProxyPayloadLen   = 65535
	maxHTTPChunkMetadataSize = 8192
)

// HTTPProxyParser 适配 HTTP/HTTPS 代理场景
type HTTPProxyParser struct {
	connMapRequestState sync.Map // map[conn]httpProxyRequestState 维护每条连接尚未接收完整的 HTTP 请求体状态
}

func NewHTTPProxyParser() *HTTPProxyParser {
	return &HTTPProxyParser{}
}

func (p *HTTPProxyParser) Clear(conn ParserConn) {
	p.connMapRequestState.Delete(conn)
}

// Parse 为 HTTPS 代理请求根据 HTTP CONNECT 的 Host 获取目的地，
// 为普通 HTTP 代理请求根据 HTTP Host 获取目的地。
func (p *HTTPProxyParser) Parse(conn ParserConn) (ParseResult, error) {
	buf, err := conn.Peek(-1)
	if err != nil {
		return ParseResult{Status: ParseRejected}, fmt.Errorf("parser read conn failed: %w", err)
	}
	if stateVal, ok := p.connMapRequestState.Load(conn); ok {
		state := stateVal.(*httpProxyRequestState)
		payloadLen, done, err := state.consume(buf, maxHTTPProxyPayloadLen)
		if err != nil {
			p.connMapRequestState.Delete(conn)
			return ParseResult{Status: ParseRejected}, err
		}
		if done {
			p.connMapRequestState.Delete(conn)
		}
		return ParseResult{
			Status:      ParseDone,
			Destination: state.destination,
			PerRequest:  true,
			PayloadLen:  payloadLen,
		}, nil
	}

	source := bytes.NewReader(buf)
	bufReader := bufio.NewReader(source)
	httpReq, err := http.ReadRequest(bufReader)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ParseResult{Status: ParseNeedMoreData}, nil
		}
		return ParseResult{Status: ParseRejected}, fmt.Errorf("invalid http request: %w", err)
	}

	headerLen := len(buf) - source.Len() - bufReader.Buffered()
	if headerLen > maxHTTPProxyPayloadLen {
		return ParseResult{Status: ParseRejected}, fmt.Errorf("HTTP proxy header is too large: %d", headerLen)
	}

	// 非 CONNECT 方法是 HTTP 代理的普通 HTTP 请求，不需要返回 ACK
	if httpReq.Method != http.MethodConnect {
		state := &httpProxyRequestState{
			destination: extractDestWithPort(httpReq.Host, 80),
			remaining:   httpReq.ContentLength,
		}
		if hasChunkedTransferEncoding(httpReq.TransferEncoding) {
			state.chunked = &httpChunkedBodyState{}
		}

		bodyLen, done, err := state.consume(buf[headerLen:], maxHTTPProxyPayloadLen-headerLen)
		if err != nil {
			return ParseResult{Status: ParseRejected}, err
		}
		if !done {
			p.connMapRequestState.Store(conn, state)
		}
		return ParseResult{
			Status:      ParseDone,
			Destination: state.destination,
			PerRequest:  true,
			PayloadLen:  headerLen + bodyLen,
		}, nil
	}

	// Connect 方法需要返回 ACK
	_, err = conn.Write([]byte(fmt.Sprintf(httpProxyAckFormat, httpReq.Proto)))
	if err != nil {
		return ParseResult{Status: ParseRejected}, err
	}
	consumed := len(buf) - source.Len() - bufReader.Buffered()
	if _, err = conn.Discard(consumed); err != nil {
		return ParseResult{Status: ParseRejected}, err
	}

	return ParseResult{
		Status:      ParseDone,
		Destination: extractDestWithPort(httpReq.Host, 443),
	}, nil
}

type httpProxyRequestState struct {
	destination string                // 当前 HTTP 请求的目标主机和端口
	remaining   int64                 // Content-Length 请求体剩余字节数
	chunked     *httpChunkedBodyState // chunked 请求体的增量解析状态
}

// consume 按请求体编码计算当前缓冲区属于本请求的字节数。
func (s *httpProxyRequestState) consume(buf []byte, limit int) (int, bool, error) {
	if limit < len(buf) {
		buf = buf[:limit]
	}
	if s.chunked != nil {
		return s.chunked.consume(buf)
	}
	if s.remaining <= 0 {
		return 0, true, nil
	}
	payloadLen := len(buf)
	if int64(payloadLen) > s.remaining {
		payloadLen = int(s.remaining)
	}
	s.remaining -= int64(payloadLen)
	return payloadLen, s.remaining == 0, nil
}

type httpChunkedPhase uint8

const (
	httpChunkedSize    httpChunkedPhase = iota // 读取 chunk 大小行
	httpChunkedData                            // 读取 chunk 数据
	httpChunkedDataEnd                         // 读取 chunk 数据后的 CRLF
	httpChunkedTrailer                         // 读取零长度 chunk 后的 Trailer
)

type httpChunkedBodyState struct {
	phase     httpChunkedPhase // 当前 chunked 解析阶段
	line      []byte           // 尚未完整接收的 chunk size 或 Trailer 行
	remaining uint64           // 当前 chunk 剩余数据字节数
	crlfPos   int              // chunk 数据结束 CRLF 的已匹配字节数
}

// consume 按 chunked 编码推进请求体状态，并返回当前缓冲区属于本请求的字节数。
func (s *httpChunkedBodyState) consume(buf []byte) (int, bool, error) {
	pos := 0
	for pos < len(buf) {
		switch s.phase {
		case httpChunkedSize, httpChunkedTrailer:
			s.line = append(s.line, buf[pos])
			pos++
			if len(s.line) > maxHTTPChunkMetadataSize {
				return 0, false, fmt.Errorf("HTTP chunk metadata is too large")
			}
			if s.line[len(s.line)-1] != '\n' {
				continue
			}
			if len(s.line) < 2 || s.line[len(s.line)-2] != '\r' {
				return 0, false, fmt.Errorf("invalid HTTP chunk line ending")
			}
			if s.phase == httpChunkedTrailer {
				done := len(s.line) == 2
				s.line = s.line[:0]
				if done {
					return pos, true, nil
				}
				continue
			}

			sizeText := strings.TrimSpace(string(s.line[:len(s.line)-2]))
			if extensionPos := strings.IndexByte(sizeText, ';'); extensionPos >= 0 {
				sizeText = sizeText[:extensionPos]
			}
			size, err := strconv.ParseUint(strings.TrimSpace(sizeText), 16, 64)
			if err != nil {
				return 0, false, fmt.Errorf("invalid HTTP chunk size: %w", err)
			}
			s.line = s.line[:0]
			if size == 0 {
				s.phase = httpChunkedTrailer
				continue
			}
			s.remaining = size
			s.phase = httpChunkedData

		case httpChunkedData:
			dataLen := len(buf) - pos
			if uint64(dataLen) > s.remaining {
				dataLen = int(s.remaining)
			}
			pos += dataLen
			s.remaining -= uint64(dataLen)
			if s.remaining == 0 {
				s.phase = httpChunkedDataEnd
			}

		case httpChunkedDataEnd:
			expected := []byte{'\r', '\n'}
			if buf[pos] != expected[s.crlfPos] {
				return 0, false, fmt.Errorf("invalid HTTP chunk data ending")
			}
			pos++
			s.crlfPos++
			if s.crlfPos == len(expected) {
				s.crlfPos = 0
				s.phase = httpChunkedSize
			}

		default:
			return 0, false, fmt.Errorf("invalid HTTP chunk parser state")
		}
	}
	return pos, false, nil
}

func hasChunkedTransferEncoding(encodings []string) bool {
	for _, encoding := range encodings {
		if strings.EqualFold(encoding, "chunked") {
			return true
		}
	}
	return false
}
