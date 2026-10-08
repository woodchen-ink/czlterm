// Package paths 解析程序自有文件的落点。
//
// 安装根目录: Windows `%LOCALAPPDATA%\CZL\czlterm`, macOS `~/Library/Application Support/CZL/czlterm`,
// Linux `~/.local/share/CZL/czlterm`。配置、连接数据、日志、缓存全部收敛在其下, 不写 Roaming。
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Product 是产品英文名, 与可执行文件名、安装目录名一致, 定名后不改。
const Product = "czlterm"

// InstanceEnv 设置后使用独立的数据目录与单实例锁, 开发调试时可与已安装的正式版同时运行。
const InstanceEnv = "CZLTERM_INSTANCE"

// Root 返回安装根目录, 不存在时创建。解析失败直接报错, 不回退到工作目录。
func Root() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("1001 LOCALAPPDATA is not set")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("1002 locate home dir: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("1002 locate home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	name := Product
	if v := os.Getenv(InstanceEnv); v != "" {
		name = Product + "-" + v
	}
	dir := filepath.Join(base, "CZL", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("1003 create app dir: %w", err)
	}
	return dir, nil
}

// Sub 返回安装根目录下的子目录 (config / data / logs / cache), 不存在时创建。
func Sub(name string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("1004 create %s dir: %w", name, err)
	}
	return dir, nil
}

// Config 是本机设置目录, 不参与同步。
func Config() (string, error) { return Sub("config") }

// Data 是连接数据目录, 整个目录是一个 git 仓库, 参与同步。
func Data() (string, error) { return Sub("data") }

// Logs 是日志目录。
func Logs() (string, error) { return Sub("logs") }

// Cache 是临时文件目录: 启动脚本、.rdp 文件、远程编辑的本地副本。
func Cache(name string) (string, error) {
	dir, err := Sub("cache")
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("1005 create cache dir: %w", err)
	}
	return dir, nil
}

// InstanceID 是单实例锁的标识。
func InstanceID() string {
	if v := os.Getenv(InstanceEnv); v != "" {
		return "net.czl.term." + v
	}
	return "net.czl.term"
}
