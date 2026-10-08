package remotefs

import (
	"io"
	"io/fs"
	"net"
	"testing"

	"github.com/pkg/sftp"
)

// memClient 返回连到内存 SFTP 服务器的客户端。
func memClient(t *testing.T) *sftp.Client {
	t.Helper()
	cs, ss := net.Pipe()
	srv := sftp.NewRequestServer(ss, sftp.InMemHandler())
	go srv.Serve()
	c, err := sftp.NewClientPipe(cs, cs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); srv.Close() })
	return c
}

func read(t *testing.T, c *sftp.Client, p string) string {
	t.Helper()
	f, err := c.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

func TestWriteFile(t *testing.T) {
	c := memClient(t)
	if err := WriteFile(c, "/a.txt", []byte("one")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := WriteFile(c, "/a.txt", []byte("two")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := read(t, c, "/a.txt"); got != "two" {
		t.Fatalf("got %q", got)
	}
	l, err := List(c, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 {
		t.Fatalf("temp files left behind: %+v", l.Entries)
	}
}

func TestWriteFileFollowsSymlink(t *testing.T) {
	c := memClient(t)
	if err := WriteFile(c, "/real.conf", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := c.Symlink("/real.conf", "/link.conf"); err != nil {
		t.Skipf("in-memory server lacks symlinks: %v", err)
	}
	if err := WriteFile(c, "/link.conf", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, c, "/real.conf"); got != "new" {
		t.Fatalf("target not updated: %q", got)
	}
	fi, err := c.Lstat("/link.conf")
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("link replaced by regular file: %v %v", fi.Mode(), err)
	}
}
