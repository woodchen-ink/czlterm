package askpass

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Active 报告当前进程是否作为 askpass 被 ssh 调起。
func Active() bool {
	return os.Getenv(EnvToken) != "" && os.Getenv(EnvURL) != ""
}

// Run 是 askpass 模式的入口, 返回进程退出码。args 为 ssh 传来的参数 (提示语)。
func Run(args []string) int {
	prompt := strings.Join(args, " ")
	if IsPasswordPrompt(prompt) {
		if pw, err := fetch(prompt); err == nil {
			fmt.Fprintln(os.Stdout, pw)
			return 0
		}
	}
	// 不是密码提示, 或桌面端没有对应密码: 交给终端里的用户。
	answer, err := askTTY(prompt)
	if err != nil {
		return 1
	}
	fmt.Fprintln(os.Stdout, answer)
	return 0
}

func fetch(prompt string) (string, error) {
	body, _ := json.Marshal(request{Prompt: prompt})
	req, err := http.NewRequest(http.MethodPost, os.Getenv(EnvURL), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv(EnvToken))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ErrNoCredential
	}
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&out); err != nil {
		return "", err
	}
	return out.Password, nil
}

// askTTY 在 ssh 所在的终端上显示提示并读一行回答。
// 只有主机指纹确认 (yes/no) 回显输入; 密码、口令、验证码一律关闭回显, 不留在终端回滚里。
func askTTY(prompt string) (string, error) {
	t, err := openTTY()
	if err != nil {
		return "", err
	}
	defer t.close()
	fmt.Fprint(t.out, prompt)
	if !strings.Contains(strings.ToLower(prompt), "(yes/no") {
		b, err := term.ReadPassword(t.fd)
		fmt.Fprintln(t.out)
		return string(b), err
	}
	line, err := bufio.NewReader(t.in).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// tty 是控制终端的读写端。fd 是输入端的文件描述符 / 句柄, 供关闭回显使用。
type tty struct {
	in    io.Reader
	out   io.Writer
	fd    int
	close func()
}
