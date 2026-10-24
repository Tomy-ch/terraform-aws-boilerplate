// Package main は、boilerplate のサンプルを派生リポジトリから取り除くツール。
//
//	check            印の形と manifest を確かめる（書き換えなし・CI 用）
//	apply [--dry-run] 印の区間を取り除き、manifest のパスを消し、使われなくなった pin を外す
//
// サンプルとは、派生リポジトリが自分の構成に差し替えるか、外部の契約を要するために外して
// よいものを指す。ファイルごと消すものは manifest が、ファイルの一部だけを消すものは
// `sample:` の印が持つ。印の形は lib/markers が持つ。
//
// apply は書く前にすべてを決める。印の解釈に失敗したら、作業ツリーへ何も書かない。
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/markers"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// namespace は、サンプルの印の名前空間。
	namespace = "sample"
	// pinFile は、GitHub Actions の pin の SSOT。
	pinFile = ".github/actions-pin.toml"
	// protectionFile は、必須検査の宣言。JSON なので印を置けない。
	protectionFile = ".github/settings/branch-protection.json"
)

// domain は、まとめて取り除くサンプルの単位。
type domain struct {
	name string
	// paths は、ファイルまたはディレクトリごと消すもの。リポジトリのルートからの相対パス。
	paths []string
	// pins は、このサンプルが持ち込んだ pin のキー。消したあとどこからも参照されなければ外す。
	pins []string
	// contexts は、このサンプルが必須検査の宣言へ持ち込んだ context。
	contexts []string
}

// manifest は、取り除くサンプルの一覧。最後の domain はこのツール自身を消す。
var manifest = []domain{
	{
		name:  "sonarcloud",
		paths: []string{".github/workflows/sonarcloud.yaml", "sonar-project.properties"},
		pins:  []string{"SonarSource/sonarqube-scan-action@v8.2.1"},
	},
	{
		name:  "dependency-review",
		paths: []string{".github/workflows/dependency-review.yaml"},
		pins:  []string{"actions/dependency-review-action@v5.0.0"},
	},
	{
		name: "sample-remove",
		paths: []string{
			"scripts/sample-remove",
			"scripts/lib/markers",
			".github/workflows/sample-remove-check.yaml",
			".makefiles/github/operation/sample-remove.mk",
		},
		contexts: []string{"sample-remove-check"},
	},
}

var (
	errUsage = xerrors.New("❌ usage: sample-remove <check|apply [--dry-run]>")
	// errNoFiles は、走査対象のファイルが1件も無いことを表す。
	errNoFiles = xerrors.New("❌ git ls-files が1件も返しませんでした")
	// errNoTargets は、印も manifest のパスも1件も無く、取り除くものが無いことを表す。
	errNoTargets = xerrors.New("❌ sample: の印と manifest のパスが1件もありません")
	// errMissingPath は、manifest のパスが実在しないことを表す。
	errMissingPath = xerrors.New("❌ manifest のパスがありません")
	// errMissingPin は、manifest の pin のキーが lockfile に無いことを表す。
	errMissingPin = xerrors.New("❌ manifest の pin のキーが " + pinFile + " にありません")
	// errBadMarker は、印を解釈できないファイルがあることを表す。
	errBadMarker = xerrors.New("❌ sample: の印を解釈できません")
	// errMissingContext は、manifest の context が必須検査の宣言に無いことを表す。
	errMissingContext = xerrors.New("❌ manifest の context が " + protectionFile + " にありません")
	// errRead は、走査対象のファイルを読めないことを表す。
	errRead = xerrors.New("❌ ファイルを読めません")
)

// plan は、apply が書き換えるものを書く前にすべて決めたもの。
type plan struct {
	// writes は、パス→新しい内容。
	writes map[string]string
	// deletes は、消すパス。
	deletes []string
	// markerCount は、取り除く印の数。
	markerCount int
	// markedFiles は、印を持つファイル。
	markedFiles []string
	// droppedPins は、外す pin のキー。
	droppedPins []string
	// droppedContexts は、必須検査の宣言から外す context。
	droppedContexts []string
}

