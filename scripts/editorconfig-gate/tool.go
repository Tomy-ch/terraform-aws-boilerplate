package main

import (
	"context"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
)

type toolFunc = toolrun.Func

const (
	stepGitInit   = "git-init"
	stepGitAdd    = "git-add"
	stepCheck     = "check"
	stepDryRun    = "dry-run"
	stepLsFiles   = "ls-files"
	stepLsDeleted = "ls-deleted"
)

// command は toolFunc の実物です。引数は段ごとに固定です。git は hook を起動しません。
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
	case stepLsFiles:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "ls-files", "-z", "--stage")
	case stepLsDeleted:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "ls-files", "-z", "--deleted")
	default:
		cmd = exec.CommandContext(ctx, "editorconfig-checker")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// quotePathConfig は、editorconfig-checker が起動する git に core.quotepath=false を渡す env です。git が引用した
// パス（非 ASCII の名前）を、editorconfig-checker は黙って走査から外します。
var quotePathConfig = []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=false"}

// checkerEnv は、env から受け継いだ git の設定の変数（GIT_CONFIG_COUNT・GIT_CONFIG_KEY_<n>・GIT_CONFIG_VALUE_<n>・
// GIT_CONFIG_PARAMETERS）を外し、quotePathConfig を足した写しを返します。GIT_CONFIG_PARAMETERS は
// GIT_CONFIG_COUNT に勝つので、残せば core.quotepath を上書きできます。
func checkerEnv(env []string) []string {
	kept := make([]string, 0, len(env)+len(quotePathConfig))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG_COUNT" || name == "GIT_CONFIG_PARAMETERS" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		kept = append(kept, kv)
	}

	return append(kept, quotePathConfig...)
}

// ansiRe は、editorconfig-checker が出力に付ける色の制御列です。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
