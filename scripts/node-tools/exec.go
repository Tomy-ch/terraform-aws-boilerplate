package main

import (
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	lockPoll  = 200 * time.Millisecond
	lockTries = 600
)

var (
	errExecUsage = xerrors.New("usage: node-tools exec -dir <abs> -lock <abs> <tool> [args...]")
	errToolName  = xerrors.New("道具の名前にパスを含められません")
	errLockBusy  = xerrors.New("install の排他を2分取れません。install が動いていなければ消してください")
	errInstall   = xerrors.New("lockfile の frozen の install が落ちました。道具を起動しません")
	errNoTool    = xerrors.New("install した先に道具がありません")
)

// runExec は、dir の lockfile を frozen で入れてから、dir/node_modules/.bin/<tool> を起動します。
// install が落ちれば起動しない。PATH は引かない —— 同名の道具へ退くと、lockfile の外の版で検査が通る。
func runExec(args []string, d deps) error {
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
	if tool == "." || tool == ".." || strings.ContainsRune(tool, filepath.Separator) {
		return xerrors.Wrap(errToolName, tool)
	}

	if err := installLocked(*dir, *lock, d); err != nil {
		return err
	}

	bin := filepath.Join(*dir, "node_modules", ".bin", tool)
	if info, err := os.Stat(bin); err != nil || info.IsDir() {
		return xerrors.Wrap(errNoTool, bin)
	}

	return d.execTool(bin, append([]string{bin}, fs.Args()[1:]...), os.Environ())
}

// installLocked は、lock を取ってから dir で frozen の install を通し、lock を返してから戻ります。
// 並行の初回 install は同じ node_modules へ shim を書き合って落ちるので、install を直列にする。
func installLocked(dir, lock string, d deps) error {
	release, err := acquireLock(lock, d)
	if err != nil {
		return err
	}
	defer release()

	cmd := d.tool(dir, stepInstall)
	// 道具の stdin（パイプで渡すコミットメッセージなど）を install に読ませない。
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = d.stderr, d.stderr
	if err := cmd.Run(); err != nil {
		return xerrors.Wrap(errInstall, err.Error())
	}

	return nil
}

// acquireLock は、lock のディレクトリを作れるまで待ちます。返す release は lock を消し、何度呼んでもよい。
// 待ちと install の間に SIGINT・SIGTERM を受けたら、lock を消してから終わる。
func acquireLock(lock string, d deps) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lock), 0o750); err != nil {
		return nil, xerrors.Wrap(err, "lock の親ディレクトリ")
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	var once sync.Once
	var held atomic.Bool
	release := func() {
		once.Do(func() {
			signal.Stop(sigs)
			close(done)
			if held.Load() {
				_ = os.Remove(lock)
			}
		})
	}
	go watchSignals(sigs, done, held.Load, lock, d.exit)

	for n := 0; ; n++ {
		err := os.Mkdir(lock, 0o750)
		if err == nil {
			held.Store(true)

			return release, nil
		}
		if !os.IsExist(err) {
			release()

			return nil, xerrors.Wrap(err, lock)
		}
		if n+1 >= lockTries {
			release()

			return nil, xerrors.Wrap(errLockBusy, lock)
		}
		d.sleep(lockPoll)
	}
}

// watchSignals は、done が閉じる前にシグナルを受けたら、取った lock を消して 128+signal で終わります。
func watchSignals(sigs <-chan os.Signal, done <-chan struct{}, held func() bool, lock string, exit func(int)) {
	select {
	case s := <-sigs:
		if held() {
			_ = os.Remove(lock)
		}
		code := 1
		if sig, ok := s.(syscall.Signal); ok {
			code = 128 + int(sig)
		}
		exit(code)
	case <-done:
	}
}
