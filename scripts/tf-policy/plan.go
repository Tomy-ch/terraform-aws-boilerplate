package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// toolFunc は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。テストは差し替えます。
type toolFunc func(dir string, env []string, step string) *exec.Cmd

// 段の名前。command が引数を固定で持ちます。
const (
	stepInit       = "init"
	stepPlan       = "plan"
	stepVerify     = "verify"
	stepConftest   = "conftest"
	stepDeprecated = "deprecated"
)

// policyInput は、reference の root が Policy の入力を出す terraform test のファイルです。
const policyInput = "tests/policy-input.tftest.hcl"

// command は toolFunc の実物です。引数は段ごとに固定で、ディレクトリは作業ディレクトリ、設定は環境変数で渡します。
// Conftest と provider-schema の段は canary だけが使い、canary が建てた固定の配置（rego・decl.json・layer-decl.json・
// data.json・layer.json・out/plans.txt・out/provider-schema.json・out/inputs）を読みます。Rego のテストは宣言そのもの
// （decl.json・layer-decl.json）に対して走り、canary が差し替えるのは適用の段の宣言（data.json・layer.json）だけです。
// provider-schema は PATH のものを起動します（make が、ビルドした置き場を PATH の先頭に置く）。
func command(dir string, env []string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepDeprecated:
		cmd = exec.CommandContext(ctx, "provider-schema", "plan-deprecated", "-out", "out/provider-schema.json", "-list", "out/plans.txt")
	case stepInit:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-backend=false", "-input=false", "-lockfile=readonly")
	case stepPlan:
		cmd = exec.CommandContext(ctx, "terraform", "test", "-filter="+policyInput, "-json", "-verbose")
	case stepVerify:
		cmd = exec.CommandContext(ctx, "conftest", "verify", "--policy", "rego", "--data", "decl.json", "--data", "layer-decl.json", "-o", "json")
	default:
		cmd = exec.CommandContext(ctx, "conftest", "test", "--combine", "--all-namespaces", "--no-color", "-o", "json",
			"--policy", "rego", "--data", "data.json", "--data", "layer.json", "--data", "out/provider-schema.json", "out/inputs")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// root は reference の root です。name はユースケースの名前、rel は走査の基点からの相対パスです。
type root struct {
	name string
	rel  string
	path string
}

// listRoots は、<base>/usecases/*/reference のディレクトリを名前の辞書順で返します。0件なら errNoInput、
// policy-input.tftest.hcl を持たない root があれば errFailed です。
func listRoots(base string) ([]root, error) {
	entries, err := os.ReadDir(filepath.Join(base, "usecases"))
	if err != nil && !xerrors.Is(err, fs.ErrNotExist) {
		return nil, xerrors.Wrapf(errRead, "%v", err)
	}
	var roots []root
	var missing []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := "usecases/" + e.Name() + "/reference"
		p := filepath.Join(base, filepath.FromSlash(rel))
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(p, filepath.FromSlash(policyInput))); err != nil {
			missing = append(missing, p)

			continue
		}
		roots = append(roots, root{name: e.Name(), rel: rel, path: p})
	}
	if len(missing) > 0 {
		return nil, xerrors.Wrapf(errFailed, "%s を持たない reference の root: %s", policyInput, strings.Join(missing, " "))
	}
	if len(roots) == 0 && len(missing) == 0 {
		return nil, xerrors.Wrapf(errNoInput, "Policy の入力にする reference の root が0件です（%s/usecases/*/reference）", base)
	}

	return roots, nil
}

