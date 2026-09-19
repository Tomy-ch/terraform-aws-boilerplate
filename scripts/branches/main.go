// Package main は、分岐のパターンの単一宣言を読み、生成先へ反映・検証するツール。
//
//	list <set>    集合の要素を1行ずつ出す
//	get <key>     単一の値を出す（default.branch / release.prefix / release.pattern / feature.prefix）
//	apply         生成先へ反映する
//	check         生成先が宣言からずれていないかを検査する
//
// **同じパターンが2箇所に在ると、片方だけが古くなる**（ADR-0603 決定1-3）。保護設定の宣言と
// workflow の起動条件は生成し、生成できない側（Go のツール群）は実行時に list / get で読む。
//
// ずれは静かに壊れる。保護対象から漏れたブランチは「保護されていない」ではなく
// 「保護されているつもり」になり、起動条件から漏れたブランチでは検査が走らないまま緑になる。
// だから check を持ち、宣言に無い workflow が on.push.branches を持っていることも違反とする。
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branches"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	protectionFile = ".github/settings/branch-protection.json"
	workflowDir    = ".github/workflows"
	refPrefix      = "refs/heads/"
	filePerm       = 0o644
	// protectedSet は保護設定へ渡す集合の名前。ここだけ用途が固定されている。
	protectedSet = "protected"
)

var (
	// errUsage は、サブコマンドやキーの与え方が誤っていることを表す。
	errUsage = xerrors.New("invalid usage")
	// errDrift は、生成先が宣言からずれていることを表す。
	errDrift = xerrors.New("生成先が宣言からずれています")
	// errOrphanWorkflow は、宣言に無い workflow が on.push.branches を持つことを表す。
	errOrphanWorkflow = xerrors.New("宣言に無い workflow が on.push.branches を持っています")
	// errMissingWorkflow は、宣言が指す workflow が存在しないことを表す。
	errMissingWorkflow = xerrors.New("宣言が指す workflow がありません")
)

// main は 1:1 テスト規約の対象外で分岐を検査できないため、判断は run に置きます。
func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、サブコマンドを解釈して固定処理へ振り分けます。
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "usage: branches <list|get|apply|check> [引数]")
	}

	// 宣言を読む前にサブコマンドを検める。逆にすると、打ち間違えたときに
	// 「ファイルが無い」と言われ、読む人は存在するファイルを探しに行く。
	switch args[0] {
	case "list", "get":
		if len(args) != 2 {
			return xerrors.Wrap(errUsage, "usage: branches "+args[0]+" <name>")
		}
	case "apply", "check":
		if len(args) != 1 {
			return xerrors.Wrap(errUsage, "usage: branches "+args[0]+"（引数は取りません）")
		}
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}

	decl, err := branches.Load(branches.File)
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		return listSet(decl, args[1], out)
	case "get":
		return getScalar(decl, args[1], out)
	case "apply":
		return applyOrCheck(decl, false, out)
	default:
		return applyOrCheck(decl, true, out)
	}
}

// listSet は集合の要素を1行ずつ書き出します。
func listSet(decl branches.Declaration, name string, out io.Writer) error {
	items, err := decl.Set(name)
	if err != nil {
		return err
	}

	for _, item := range items {
		fmt.Fprintln(out, item)
	}

	return nil
}

// getScalar は単一の値を書き出します。
func getScalar(decl branches.Declaration, key string, out io.Writer) error {
	value, err := decl.Scalar(key)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, value)

	return nil
}

// applyOrCheck は生成先を宣言へ揃えます。dryRun=true は書き換えず、ずれを報告して
// 非ゼロで終わります。**全ファイルを読み切って判定を確定させてから書き込む** ——
// 1ファイルずつ書きながら進むと、途中で中断したときに作業ツリーだけが半端に変わります。
func applyOrCheck(decl branches.Declaration, dryRun bool, out io.Writer) error {
	plans, err := plan(decl)
	if err != nil {
		return err
	}

	var drifted []string
	for _, p := range plans {
		if p.before != p.after {
			drifted = append(drifted, p.path)
		}
	}

	if dryRun {
		if len(drifted) > 0 {
			for _, path := range drifted {
				fmt.Fprintf(out, "❌ %s が .github/branches.toml からずれています\n", path)
			}

			return xerrors.Wrapf(errDrift, "%d 件", len(drifted))
		}

		fmt.Fprintf(out, "✅ branches-check: 生成先 %d 件が宣言と一致しています\n", len(plans))

		return nil
	}

	for _, p := range plans {
		if p.before == p.after {
			continue
		}
		if err := os.WriteFile(p.path, []byte(p.after), filePerm); err != nil {
			return xerrors.Wrap(err, p.path)
		}
	}

	fmt.Fprintf(out, "✅ branches-apply: %d ファイルへ反映しました（対象 %d 件）\n", len(drifted), len(plans))

	return nil
}

// rewrite は1ファイルぶんの書き換え計画。
type rewrite struct {
	path   string
	before string
	after  string
}

