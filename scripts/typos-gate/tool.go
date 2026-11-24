package main

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// toolFunc は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。テストは差し替えます。
type toolFunc func(dir string, env []string, step string) *exec.Cmd

// 段の名前。command が引数を固定で持ちます。
const (
	stepMain    = "main"
	stepScan    = "scan"
	stepFiles   = "files"
	stepLsFiles = "ls-files"
)

// fixtureConfig は、canary の fixture に複写する設定の名前です。
const fixtureConfig = ".typos.toml"

// command は toolFunc の実物です。引数は段ごとに固定です。
func command(dir string, env []string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepMain:
		cmd = exec.CommandContext(ctx, "typos", "--format", "brief")
	case stepScan:
		cmd = exec.CommandContext(ctx, "typos", "--config", fixtureConfig, "--format", "brief")
	case stepFiles:
		cmd = exec.CommandContext(ctx, "typos", "--files")
	default:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.quotepath=false", "ls-files", "-z")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// result は、道具の stdout、stdout と stderr を出た順に束ねたもの、終了コードです。
type result struct {
	stdout   string
	combined string
	code     int
}

// runTool は cmd を走らせます。起動できない、またはシグナルで終わったときは errTool です。0 以外の終了コードは
// エラーにせず result で返し、判定は呼び出し側が持ちます。
func runTool(cmd *exec.Cmd) (result, error) {
	var stdout bytes.Buffer
	combined := &lockedBuffer{}
	cmd.Stdout = io.MultiWriter(&stdout, combined)
	cmd.Stderr = combined
	err := cmd.Run()
	r := result{stdout: stdout.String(), combined: combined.String()}
	if err == nil {
		return r, nil
	}
	var exit *exec.ExitError
	if !xerrors.As(err, &exit) {
		return r, xerrors.Wrapf(errTool, "%s: %v", cmd.Path, err)
	}
	if exit.ExitCode() < 0 {
		return r, xerrors.Wrapf(errTool, "%s がシグナルで終わりました: %v\n%s", cmd.Path, err, r.combined)
	}
	r.code = exit.ExitCode()

	return r, nil
}

// lockedBuffer は、stdout と stderr を別々の goroutine から受ける束ね先です。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
