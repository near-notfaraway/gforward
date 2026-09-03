package client

import (
	"sync"

	"github.com/panjf2000/gnet/v2"
)

// session 维护一个用户连接当前的服务端路由与拨号状态。
type session struct {
	sendMu     sync.Mutex // 串行化同一服务端连接上的上行写提交，避免 pending 回放与后续转发乱序
	dest       string     // 缓存的固定目标地址，PerRequest 模式下为空
	serverConn gnet.Conn  // 与服务端建立的连接，拨号完成前为 nil
	dialing    bool       // 是否正在异步拨号
	pending    [][]byte   // 拨号完成前按到达顺序缓存的上行 packet
}

// upstreamAction 标识 admitUpstream 在锁内做出的上行处理决策类型。
type upstreamAction uint8

const (
	upstreamActionQueued  upstreamAction = iota // 拨号进行中，packet 已入队，锁外无需 I/O
	upstreamActionForward                       // 命中就绪路由，锁外向服务端连接写入 packet
	upstreamActionDial                          // 首次上行，锁外发起异步拨号
)

// upstreamActionResult 承载 admitUpstream 的决策，供 forwarder 在锁外执行 I/O。
type upstreamActionResult struct {
	action upstreamAction // 决策类型

	serverConn gnet.Conn // upstreamActionForward：服务端连接
	packet     []byte    // upstreamActionForward：待转发的 packet（Marshal 分配新切片，锁外持有安全）
	session    *session  // upstreamActionForward / upstreamActionDial：当前会话，用于锁外串行化写提交或拨号 token
}

// sessionTable 维护用户连接与服务端连接之间的双向路由映射，并保证跨映射操作的原子性。
type sessionTable struct {
	mu       sync.RWMutex            // 保护双向映射的并发访问
	byUser   map[gnet.Conn]*session  // 用户连接到当前会话的映射
	byServer map[gnet.Conn]gnet.Conn // 服务端连接到用户连接的反向映射
}

func newSessionTable() *sessionTable {
	return &sessionTable{
		byUser:   make(map[gnet.Conn]*session),
		byServer: make(map[gnet.Conn]gnet.Conn),
	}
}

// admitUpstream 在锁内决定上行数据的排队、转发或拨号动作，并返回锁外应执行的 I/O 决策。
// cachedDest 仅在新建会话时写入 session.dest：非空表示希望后续包复用（TCP 模式），
// 空串表示 PerRequest 模式，每个包由 caller 重新解析目标。已存在会话时忽略该参数。
func (t *sessionTable) admitUpstream(userConn gnet.Conn, cachedDest string, packet []byte) upstreamActionResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	sess, ok := t.byUser[userConn]

	// 无会话：登记拨号中的会话并缓存首个 packet
	if !ok {
		newSess := &session{
			dest:    cachedDest,
			dialing: true,
			pending: [][]byte{packet},
		}
		t.byUser[userConn] = newSess
		return upstreamActionResult{action: upstreamActionDial, session: newSess}
	}

	// 拨号中：入队等待完成
	if sess.dialing {
		sess.pending = append(sess.pending, packet)
		return upstreamActionResult{action: upstreamActionQueued}
	}

	// 就绪：直接转发
	return upstreamActionResult{action: upstreamActionForward, serverConn: sess.serverConn, packet: packet, session: sess}
}

// getDest 返回用户连接对应会话缓存的目标地址；未建立会话或 PerRequest 模式下返回空串与 false。
func (t *sessionTable) getDest(userConn gnet.Conn) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	sess, ok := t.byUser[userConn]
	if !ok || sess.dest == "" {
		return "", false
	}
	return sess.dest, true
}

// completeDial 在拨号成功、服务端连接就绪时一次性完成路由收尾：注册反向映射、记录
// 服务端连接、翻转就绪并交出待回放的缓存数据。client 的连接就绪与下行数据同经 RecvChan
// 单 goroutine FIFO 处理，Open 必早于该连接的任何 Data，故注册与就绪可原子合并，无需分阶段。
// 调用方必须先持有 sess.sendMu，以保证 pending 回放先于后续 ready 转发提交。
// matched 为 false 表示会话已被关闭，调用方应关闭该连接。
func (t *sessionTable) completeDial(userConn gnet.Conn, sess *session, serverConn gnet.Conn) (pending [][]byte, matched bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cur, ok := t.byUser[userConn]
	if !ok || cur != sess {
		return nil, false
	}
	sess.serverConn = serverConn
	sess.dialing = false
	t.byServer[serverConn] = userConn
	pending = sess.pending
	sess.pending = nil
	return pending, true
}

// abortDial 在拨号失败（OnOpen 未触发、无服务端连接）时移除拨号中的会话。
// 带会话指针校验，仅当映射仍指向本次拨号的会话时才删除，避免用户连接复用后误删新会话。
// matched 为 false 表示会话已被关闭或替换，调用方无需再清理。
func (t *sessionTable) abortDial(userConn gnet.Conn, sess *session) (matched bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cur, ok := t.byUser[userConn]
	if !ok || cur != sess {
		return false
	}
	delete(t.byUser, userConn)
	return true
}

// purgeByUser 移除用户连接对应的会话，并返回需关闭的服务端连接（无则为 nil）。
func (t *sessionTable) purgeByUser(userConn gnet.Conn) (serverConn gnet.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sess, ok := t.byUser[userConn]
	if !ok {
		return nil
	}
	delete(t.byUser, userConn)
	if sess.serverConn != nil {
		delete(t.byServer, sess.serverConn)
		return sess.serverConn
	}
	return nil
}

// purgeByServer 依据服务端连接清理双向映射，并返回需关闭的用户连接（无则为 nil）。
// 仅在反向映射仍指向该服务端连接时清理正向映射，避免复用时误删。
func (t *sessionTable) purgeByServer(serverConn gnet.Conn) (userConn gnet.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()

	userConn, ok := t.byServer[serverConn]
	if !ok {
		return nil
	}
	delete(t.byServer, serverConn)

	sess, ok := t.byUser[userConn]
	if ok && sess.serverConn == serverConn {
		delete(t.byUser, userConn)
		return userConn
	}
	return nil
}

// getByServer 依据服务端连接查找对应的用户连接。
func (t *sessionTable) getByServer(serverConn gnet.Conn) (gnet.Conn, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	userConn, ok := t.byServer[serverConn]
	return userConn, ok
}
