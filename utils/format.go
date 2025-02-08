package utils

import "github.com/panjf2000/gnet/v2"

func FormatGNetConn(conn gnet.Conn) string {
	remoteAddrStr := "<nil>"
	if conn.RemoteAddr() != nil {
		remoteAddrStr = conn.RemoteAddr().String()
	}
	localAddrStr := "<nil>"
	if conn.LocalAddr() != nil {
		localAddrStr = conn.LocalAddr().String()
	}
	return localAddrStr + "->" + remoteAddrStr
}
