// Package gitsync 用系统 git 把数据目录同步到用户自己的私有仓库。
//
// 每次同步: 提交本地改动 → fetch → rebase 到远端 → push。一连接一文件, 不同设备改不同连接时
// rebase 不会冲突; 同一连接两边都改了才会冲突, 此时中止 rebase、保留本地提交并报错, 由用户处理。
// git 的认证沿用用户已有的配置 (SSH 密钥或凭据助手), 程序不经手 git 凭据。
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/platform"
)

const branch = "main"

// Sync 执行一次完整同步。remote 为空时只做本地提交 (保留历史, 便于以后接上远端)。
// auth 为零值时沿用用户自己的 git 认证配置。
func Sync(ctx context.Context, dir, remote string, auth Auth) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("6001 git not found in PATH, install git")
	}
	args, env, cleanup, err := auth.prepare(remote)
	if err != nil {
		return err
	}
	defer cleanup()
	git := func(ctx context.Context, dir string, a ...string) (string, error) {
		return runGit(ctx, dir, env, append(append([]string{}, args...), a...)...)
	}
	if err := ensureRepo(ctx, git, dir, remote); err != nil {
		return err
	}
	if err := commitAll(ctx, git, dir); err != nil {
		return err
	}
	if remote == "" {
		return nil
	}
	if _, err := git(ctx, dir, "fetch", "origin"); err != nil {
		return fmt.Errorf("6002 git fetch: %w", err)
	}
	if hasRef(ctx, git, dir, "refs/remotes/origin/"+branch) {
		if !hasRef(ctx, git, dir, "HEAD") {
			// 新设备第一次同步、本地没有任何提交: 直接以远端为起点。
			if _, err := git(ctx, dir, "reset", "--mixed", "origin/"+branch); err != nil {
				return fmt.Errorf("6003 git reset: %w", err)
			}
			if err := commitAll(ctx, git, dir); err != nil {
				return err
			}
		} else if _, err := git(ctx, dir, "rebase", "origin/"+branch); err != nil {
			_, _ = git(ctx, dir, "rebase", "--abort")
			// 两边各自初始化过仓库时历史不相关, rebase 会失败; 合并一次把两段历史接起来。
			if _, mergeErr := git(ctx, dir, "merge", "--allow-unrelated-histories", "--no-edit", "origin/"+branch); mergeErr != nil {
				_, _ = git(ctx, dir, "merge", "--abort")
				return fmt.Errorf("6004 sync conflict: the same connection was changed on two devices, resolve it in %s: %w", dir, err)
			}
		}
	}
	if _, err := git(ctx, dir, "push", "origin", "HEAD:"+branch); err != nil {
		return fmt.Errorf("6005 git push: %w", err)
	}
	return nil
}

// ensureRepo 初始化仓库并让 origin 指向 remote。
// gitFunc 执行一条 git 命令, 已带上本次同步的认证参数。
type gitFunc func(ctx context.Context, dir string, args ...string) (string, error)

func ensureRepo(ctx context.Context, git gitFunc, dir, remote string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := git(ctx, dir, "init", "-b", branch); err != nil {
			return fmt.Errorf("6006 git init: %w", err)
		}
		_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.tmp\n"), 0o600)
	}
	cur, _ := git(ctx, dir, "remote", "get-url", "origin")
	cur = strings.TrimSpace(cur)
	switch {
	case remote == "" && cur != "":
		_, _ = git(ctx, dir, "remote", "remove", "origin")
	case remote != "" && cur == "":
		if _, err := git(ctx, dir, "remote", "add", "origin", remote); err != nil {
			return fmt.Errorf("6007 git remote add: %w", err)
		}
	case remote != "" && cur != remote:
		if _, err := git(ctx, dir, "remote", "set-url", "origin", remote); err != nil {
			return fmt.Errorf("6007 git remote set-url: %w", err)
		}
	}
	return nil
}

func commitAll(ctx context.Context, git gitFunc, dir string) error {
	if _, err := git(ctx, dir, "add", "-A"); err != nil {
		return fmt.Errorf("6008 git add: %w", err)
	}
	// 没有暂存改动时 diff --cached --quiet 返回 0。
	if _, err := git(ctx, dir, "diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	host, _ := os.Hostname()
	msg := "sync from " + host
	if _, err := git(ctx, dir, "commit", "-m", msg); err != nil {
		return fmt.Errorf("6009 git commit: %w", err)
	}
	return nil
}

func hasRef(ctx context.Context, git gitFunc, dir, ref string) bool {
	_, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// runGit 在 dir 中执行 git 命令。env 由 Auth.prepare 给出, 总含 GIT_TERMINAL_PROMPT=0:
// 没有终端, 交互式提示只会让同步挂到超时。
func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	host, _ := os.Hostname()
	// 用户没配全局身份时 commit 会失败; 用 -c 补一个, 不改用户的 git 配置。
	full := append([]string{"-c", "user.name=czlterm", "-c", "user.email=czlterm@" + sanitizeHost(host)}, args...)
	cmd := platform.Command(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			return out.String(), err
		}
		return out.String(), errors.New(lastLine(msg))
	}
	return out.String(), nil
}

func sanitizeHost(h string) string {
	h = strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			return r
		}
		return -1
	}, h))
	if h == "" {
		return "localhost"
	}
	return h
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
