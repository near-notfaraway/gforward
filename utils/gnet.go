package utils

import (
	"sync"

	"github.com/panjf2000/gnet/v2"
	"github.com/sirupsen/logrus"
)

// AsyncWrite 将写入提交到连接事件循环，并确保立即错误和回调错误只处理一次。
func AsyncWrite(conn gnet.Conn, buf []byte, logger *logrus.Entry, onError func()) error {
	var writeErrorOnce sync.Once
	handleResult := func(err error) {
		if err == nil {
			logger.Debugf("write buffer success: len %d", len(buf))
			return
		}
		writeErrorOnce.Do(func() {
			logger.Errorf("write buffer failed: %s", err)
			if onError != nil {
				onError()
			}
		})
	}

	err := conn.AsyncWrite(buf, func(_ gnet.Conn, err error) error {
		handleResult(err)
		return nil
	})
	if err != nil {
		handleResult(err)
	}
	return err
}
