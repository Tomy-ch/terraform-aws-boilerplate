// Package main は、npm 側の道具（docker/tools/node の lockfile が固定する道具）をホストで扱うツール
// （ADR-0503 決定7・17・18）。
//
//	exec -dir <abs> -lock <abs> <tool> [args...]  lockfile を frozen で入れてから、入れた先の道具を起動する
//	lock-check -dir <dir>                         canary の後に、lockfile が package.json と供給網の窓に合うかを検証する
//	mise-npm-disabled                             mise が mise.toml の npm: の道具を入れる対象にしていないかを検証する
//
// どのサブコマンドもホストで起動する（make の RUN_SCRIPT_HOST）。
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var errUsage = xerrors.New("usage: node-tools <exec|lock-check|mise-npm-disabled> [flags]")

type deps struct {
	tool     toolFunc
	execTool func(bin string, argv, env []string) error
	self     func() (string, error)
	wd       func() (string, error)
	sleep    func(time.Duration)
	stderr   io.Writer
}

func main() {
	log.SetFlags(0)

	// SIGINT・SIGTERM で ctx が終わり、起動中の子（pnpm・canary の子）を止め、lock と一時ディレクトリを片付けてから終わる。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	d := deps{
		tool:     command,
		execTool: execProcess,
		self:     os.Executable,
		wd:       os.Getwd,
		sleep:    time.Sleep,
		stderr:   os.Stderr,
	}
	err := run(ctx, os.Args[1:], d)
	stop()
	if err != nil {
		log.Fatalf("❌ node-tools: %v", err)
	}
}

func run(ctx context.Context, args []string, d deps) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "exec":
		return runExec(ctx, args[1:], d)
	case "lock-check":
		return runLockCheck(ctx, args[1:], d)
	case "mise-npm-disabled":
		return runMiseNPMDisabled(ctx, args[1:], d)
	default:
		return xerrors.Wrap(errUsage, args[0])
	}
}

type toolFunc func(ctx context.Context, dir string, step string) *exec.Cmd

const (
	stepInstall   = "install"
	stepLockCheck = "lock-check"
	stepMiseLs    = "mise-ls"
)

func command(ctx context.Context, dir string, step string) *exec.Cmd {
	var cmd *exec.Cmd
	switch step {
	case stepInstall:
		cmd = exec.CommandContext(ctx, "pnpm", "install", "--frozen-lockfile", "--prod", "--reporter=silent")
	case stepLockCheck:
		cmd = exec.CommandContext(ctx, "pnpm", "install", "--frozen-lockfile", "--lockfile-only")
	case stepMiseLs:
		cmd = exec.CommandContext(ctx, "mise", "ls", "--current", "--local", "--json")
	default:
		panic(fmt.Sprintf("node-tools: 知らない段 %q", step))
	}
	cmd.Dir = dir

	return cmd
}

// execProcess は、この process を bin に置き換えます。戻るのは置き換えに失敗したときだけです。
func execProcess(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env) //nolint:gosec // bin は node_modules/.bin の下の道具に限り（runExec が確かめる）、引数を道具へ渡すのがこの前置きの役目である。npm の道具を固定の引数でしか起動しなくなれば外す
}
