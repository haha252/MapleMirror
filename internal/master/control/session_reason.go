package control

import "net"

func controlReadCloseReason(err error) string {
	if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
		return "控制连接超时"
	}
	return err.Error()
}
