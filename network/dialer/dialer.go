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

	dialResultChan      chan *DialResult // 默认回投异步拨号结果给调用方
	dialErrorToRecvChan bool             // 是否将拨号失败作为 RecvMsg 投递到 recvChan
	dialSem             chan struct{}    // 限制并发拨号数的信号量
}

// DialResult 承载一次异步拨号的结果。
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

// SetDialErrorToRecvChan 设置是否将异步拨号失败投递到 RecvChan。
// 需在首次拨号前设置；开启后成功路径由 RecvEventOpen 表示，DialResultChan 不再接收后续拨号结果。
func (d *Dialer) SetDialErrorToRecvChan(enabled bool) {
	d.dialErrorToRecvChan = enabled
}

// SetOnDialOpen 注册连接就绪回调，在拨号连接的任何读/关事件之前于事件循环上触发。
// 调用方可在回调中用 conn.Context() 取回拨号时透传的 token 并原子地预注册路由。
// 需在首次拨号前设置；未设置则连接就绪事件会通过 RecvChan 投递给调用方。
func (d *Dialer) SetOnDialOpen(onOpen func(conn gnet.Conn) gnet.Action) {
	d.handler.onOpen = onOpen
}

// Dial 同步建立到目标的连接并交给 gnet 托管。
// token 会通过 EnrollContext 绑定到连接，可在 OnDialOpen 回调或 RecvEventOpen 中取回。
func (d *Dialer) Dial(network, address string, token any) (gnet.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	gnetConn, err := d.client.EnrollContext(conn, token)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return gnetConn, nil
}

// AsyncDial 在独立 goroutine 中拨号，避免慢拨号阻塞调用方，并按配置回投拨号结果。
// token 由调用方用于关联本次拨号的上下文，会绑定到连接并原样带回；并发拨号数由 dialSem 限制在 maxConcurrentDials。
func (d *Dialer) AsyncDial(network, address string, token any) {
	go func() {
		d.dialSem <- struct{}{}
		defer func() { <-d.dialSem }()

		conn, err := d.Dial(network, address, token)
		if d.dialErrorToRecvChan {
			if err != nil {
				d.handler.recvChan <- &message.RecvMsg{
					Event: message.RecvEventDialError,
					Err:   err,
					Token: token,
				}
			}
			return
		}
		d.dialResultChan <- &DialResult{
			Conn:  conn,
			Err:   err,
			Token: token,
		}
	}()
}
