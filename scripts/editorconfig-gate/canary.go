package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// marker は、範囲を開く無効化の印です。
const marker = "editorconfig-" + "checker-" + "disable"

const canaryFile = "canary.yaml"

// nonASCIIFile は、check-canary が canaryFile と同じ違反を置く、非 ASCII の名前のファイルです。core.quotepath=false
// が editorconfig-checker の git に届かなければ、このファイルは黙って走査から外れます。
const nonASCIIFile = "nested/deep/ünï.yaml"

// tabYAML は、.editorconfig が YAML に宣言する space に反する字下げです。
const tabYAML = "a:\n\tb: c\n"

const (
	editorconfigName  = ".editorconfig"
	checkerConfigName = ".editorconfig-checker.json"
)

type canarySpec struct {
	files map[string]string
	step  string
	judge func(r toolrun.Result) (bool, string)
	ok    string
}

// fixturePaths は、canary の fixture の置き場と、複写する2つの設定です。
type fixturePaths struct {
	work, editorconfig, checkerConfig string
}

func runCheckCanary(p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	return runCanary("check-canary", canarySpec{
		files: map[string]string{canaryFile: tabYAML, nonASCIIFile: tabYAML},
		step:  stepCheck,
		judge: judgeCheck,
		ok:    "tab で字下げした YAML で落ち、非 ASCII の名前のファイルも検査しました",
	}, p, w, tool, env)
}

func runDirectiveCanary(p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	return runCanary("directive-canary", canarySpec{
		files: map[string]string{canaryFile: "# " + marker + "\n" + tabYAML, "control.yaml": tabYAML},
		step:  stepCheck,
		judge: judgeDirective,
		ok:    "閉じない印の後ろは検査されませんでした（editorconfig-directive-lint の前提どおり）",
	}, p, w, tool, env)
}

// excludeDir と excludeControl は、exclude-canary が宣言した除外の名前と対照を置く入れ子のディレクトリと、対照の名前
// です。対照は宣言に無く、既定の除外にも当たらない名前です。
const (
	excludeDir             = "nested/deep"
	excludeControl         = "control.txt"
	excludeControlNonASCII = "cöntrol.txt"
)

func runExcludeCanary(p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	return runCanary("exclude-canary", canarySpec{
		files: excludeCanaryFiles(),
		step:  stepDryRun,
		judge: judgeExclude,
		ok:    "宣言した除外の名前はどの深さでも走査の対象から外れ、対照は対象に入りました",
	}, p, w, tool, env)
}

func excludeCanaryFiles() map[string]string {
	files := map[string]string{path.Join(excludeDir, excludeControl): "x\n", path.Join(excludeDir, excludeControlNonASCII): "x\n"}
	for _, n := range excludedNames {
		files[path.Join(excludeDir, n)] = "x\n"
	}

	return files
}

// judgeExclude は、0 で終わり、-dry-run の stdout が対照を載せ、宣言した除外の名前を1つも載せなければ成り立ちます。
func judgeExclude(r toolrun.Result) (bool, string) {
	if r.Code != 0 {
		return false, fmt.Sprintf("-dry-run が rc=%d で終わりました", r.Code)
	}
	listed := map[string]bool{}
	for _, p := range dryRunPaths(r.Stdout) {
		listed[p] = true
	}
	if !listed[path.Join(excludeDir, excludeControl)] {
		return false, "対照が -dry-run に載りませんでした。列挙が働いていません"
	}
	if !listed[path.Join(excludeDir, excludeControlNonASCII)] {
		return false, "非 ASCII の名前の対照が -dry-run に載りませんでした。core.quotepath=false が git に届いていません"
	}
	var leaked []string
	for _, n := range excludedNames {
		if listed[path.Join(excludeDir, n)] {
			leaked = append(leaked, n)
		}
	}
	if len(leaked) > 0 {
		return false, fmt.Sprintf("宣言した除外 %q が -dry-run に載りました。editorconfig-checker の既定の除外を読んで excludedNames を見直す", leaked)
	}

	return true, ""
}

func runCanary(name string, spec canarySpec, p fixturePaths, w io.Writer, tool toolFunc, env []string) error {
	if err := buildFixture(p.work, p.editorconfig, p.checkerConfig, spec.files); err != nil {
		return err
	}
	r, err := runInFixture(p.work, env, tool, spec.step)
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

// buildFixture は、まだ無い work を作り、2つの設定の複写と files（work からの相対パス）を置きます。.editorconfig が無い・空なら errRead、
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
	if err := os.MkdirAll(work, 0o750); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	all := map[string]string{editorconfigName: string(ec), checkerConfigName: string(cc)}
	for name, body := range files {
		all[name] = body
	}
	for name, body := range all {
		p := filepath.Join(work, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return xerrors.Wrapf(errRead, "%v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return xerrors.Wrapf(errRead, "%v", err)
		}
	}

	return nil
}

func runInFixture(work string, env []string, tool toolFunc, step string) (toolrun.Result, error) {
	for _, step := range []string{stepGitInit, stepGitAdd} {
		r, err := toolrun.Run(tool(context.Background(), work, env, step))
		if err != nil {
			return r, err
		}
		if r.Code != 0 {
			return r, xerrors.Wrapf(errTool, "fixture の %s が rc=%d で終わりました\n%s", step, r.Code, r.Combined)
		}
	}

	return toolrun.Run(tool(context.Background(), work, checkerEnv(env), step))
}

// judgeCheck は、0 以外で終わり、出力が canaryFile と nonASCIIFile の両方に字下げの違反を名指していれば成り立ちます。
func judgeCheck(r toolrun.Result) (bool, string) {
	flagged := flaggedFiles(stripANSI(r.Combined))
	switch {
	case r.Code == 0:
		return false, "tab で字下げした YAML で落ちませんでした"
	case flagged[canaryFile] && flagged[nonASCIIFile]:
		return true, ""
	case flagged[canaryFile]:
		return false, "非 ASCII の名前のファイルを検査しませんでした。core.quotepath=false が git に届いていません"
	default:
		return false, fmt.Sprintf("想定外の失敗です (rc=%d)", r.Code)
	}
}

// flaggedFiles は、editorconfig-checker の出力（字下げの無い `<パス>:` の見出しと、その下の字下げした違反の行）
// から、字下げの違反を持つパスを返します。
func flaggedFiles(out string) map[string]bool {
	flagged := map[string]bool{}
	current := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line != "" && line[0] != ' ' && line[0] != '\t' && strings.HasSuffix(line, ":"):
			current = strings.TrimSuffix(line, ":")
		case current != "" && strings.Contains(line, "wrong indent"):
			flagged[current] = true
		}
	}

	return flagged
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
