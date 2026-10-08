// Package facts 采集并保存服务器的机器配置与系统信息 (系统、内核、CPU、内存、磁盘、运行时长)。
//
// 每次 SSH 连接后在后台经进程内 SSH 采集一次。稳定信息 (系统、硬件) 写进 `data/facts/<id>.json`
// 随连接一起同步, 只在内容变化时重写, 不让每次连接都产生一个 git 提交; 易变信息 (已用磁盘、
// 可用内存、运行时长、负载) 只写本机 `cache/facts/<id>.json`。
package facts

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/sshx"
)

// Stable 是不常变化、参与同步的部分。
type Stable struct {
	// OSID 取 /etc/os-release 的 ID (ubuntu / debian / centos …), macOS 为 macos, Windows 为 windows。
	OSID        string `json:"osId"`
	OSLike      string `json:"osLike"`
	OSName      string `json:"osName"`
	OSVersion   string `json:"osVersion"`
	Kernel      string `json:"kernel"`
	Arch        string `json:"arch"`
	Hostname    string `json:"hostname"`
	CPU         string `json:"cpu"`
	Cores       int    `json:"cores"`
	MemoryBytes int64  `json:"memoryBytes"`
	DiskBytes   int64  `json:"diskBytes"`
}

// Volatile 是每次都在变、只存本机的部分。
type Volatile struct {
	MemoryAvailableBytes int64  `json:"memoryAvailableBytes"`
	DiskUsedBytes        int64  `json:"diskUsedBytes"`
	UptimeSeconds        int64  `json:"uptimeSeconds"`
	Load                 string `json:"load"`
}

// Facts 是一台机器的完整信息。
type Facts struct {
	Stable
	Volatile
	// CollectedAt 为 RFC3339 UTC 时间, 为空表示从未采集。
	CollectedAt string `json:"collectedAt"`
}

// unixScript 不能让任何一步失败中断整个脚本: POSIX sh 里 "." 一个不存在的文件会直接退出。
const unixScript = `[ -r /etc/os-release ] && . /etc/os-release
echo "os_id=${ID:-}"
echo "os_like=${ID_LIKE:-}"
echo "os_name=${PRETTY_NAME:-}"
echo "os_version=${VERSION_ID:-}"
if [ "$(uname -s)" = Darwin ]; then
  v=$(sw_vers -productVersion 2>/dev/null)
  echo "os_id=macos"; echo "os_name=macOS $v"; echo "os_version=$v"
fi
if [ "$(uname -s)" = FreeBSD ]; then echo "os_id=freebsd"; echo "os_name=FreeBSD $(uname -r)"; fi
echo "kernel=$(uname -sr)"
echo "arch=$(uname -m)"
echo "hostname=$(hostname 2>/dev/null || uname -n)"
cpu=$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2-)
[ -z "$cpu" ] && cpu=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || sysctl -n hw.model 2>/dev/null)
echo "cpu=$cpu"
echo "cores=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null)"
echo "memory_kb=$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null)"
echo "memory_available_kb=$(awk '/^MemAvailable:/{print $2}' /proc/meminfo 2>/dev/null)"
echo "memory_bytes=$(sysctl -n hw.memsize 2>/dev/null || sysctl -n hw.physmem 2>/dev/null)"
df -Pk / 2>/dev/null | awk 'NR==2{print "disk_total_kb="$2; print "disk_used_kb="$3}'
up=$(cut -d. -f1 /proc/uptime 2>/dev/null)
if [ -z "$up" ]; then
  b=$(sysctl -n kern.boottime 2>/dev/null | sed -n 's/.*sec = \([0-9]*\).*/\1/p')
  [ -n "$b" ] && up=$(( $(date +%s) - b ))
fi
echo "uptime=$up"
echo "load=$(cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null | tr -d '{}')"
exit 0
`

const windowsScript = `$ErrorActionPreference = 'SilentlyContinue'
$o = Get-CimInstance Win32_OperatingSystem
$c = Get-CimInstance Win32_Processor | Select-Object -First 1
$d = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='$($env:SystemDrive)'"
"os_id=windows"
"os_name=$($o.Caption)"
"os_version=$($o.Version)"
"kernel=Windows NT $($o.Version)"
"arch=$($o.OSArchitecture)"
"hostname=$env:COMPUTERNAME"
"cpu=$($c.Name)"
"cores=$($c.NumberOfLogicalProcessors)"
"memory_kb=$($o.TotalVisibleMemorySize)"
"memory_available_kb=$($o.FreePhysicalMemory)"
"disk_total_kb=$([int64]($d.Size / 1024))"
"disk_used_kb=$([int64](($d.Size - $d.FreeSpace) / 1024))"
"uptime=$([int64]((Get-Date) - $o.LastBootUpTime).TotalSeconds)"
`

