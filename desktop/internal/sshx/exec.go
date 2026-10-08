package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// ExecResult 是远程命令的结果。输出超过上限时截断并标记。
type ExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exitCode"`
	Truncated bool   `json:"truncated"`
}

// limitedBuffer 写满上限后丢弃后续内容, 记录是否发生截断。
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room < len(p) {
		b.truncated = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

// Exec 在远程执行命令, stdin 非 nil 时作为命令的标准输入。ctx 取消时发 KILL 信号并关闭会话。
func Exec(ctx context.Context, client *ssh.Client, command string, stdin io.Reader, maxOutput int) (ExecResult, error) {
	sess, err := client.NewSession()
	if err != nil {
		return ExecResult{}, fmt.Errorf("5010 open ssh session: %w", err)
	}
	defer sess.Close()

	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	sess.Stdout, sess.Stderr = stdout, stderr
	if stdin != nil {
		sess.Stdin = stdin
	}

	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		sess.Close()
		return ExecResult{}, fmt.Errorf("5011 command timed out or was cancelled")
	}

	res := ExecResult{
		Stdout:    stdout.buf.String(),
		Stderr:    stderr.buf.String(),
		Truncated: stdout.truncated || stderr.truncated,
	}
	var exitErr *ssh.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		res.ExitCode = exitErr.ExitStatus()
	default:
		return res, fmt.Errorf("5012 run command: %w", runErr)
	}
	return res, nil
}
