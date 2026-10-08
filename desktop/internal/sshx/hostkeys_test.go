package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestKnownAlgorithms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewPublicKey(pub)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	line := knownhosts.Line([]string{knownhosts.Normalize("example.com:2222")}, key)
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := knownAlgorithms("example.com", 2222); !reflect.DeepEqual(got, []string{ssh.KeyAlgoED25519}) {
		t.Errorf("got %v", got)
	}
	if got := knownAlgorithms("other.com", 22); got != nil {
		t.Errorf("unknown host should use defaults, got %v", got)
	}
}
