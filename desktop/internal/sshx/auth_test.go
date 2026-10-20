package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestVaultAuthNeverFallsBack 验证保险库凭据为空时拒绝连接, 有凭据时只暴露指定认证方法。
func TestVaultAuthNeverFallsBack(t *testing.T) {
	p := NewPool()
	if _, err := p.config(Hop{Vault: true}); err == nil || !strings.HasPrefix(err.Error(), "4108 ") {
		t.Fatalf("empty vault credentials must fail closed: %v", err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]ssh.AuthMethod{
		"password":             ssh.Password("example"),
		"publickey":            ssh.PublicKeys(signer),
		"keyboard-interactive": ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) { return nil, nil }),
	}
	for _, tc := range []struct {
		name string
		hop  Hop
		want []string
	}{
		{"password", Hop{Vault: true, Password: "example"}, []string{"password", "keyboard-interactive"}},
		{"key", Hop{Vault: true, Signers: []ssh.Signer{signer}}, []string{"publickey"}},
		{"key-and-password", Hop{Vault: true, Password: "example", Signers: []ssh.Signer{signer}}, []string{"publickey", "password", "keyboard-interactive"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := p.config(tc.hop)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Auth) != len(tc.want) {
				t.Fatalf("auth methods = %d, want %d", len(cfg.Auth), len(tc.want))
			}
			for i, method := range cfg.Auth {
				if reflect.TypeOf(method) != reflect.TypeOf(expected[tc.want[i]]) {
					t.Errorf("method type = %T, want %s", method, tc.want[i])
				}
			}
		})
	}
}

// TestChainKeyIncludesAuthSource 防止切换到空保险库凭据后复用先前的系统认证连接。
func TestChainKeyIncludesAuthSource(t *testing.T) {
	system := chainKey([]Hop{{Host: "example.test", Port: 22, User: "wood"}})
	vault := chainKey([]Hop{{Host: "example.test", Port: 22, User: "wood", Vault: true}})
	if system == vault {
		t.Fatal("credential source missing from pool key")
	}
}
