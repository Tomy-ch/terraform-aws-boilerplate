package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	canaryTool   = "markdownlint-cli2"
	canaryMarker = "NODE_TOOLS_CANARY_FALLBACK"
)

var (
	errLockCheckUsage = xerrors.New("usage: node-tools lock-check -dir <dir>")
	errCanaryShape    = xerrors.New("canary: 写しを書き換えられませんでした（ファイルの形が変わった）")
	errCanaryFallback = xerrors.New("canary: PATH の同名の道具へ退きました")
	errCanaryLaunched = xerrors.New("canary: 写しで道具が起動しました（frozen の install が外れた）")
	errCanaryReason   = xerrors.New("canary: 期待と違う理由で落ちました")
	errCanaryNoCases  = xerrors.New("canary: 確かめるケースが0件です")

	canaryFiles = []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"}

	driftPattern     = regexp.MustCompile(`"` + regexp.QuoteMeta(canaryTool) + `": "[^"]*"`)
	integrityPattern = regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(canaryTool) + `@[^\n]*\n[^\n]*integrity: sha512-(....)`)
)

// canaryCase は、写しの1か所を壊したときに、道具の起動の前に出るべき pnpm のエラーです。
type canaryCase struct {
	name   string
	file   string
	mutate func([]byte) ([]byte, error)
	want   string
}

var canaryCases = []canaryCase{
	{name: "drift", file: "package.json", mutate: mutateDrift, want: "ERR_PNPM_OUTDATED_LOCKFILE"},
	{name: "integrity", file: "pnpm-lock.yaml", mutate: mutateIntegrity, want: "ERR_PNPM_TARBALL_INTEGRITY"},
}

// runLockCheck は、canary を走らせ、通ったときだけ dir の lockfile を照合します。
// 照合では、lockfile のずれと供給網の窓（pnpm-workspace.yaml の minimumReleaseAge）を pnpm が見る。
func runLockCheck(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("lock-check", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	dir := fs.String("dir", "", "package.json と lockfile を持つディレクトリ")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(errLockCheckUsage, err.Error())
	}
	if *dir == "" || fs.NArg() > 0 {
		return errLockCheckUsage
	}

	if err := runCanary(ctx, *dir, canaryCases, d); err != nil {
		return err
	}
	fmt.Fprintln(d.stderr, "✅ node-tools canary: lockfile のずれと integrity の不一致で、道具の起動の前に落ちました")

	cmd := d.tool(ctx, *dir, stepLockCheck)
	cmd.Stdout, cmd.Stderr = d.stderr, d.stderr
	if err := cmd.Run(); err != nil {
		return xerrors.Wrap(err, "lockfile の照合")
	}

	return nil
}

// runCanary は、dir の宣言を写して1か所ずつ壊し、この道具自身の exec を子プロセスとして起動します。
// PATH の先頭に同名の偽の道具を置き、どのケースも期待の pnpm のエラーで、道具の起動の前に落ちることを要求する。
func runCanary(ctx context.Context, dir string, cases []canaryCase, d deps) error {
	cwd, err := d.wd()
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリの取得")
	}
	self, err := d.self()
	if err != nil {
		return xerrors.Wrap(err, "この道具の実行ファイル")
	}
	src, err := filepath.Abs(dir)
	if err != nil {
		return xerrors.Wrap(err, dir)
	}
	// pnpm の store と同じファイルシステムに置くため、リポジトリの tmp/ の下に作る。
	parent := filepath.Join(cwd, "tmp")
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return xerrors.Wrap(err, parent)
	}
	root, err := os.MkdirTemp(parent, "node-tools-canary.")
	if err != nil {
		return xerrors.Wrap(err, "canary の一時ディレクトリ")
	}
	defer func() { _ = os.RemoveAll(root) }()

	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		return xerrors.Wrap(err, bin)
	}
	if err := writeFake(bin); err != nil {
		return err
	}
	env := append(withoutPath(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ran := 0
	for _, c := range cases {
		caseDir := filepath.Join(root, c.name)
		if err := copyDecl(src, caseDir, c); err != nil {
			return err
		}
		cmd := canaryCommand(ctx, self, caseDir, filepath.Join(root, "lock"))
		cmd.Env = env
		out, runErr := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return errInterrupted
		}
		if err := judgeCanary(c, out, runErr); err != nil {
			fmt.Fprintf(d.stderr, "%s\n", out)

			return err
		}
		ran++
	}
	// ケースの一覧が空になったら、canary は何も確かめずに通る。
	if ran == 0 {
		return errCanaryNoCases
	}

	return nil
}

