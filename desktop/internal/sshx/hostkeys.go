package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	probeOnce sync.Once
	probeKey  ssh.PublicKey
)

// knownAlgorithms 返回 known_hosts 里为该主机记录的主机密钥算法, 没有记录时返回 nil (用库默认顺序)。
//
// Go 的 ssh 库默认优先协商 ECDSA, OpenSSH 优先 ed25519; known_hosts 里只有 ed25519 时,
// 不限定算法就会拿到一把"不认识"的 ECDSA 密钥并误报主机密钥不一致。
func knownAlgorithms(host string, port int) []string {
	path, err := knownHostsPath()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil
	}
	probeOnce.Do(func() {
		pub, _, _ := ed25519.GenerateKey(rand.Reader)
		probeKey, _ = ssh.NewPublicKey(pub)
	})
	if probeKey == nil {
		return nil
	}
	// 用一把随机密钥去查: 必然不匹配, KeyError.Want 里就是该主机已记录的全部密钥。
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	remote := &net.TCPAddr{IP: net.ParseIP(host), Port: port}
	if remote.IP == nil {
		remote.IP = net.IPv4zero
	}
	var ke *knownhosts.KeyError
	if err := cb(addr, remote, probeKey); !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	var algos []string
	seen := map[string]bool{}
	add := func(a ...string) {
		for _, x := range a {
			if !seen[x] {
				seen[x] = true
				algos = append(algos, x)
			}
		}
	}
	for _, k := range ke.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return algos
}
