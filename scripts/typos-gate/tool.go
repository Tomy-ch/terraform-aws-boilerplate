package main

import (
	"context"
	"os/exec"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
)

type toolFunc = toolrun.Func

const (
	stepMain      = "main"
	stepScan      = "scan"
	stepFiles     = "files"
	stepLsFiles   = "ls-files"
	stepLsDeleted = "ls-deleted"
)

const fixtureConfig = ".typos.toml"

// command は toolFunc の実物です。引数は段ごとに固定です。
func command(ctx context.Context, dir string, env []string, step string, _ ...string) *exec.Cmd {
	var cmd *exec.Cmd
	switch step {
	case stepMain:
		cmd = exec.CommandContext(ctx, "typos", "--format", "brief")
	case stepScan:
		cmd = exec.CommandContext(ctx, "typos", "--config", fixtureConfig, "--format", "brief")
	case stepFiles:
		cmd = exec.CommandContext(ctx, "typos", "--files")
	case stepLsDeleted:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.quotepath=false", "ls-files", "-z", "--deleted")
	default:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.quotepath=false", "ls-files", "-z")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}
