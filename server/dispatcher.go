package server

import (
	"sync/atomic"

	"github.com/near-notfaraway/gforward/network/dialer"
	"github.com/near-notfaraway/gforward/network/message"
	"github.com/near-notfaraway/gforward/protocol"
	"github.com/near-notfaraway/gforward/utils"
	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

const msgChanSize = 256 // 最大消息通道缓冲大小，避免 goroutine 与 fd 无上限增长

// dispatchContext 表示当前连接的分发上下文，包含固定分配的 worker 下标。
type dispatchContext struct {
	workerIndex int // 当前连接固定分配的 worker 下标
}

// dispatchWorker 绑定一个消息处理器与其固定分发通道
type dispatchWorker struct {
	channel chan<- *message.RecvMsg // 按连接固定分发消息的处理通道，仅由 Dispatcher 投递
	handler *msgHandler             // 消费该通道消息的处理器
}

// dispatcher 分发器，负责将客户端上行流量分发给 worker 处理
type dispatcher struct {
	gnet.BuiltinEventEngine // 提供未覆盖事件的默认实现

	workers          []*dispatchWorker       // 固定分发消息的处理器及其通道
	nextWorkerIndex  atomic.Uint64           // 轮询分配新连接时使用的 worker 下标
	internalProtocol protocol.InternalPacket // 解码客户端上行流量的协议原型
	uploadLogger     *logrus.Entry           // 客户端到目标站点方向的日志
}

// NewDispatcher 创建指定数量的消息处理器及其固定分发通道。
func NewDispatcher(workerNum int) *dispatcher {
	downloadLogger := logrus.WithFields(logrus.Fields{
		"role":      "server",
		"direction": "dest->client",
	})
	uploadLogger := logrus.WithFields(logrus.Fields{
		"role":      "server",
		"direction": "client->dest",
	})

	workers := make([]*dispatchWorker, workerNum)
	for i := range workers {
		ch := make(chan *message.RecvMsg, msgChanSize)
		dl := dialer.NewDialer(protocol.PacketTypePlain, downloadLogger)
		hdl := newMsgHandler(dl, ch)
		hdl.start()
		workers[i] = &dispatchWorker{channel: ch, handler: hdl}
	}

	return &dispatcher{
		workers:          workers,
		internalProtocol: &protocol.ForwardPacket{},
		uploadLogger:     uploadLogger,
	}
}

func (d *dispatcher) OnOpen(conn gnet.Conn) ([]byte, gnet.Action) {
	workerIndex := int((d.nextWorkerIndex.Add(1) - 1) % uint64(len(d.workers)))
	conn.SetContext(&dispatchContext{workerIndex: workerIndex})
	return nil, gnet.None
}

func (d *dispatcher) OnClose(conn gnet.Conn, err error) (action gnet.Action) {
	logger := d.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))
	logger.Debugf("closed conn by err: %s", err)
	d.dispatch(&message.RecvMsg{Event: message.RecvEventClose, Conn: conn, Logger: logger})
	return gnet.None
}

// OnTraffic 解析当前缓冲区中的完整包，读干缓冲区后将本次解析出的所有包合并分发。
func (d *dispatcher) OnTraffic(conn gnet.Conn) gnet.Action {
	logger := d.uploadLogger.WithField("fromConn", utils.FormatGNetConn(conn))
	msg, rejected := message.ParseAvailable(conn, d.internalProtocol, logger)
	if rejected {
		return gnet.Close
	}
	if len(msg.Pkts) > 0 {
		d.dispatch(msg)
	}
	return gnet.None
}

// dispatch 向目标 worker 投递一次分发消息。
func (d *dispatcher) dispatch(msg *message.RecvMsg) {
	workerIndex := msg.Conn.Context().(*dispatchContext).workerIndex
	d.workers[workerIndex].channel <- msg
	msg.Logger.Debugf("dispatch event %d with %d packets to chan %d", msg.Event, len(msg.Pkts), workerIndex)
}
