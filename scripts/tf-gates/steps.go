package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type toolFunc = toolrun.Func

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

func command(ctx context.Context, dir string, env []string, step string, _ ...string) *exec.Cmd {
	var cmd *exec.Cmd
	switch step {
	case stepFmt:
		cmd = exec.CommandContext(ctx, "terraform", "fmt", "-recursive")
	case stepFmtCheck:
		cmd = exec.CommandContext(ctx, "terraform", "fmt", "-check", "-diff", "-recursive")
	case stepInit:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-backend=false", "-input=false")
	case stepInitReadonly:
		// root の init は lock を書き換えない。lock の更新は人が root で行う。
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

// 検出を表す終了コードです。terraform fmt -check は差分があれば 3、TFLint は指摘があれば 2 で終わり、ほかの 0 以外は
// 道具が検査を成り立たせられなかったことです。どちらも公式の文書は値を持ちません。値がずれても 0 以外は通しません。
var (
	detectFmtCheck = []int{3}
	detectLint     = []int{2}
)

// runEach は、targets のそれぞれで step を起動し、出力を w へ流し、落ちたディレクトリを集めて失敗にします。
// detect が空なら 0 以外をすべて落ちたディレクトリとし、空でなければ detect の値だけを落ちたディレクトリ、ほかの
// 0 以外を [errToolFailed] とします。[errTool] と [errToolFailed] のときは、残りを起動せずに返します。それまでに
// 落ちたディレクトリは文面にだけ添えます。
func runEach(name string, targets []target, w io.Writer, detect []int, cmdOf func(target) *exec.Cmd) error {
	var failed []string
	abort := func(err error, t target) error {
		if len(failed) > 0 {
			return xerrors.Wrapf(err, "%s: %s で打ち切りました（それまでに落ちたディレクトリ: %s）", name, t.path, strings.Join(failed, " "))
		}

		return xerrors.Wrapf(err, "%s: %s で打ち切りました", name, t.path)
	}
	for _, t := range targets {
		cmd := cmdOf(t)
		cmd.Stdout, cmd.Stderr = w, w
		code, err := toolrun.Exec(cmd)
		if err != nil {
			return abort(err, t)
		}
		if code != 0 && len(detect) > 0 && !slices.Contains(detect, code) {
			return abort(xerrors.Wrapf(errToolFailed, "exit status %d", code), t)
		}
		if code != 0 {
			fmt.Fprintf(w, "%s: %s: exit status %d\n", name, t.path, code)
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
		_, err := os.Stat(p)
		if xerrors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return xerrors.Wrapf(err, "%s", p)
		}
		targets = append(targets, target{rel: r, path: p})
	}
	env := tfexec.Env(os.Environ())

	var detect []int
	if check {
		detect = detectFmtCheck
	}

	return runEach(name, targets, w, detect, func(t target) *exec.Cmd { return tool(context.Background(), t.path, env, step) })
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
	return runEach("tf-init", targets, w, nil, func(t target) *exec.Cmd {
		step := stepInit
		if isRoot(t.rel) {
			step = stepInitReadonly
		}

		return tool(context.Background(), t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), step)
	})
}

type canaryFunc func(args []string, w io.Writer, tool toolFunc) error

// runValidate は、canary を走らせてから、各ディレクトリの `terraform validate -json` の出力と stderr を -out に置き、
// 一覧（manifest.tsv）を書き直します。判定は result が持ちます。一覧に載るファイルだけが読まれます。root はリポジトリ
// 直下です。
func runValidate(args []string, w io.Writer, tool toolFunc, canary canaryFunc, root string) error {
	var out, canaryData, canaryWork, canaryReference string
	data, targets, err := parseData("validate", args, w, func(f *flag.FlagSet) {
		f.StringVar(&out, "out", "", "出力と一覧を置くディレクトリ")
		f.StringVar(&canaryData, "canary-data", "", "canary の TF_DATA_DIR を置くディレクトリ（絶対パス）")
		f.StringVar(&canaryWork, "canary-work", "", "canary の fixture を建てるディレクトリ")
		f.StringVar(&canaryReference, "canary-reference", "", "canary に写す versions.tf と lock の root")
	})
	if err != nil {
		return err
	}
	if out == "" || canaryData == "" || canaryWork == "" || canaryReference == "" {
		return xerrors.Wrap(errUsage, "tf-gates validate は -out・-canary-data・-canary-work・-canary-reference が要ります")
	}
	if _, err := workdir.Resolve(root, canaryWork); err != nil {
		return xerrors.Wrapf(errUsage, "-canary-work %v", err)
	}
	if err := canary([]string{"-data", canaryData, "-work", canaryWork, "-reference", canaryReference}, w, tool); err != nil {
		return xerrors.Wrap(err, "canary が落ちたので、本走査を行いません")
	}

	return validateTargets(data, out, targets, tool)
}

// validateTargets は、[errTool] のときは、残りを起動せずに一覧を書かないまま [errTool] を返します。前の起動の一覧は先に消し、打ち切った後に古い一覧が残らないようにします。
func validateTargets(data, out string, targets []target, tool toolFunc) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", out)
	}
	manifestPath := filepath.Join(out, "manifest.tsv")
	if err := os.Remove(manifestPath); err != nil && !xerrors.Is(err, fs.ErrNotExist) {
		return xerrors.Wrapf(err, "%s の削除", manifestPath)
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
		cmd := tool(context.Background(), t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), stepValidate)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		code, err := toolrun.Exec(cmd)
		_ = stdout.Close()
		_ = stderr.Close()
		if err != nil {
			return xerrors.Wrapf(err, "%s の validate で打ち切りました", t.path)
		}
		fmt.Fprintf(&manifest, "%s\t%d\t%s\t%s\n", t.path, code, jsonPath, errPath)
	}
	if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", manifestPath)
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

	return runEach("tf-test", tested, w, nil, func(t target) *exec.Cmd {
		return tool(context.Background(), t.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, t.rel)), stepTest)
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
	if err := runEach("tf-lint の ruleset の取得", []target{{rel: ".", path: base}}, w, nil, func(t target) *exec.Cmd {
		return tool(context.Background(), t.path, env, stepLintInit)
	}); err != nil {
		return err
	}

	return runEach("tf-lint", targets, w, detectLint, func(t target) *exec.Cmd { return tool(context.Background(), t.path, env, stepLint) })
}