func main() {
	log.SetFlags(0)
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("❌ 作業ディレクトリを取得できません: %v", err)
	}
	if err := run(os.Args[1:], root, gitLsFiles, os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// gitLsFiles は、root で追跡されているファイルの一覧を返します。
func gitLsFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrap(err, "git ls-files")
	}
	var files []string
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) > 0 {
			files = append(files, string(f))
		}
	}
	return files, nil
}

// run は、サブコマンドを選んで実行します。
func run(args []string, root string, list func(string) ([]string, error), w io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch {
	case args[0] == "check" && len(args) == 1:
		p, err := build(root, manifest, list)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "✅ sample-remove check: 印 %d 個（%d ファイル）、manifest のパス %d 件、pin %d 件を確認しました\n",
			p.markerCount, len(p.markedFiles), len(p.deletes), countPins(manifest))
		return nil
	case args[0] == "apply" && len(args) == 1, args[0] == "apply" && len(args) == 2 && args[1] == "--dry-run":
		p, err := build(root, manifest, list)
		if err != nil {
			return err
		}
		report(w, p)
		if len(args) == 2 {
			fmt.Fprintln(w, "（--dry-run のため何も書き換えていません）")
			return nil
		}
		return apply(root, p)
	default:
		return errUsage
	}
}

// countPins は、manifest が宣言する pin の数を返します。
func countPins(ds []domain) int {
	n := 0
	for _, d := range ds {
		n += len(d.pins)
	}
	return n
}

// build は、manifest と追跡されているファイルから plan を作ります。何も書き換えません。
func build(root string, ds []domain, list func(string) ([]string, error)) (plan, error) {
	p := plan{writes: map[string]string{}}
	for _, d := range ds {
		for _, path := range d.paths {
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				return plan{}, xerrors.Wrapf(errMissingPath, "%s（%s）", path, d.name)
			}
			p.deletes = append(p.deletes, path)
		}
	}
	files, err := list(root)
	if err != nil {
		return plan{}, err
	}
	if len(files) == 0 {
		return plan{}, errNoFiles
	}
	m := markers.New(namespace)
	for _, f := range files {
		if under(f, p.deletes) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return plan{}, xerrors.Wrapf(errRead, "%s: %v", f, err)
		}
		content := string(b)
		n, err := m.Count(content)
		if err != nil {
			return plan{}, xerrors.Join(errBadMarker, xerrors.Wrap(err, f))
		}
		if n == 0 {
			continue
		}
		stripped, err := m.Strip(content)
		if err != nil {
			return plan{}, xerrors.Join(errBadMarker, xerrors.Wrap(err, f))
		}
		p.writes[f] = stripped
		p.markerCount += n
		p.markedFiles = append(p.markedFiles, f)
	}
	if p.markerCount == 0 && len(p.deletes) == 0 {
		return plan{}, errNoTargets
	}
	if err := prunePins(root, ds, files, &p); err != nil {
		return plan{}, err
	}
	if err := dropContexts(root, ds, &p); err != nil {
		return plan{}, err
	}
	return p, nil
}

// prunePins は、manifest の pin のうち、取り除いたあとどの workflow / action からも参照されない
// ものを lockfile から外す書き換えを p へ加えます。
func prunePins(root string, ds []domain, files []string, p *plan) error {
	lockPath := filepath.Join(root, pinFile)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", pinFile, err)
	}
	lock := string(b)
	if s, ok := p.writes[pinFile]; ok {
		lock = s
	}
	lines := strings.Split(lock, "\n")
	for _, d := range ds {
		for _, key := range d.pins {
			idx := -1
			for i, line := range lines {
				if strings.HasPrefix(line, `"`+key+`" =`) {
					idx = i
					break
				}
			}
			if idx < 0 {
				return xerrors.Wrapf(errMissingPin, "%s（%s）", key, d.name)
			}
			referenced, err := pinReferenced(root, key, files, p)
			if err != nil {
				return err
			}
			if referenced {
				continue
			}
			lines = append(lines[:idx], lines[idx+1:]...)
			p.droppedPins = append(p.droppedPins, key)
		}
	}
	if len(p.droppedPins) > 0 {
		p.writes[pinFile] = strings.Join(lines, "\n")
	}
	return nil
}

