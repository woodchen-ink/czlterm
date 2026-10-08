// Package conn 定义连接模型与本地 JSON 存储。
//
// 每个连接一个文件 (`data/connections/<id>.json`), 字段顺序固定、缩进输出:
// 数据目录是 git 仓库, 一连接一文件让多设备同时改不同连接时不冲突, diff 也可读。
// 文件里不含任何凭据, 凭据只以 Vaultwarden 条目 ID 的形式引用。
package conn

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// 协议取值。
const (
	ProtoSSH = "ssh"
	ProtoRDP = "rdp"
	ProtoVNC = "vnc"
)

// 凭据来源。
const (
	// AuthSystem 不提供凭据, 交给系统 ssh 的默认行为 (~/.ssh/config、系统 agent、默认私钥)。
	AuthSystem = "system"
	// AuthVault 从 Vaultwarden 条目取凭据: 登录条目取用户名与密码, SSH 密钥条目取私钥。
	AuthVault = "vault"
)

// Connection 是一条连接配置。
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	// Username 为空时取 Vaultwarden 登录条目里的用户名。
	Username string `json:"username"`
	Auth     string `json:"auth"`
	// VaultItem 是密码所在的 Vaultwarden 条目 (登录条目)。
	VaultItem string `json:"vaultItem"`
	// KeyItem 是私钥所在的 Vaultwarden 条目 (SSH 密钥条目), 仅 SSH 使用; 与 VaultItem 可同时设置。
	KeyItem string `json:"keyItem"`
	// Jump 是跳板机连接的 ID, 仅 SSH 使用。
	Jump  string `json:"jump"`
	Notes string `json:"notes"`
	// UpdatedAt 为 RFC3339 UTC 时间。
	UpdatedAt string `json:"updatedAt"`
}

// DefaultPort 返回协议的默认端口。
func DefaultPort(protocol string) int {
	switch protocol {
	case ProtoRDP:
		return 3389
	case ProtoVNC:
		return 5900
	default:
		return 22
	}
}

// Normalize 清理用户输入并补默认值, 返回校验错误。
func (c *Connection) Normalize() error {
	c.Name = strings.TrimSpace(c.Name)
	c.Group = strings.Trim(strings.TrimSpace(c.Group), "/")
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))

	switch c.Protocol {
	case ProtoSSH, ProtoRDP, ProtoVNC:
	default:
		return fmt.Errorf("2001 unsupported protocol %q", c.Protocol)
	}
	if c.Host == "" {
		return fmt.Errorf("2002 host is required")
	}
	// 以 - 开头会被 ssh / mstsc 当成命令行选项 (如 -oProxyCommand=...)。
	if strings.HasPrefix(c.Host, "-") || strings.HasPrefix(c.Username, "-") {
		return fmt.Errorf("2007 host or username must not start with '-'")
	}
	if strings.ContainsAny(c.Host, " \t\r\n'\"`$;&|<>") {
		return fmt.Errorf("2003 host contains invalid characters")
	}
	if strings.ContainsAny(c.Username, " \t\r\n'\"`$;&|<>") {
		return fmt.Errorf("2004 username contains invalid characters")
	}
	if c.Port == 0 {
		c.Port = DefaultPort(c.Protocol)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("2005 port out of range")
	}
	if c.Name == "" {
		c.Name = c.Host
	}
	if c.Auth != AuthVault {
		c.Auth = AuthSystem
		c.VaultItem, c.KeyItem = "", ""
	}
	if c.Protocol != ProtoSSH {
		c.Jump, c.KeyItem = "", ""
	}
	if c.Jump == c.ID && c.ID != "" {
		return fmt.Errorf("2006 connection cannot jump through itself")
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

// NewID 生成连接 ID: 16 位十六进制, 足够在个人规模下不冲突, 文件名也短。
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ValidID 判断 ID 能否安全地用作文件名。
func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r == '-') {
			return false
		}
	}
	return true
}
