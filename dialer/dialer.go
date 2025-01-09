package dialer

import (
	"fmt"
	"github.com/panjf2000/gnet/v2"
)

type Dialer struct {
	handler   *DialHandler
	client    *gnet.Client
	readyChan chan gnet.Conn
}

func NewDialer(caller, proto string) *Dialer {
	handler := NewDialHandler(caller, proto)
	client, err := gnet.NewClient(handler)
	if err != nil {
		panic(fmt.Errorf("create dialer client failed: %w", err))
	}
	err = client.Start()
	if err != nil {
		panic(fmt.Errorf("start dialer client failed: %w", err))
	}
	return &Dialer{
		handler:   handler,
		client:    client,
		readyChan: make(chan gnet.Conn),
	}
}

func (d *Dialer) RecvChan() <-chan *RecvPkt {
	return d.handler.recvChan
}

func (d *Dialer) Dial(network, address string) (gnet.Conn, error) {
	return d.client.Dial(network, address)
}
