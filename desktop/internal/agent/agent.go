// Package agent 在进程内为每个连接开一个独立的 ssh-agent 端点, 供系统 ssh 通过 SSH_AUTH_SOCK 使用。
//
// 私钥从 Vaultwarden 解密后只放在这里的内存 keyring 中, 不写磁盘, 也不进系统 ssh-agent。
// 一连接一端点: 每个端点只持有该连接 (含跳板链) 的私钥, ssh 不会逐个尝试一堆无关的密钥而撞上服务器的 MaxAuthTries。
// 端点在保险库锁定或程序退出时全部关闭。
//
// macOS / Linux 用 Unix socket, 放在系统临时目录下的私有子目录 (安装目录路径含空格且可能超过
// socket 路径的 104 字节上限); Windows 用仅当前用户可访问的命名管道。
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// Server 管理所有端点。
type Server struct {
	mu        sync.Mutex
	endpoints map[string]*endpoint
}

type endpoint struct {
	path    string
	ln      net.Listener
	keyring sshagent.Agent
	// fingerprint 用于判断同一连接换了密钥时需要替换 keyring 内容。
	fingerprint string
}

// New 创建 Server。
func New() *Server {
	return &Server{endpoints: map[string]*endpoint{}}
}

// Key 是要放进端点的一把私钥。
type Key struct {
	PrivateKey string
	Passphrase string
	Comment    string
}

// Serve 确保 id 对应的端点在运行且恰好持有 keys, 返回给 SSH_AUTH_SOCK 用的路径。
// id 取目标连接的 ID; 跳板链上各跳的私钥放在同一个端点里, 因为 ProxyJump 拉起的子 ssh
// 只继承环境变量, 不继承命令行上的 -o IdentityAgent。
func (s *Server) Serve(id string, keys []Key) (string, error) {
	var added []sshagent.AddedKey
	var fps []string
	for _, k := range keys {
		raw, err := ParseKey(k.PrivateKey, k.Passphrase)
		if err != nil {
			return "", err
		}
		signer, err := ssh.NewSignerFromKey(raw)
		if err != nil {
			return "", fmt.Errorf("4101 unsupported private key: %w", err)
		}
		added = append(added, sshagent.AddedKey{PrivateKey: raw, Comment: k.Comment})
		fps = append(fps, ssh.FingerprintSHA256(signer.PublicKey()))
	}
	fp := strings.Join(fps, ",")

	s.mu.Lock()
	defer s.mu.Unlock()

	ep := s.endpoints[id]
	if ep != nil && ep.fingerprint == fp {
		return ep.path, nil
	}
	if ep == nil {
		path, ln, err := listen(id + "-" + randHex(4))
		if err != nil {
			return "", fmt.Errorf("4102 start agent endpoint: %w", err)
		}
		ep = &endpoint{path: path, ln: ln, keyring: &chainAgent{local: sshagent.NewKeyring().(sshagent.ExtendedAgent)}}
		s.endpoints[id] = ep
		go acceptLoop(ep)
	}
	_ = ep.keyring.RemoveAll()
	for _, k := range added {
		if err := ep.keyring.Add(k); err != nil {
			return "", fmt.Errorf("4103 add key to agent: %w", err)
		}
	}
	ep.fingerprint = fp
	return ep.path, nil
}

// CloseAll 关闭全部端点并清空密钥。保险库锁定与程序退出时调用。
func (s *Server) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ep := range s.endpoints {
		_ = ep.keyring.RemoveAll()
		_ = ep.ln.Close()
		cleanup(ep.path)
		delete(s.endpoints, id)
	}
}

// ParseKey 解析 OpenSSH / PEM 私钥, 有口令时用 passphrase 解密。
func ParseKey(privateKey, passphrase string) (any, error) {
	if privateKey == "" {
		return nil, errors.New("4104 vault item has no private key")
	}
	key, err := ssh.ParseRawPrivateKey([]byte(privateKey))
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		if passphrase == "" {
			return nil, errors.New("4105 private key is encrypted, add a 'passphrase' field to the vault item")
		}
		key, err = ssh.ParseRawPrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	}
	if err != nil {
		return nil, fmt.Errorf("4106 parse private key: %w", err)
	}
	return key, nil
}

// acceptLoop 为每个连入的 ssh 进程提供 agent 协议服务, 监听关闭时退出。
func acceptLoop(ep *endpoint) {
	for {
		c, err := ep.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			_ = sshagent.ServeAgent(ep.keyring, c)
		}()
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
