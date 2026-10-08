package conn

import (
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	c := Connection{Protocol: "SSH", Host: " h ", Auth: "bogus", VaultItem: "x"}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Port != 22 || c.Name != "h" || c.Auth != AuthSystem || c.VaultItem != "" {
		t.Errorf("unexpected %+v", c)
	}
	for _, bad := range []Connection{
		{Protocol: "ssh", Host: "-oProxyCommand=x"},
		{Protocol: "ssh", Host: "h;rm"},
		{Protocol: "ftp", Host: "h"},
		{Protocol: "ssh", Host: "h", Port: 70000},
	} {
		if err := bad.Normalize(); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}

func TestJumpChain(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Save(Connection{Protocol: "ssh", Host: "a"})
	b, err := s.Save(Connection{Protocol: "ssh", Host: "b", Jump: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	a.Jump = b.ID
	if _, err := s.Save(a); err == nil {
		t.Error("loop should be rejected")
	}
	if err := s.Delete(a.ID); err == nil {
		t.Error("deleting a jump host in use should fail")
	}
	if _, err := s.Get("../" + filepath.Base(a.ID)); err == nil {
		t.Error("path traversal id should be rejected")
	}
}
