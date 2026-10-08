package facts

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestParseLinux(t *testing.T) {
	out := "os_id=ubuntu\nos_like=debian\nos_name=Ubuntu 24.04.1 LTS\nos_version=24.04\nkernel=Linux 6.8.0\narch=x86_64\n" +
		"hostname=web-01\ncpu=  Intel(R) Xeon(R)   CPU\ncores=8\nmemory_kb=16384000\nmemory_available_kb=8000000\n" +
		"memory_bytes=\ndisk_total_kb=100000\ndisk_used_kb=25000\nuptime=3600\nload=0.10 0.20 0.30\n"
	f := parse(out)
	if f.OSID != "ubuntu" || f.Cores != 8 || f.MemoryBytes != 16384000*1024 || f.DiskUsedBytes != 25000*1024 || f.CPU != "Intel(R) Xeon(R) CPU" {
		t.Fatalf("unexpected %+v", f)
	}
}

// 本机 shell 跑一遍采集脚本, 确认脚本本身在 macOS / Linux 上不报错且能解析出关键字段。
func TestUnixScriptLocally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(unixScript)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	f := parse(string(out))
	if f.Kernel == "" || f.Arch == "" || f.Cores == 0 || f.MemoryBytes == 0 || f.DiskBytes == 0 {
		t.Fatalf("missing fields: %+v\n%s", f, out)
	}
	if runtime.GOOS == "darwin" && f.OSID != "macos" {
		t.Fatalf("os id = %q", f.OSID)
	}
}

func TestStoreOnlyRewritesStableOnChange(t *testing.T) {
	s, err := Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := Facts{Stable: Stable{OSID: "debian", Cores: 2}, Volatile: Volatile{UptimeSeconds: 1}}
	if changed, _ := s.Save("abc", f); !changed {
		t.Fatal("first save should change")
	}
	f.UptimeSeconds = 99
	if changed, _ := s.Save("abc", f); changed {
		t.Fatal("volatile change must not rewrite synced file")
	}
	if got := s.Get("abc"); got.UptimeSeconds != 99 || got.OSID != "debian" {
		t.Fatalf("get = %+v", got)
	}
}
