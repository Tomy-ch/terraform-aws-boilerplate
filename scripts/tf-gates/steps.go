package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// toolFunc は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。テストは差し替えます。
type toolFunc func(dir string, env []string, step string) *exec.Cmd

// 段の名前。command が引数を固定で持ちます。
const (
	stepFmt          = "fmt"
	stepFmtCheck     = "fmt-check"
	stepInit         = "init"
	stepInitReadonly = "init-readonly"
	stepValidate     = "validate"
	stepTest         = "test"
	stepLintInit     = "lint-init"
	stepLint         = "lint"
)

// command は toolFunc の実物です。引数は段ごとに固定で、ディレクトリは作業ディレクトリ、設定は環境変数で渡します。
// root の init は lock を書き換えない（ADR-0205 決定9。lock の更新は人が root で行う）。
func command(dir string, env []string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepFmt:
		cmd = exec.CommandContext(ctx, "terraform", "fmt", "-recursive")
	case stepFmtCheck:
		cmd = exec.CommandContext(ctx, "terraform", "fmt", "-check", "-diff", "-recursive")
	case stepInit:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-backend=false", "-input=false")
	case stepInitReadonly:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-backend=false", "-input=false", "-lockfile=readonly")
	case stepValidate:
		cmd = exec.CommandContext(ctx, "terraform", "validate", "-json")
	case stepTest:
		cmd = exec.CommandContext(ctx, "terraform", "test")
	case stepLintInit:
		cmd = exec.CommandContext(ctx, "tflint", "--init")
	default:
		cmd = exec.CommandContext(ctx, "tflint")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// runEach は、targets のそれぞれで step を起動し、出力を w へ流し、落ちたディレクトリを集めて失敗にします。
func runEach(name string, targets []target, w io.Writer, cmdOf func(target) *exec.Cmd) error {
	var failed []string
	for _, t := range targets {
		cmd := cmdOf(t)
		cmd.Stdout, cmd.Stderr = w, w
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(w, "%s: %s: %v\n", name, t.path, err)
			failed = append(failed, t.path)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(w, "❌ %s が落ちたディレクトリ: %s\n", name, strings.Join(failed, " "))

		return xerrors.Wrapf(errDirsFailed, "%s: %d 件", name, len(failed))
	}

	return nil
}

// runFmt は、root ごとに terraform fmt を起動します。fmt は root の下を再帰で見るので、ディレクトリごとには起動しません。
func runFmt(args []string, w io.Writer, tool toolFunc) error {
	var check bool
	s, err := parseCommon("fmt", args, w, func(f *flag.FlagSet) {
		f.BoolVar(&check, "check", false, "書き換えずに、整っていなければ落とす")
	})
	if err != nil {
		return err
	}
	if _, err := requireDirs(s); err != nil {
		return err
	}
	name, step := "tf-fmt", stepFmt
	if check {
		name, step = "tf-fmt-check", stepFmtCheck
	}
	targets := make([]target, 0, len(s.roots))
	for _, r := range s.roots {
		p := filepath.Join(s.base, filepath.FromSlash(r))
		if _, err := os.Stat(p); err != nil {
			continue
		}
		targets = append(targets, target{rel: r, path: p})
	}
	env := tfexec.Env(os.Environ())

	return runEach(name, targets, w, func(t target) *exec.Cmd { return tool(t.path, env, step) })
}

// parseData は、-data を持つサブコマンドの引数を読み、対象のディレクトリを返します。
func parseData(name string, args []string, w io.Writer, extra func(*flag.FlagSet)) (data string, targets []target, err error) {
	s, err := parseCommon(name, args, w, func(f *flag.FlagSet) {
		f.StringVar(&data, "data", "", "ディレクトリごとの TF_DATA_DIR を置くディレクトリ（絶対パス）")
		if extra != nil {
			extra(f)
		}
	})
	if err != nil {
		return "", nil, err
	}
	if !filepath.IsAbs(data) {
		return "", nil, xerrors.Wrapf(errUsage, "-data %q は絶対パスにする（terraform は相対の TF_DATA_DIR を作業ディレクトリから解く）", data)
	}
	targets, err = requireDirs(s)

	return data, targets, err
}

func runInit(args []string, w io.Writer, tool toolFunc) error {
	data, targets, err := parseData("init", args, w, nil)
	if err != nil {
		return err
	}
	return initTargets(data, targets, w, tool)
}

func initTargets(data string, targets []target, w io.Writer, tool toolFunc) error {
	return runEach("tf-init", targets, w, func(t target) *exec.Cmd {
		step := stepInit
		if isRoot(t.rel) {
			step = stepInitReadonly
		}

		return tool(t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), step)
	})
}

