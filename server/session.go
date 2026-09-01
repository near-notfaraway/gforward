package server

import (
	"sync"

	"github.com/panjf2000/gnet/v2"
)

// session 维护一个客户端连接当前的目标路由与拨号状态。
type session struct {
	dest     string    // 当前会话指向的目标主机和端口
	destConn gnet.Conn // 与目标站点建立的连接，拨号完成前为 nil
	dialing  bool      // 是否正在异步拨号
	pending  [][]byte  // 拨号完成前按到达顺序缓存的上行 payload
}

// upstreamAction 标识 admitUpstream 在锁内做出的上行处理决策类型。
type upstreamAction uint8

const (
	upstreamActionQueued  upstreamAction = iota // 拨号进行中，payload 已入队，锁外无需 I/O
	upstreamActionForward                       // 命中就绪路由，锁外向目标连接写入 payload
	upstreamActionDial                          // 新建或切换目标，锁外可选关闭旧连接并发起拨号
)

// upstreamActionResult 承载 admitUpstream 的决策，供 msgHandler 在锁外执行 I/O。
type upstreamActionResult struct {
	action upstreamAction // 决策类型

	destConn gnet.Conn // upstreamActionForward：目标连接
	payload  []byte    // upstreamActionForward：待转发的负载（Unmarshal 时已拷贝，锁外持有安全）

	oldDestConn gnet.Conn // upstreamActionDial：目标切换时需关闭的旧连接，无则为 nil
	session     *session  // upstreamActionDial：新建的拨号中会话，兼作拨号 token 与失效校验依据
	dest        string    // upstreamActionDial：拨号目标地址
}

// sessionTable 维护客户端连接与目标连接之间的双向路由映射，并保证跨映射操作的原子性。
type sessionTable struct {
	mu       sync.RWMutex            // 保护双向映射的并发访问
	byClient map[gnet.Conn]*session  // 客户端连接到当前会话的映射
	byDest   map[gnet.Conn]gnet.Conn // 目标连接到客户端连接的反向映射
}

func newSessionTable() *sessionTable {
	return &sessionTable{
		byClient: make(map[gnet.Conn]*session),
		byDest:   make(map[gnet.Conn]gnet.Conn),
	}
}

// admitUpstream 在锁内完成上行数据的查找、排队、目标切换或新建决策，并返回锁外应执行的 I/O 动作。
func (t *sessionTable) admitUpstream(clientConn gnet.Conn, dest string, payload []byte) upstreamActionResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	sess, ok := t.byClient[clientConn]

	// 命中同目标的现有会话：拨号中入队，已就绪则转发
	if ok && sess.dest == dest {
		if sess.dialing {
			sess.pending = append(sess.pending, payload)
			return upstreamActionResult{action: upstreamActionQueued}
		}
		return upstreamActionResult{action: upstreamActionForward, destConn: sess.destConn, payload: payload}
	}

	// 目标切换：拆除旧会话，其在途拨号结果会因 session 指针不匹配而被丢弃
	var oldDestConn gnet.Conn
	if ok {
		delete(t.byClient, clientConn)
		if sess.destConn != nil {
			delete(t.byDest, sess.destConn)
			oldDestConn = sess.destConn
		}
	}

	// 新建拨号中的会话并缓存首个 payload
	newSess := &session{
		dest:    dest,
		dialing: true,
		pending: [][]byte{payload},
	}
	t.byClient[clientConn] = newSess
	return upstreamActionResult{action: upstreamActionDial, oldDestConn: oldDestConn, session: newSess, dest: dest}
}

// completeDial 校验拨号结果对应的会话是否仍匹配，匹配且成功则就绪并返回待回放的缓存数据。
// matched 为 false 表示会话已被关闭或切换，调用方应丢弃并关闭 conn；
// matched 为 true 且 dialErr 非 nil 表示拨号失败，会话已被移除，pending 为 nil。
func (t *sessionTable) completeDial(clientConn gnet.Conn, sess *session, conn gnet.Conn, dialErr error) (pending [][]byte, matched bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cur, ok := t.byClient[clientConn]
	if !ok || cur != sess {
		return nil, false
	}
	if dialErr != nil {
		delete(t.byClient, clientConn)
		return nil, true
	}

	sess.destConn = conn
	sess.dialing = false
	pending = sess.pending
	sess.pending = nil
	t.byDest[conn] = clientConn
	return pending, true
}

// closeClient 移除客户端连接对应的会话，并返回需关闭的目标连接（无则为 nil）。
func (t *sessionTable) closeClient(clientConn gnet.Conn) (destConn gnet.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sess, ok := t.byClient[clientConn]
	if !ok {
		return nil
	}
	delete(t.byClient, clientConn)
	if sess.destConn != nil {
		delete(t.byDest, sess.destConn)
		return sess.destConn
	}
	return nil
}

// removeByDest 依据目标连接清理双向映射，仅在反向映射仍指向该目标时删除正向映射，避免复用时误删。
func (t *sessionTable) removeByDest(destConn gnet.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()

	clientConn, ok := t.byDest[destConn]
	if !ok {
		return
	}
	delete(t.byDest, destConn)

	sess, ok := t.byClient[clientConn]
	if ok && sess.destConn == destConn {
		delete(t.byClient, clientConn)
	}
}

// clientByDest 依据目标连接查找对应的客户端连接。
func (t *sessionTable) clientByDest(destConn gnet.Conn) (gnet.Conn, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	clientConn, ok := t.byDest[destConn]
	return clientConn, ok
}
