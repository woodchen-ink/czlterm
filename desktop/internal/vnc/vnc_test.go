package vnc

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// serve 起一个把 WebSocket 交给会话的 httptest 服务器, 返回 ws:// 地址。
func serve(t *testing.T, s *Session) string {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.Serve(ws)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// listen 起一个 TCP 服务器, 每个连接交给 handle。
func listen(t *testing.T, handle func(net.Conn)) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestRelayForwardsBothWays(t *testing.T) {
	host, port := listen(t, func(c net.Conn) {
		defer c.Close()
		// 模拟 RFB: 服务器先发版本号, 再把收到的内容原样发回。
		_, _ = c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		_, _ = c.Write(buf[:n])
	})
	m := NewManager()
	s := m.Create(Target{Host: host, Port: port})
	ws, _, err := websocket.DefaultDialer.Dial(serve(t, s), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	_, got, err := ws.ReadMessage()
	if err != nil || string(got) != "RFB 003.008\n" {
		t.Fatalf("greeting = %q, %v", got, err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, got, err = ws.ReadMessage()
	if err != nil || string(got) != "hello" {
		t.Fatalf("echo = %q, %v", got, err)
	}
	// 服务器断开后 WebSocket 以正常关闭码结束。
	_, _, err = ws.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.CloseNormalClosure {
		t.Fatalf("close = %v", err)
	}
}

func TestDialFailureClosesWithCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // 端口已释放, 拨号被拒绝

	s := NewManager().Create(Target{Host: "127.0.0.1", Port: port})
	ws, _, err := websocket.DefaultDialer.Dial(serve(t, s), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_, _, err = ws.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != CloseDialFailed || !strings.HasPrefix(ce.Text, "4024 dial vnc server") {
		t.Fatalf("close = %v", err)
	}
}

func TestCloseDisconnectsActiveRelay(t *testing.T) {
	host, port := listen(t, func(c net.Conn) {
		defer c.Close()
		_, _ = c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 64)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	})
	m := NewManager()
	s := m.Create(Target{Host: host, Port: port, Password: "secret"})
	ws, _, err := websocket.DefaultDialer.Dial(serve(t, s), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, _, err := ws.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if n := m.Running(); n != 1 {
		t.Fatalf("running = %d, want 1", n)
	}
	m.ForgetPasswords()
	if got := s.Info().Password; got != "" {
		t.Fatalf("password after ForgetPasswords = %q", got)
	}

	m.Close(s.Info().ID)
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("relay still open after Close")
	}
	if _, err := m.Get(s.Info().ID); err == nil || !strings.HasPrefix(err.Error(), "4025") {
		t.Fatalf("get after close = %v", err)
	}
	if n := m.Running(); n != 0 {
		t.Fatalf("running = %d, want 0", n)
	}
}