// runValidate は、各ディレクトリの `terraform validate -json` の出力と stderr を -out に置き、一覧（manifest.tsv）を
// 書き直します。判定は result が持ちます。一覧に載るファイルだけが読まれます。
func runValidate(args []string, w io.Writer, tool toolFunc) error {
	var out string
	data, targets, err := parseData("validate", args, w, func(f *flag.FlagSet) {
		f.StringVar(&out, "out", "", "出力と一覧を置くディレクトリ")
	})
	if err != nil {
		return err
	}
	if out == "" {
		return xerrors.Wrap(errUsage, "tf-gates validate は -out が要ります")
	}

	return validateTargets(data, out, targets, tool)
}

func validateTargets(data, out string, targets []target, tool toolFunc) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", out)
	}
	var manifest strings.Builder
	for i, t := range targets {
		jsonPath := filepath.Join(out, strconv.Itoa(i+1)+".json")
		errPath := filepath.Join(out, strconv.Itoa(i+1)+".err")
		stdout, err := os.Create(filepath.Clean(jsonPath))
		if err != nil {
			return xerrors.Wrapf(err, "%s の作成", jsonPath)
		}
		stderr, err := os.Create(filepath.Clean(errPath))
		if err != nil {
			_ = stdout.Close()

			return xerrors.Wrapf(err, "%s の作成", errPath)
		}
		cmd := tool(t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), stepValidate)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		code := 0
		if err := cmd.Run(); err != nil {
			code = -1
			var exit *exec.ExitError
			if xerrors.As(err, &exit) {
				code = exit.ExitCode()
			}
		}
		_ = stdout.Close()
		_ = stderr.Close()
		if code < 0 {
			return xerrors.Wrapf(errDirsFailed, "%s の validate を起動できません", t.path)
		}
		fmt.Fprintf(&manifest, "%s\t%d\t%s\t%s\n", t.path, code, jsonPath, errPath)
	}
	if err := os.WriteFile(filepath.Join(out, "manifest.tsv"), []byte(manifest.String()), 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", filepath.Join(out, "manifest.tsv"))
	}

	return nil
}

// runTest は、*.tftest.hcl を持つディレクトリで terraform test を起動します。そうしたディレクトリが0件なら落とします。
func runTest(args []string, w io.Writer, tool toolFunc) error {
	data, targets, err := parseData("test", args, w, nil)
	if err != nil {
		return err
	}
	var tested []target
	for _, t := range targets {
		ok, err := hasTests(t.path)
		if err != nil {
			return err
		}
		if ok {
			tested = append(tested, t)
		}
	}
	if len(tested) == 0 {
		return xerrors.Wrap(errNoTargets, "*.tftest.hcl を持つディレクトリが0件です")
	}

	return runEach("tf-test", tested, w, func(t target) *exec.Cmd {
		return tool(t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), stepTest)
	})
}

// runLint は、TFLint の ruleset を取得してから、各ディレクトリで TFLint を起動します。
func runLint(args []string, w io.Writer, tool toolFunc) error {
	var config, plugins string
	s, err := parseCommon("lint", args, w, func(f *flag.FlagSet) {
		f.StringVar(&config, "config", "", "TFLint の設定（.tflint.hcl）")
		f.StringVar(&plugins, "plugin-dir", "", "ruleset を置くディレクトリ")
	})
	if err != nil {
		return err
	}
	if config == "" || plugins == "" {
		return xerrors.Wrap(errUsage, "tf-gates lint は -config と -plugin-dir が要ります")
	}
	targets, err := requireDirs(s)
	if err != nil {
		return err
	}
	base := s.base
	config, err = filepath.Abs(config)
	if err != nil {
		return xerrors.Wrapf(err, "%s", config)
	}
	plugins, err = filepath.Abs(plugins)
	if err != nil {
		return xerrors.Wrapf(err, "%s", plugins)
	}
	if err := os.MkdirAll(plugins, 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", plugins)
	}
	env := tfexec.Env(os.Environ(), "TFLINT_CONFIG_FILE="+config, "TFLINT_PLUGIN_DIR="+plugins)
	if err := runEach("tf-lint の ruleset の取得", []target{{rel: ".", path: base}}, w, func(t target) *exec.Cmd {
		return tool(t.path, env, stepLintInit)
	}); err != nil {
		return err
	}

	return runEach("tf-lint", targets, w, func(t target) *exec.Cmd { return tool(t.path, env, stepLint) })
}
