package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestTrustHostKeyReplacesOnlyThatHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	path := filepath.Join(home, ".ssh", "known_hosts")

	const host = "203.0.113.5"
	oldKey, newHostKey, otherKey := newKey(t), newKey(t), newKey(t)
	original := strings.Join([]string{
		"# comment",
		knownhosts.Line([]string{host}, oldKey),
		knownhosts.Line([]string{knownhosts.HashHostname(host)}, oldKey),
		knownhosts.Line([]string{host, "alias.example"}, oldKey),
		knownhosts.Line([]string{"other.example"}, otherKey),
		knownhosts.Line([]string{"203.0.113.*"}, otherKey),
		"@revoked " + knownhosts.Line([]string{host}, otherKey),
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	p := NewPool()
	got := make(chan HostKeyChange, 1)
	p.SetHostKeyHandler(func(c HostKeyChange) { got <- c })
	remote := &net.TCPAddr{IP: net.ParseIP(host), Port: 22}
	err := p.hostKeyCallback(host+":22", remote, newHostKey)
	if err == nil || !strings.HasPrefix(err.Error(), "5006 ") {
		t.Fatalf("want 5006, got %v", err)
	}
	var change HostKeyChange
	select {
	case change = <-got:
	case <-time.After(time.Second):
		t.Fatal("host key handler not called")
	}
	if change.Host != host || change.Fingerprint != ssh.FingerprintSHA256(newHostKey) || len(change.Known) == 0 {
		t.Fatalf("unexpected change %+v", change)
	}

	if err := p.TrustHostKey(host, ssh.FingerprintSHA256(otherKey)); err == nil || !strings.HasPrefix(err.Error(), "5009 ") {
		t.Fatalf("mismatched fingerprint should be refused, got %v", err)
	}
	if err := p.TrustHostKey(host, change.Fingerprint); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	oldB64 := strings.Fields(knownhosts.Line(nil, oldKey))[1]
	if strings.Contains(text, oldB64) && !strings.Contains(text, "alias.example "+oldKey.Type()+" "+oldB64) {
		t.Errorf("old key should only remain for alias.example:\n%s", text)
	}
	for _, want := range []string{"# comment", "other.example", "203.0.113.*", "@revoked", "alias.example"} {
		if !strings.Contains(text, want) {
			t.Errorf("unrelated entry %q lost:\n%s", want, text)
		}
	}
	if backup, _ := os.ReadFile(path + ".old"); string(backup) != original {
		t.Error("known_hosts.old should hold the original file")
	}

	if err := p.hostKeyCallback(host+":22", remote, newHostKey); err != nil {
		t.Errorf("new key should be accepted after trust: %v", err)
	}
	if err := p.TrustHostKey(host, change.Fingerprint); err == nil {
		t.Error("a trusted change should not be reusable")
	}
}