// Collect 在远程执行采集脚本。先按 POSIX shell 跑; 没有 sh 的 Windows 服务器退回 PowerShell。
func Collect(ctx context.Context, client *ssh.Client) (Facts, error) {
	res, err := sshx.Exec(ctx, client, "sh -s", strings.NewReader(unixScript), 64<<10)
	if err == nil && strings.Contains(res.Stdout, "kernel=") {
		return parse(res.Stdout), nil
	}
	res, err2 := sshx.Exec(ctx, client, "powershell -NoLogo -NoProfile -NonInteractive -Command -", strings.NewReader(windowsScript), 64<<10)
	if err2 == nil && strings.Contains(res.Stdout, "os_id=windows") {
		return parse(res.Stdout), nil
	}
	if err == nil {
		err = err2
	}
	if err == nil {
		err = errors.New("unrecognized output")
	}
	return Facts{}, fmt.Errorf("5201 collect system info: %w", err)
}

// parse 解析 key=value 行。同一个键出现多次时后者覆盖前者 (macOS 段覆盖 os-release 段)。
func parse(out string) Facts {
	kv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimRight(sc.Text(), "\r"), "=")
		if ok {
			if v = strings.TrimSpace(v); v != "" {
				kv[k] = v
			}
		}
	}
	num := func(k string) int64 {
		n, _ := strconv.ParseInt(strings.Fields(kv[k] + " 0")[0], 10, 64)
		return n
	}
	f := Facts{CollectedAt: time.Now().UTC().Format(time.RFC3339)}
	f.OSID = strings.ToLower(kv["os_id"])
	f.OSLike = strings.ToLower(kv["os_like"])
	f.OSName = kv["os_name"]
	f.OSVersion = kv["os_version"]
	f.Kernel = kv["kernel"]
	f.Arch = kv["arch"]
	f.Hostname = kv["hostname"]
	f.CPU = strings.Join(strings.Fields(kv["cpu"]), " ")
	f.Cores = int(num("cores"))
	f.MemoryBytes = num("memory_kb") * 1024
	if f.MemoryBytes == 0 {
		f.MemoryBytes = num("memory_bytes")
	}
	f.DiskBytes = num("disk_total_kb") * 1024
	f.MemoryAvailableBytes = num("memory_available_kb") * 1024
	f.DiskUsedBytes = num("disk_used_kb") * 1024
	f.UptimeSeconds = num("uptime")
	f.Load = kv["load"]
	if f.OSName == "" && f.Kernel != "" {
		f.OSName = f.Kernel
	}
	return f
}

// Store 读写 facts 文件。
type Store struct {
	dataDir, cacheDir string
	mu                sync.Mutex
}

// Open 打开存储, 目录不存在时创建。
func Open(dataDir, cacheDir string) (*Store, error) {
	s := &Store{dataDir: filepath.Join(dataDir, "facts"), cacheDir: filepath.Join(cacheDir, "facts")}
	for _, d := range []string{s.dataDir, s.cacheDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("5202 create facts dir: %w", err)
		}
	}
	return s, nil
}

// Get 返回一台机器的信息; 从未采集时返回零值。本机缓存优先, 没有时用同步来的稳定信息。
func (s *Store) Get(id string) Facts {
	if !conn.ValidID(id) {
		return Facts{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var f Facts
	if readJSON(filepath.Join(s.cacheDir, id+".json"), &f) == nil {
		return f
	}
	var st struct {
		Stable
		CollectedAt string `json:"collectedAt"`
	}
	if readJSON(filepath.Join(s.dataDir, id+".json"), &st) == nil {
		f.Stable, f.CollectedAt = st.Stable, st.CollectedAt
	}
	return f
}

// Save 保存采集结果。返回稳定信息是否有变化 (调用方据此决定是否触发同步)。
func (s *Store) Save(id string, f Facts) (bool, error) {
	if !conn.ValidID(id) {
		return false, errors.New("2014 invalid connection id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSON(filepath.Join(s.cacheDir, id+".json"), f); err != nil {
		return false, err
	}
	var prev struct {
		Stable
	}
	path := filepath.Join(s.dataDir, id+".json")
	if readJSON(path, &prev) == nil && reflect.DeepEqual(prev.Stable, f.Stable) {
		return false, nil
	}
	return true, writeJSON(path, struct {
		Stable
		CollectedAt string `json:"collectedAt"`
	}{f.Stable, f.CollectedAt})
}

// Delete 删除一台机器的信息。连接删除时调用。
func (s *Store) Delete(id string) {
	if !conn.ValidID(id) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	os.Remove(filepath.Join(s.cacheDir, id+".json"))
	os.Remove(filepath.Join(s.dataDir, id+".json"))
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("5203 write facts: %w", err)
	}
	return os.Rename(tmp, path)
}
