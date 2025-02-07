package utils

import "github.com/panjf2000/gnet/v2"

func FormatGNetConn(conn gnet.Conn) string {
	return conn.RemoteAddr().String() + " -> " + conn.LocalAddr().String()
}
