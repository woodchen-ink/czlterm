package launch

import (
	"strings"
	"testing"
)

// TestSSHProxyEnvironment 验证父进程的密码及 agent 状态不能跨跳泄漏。
func TestSSHProxyEnvironment(t *testing.T) {
	inherited := []string{"PATH=/bin", "ssh_auth_sock=/target-vault.sock", "ssh_askpass=target-helper", "CZLTERM_ASKPASS_TOKEN=target-token", "CZLTERM_ASKPASS_URL=target-url", "SSH_ASKPASS_REQUIRE=force", "DISPLAY=:0"}
	for _, tc := range []struct {
		name         string
		scoped       []string
		want, absent []string
	}{
		{"original-agent", []string{"SSH_AUTH_SOCK=/original.sock"}, []string{"PATH=/bin", "SSH_AUTH_SOCK=/original.sock"}, []string{"target-vault", "target-token", "target-helper", "target-url", "DISPLAY", "SSH_ASKPASS_REQUIRE"}},
		{"originally-absent", nil, []string{"PATH=/bin"}, []string{"SSH_AUTH_SOCK", "ssh_auth_sock", "target-token", "target-helper", "target-url"}},
		{"own-password", []string{"SSH_ASKPASS=own-helper", "CZLTERM_ASKPASS_TOKEN=own-token"}, []string{"own-helper", "own-token"}, []string{"target-token", "target-helper", "target-vault"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(sshProxyEnvironment(inherited, tc.scoped), "\n")
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("leaked %q in %s", absent, got)
				}
			}
		})
	}
}

// TestSSHAgentPathTokens 防止本地路径被 OpenSSH 当成环境变量或百分号 token 展开。
func TestSSHAgentPathTokens(t *testing.T) {
	body, err := sshHopConfig(SSHHop{Vault: true, Agent: "/tmp/100%/key.sock"}, "")
	if err != nil || !strings.Contains(body, "100%%/key.sock") {
		t.Fatalf("percent escape: %s %v", body, err)
	}
	if _, err := sshHopConfig(SSHHop{Vault: true, Agent: "/tmp/${HOME}/key.sock"}, ""); err == nil {
		t.Fatal("environment substitution must be rejected")
	}
}