func canaryCommand(ctx context.Context, self, dir, lock string) *exec.Cmd {
	return exec.CommandContext(ctx, self, "exec", "-dir", dir, "-lock", lock, canaryTool, "--version") //nolint:gosec // self は os.Executable が返すこの道具自身で、引数は canary の写しと lock だけ。canary が自分を子プロセスで起動しなくなれば外す
}

func judgeCanary(c canaryCase, out []byte, runErr error) error {
	switch {
	case bytes.Contains(out, []byte(canaryMarker)):
		return xerrors.Wrap(errCanaryFallback, c.name)
	case runErr == nil:
		return xerrors.Wrap(errCanaryLaunched, c.name)
	case bytes.Contains(out, []byte(c.want)):
		return nil
	default:
		return xerrors.Wrapf(errCanaryReason, "%s: %s を期待: %v", c.name, c.want, runErr)
	}
}

func writeFake(bin string) error {
	root, err := os.OpenRoot(bin)
	if err != nil {
		return xerrors.Wrap(err, bin)
	}
	defer func() { _ = root.Close() }()
	if err := root.WriteFile(canaryTool, []byte("#!/bin/sh\necho "+canaryMarker+"\n"), 0o600); err != nil {
		return xerrors.Wrap(err, "偽の道具")
	}
	// 実行できなければ、PATH へ退いたかどうかを確かめられない。
	if err := root.Chmod(canaryTool, 0o500); err != nil {
		return xerrors.Wrap(err, "偽の道具")
	}

	return nil
}

func copyDecl(src, dst string, c canaryCase) error {
	if err := os.Mkdir(dst, 0o750); err != nil {
		return xerrors.Wrap(err, dst)
	}
	from, err := os.OpenRoot(src)
	if err != nil {
		return xerrors.Wrap(err, src)
	}
	defer func() { _ = from.Close() }()
	to, err := os.OpenRoot(dst)
	if err != nil {
		return xerrors.Wrap(err, dst)
	}
	defer func() { _ = to.Close() }()
	for _, name := range canaryFiles {
		b, err := from.ReadFile(name)
		if err != nil {
			return xerrors.Wrap(err, name)
		}
		if name == c.file {
			if b, err = c.mutate(b); err != nil {
				return xerrors.Wrapf(err, "%s: %s", c.name, name)
			}
		}
		if err := to.WriteFile(name, b, 0o600); err != nil {
			return xerrors.Wrap(err, name)
		}
	}

	return nil
}

func mutateDrift(b []byte) ([]byte, error) {
	if len(driftPattern.FindAll(b, -1)) != 1 {
		return nil, errCanaryShape
	}
	out := driftPattern.ReplaceAll(b, []byte(`"`+canaryTool+`": "0.0.0"`))
	if bytes.Equal(out, b) {
		return nil, errCanaryShape
	}

	return out, nil
}

// mutateIntegrity は、lockfile の canaryTool の integrity の先頭4文字を書き換えます。
// 元が AAAA なら BBBB にする（書き換えが元と同じになって照合が通るのを避ける）。
func mutateIntegrity(b []byte) ([]byte, error) {
	m := integrityPattern.FindAllSubmatchIndex(b, -1)
	if len(m) != 1 {
		return nil, errCanaryShape
	}
	start, end := m[0][2], m[0][3]
	repl := "AAAA"
	if string(b[start:end]) == repl {
		repl = "BBBB"
	}
	out := make([]byte, 0, len(b))
	out = append(out, b[:start]...)
	out = append(out, repl...)
	out = append(out, b[end:]...)

	return out, nil
}

func withoutPath(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "PATH=") {
			out = append(out, e)
		}
	}

	return out
}
