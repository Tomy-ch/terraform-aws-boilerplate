package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// marker は、範囲を開く無効化の印です。
const marker = "editorconfig-" + "checker-" + "disable"

const canaryFile = "canary.yaml"

// tabYAML は、tab で字下げした YAML です。.editorconfig は YAML に space を宣言します。
const tabYAML = "a:\n\tb: c\n"

const (
	editorconfigName  = ".editorconfig"
	checkerConfigName = ".editorconfig-checker.json"
)

type canarySpec struct {
	files map[string]string
	judge func(r toolrun.Result) (bool, string)
	ok    string
}

// fixturePaths は、canary の fixture の置き場と、複写する2つの設定です。
type fixturePaths struct {
	work, editorconfig, checkerConfig string
}

func runCheckCanary(p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	return runCanary("check-canary", canarySpec{
		files: map[string]string{canaryFile: tabYAML},
		judge: judgeCheck,
		ok:    "tab で字下げした YAML で落ちました",
	}, p, w, tool, env)
}

func runDirectiveCanary(p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	return runCanary("directive-canary", canarySpec{
		files: map[string]string{canaryFile: "# " + marker + "\n" + tabYAML, "control.yaml": tabYAML},
		judge: judgeDirective,
		ok:    "閉じない印の後ろは検査されませんでした（editorconfig-directive-lint の前提どおり）",
	}, p, w, tool, env)
}

// runCanary は、fixture を建ててその中のリポジトリで editorconfig-checker を走らせ、spec.judge で判定します。
func runCanary(name string, spec canarySpec, p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	if err := buildFixture(p.work, p.editorconfig, p.checkerConfig, spec.files); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(p.work) }()
	r, err := runInFixture(p.work, env, tool)
	if err != nil {
		return err
	}
	if ok, reason := spec.judge(r); !ok {
		fmt.Fprintf(w, "--- editorconfig-checker（rc=%d）:\n%s\n", r.Code, r.Combined)

		return xerrors.Wrap(errCanary, reason)
	}
	fmt.Fprintf(w, "✅ editorconfig %s: %s\n", name, spec.ok)

	return nil
}

// buildFixture は、work を作り直し、2つの設定の複写と files を置きます。.editorconfig が無い・空なら errRead、
// .editorconfig-checker.json が `{}` でなければ errScope です。
func buildFixture(work, editorconfig, checkerConfig string, files map[string]string) error {
	ec, err := os.ReadFile(filepath.Clean(editorconfig))
	if err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	if len(bytes.TrimSpace(ec)) == 0 {
		return xerrors.Wrapf(errRead, "%s が空です", editorconfig)
	}
	cc, err := readCheckerConfig(checkerConfig)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(work); err != nil {
		return xerrors.Wrapf(errRead, "%s の削除: %v", work, err)
	}
	if err := os.MkdirAll(work, 0o750); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	all := map[string]string{editorconfigName: string(ec), checkerConfigName: string(cc)}
	for name, body := range files {
		all[name] = body
	}
	for name, body := range all {
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o600); err != nil {
			return xerrors.Wrapf(errRead, "%v", err)
		}
	}

	return nil
}

// runInFixture は、work にリポジトリを建ててファイルを追跡させ、引数なしの editorconfig-checker を走らせます。
func runInFixture(work string, env []string, tool toolFunc) (toolrun.Result, error) {
	for _, step := range []string{stepGitInit, stepGitAdd} {
		r, err := toolrun.Run(tool(work, env, step))
		if err != nil {
			return r, err
		}
		if r.Code != 0 {
			return r, xerrors.Wrapf(errTool, "fixture の %s が rc=%d で終わりました\n%s", step, r.Code, r.Combined)
		}
	}

	return toolrun.Run(tool(work, env, stepCheck))
}

// judgeCheck は、0 以外で終わり、出力が canary.yaml の後に字下げの違反を名指していれば成り立ちます。
func judgeCheck(r toolrun.Result) (bool, string) {
	out := stripANSI(r.Combined)
	switch {
	case r.Code == 0:
		return false, "tab で字下げした YAML で落ちませんでした"
	case containsInOrder(out, canaryFile, "wrong indent"):
		return true, ""
	default:
		return false, fmt.Sprintf("想定外の失敗です (rc=%d)", r.Code)
	}
}

// judgeDirective は、0 以外で終わり、出力が印の無い対照（control.yaml）だけを名指していれば成り立ちます。
// 印を置いた canary.yaml の違反が出れば、閉じない印の後ろも検査されています。
func judgeDirective(r toolrun.Result) (bool, string) {
	out := stripANSI(r.Combined)
	switch {
	case r.Code == 0:
		return false, "印の無い対照の違反でも落ちませんでした。走査が働いていません"
	case strings.Contains(out, canaryFile):
		return false, "閉じない印の後ろの違反を検査しました。editorconfig-directive-lint の分類を見直す"
	case strings.Contains(out, "control.yaml"):
		return true, ""
	default:
		return false, fmt.Sprintf("想定外の失敗です (rc=%d)", r.Code)
	}
}

func containsInOrder(s, first, second string) bool {
	_, after, ok := strings.Cut(s, first)

	return ok && strings.Contains(after, second)
}
