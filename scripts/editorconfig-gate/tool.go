package main

import (
	"context"
	"os/exec"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
)

type toolFunc = toolrun.Func

const (
	stepGitInit = "git-init"
	stepGitAdd  = "git-add"
	stepCheck   = "check"
	stepDryRun  = "dry-run"
)

// command は toolFunc の実物です。引数は段ごとに固定です。fixture の git は hook を起動しません。
func command(dir string, env []string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepGitInit:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "init", "-q")
	case stepGitAdd:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "add", "-A")
	case stepDryRun:
		cmd = exec.CommandContext(ctx, "editorconfig-checker", "-dry-run")
	default:
		cmd = exec.CommandContext(ctx, "editorconfig-checker")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// ansiRe は、editorconfig-checker が出力に付ける色の制御列です。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
