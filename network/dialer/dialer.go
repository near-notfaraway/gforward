package dialer

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/near-notfaraway/gforward/network/message"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

const (
	dialTimeout        = 2 * time.Second // 单次拨号的超时上限
	maxConcurrentDials = 256             // 异步拨号的最大并发数，避免 goroutine 与 fd 无上限增长
	dialResultChanSize = 20              // 异步拨号结果通道的缓冲大小
)

type Dialer struct {
	handler *DialHandler // 处理出站连接收到的数据
	client  *gnet.Client // 管理 gnet 出站连接和拨号

	dialResultChan chan *DialResult // 回投异步拨号结果给调用方
	dialSem        chan struct{}    // 限制并发拨号数的信号量
}

// DialResult 承载一次异步拨号的结果，通过 DialResultChan 回投给调用方。
type DialResult struct {
	Conn  gnet.Conn // 拨号得到的连接，失败时为 nil
	Err   error     // 拨号错误，非 nil 表示失败
	Token any       // 调用方发起拨号时传入的关联标识，原样带回以匹配拨号上下文
}

func NewDialer(proto string, logger *logrus.Entry) *Dialer {
	handler := NewDialHandler(proto, logger)
	client, err := gnet.NewClient(handler)
	if err != nil {
		panic(fmt.Errorf("create dialer client failed: %w", err))
	}
	err = client.Start()
	if err != nil {
		panic(fmt.Errorf("start dialer client failed: %w", err))
	}
	return &Dialer{
		handler:        handler,
		client:         client,
		dialResultChan: make(chan *DialResult, dialResultChanSize),
		dialSem:        make(chan struct{}, maxConcurrentDials),
	}
}

func (d *Dialer) RecvChan() <-chan *message.RecvMsg {
	return d.handler.recvChan
}

func (d *Dialer) DialResultChan() <-chan *DialResult {
	return d.dialResultChan
}

func (d *Dialer) Dial(network, address string) (gnet.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	gnetConn, err := d.client.Enroll(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return gnetConn, nil
}

// AsyncDial 在独立 goroutine 中拨号，避免慢拨号阻塞调用方，完成后通过 DialResultChan 回投结果。
// token 由调用方用于关联本次拨号的上下文，会原样带回；并发拨号数由 dialSem 限制在 maxConcurrentDials。
func (d *Dialer) AsyncDial(network, address string, token any) {
	go func() {
		d.dialSem <- struct{}{}
		defer func() { <-d.dialSem }()

		conn, err := d.Dial(network, address)
		d.dialResultChan <- &DialResult{
			Conn:  conn,
			Err:   err,
			Token: token,
		}
	}()
}
