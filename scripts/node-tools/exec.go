package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	lockPoll  = 200 * time.Millisecond
	lockTries = 600
)

var (
	errExecUsage   = xerrors.New("usage: node-tools exec -dir <abs> -lock <abs> <tool> [args...]")
	errToolName    = xerrors.New("道具の名前にパスを含められません")
	errLockBusy    = xerrors.New("install の排他を2分取れません。install が動いていなければ消してください")
	errInstall     = xerrors.New("lockfile の frozen の install が落ちました。道具を起動しません")
	errNoTool      = xerrors.New("install した先に道具がありません")
	errInterrupted = xerrors.New("シグナルで中断しました")
)

// runExec は、dir の lockfile を frozen で入れてから、dir/node_modules/.bin/<tool> を起動します。
// install が落ちれば起動しない。PATH は引かない —— 同名の道具へ退くと、lockfile の外の版で検査が通る。
func runExec(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	dir := fs.String("dir", "", "package.json と lockfile を持つディレクトリ（絶対パス）")
	lock := fs.String("lock", "", "install の排他の印にするディレクトリ（絶対パス）")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(errExecUsage, err.Error())
	}
	if !filepath.IsAbs(*dir) || !filepath.IsAbs(*lock) || fs.NArg() == 0 {
		return errExecUsage
	}
	tool := fs.Arg(0)
	if tool == "" || tool == "." || tool == ".." || strings.ContainsRune(tool, filepath.Separator) {
		return xerrors.Wrap(errToolName, tool)
	}

	if err := installLocked(ctx, *dir, *lock, d); err != nil {
		return err
	}

	bin := filepath.Join(*dir, "node_modules", ".bin", tool)
	if info, err := os.Stat(bin); err != nil || info.IsDir() {
		return xerrors.Wrap(errNoTool, bin)
	}

	// install の後に届いたシグナルは ctx を閉じるだけで process を止めないので、起動の前に見る。
	if ctx.Err() != nil {
		return errInterrupted
	}

	return d.execTool(bin, append([]string{bin}, fs.Args()[1:]...), os.Environ())
}

// installLocked は、lock を取ってから dir で frozen の install を通し、lock を返してから戻ります。
// 並行の初回 install は同じ node_modules へ shim を書き合って落ちるので、install を直列にする。
// ctx が終われば install を止め、lock を返してから errInterrupted で戻る。
func installLocked(ctx context.Context, dir, lock string, d deps) error {
	release, err := acquireLock(ctx, lock, d)
	if err != nil {
		return err
	}
	defer release()

	cmd := d.tool(ctx, dir, nil, stepInstall)
	// 道具の stdin（パイプで渡すコミットメッセージなど）を install に読ませない。
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = d.stderr, d.stderr
	code, runErr := toolrun.Exec(cmd)
	if ctx.Err() != nil {
		return errInterrupted
	}
	if runErr != nil {
		return xerrors.Wrap(runErr, "install")
	}
	if code != 0 {
		return xerrors.Wrapf(errInstall, "exit status %d", code)
	}

	return nil
}

// acquireLock は、lock のディレクトリを作れるまで待ちます。返す release は lock を消し、何度呼んでもよい。
func acquireLock(ctx context.Context, lock string, d deps) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lock), 0o750); err != nil {
		return nil, xerrors.Wrap(err, "lock の親ディレクトリ")
	}
	for n := 0; ; n++ {
		if ctx.Err() != nil {
			return nil, errInterrupted
		}
		err := os.Mkdir(lock, 0o750)
		if err == nil {
			var once sync.Once

			return func() { once.Do(func() { _ = os.Remove(lock) }) }, nil
		}
		if !os.IsExist(err) {
			return nil, xerrors.Wrap(err, lock)
		}
		if n+1 >= lockTries {
			return nil, xerrors.Wrap(errLockBusy, lock)
		}
		d.sleep(lockPoll)
	}
}
