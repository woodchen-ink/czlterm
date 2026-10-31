package sshx

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyChange 描述一次主机密钥不符: 服务器出示的密钥与 known_hosts 里的记录不同。
type HostKeyChange struct {
	// Host 是 known_hosts 中的主机写法, 非 22 端口为 [host]:port。
	Host        string   `json:"host"`
	KeyType     string   `json:"keyType"`
	Fingerprint string   `json:"fingerprint"`
	Known       []string `json:"known"`
}

// SetHostKeyHandler 设置主机密钥不符时的回调, 由界面提示用户确认是否信任新密钥。
func (p *Pool) SetHostKeyHandler(fn func(HostKeyChange)) {
	p.hostsMu.Lock()
	p.onHostKey = fn
	p.hostsMu.Unlock()
}

// hostKeyCallback 按 known_hosts 校验, 未知主机记录后放行。
func (p *Pool) hostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}
	p.hostsMu.Lock()
	defer p.hostsMu.Unlock()

	if _, err := os.Stat(path); err == nil {
		cb, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("5005 read known_hosts: %w", err)
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) || len(ke.Want) > 0 {
			if ke != nil && len(ke.Want) > 0 {
				p.recordChangeLocked(knownhosts.Normalize(hostname), key, ke.Want)
			}
			return fmt.Errorf("5006 host key mismatch for %s, check ~/.ssh/known_hosts: %w", hostname, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("5007 write known_hosts: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n")
	return err
}

// recordChangeLocked 记下服务器出示的新密钥, 供用户确认后写入; 只信任实际握手中见过的那一把。
func (p *Pool) recordChangeLocked(host string, key ssh.PublicKey, want []knownhosts.KnownKey) {
	p.changes[host] = key
	if p.onHostKey == nil {
		return
	}
	c := HostKeyChange{Host: host, KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key)}
	for _, k := range want {
		c.Known = append(c.Known, ssh.FingerprintSHA256(k.Key))
	}
	// 回调可能要很久 (发事件、写日志), 不占着 known_hosts 的锁。
	go p.onHostKey(c)
}

// TrustHostKey 用最近一次握手见到的新密钥替换 host 在 known_hosts 里的记录。
// fingerprint 必须与当时提示给用户的一致, 防止确认期间服务器又换了密钥。
func (p *Pool) TrustHostKey(host, fingerprint string) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}
	p.hostsMu.Lock()
	defer p.hostsMu.Unlock()
	key := p.changes[host]
	if key == nil || ssh.FingerprintSHA256(key) != fingerprint {
		return errors.New("5009 host key change not found, connect again to re-check")
	}
	if err := replaceKnownHost(path, host, key); err != nil {
		return err
	}
	delete(p.changes, host)
	return nil
}

// replaceKnownHost 删除 host 的全部记录 (含哈希写法与多主机行中的该项) 后追加 key, 等同 ssh-keygen -R 再接受新密钥。
// 原文件备份为 known_hosts.old; 通配符、@cert-authority 与 @revoked 行保持不动。
func replaceKnownHost(path, host string, key ssh.PublicKey) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("5005 read known_hosts: %w", err)
	}
	var out bytes.Buffer
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line == "" {
			continue
		}
		out.WriteString(dropHost(line, host))
	}
	if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteByte('\n')
	}
	out.WriteString(knownhosts.Line([]string{host}, key) + "\n")

	if len(data) > 0 {
		if err := os.WriteFile(path+".old", data, 0o600); err != nil {
			return fmt.Errorf("5007 write known_hosts: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o600); err != nil {
		return fmt.Errorf("5007 write known_hosts: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("5007 write known_hosts: %w", err)
	}
	return nil
}

// dropHost 返回去掉 host 之后的行; 整行只属于 host 时返回空串, 无关行原样返回。
func dropHost(line, host string) string {
	marker, hosts, _, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil || marker != "" {
		// 注释、空行与解析不了的行都原样保留。
		return line
	}
	var keep []string
	for _, h := range hosts {
		if !hostMatches(h, host) {
			keep = append(keep, h)
		}
	}
	if len(keep) == len(hosts) {
		return line
	}
	if len(keep) == 0 {
		return ""
	}
	// 主机列表是行首第一个字段, 后面的密钥与注释原样保留。
	trimmed := strings.TrimLeft(line, " \t")
	i := strings.IndexAny(trimmed, " \t")
	if i < 0 {
		return line
	}
	return strings.Join(keep, ",") + trimmed[i:]
}

// hostMatches 判断 known_hosts 主机字段是否精确指向 host; 通配符与否定模式不算。
func hostMatches(field, host string) bool {
	if strings.HasPrefix(field, "|1|") {
		parts := strings.Split(field[3:], "|")
		if len(parts) != 2 {
			return false
		}
		salt, err1 := base64.StdEncoding.DecodeString(parts[0])
		want, err2 := base64.StdEncoding.DecodeString(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		mac := hmac.New(sha1.New, salt)
		mac.Write([]byte(host))
		return hmac.Equal(mac.Sum(nil), want)
	}
	if strings.ContainsAny(field, "*?!") {
		return false
	}
	return knownhosts.Normalize(field) == host
}

func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("5008 locate home dir: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}
