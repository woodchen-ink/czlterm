package launch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSSHHopConfig 验证保险库只启用所选认证, 不读取系统配置。
func TestSSHHopConfig(t *testing.T) {
	for _, tc := range []struct {
		name         string
		hop          SSHHop
		want, absent []string
	}{
		{"password", SSHHop{Vault: true, Password: true}, []string{"IdentityFile none", "IdentityAgent none", "PubkeyAuthentication no", "PreferredAuthentications keyboard-interactive,password"}, []string{"Include", "publickey", "PasswordAuthentication no"}},
		{"key", SSHHop{Vault: true, Agent: "/tmp/vault.sock"}, []string{"IdentityFile none", `IdentityAgent "/tmp/vault.sock"`, "PasswordAuthentication no", "PreferredAuthentications publickey"}, []string{"Include", "IdentityAgent none", "PasswordAuthentication yes"}},
		{"key-and-password", SSHHop{Vault: true, Agent: "/tmp/vault.sock", Password: true}, []string{"PreferredAuthentications publickey,keyboard-interactive,password"}, []string{"Include", "IdentityAgent none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.hop.Port, tc.hop.User = 2222, "wood"
			body, err := sshHopConfig(tc.hop, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range append(tc.want, "Port 2222", `User "wood"`, "ServerAliveInterval 30") {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in:\n%s", want, body)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(body, absent) {
					t.Errorf("unexpected %q in:\n%s", absent, body)
				}
			}
		})
	}
	if _, err := sshHopConfig(SSHHop{Vault: true}, ""); err == nil || !strings.HasPrefix(err.Error(), "4108 ") {
		t.Fatalf("empty vault credentials must fail closed: %v", err)
	}
}

// TestSystemSSHArgs 保留系统原生配置上下文, 不注入任何认证选项。
func TestSystemSSHArgs(t *testing.T) {
	args := systemSSHArgs(SSHHop{Host: "example.test", Port: 2222, User: "wood"}, "helper-command")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p 2222", "-l wood", "ServerAliveInterval=30", "ProxyCommand=helper-command"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, absent := range []string{"-F", "IdentityAgent", "IdentityFile", "PreferredAuthentications"} {
		if strings.Contains(joined, absent) {
			t.Errorf("system auth overridden: %v", args)
		}
	}
}

// TestSSHConfigWithOpenSSH 只调用配置解析器, 不建立网络连接。
func TestSSHConfigWithOpenSSH(t *testing.T) {
	binary, err := SSHBinary()
	if err != nil {
		t.Skip("OpenSSH is not installed")
	}
	for _, tc := range []struct {
		name string
		hop  SSHHop
		want []string
	}{
		{"password", SSHHop{Vault: true, Password: true}, []string{"pubkeyauthentication false", "identityagent none", "identityfile none", "preferredauthentications keyboard-interactive,password"}},
		{"key", SSHHop{Vault: true, Agent: "/tmp/vault.sock"}, []string{"pubkeyauthentication true", "passwordauthentication no", "identityfile none", "identityagent /tmp/vault.sock", "preferredauthentications publickey"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.hop.Port, tc.hop.User = 22, "wood"
			body, err := sshHopConfig(tc.hop, "")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "with spaces.config")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(binary, "-G", "-F", path, "--", "10.0.2.5").CombinedOutput()
			if err != nil {
				t.Fatalf("ssh -G: %v\n%s", err, out)
			}
			normalized := strings.ReplaceAll(string(out), "\r\n", "\n")
			normalized = strings.NewReplacer("pubkeyauthentication yes\n", "pubkeyauthentication true\n", "pubkeyauthentication no\n", "pubkeyauthentication false\n").Replace(normalized)
			for _, want := range tc.want {
				if !strings.Contains(normalized, want+"\n") {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
		})
	}
}

// TestSSHProxyCommand 跨平台保护带空格和百分号的路径, 不把命令再套总外层引号。
func TestSSHProxyCommand(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		got := sshProxyCommand("/test dir/czlterm", "/test dir/100%/jump.json", goos)
		for _, want := range []string{"ssh-proxy", "100%%/jump.json", "%h", "%p"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in %s", goos, want, got)
			}
		}
	}
}

// TestSSHCommandMixedChain 同用户同主机不同端口也只携带各跳自己的参数和令牌。
func TestSSHCommandMixedChain(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", base)
	}
	hops := []SSHHop{
		{Host: "127.0.0.1", Port: 2222, User: "wood", Env: []string{"SSH_AUTH_SOCK=/original.sock"}},
		{Host: "127.0.0.1", Port: 2223, User: "wood", Vault: true, Password: true, Env: []string{"CZLTERM_ASKPASS_TOKEN=target-token"}},
	}
	args, files, err := SSHCommand("/usr/bin/ssh", "/usr/bin/czlterm", hops)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		defer os.Remove(file)
	}
	if len(files) != 2 || len(args) != 4 || args[0] != "-F" {
		t.Fatalf("unexpected plan: %v %v", args, files)
	}
	var spec sshProxySpec
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "-F") || !strings.Contains(strings.Join(spec.Args, " "), "-p 2222") || strings.Contains(string(raw), "target-token") {
		t.Fatalf("system hop polluted: %s", raw)
	}
	last, err := os.ReadFile(files[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(last), "Port 2223") || !strings.Contains(string(last), filepath.ToSlash(files[0])) || !strings.Contains(string(last), "PubkeyAuthentication no") {
		t.Fatalf("incorrect target config:\n%s", last)
	}
	// 下一跳的manifest只能取上一跳scope, 不能合并原有全部跳的凭据。
	hops = append(hops, SSHHop{Host: "127.0.0.1", Port: 2224, User: "wood", Vault: true, Password: true, Env: []string{"CZLTERM_ASKPASS_TOKEN=last-token"}})
	_, more, err := SSHCommand("/usr/bin/ssh", "/usr/bin/czlterm", hops)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range more {
		defer os.Remove(file)
	}
	raw, err = os.ReadFile(more[2])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "target-token") || strings.Contains(string(raw), "last-token") {
		t.Fatalf("incorrect password scope: %s", raw)
	}
	hops = append(hops, SSHHop{Vault: true})
	_, failed, err := SSHCommand("/usr/bin/ssh", "/usr/bin/czlterm", hops)
	if err == nil {
		t.Fatal("expected empty vault credential error")
	}
	for _, file := range failed {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("file not cleaned: %s", file)
		}
	}
}

// TestScriptsCleanSSHConfigs 保留原退出码后再删除临时配置。
func TestScriptsCleanSSHConfigs(t *testing.T) {
	s := Spec{Program: "ssh", CleanupFiles: []string{"/tmp/ssh 'config"}}
	sh, ps := shScript(s), powershellScript(s)
	if !strings.Contains(sh, "rm -f -- "+shQuote(s.CleanupFiles[0])) || strings.Index(sh, "code=$?") > strings.Index(sh, "rm -f --") {
		t.Fatalf("incorrect cleanup:\n%s", sh)
	}
	if !strings.Contains(ps, "Remove-Item -LiteralPath "+psQuote(s.CleanupFiles[0])) || strings.Index(ps, "$code = $LASTEXITCODE") > strings.Index(ps, "Remove-Item -LiteralPath "+psQuote(s.CleanupFiles[0])) || !strings.Contains(ps, "if ($code -ne 0)") {
		t.Fatalf("incorrect cleanup:\n%s", ps)
	}
}
