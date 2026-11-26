package main

import (
	"context"
	"os/exec"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
)

type toolFunc = toolrun.Func

const (
	stepCanary = "canary"
	stepMain   = "main"
)

// command は toolFunc の実物です。canary と本走査は同じ引数で、作業ディレクトリだけが違います。
func command(ctx context.Context, dir string, env []string, _ string, _ ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "gofmt", "-l", ".")
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}
