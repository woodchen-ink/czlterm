//go:build !windows

package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// 端点能列出并用 Vaultwarden 私钥签名; 锁定后端点关闭。
func TestServeAndClose(t *testing.T) {
	upstreamSock := filepath.Join(t.TempDir(), "upstream.sock")
	t.Setenv("SSH_AUTH_SOCK", upstreamSock)
	upstreamKeys := sshagent.NewKeyring()
	_, unrelated, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := upstreamKeys.Add(sshagent.AddedKey{PrivateKey: unrelated}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", upstreamSock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _ = sshagent.ServeAgent(upstreamKeys, conn) }()
		}
	}()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	sock, err := s.Serve("conn1", []Key{{PrivateKey: string(pem.EncodeToMemory(block)), Comment: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	client := sshagent.NewClient(c)
	keys, err := client.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("list: %v %v", keys, err)
	}
	if _, err := client.Sign(keys[0], []byte("data")); err != nil {
		t.Fatalf("sign: %v", err)
	}
	c.Close()

	// 同一连接、同一把钥匙再次 Serve 复用端点。
	again, _ := s.Serve("conn1", []Key{{PrivateKey: string(pem.EncodeToMemory(block))}})
	if again != sock {
		t.Fatalf("endpoint not reused: %s vs %s", again, sock)
	}

	s.CloseAll()
	if _, err := net.Dial("unix", sock); err == nil {
		t.Fatal("endpoint should be closed after CloseAll")
	}
}

func TestEncryptedKeyNeedsPassphrase(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("pw"))
	pemKey := string(pem.EncodeToMemory(block))
	if _, err := ParseKey(pemKey, ""); err == nil {
		t.Fatal("expected passphrase error")
	}
	if _, err := ParseKey(pemKey, "pw"); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
}
