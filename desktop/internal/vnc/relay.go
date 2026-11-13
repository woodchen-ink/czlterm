package vnc

import (
	"net"
	"time"

	"github.com/gorilla/websocket"
)

// writeTimeout 是单次写 WebSocket 的超时, 页面卡住时断开而不是无限堆积。
const writeTimeout = 15 * time.Second

// Relay 在 WebSocket 二进制帧与 TCP 字节流之间双向转发, 任一侧断开即关闭两侧后返回。
func Relay(ws *websocket.Conn, c net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64<<10)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				_ = ws.SetWriteDeadline(time.Now().Add(writeTimeout))
				if ws.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				// 服务器主动断开: 正常关闭 WebSocket, 页面据此显示已断开。
				closeWith(ws, websocket.CloseNormalClosure, "")
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			if _, err := c.Write(data); err != nil {
				return
			}
		}
	}()
	<-done
	_ = c.Close()
	_ = ws.Close()
	<-done
}

// closeWith 发送关闭帧。原因按协议上限 123 字节截断。
func closeWith(ws *websocket.Conn, code int, reason string) {
	if len(reason) > 123 {
		reason = reason[:123]
	}
	_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}
