package dialer

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

type Dialer struct {
	handler *DialHandler // 处理出站连接收到的数据
	client  *gnet.Client // 管理 gnet 出站连接和拨号
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
		handler: handler,
		client:  client,
	}
}

func (d *Dialer) RecvChan() <-chan *RecvPkt {
	return d.handler.recvChan
}

func (d *Dialer) Dial(network, address string) (gnet.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
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
