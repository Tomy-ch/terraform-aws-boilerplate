package main

import (
	"context"
	"os/exec"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
)

type toolFunc = toolrun.Func

const (
	stepResolve = "resolve"
	stepCount   = "count"
)

// command は toolFunc の実物です。引数は段ごとに固定で、rev はどれも stdin で渡します。知らない段は失敗する
// コマンドにします。
func command(ctx context.Context, dir string, env []string, step string, _ ...string) *exec.Cmd {
	var cmd *exec.Cmd
	switch step {
	case stepResolve:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "cat-file", "--batch-check=%(objectname) %(objecttype)")
	case stepCount:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "rev-list", "--count", "--no-merges", "--stdin")
	default:
		cmd = exec.CommandContext(ctx, "false")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}
