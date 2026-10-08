package askpass

import "testing"

func TestMatch(t *testing.T) {
	creds := []Credential{{Target: "root@jump", Password: "j"}, {Target: "admin@10.0.0.5", Password: "t"}}
	cases := []struct {
		prompt string
		want   string
		ok     bool
	}{
		{"root@jump's password: ", "j", true},
		{"admin@10.0.0.5's password: ", "t", true},
		{"(admin@10.0.0.5) Password: ", "t", true}, // keyboard-interactive 带目标
		{"(root@jump) Password: ", "j", true},
		{"Password: ", "", false}, // 无目标且有多跳: 不猜
		{"other@host's password: ", "", false},
	}
	for _, c := range cases {
		got, ok := match(creds, 2, c.prompt)
		if got != c.want || ok != c.ok {
			t.Errorf("match(%q) = %q,%v want %q,%v", c.prompt, got, ok, c.want, c.ok)
		}
	}
	if got, ok := match(creds[:1], 1, "Password: "); !ok || got != "j" {
		t.Errorf("single hop should answer generic prompt, got %q,%v", got, ok)
	}
	// 跳板机用私钥、目标机用密码: 跳板机的无目标提示不能拿到目标机密码。
	if _, ok := match(creds[1:], 2, "Password: "); ok {
		t.Error("generic prompt must not be answered when the chain has several hops")
	}
}

func TestIsPasswordPrompt(t *testing.T) {
	yes := []string{"root@h's password: ", "Password: "}
	no := []string{
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ",
		"Enter passphrase for key '/x': ",
		"Verification code: ",
	}
	for _, p := range yes {
		if !IsPasswordPrompt(p) {
			t.Errorf("%q should be a password prompt", p)
		}
	}
	for _, p := range no {
		if IsPasswordPrompt(p) {
			t.Errorf("%q should not be a password prompt", p)
		}
	}
}