// pinReferenced は、pin のキーが指す action を、取り除いたあとに残る workflow / action が
// `uses:` で参照しているかを返します。
func pinReferenced(root, key string, files []string, p *plan) (bool, error) {
	action := key[:strings.LastIndex(key, "@")]
	uses := regexp.MustCompile(`uses:\s*["']?` + regexp.QuoteMeta(action) + `@`)
	for _, f := range files {
		if !strings.HasPrefix(f, ".github/workflows/") && !strings.HasPrefix(f, ".github/actions/") {
			continue
		}
		if under(f, p.deletes) {
			continue
		}
		content, ok := p.writes[f]
		if !ok {
			b, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				return false, xerrors.Wrapf(errRead, "%s: %v", f, err)
			}
			content = string(b)
		}
		if uses.MatchString(content) {
			return true, nil
		}
	}
	return false, nil
}

// dropContexts は、manifest の context を必須検査の宣言から外す書き換えを p へ加えます。
//
// 宣言は1つの context を `{` / `"context": "<名前>"` / `}` の3行で書く。この形に当たらない
// context は、外したつもりで残ることになるのでエラーにする。
func dropContexts(root string, ds []domain, p *plan) error {
	var targets []string
	for _, d := range ds {
		targets = append(targets, d.contexts...)
	}
	if len(targets) == 0 {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(root, protectionFile))
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", protectionFile, err)
	}
	lines := strings.Split(string(b), "\n")
	for _, ctx := range targets {
		idx := -1
		for i, line := range lines {
			if strings.TrimSpace(line) == `"context": "`+ctx+`"` {
				idx = i
				break
			}
		}
		if idx < 1 || idx+1 >= len(lines) || strings.TrimSpace(lines[idx-1]) != "{" || !strings.HasPrefix(strings.TrimSpace(lines[idx+1]), "}") {
			return xerrors.Wrap(errMissingContext, ctx)
		}
		last := strings.TrimSpace(lines[idx+1]) == "}"
		lines = append(lines[:idx-1], lines[idx+2:]...)
		// 末尾の要素を外したら、1つ前の要素の後ろの `,` を外す。
		if last && idx-2 >= 0 && strings.TrimSpace(lines[idx-2]) == "}," {
			lines[idx-2] = strings.TrimSuffix(lines[idx-2], ",")
		}
		p.droppedContexts = append(p.droppedContexts, ctx)
	}
	p.writes[protectionFile] = strings.Join(lines, "\n")
	return nil
}

// under は、path が prefixes のいずれか（またはその配下）に当たるかを返します。
func under(path string, prefixes []string) bool {
	for _, pre := range prefixes {
		if path == pre || strings.HasPrefix(path, pre+"/") {
			return true
		}
	}
	return false
}

// report は、plan の内容を書き出します。
func report(w io.Writer, p plan) {
	fmt.Fprintf(w, "消すパス（%d 件）:\n", len(p.deletes))
	for _, d := range p.deletes {
		fmt.Fprintf(w, "  - %s\n", d)
	}
	files := append([]string(nil), p.markedFiles...)
	sort.Strings(files)
	fmt.Fprintf(w, "印を取り除くファイル（%d 件、印 %d 個）:\n", len(files), p.markerCount)
	for _, f := range files {
		fmt.Fprintf(w, "  - %s\n", f)
	}
	fmt.Fprintf(w, "外す pin（%d 件）:\n", len(p.droppedPins))
	for _, k := range p.droppedPins {
		fmt.Fprintf(w, "  - %s\n", k)
	}
	fmt.Fprintf(w, "必須検査から外す context（%d 件）:\n", len(p.droppedContexts))
	for _, c := range p.droppedContexts {
		fmt.Fprintf(w, "  - %s\n", c)
	}
}

// apply は、plan を作業ツリーへ反映します。書き換えをすべて済ませてから消します。
func apply(root string, p plan) error {
	changes := make(map[string]string, len(p.writes))
	for f, c := range p.writes {
		changes[filepath.Join(root, f)] = c
	}
	if err := atomicwrite.Apply(changes, 0o644); err != nil {
		return xerrors.Wrap(err, "❌ 書き換えに失敗しました")
	}
	for _, d := range p.deletes {
		if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
			return xerrors.Wrapf(err, "❌ %s を消せません", d)
		}
	}
	return nil
}
