package launch

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	if got := shQuote(`a'b$c`); got != `'a'\''b$c'` {
		t.Errorf("shQuote = %s", got)
	}
	if got := psQuote(`a'b$c`); got != `'a''b$c'` {
		t.Errorf("psQuote = %s", got)
	}
}

func TestScriptsKeepSecretsOutOfArgs(t *testing.T) {
	s := Spec{Title: "web's box", Program: "/usr/bin/ssh", Args: []string{"-p", "2222", "--", "root@h"}, Env: []string{"SSH_AUTH_SOCK=/tmp/x y"}}
	sh := shScript(s)
	if !strings.Contains(sh, "export SSH_AUTH_SOCK='/tmp/x y'") || !strings.Contains(sh, `'/usr/bin/ssh' '-p' '2222' '--' 'root@h'`) {
		t.Errorf("unexpected sh script:\n%s", sh)
	}
	ps := powershellScript(s)
	if !strings.Contains(ps, "$env:SSH_AUTH_SOCK = '/tmp/x y'") || !strings.Contains(ps, "'web''s box'") {
		t.Errorf("unexpected ps script:\n%s", ps)
	}
}

func TestVNCPassword(t *testing.T) {
	// vncpasswd 对 "password" 的公认输出。
	if got := hex.EncodeToString(obfuscateVNCPassword("password")); got != "dbd83cfd727a1458" {
		t.Errorf("obfuscate = %s", got)
	}
}

func TestScriptName(t *testing.T) {
	n := scriptName("KS-5-HIL / 生产")
	if !strings.HasPrefix(n, "czlterm-KS-5-HIL") || strings.ContainsAny(n, " /") {
		t.Errorf("scriptName = %q", n)
	}
	if n := scriptName("数据库"); !strings.HasPrefix(n, "czlterm-session-") {
		t.Errorf("non-ascii only title = %q", n)
	}
}
