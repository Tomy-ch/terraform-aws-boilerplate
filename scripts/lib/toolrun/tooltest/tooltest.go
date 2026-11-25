// Package tooltest は、toolrun.Func を差し替えるテストのための偽の道具を持ちます。
//
// 偽の道具は、制御用のディレクトリ（作業ディレクトリとは別）に置いた sh のスクリプトで、段 step ごとに
// `<段>.out` を stdout へ、`<段>.err` を stderr へ出し、`<段>.rc` の終了コードで終わり、`<段>.kill` があれば自身を
// シグナルで止めます。`<段>@<鍵>.out` と `.rc` は、その作業ディレクトリでだけ段の既定に勝ちます。鍵は作業ディレクトリの
// 名前で、[WithBase] を渡したときは base からの相対パスの `/` を `:` にしたものです。起動ごとに「作業ディレクトリ
// （symlink を解いた実パス） 段 GIT_DIR」を calls.log へ、「段 operand…」を args.log へ1行ずつ控えます。
package tooltest

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fake は偽の道具の制御です。
type Fake struct {
	t       *testing.T
	dir     string
	prelude string
	base    string
}

// Option は [New] の選択肢です。
type Option func(*Fake)

// WithPrelude は、段の出力より前に、作業ディレクトリで走る sh の断片 prelude を足します。断片は制御用の
// ディレクトリを $ctl、段を $step で参照できます。
func WithPrelude(prelude string) Option {
	return func(f *Fake) { f.prelude = prelude }
}

// WithBase は、上書きの鍵を作業ディレクトリの名前でなく base からの相対パスにします。名前が同じ別のディレクトリを
// 分けて上書きするときに使います。base は symlink を解いて比べます。
func WithBase(base string) Option {
	return func(f *Fake) { f.base = base }
}

// New は、t.TempDir の下に偽の道具を置きます。
func New(t *testing.T, opts ...Option) *Fake {
	t.Helper()
	f := &Fake{t: t, dir: t.TempDir()}
	for _, o := range opts {
		o(f)
	}
	key := `$(basename "$cwd")`
	if f.base != "" {
		base, err := filepath.EvalSymlinks(f.base)
		if err != nil {
			t.Fatalf("base を解けません: %v", err)
		}
		key = `$(printf '%s' "${cwd#'` + base + `'/}" | tr / :)`
	}
	body := `#!/bin/sh
ctl='` + f.dir + `'
step="$1"
shift
cwd=$(pwd -P)
echo "$cwd $step ${GIT_DIR:-}" >> "$ctl/calls.log"
if [ $# -gt 0 ]; then echo "$step $*"; else echo "$step"; fi >> "$ctl/args.log"
if [ -e "$ctl/$step.kill" ]; then kill -9 $$; fi
` + f.prelude + `
at="$step@` + key + `"
if [ -e "$ctl/$at.out" ]; then cat "$ctl/$at.out"; elif [ -e "$ctl/$step.out" ]; then cat "$ctl/$step.out"; fi
if [ -e "$ctl/$step.err" ]; then cat "$ctl/$step.err" >&2; fi
if [ -e "$ctl/$at.rc" ]; then exit "$(cat "$ctl/$at.rc")"; fi
if [ -e "$ctl/$step.rc" ]; then exit "$(cat "$ctl/$step.rc")"; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(f.dir, "tool"), []byte(body), 0o600); err != nil {
		t.Fatalf("偽の道具を書けません: %v", err)
	}

	return f
}

// Set は、制御用のディレクトリに `<step>.<suffix>` を body で書きます。
func (f *Fake) Set(step, suffix, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, step+"."+suffix), []byte(body), 0o600); err != nil {
		f.t.Fatalf("%s.%s を書けません: %v", step, suffix, err)
	}
}

// Tool は、偽の道具を起動する toolrun.Func を返します。スクリプトは exec せず /bin/sh に読ませます。並行のテストが
// 別の偽の道具を書いている間に fork した子がその書き込みの fd を持ち越すと、書いたばかりのファイルの exec が
// ETXTBSY で落ちるためです。
func (f *Fake) Tool() func(ctx context.Context, dir string, env []string, step string, operands ...string) *exec.Cmd {
	script := filepath.Join(f.dir, "tool")
	return func(ctx context.Context, d string, env []string, step string, operands ...string) *exec.Cmd {
		//nolint:gosec // G204: テストだけが使う偽の道具で、New が t.TempDir の下に自ら書いたスクリプトを起動する。gosec の除外が _test.go 以外のテスト補助の package を名指せるようになれば外す
		c := exec.CommandContext(ctx, "/bin/sh", append([]string{script, step}, operands...)...)
		c.Dir, c.Env = d, env
		return c
	}
}

// Calls は、起動ごとに控えた行を起動の順に返します。1度も起動していなければ nil です。
func (f *Fake) Calls() []string {
	f.t.Helper()
	raw, err := fs.ReadFile(os.DirFS(f.dir), "calls.log")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatalf("calls.log を読めません: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// Args は、起動ごとに控えた「段 operand…」を起動の順に返します。1度も起動していなければ nil です。
func (f *Fake) Args() []string {
	f.t.Helper()
	raw, err := fs.ReadFile(os.DirFS(f.dir), "args.log")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatalf("args.log を読めません: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// Read は、制御用のディレクトリのファイル name の中身を返します。[WithPrelude] の断片が $ctl へ書いたものを読みます。
func (f *Fake) Read(name string) string {
	f.t.Helper()
	raw, err := fs.ReadFile(os.DirFS(f.dir), name)
	if err != nil {
		f.t.Fatalf("%s を読めません: %v", name, err)
	}
	return string(raw)
}

// Missing は、在らない道具を起動するコマンドを作る toolrun.Func です。
func Missing(ctx context.Context, dir string, env []string, _ string, _ ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "/nonexistent/tooltest-tool")
	c.Dir, c.Env = dir, env
	return c
}