// errorMessages は、terraform test -json の出力から error の行の本文を返します。
func errorMessages(jsonl []byte) []string {
	var msgs []string
	sc := bufio.NewScanner(bytes.NewReader(jsonl))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var line struct {
			Level   string `json:"@level"`
			Message string `json:"@message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Level == "error" {
			msgs = append(msgs, line.Message)
		}
	}

	return msgs
}

// planRoots は、roots のそれぞれで Policy の入力を出す terraform test を走らせ、出力を <out>/runs/<name>.jsonl に
// 置き、一覧を <out>/plans.txt に書きます。落ちた root は最後まで走らせてから名指します。
func planRoots(roots []root, data, out string, w io.Writer, tool toolFunc) error {
	runs := filepath.Join(out, "runs")
	if err := os.RemoveAll(out); err != nil {
		return xerrors.Wrapf(err, "%s の削除", out)
	}
	if err := os.MkdirAll(runs, 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", runs)
	}
	var plans, failed []string
	for _, r := range roots {
		var stdout bytes.Buffer
		cmd := tool(r.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, r.rel)), stepPlan)
		cmd.Stdout, cmd.Stderr = &stdout, w
		runErr := cmd.Run()
		jsonl := filepath.Join(runs, r.name+".jsonl")
		if err := os.WriteFile(jsonl, stdout.Bytes(), 0o600); err != nil {
			return xerrors.Wrapf(err, "%s の書き込み", jsonl)
		}
		if runErr != nil {
			for _, m := range errorMessages(stdout.Bytes()) {
				fmt.Fprintln(w, m)
			}
			failed = append(failed, r.path)

			continue
		}
		plans = append(plans, jsonl)
	}
	if err := os.WriteFile(filepath.Join(out, "plans.txt"), []byte(joinLines(plans)), 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", filepath.Join(out, "plans.txt"))
	}
	if len(failed) > 0 {
		fmt.Fprintf(w, "❌ tf-policy が落ちたディレクトリ: %s\n", strings.Join(failed, " "))

		return xerrors.Wrapf(errFailed, "plan が落ちた root %d 件", len(failed))
	}

	return nil
}

// canaryFunc は、本走査の前に走らせる canary です。テストは差し替えます。
type canaryFunc func(args []string, w io.Writer, tool toolFunc) error

func runPlan(args []string, w io.Writer, tool toolFunc, canary canaryFunc) error {
	fset := flag.NewFlagSet("tf-policy plan", flag.ContinueOnError)
	fset.SetOutput(w)
	base := fset.String("base", "", "走査の基点（TF_BASE）")
	data := fset.String("data", "", "ディレクトリごとの TF_DATA_DIR を置くディレクトリ（tf-init と同じ。絶対パス）")
	out := fset.String("out", "", "出力の置き場（毎回作り直す）")
	canaryData := fset.String("canary-data", "", "canary の TF_DATA_DIR を置くディレクトリ（絶対パス）")
	canaryWork := fset.String("canary-work", "", "canary の fixture を建てるディレクトリ")
	policy := fset.String("policy", "", "Policy の木（conftest/plan）")
	planDecl := fset.String("decl", "", "Policy の宣言（conftest/plan/decl.json）")
	layerDecl := fset.String("layer", "", "層の宣言（conftest/hcl/layer/decl.json）")
	reference := fset.String("reference", "", "canary の aws の fixture に写す versions.tf と lock の root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *base == "" || *out == "" || !filepath.IsAbs(*data) || fset.NArg() > 0 ||
		*canaryData == "" || *canaryWork == "" || *policy == "" || *planDecl == "" || *layerDecl == "" || *reference == "" {
		return xerrors.Wrap(errUsage, "tf-policy plan -base <dir> -data <絶対パス> -out <dir> -canary-data <絶対パス> -canary-work <dir> -policy <dir> -decl <path> -layer <path> -reference <dir>")
	}
	if err := canary([]string{"-data", *canaryData, "-work", *canaryWork, "-policy", *policy, "-decl", *planDecl, "-layer", *layerDecl, "-reference", *reference}, w, tool); err != nil {
		return xerrors.Wrap(err, "canary が落ちたので、本走査を行いません")
	}
	roots, err := listRoots(*base)
	if err != nil {
		return err
	}

	return planRoots(roots, *data, *out, w, tool)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n") + "\n"
}

// readLines は、1行に1件の一覧を読み、空の行を除いて返します。
func readLines(p string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%s: %v", p, err)
	}

	return slices.DeleteFunc(strings.Split(string(raw), "\n"), func(s string) bool { return s == "" }), nil
}
