//go:build !windows

package agent

import (
	"net"
	"os"
	"path/filepath"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// listen 在私有临时目录下创建 Unix socket (0600)。
func listen(name string) (string, net.Listener, error) {
	dir, err := paths.PrivateTemp()
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, name+".sock")
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, err
	}
	_ = os.Chmod(path, 0o600)
	return path, ln, nil
}

func cleanup(path string) { _ = os.Remove(path) }

// defaultUpstream 为空: macOS / Linux 的系统 agent 总是经 SSH_AUTH_SOCK 告知。
const defaultUpstream = ""

func dialUpstream(sock string) (net.Conn, error) { return net.Dial("unix", sock) }
