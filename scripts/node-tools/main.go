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
	"io"
	"log"
	"os"
	"os/exec"
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
	exit     func(int)
	stderr   io.Writer
	stdout   io.Writer
}

func main() {
	log.SetFlags(0)

	d := deps{
		tool:     command,
		execTool: execProcess,
		self:     os.Executable,
		wd:       os.Getwd,
		sleep:    time.Sleep,
		exit:     os.Exit,
		stderr:   os.Stderr,
		stdout:   os.Stdout,
	}
	if err := run(os.Args[1:], d); err != nil {
		log.Fatalf("❌ node-tools: %v", err)
	}
}

func run(args []string, d deps) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "exec":
		return runExec(args[1:], d)
	case "lock-check":
		return runLockCheck(args[1:], d)
	case "mise-npm-disabled":
		return runMiseNPMDisabled(args[1:], d)
	default:
		return xerrors.Wrap(errUsage, args[0])
	}
}

type toolFunc func(dir string, step string) *exec.Cmd

const (
	stepInstall   = "install"
	stepLockCheck = "lock-check"
	stepMiseLs    = "mise-ls"
)

func command(dir string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepInstall:
		cmd = exec.CommandContext(ctx, "pnpm", "install", "--frozen-lockfile", "--prod", "--reporter=silent")
	case stepLockCheck:
		cmd = exec.CommandContext(ctx, "pnpm", "install", "--frozen-lockfile", "--lockfile-only")
	default:
		cmd = exec.CommandContext(ctx, "mise", "ls", "--current", "--local", "--json")
	}
	cmd.Dir = dir

	return cmd
}

// execProcess は、この process を bin に置き換えます。戻るのは置き換えに失敗したときだけです。
func execProcess(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env) //nolint:gosec // bin は node_modules/.bin の下の道具に限り（runExec が確かめる）、引数を道具へ渡すのがこの前置きの役目である
}