// plan は生成先ごとの書き換え後の内容を決めます。宣言が指す先の不在も、宣言に無い
// 生成先の存在も、ここで違反として返します。**片側だけを見ると、取りこぼした宣言が
// 古いパターンのまま動き続けます。**
func plan(decl branches.Declaration) ([]rewrite, error) {
	protected, err := decl.Set(protectedSet)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(protectionFile)
	if err != nil {
		return nil, xerrors.Wrap(err, protectionFile)
	}
	rewritten, err := rewriteProtection(string(content), protected)
	if err != nil {
		return nil, xerrors.Wrap(err, protectionFile)
	}
	plans := []rewrite{{path: protectionFile, before: string(content), after: rewritten}}

	mapping := decl.Workflows()
	names := make([]string, 0, len(mapping))
	for name := range mapping {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		items, err := decl.Set(mapping[name])
		if err != nil {
			return nil, xerrors.Wrap(err, name)
		}

		path := filepath.Join(workflowDir, name)
		body, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, xerrors.Wrap(errMissingWorkflow, path)
		}

		after, err := rewritePushBranches(string(body), items)
		if err != nil {
			return nil, xerrors.Wrap(err, path)
		}
		plans = append(plans, rewrite{path: path, before: string(body), after: after})
	}

	if err := checkOrphans(decl); err != nil {
		return nil, err
	}

	return plans, nil
}

// checkOrphans は、宣言に無い workflow が on.push.branches を持っていないかを見ます。
func checkOrphans(decl branches.Declaration) error {
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return xerrors.Wrap(err, workflowDir)
	}

	mapping := decl.Workflows()

	var orphans []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if _, ok := mapping[e.Name()]; ok {
			continue
		}

		body, err := os.ReadFile(filepath.Clean(filepath.Join(workflowDir, e.Name())))
		if err != nil {
			return xerrors.Wrap(err, e.Name())
		}
		if _, _, found := findPushBranches(string(body)); found {
			orphans = append(orphans, e.Name())
		}
	}

	if len(orphans) > 0 {
		return xerrors.Wrap(errOrphanWorkflow, strings.Join(orphans, " / "))
	}

	return nil
}

// rewriteProtection は保護設定の include を宣言から組み直します。値は refs/heads/ を
// 前置した形で、順序は宣言のとおりとします。
func rewriteProtection(content string, protected []string) (string, error) {
	lines := strings.Split(content, "\n")

	start, end := -1, -1
	for i, line := range lines {
		if strings.Contains(line, `"include"`) {
			start = i

			continue
		}
		if start >= 0 && strings.TrimSpace(line) == "]" {
			end = i

			break
		}
	}
	if start < 0 || end < 0 {
		return "", xerrors.Wrap(errDrift, `conditions.ref_name.include が見つかりません`)
	}

	indent := strings.Repeat(" ", len(lines[start])-len(strings.TrimLeft(lines[start], " "))+2)
	block := []string{lines[start]}
	for i, p := range protected {
		comma := ","
		if i == len(protected)-1 {
			comma = ""
		}
		block = append(block, indent+`"`+refPrefix+p+`"`+comma)
	}

	return strings.Join(slices.Concat(lines[:start], block, lines[end:]), "\n"), nil
}

// findPushBranches は on.push.branches の要素行の範囲を返します。
// found=false は、その workflow が push 側の起動条件を絞っていないことを表します。
func findPushBranches(content string) (int, int, bool) {
	lines := strings.Split(content, "\n")

	inOn, inPush := false, false
	for i, line := range lines {
		switch {
		case line == "on:":
			inOn = true
		case inOn && line == "  push:":
			inPush = true
		case inOn && len(line) > 0 && !strings.HasPrefix(line, " "):
			return 0, 0, false
		case inPush && line == "    branches:":
			end := i + 1
			for end < len(lines) && strings.HasPrefix(lines[end], "      - ") {
				end++
			}

			return i + 1, end, true
		case inPush && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    "):
			inPush = false
		}
	}

	return 0, 0, false
}

// rewritePushBranches は on.push.branches を宣言から組み直します。
func rewritePushBranches(content string, items []string) (string, error) {
	start, end, found := findPushBranches(content)
	if !found {
		return "", xerrors.Wrap(errDrift, "on.push.branches が見つかりません")
	}

	lines := strings.Split(content, "\n")
	block := make([]string, 0, len(items))
	for _, item := range items {
		block = append(block, "      - "+quoteIfNeeded(item))
	}

	return strings.Join(slices.Concat(lines[:start], block, lines[end:]), "\n"), nil
}

// quoteIfNeeded は、YAML が別の意味に取る文字を含む値を引用符で囲みます。
// `*` を裸で置くとエイリアスの指定として読まれます。
func quoteIfNeeded(item string) string {
	if strings.ContainsAny(item, "*[]{}#&,") {
		return "'" + item + "'"
	}

	return item
}
