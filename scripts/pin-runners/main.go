// pin-runners は、workflow の `runs-on:` が名指しする GitHub-hosted runner の label を、
// 単一の宣言へ固定します。
//
// `ubuntu-latest` のような浮動の label が指す OS は、GitHub の都合で入れ替わります。
// 入れ替わりはこちらの履歴に現れないので、ある日から別の OS で検査していたことに、
// 何かが壊れるまで気づけません。宣言を1つ置いて各 workflow へ写しを配ることで、
// 「版が動くこと」と「動いたことに気づけること」を分けます。
//
// 版を上げる手順は、宣言を書き換えて `make pin-runners-apply` を実行することです。
// そのとき diff に出るのは、移行そのものだけになります。
package main

import (
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lockfile"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/yamlblock"
)

const (
	toolName    = "pin-runners"
	pinFile     = ".github/runners-pin.toml"
	workflowDir = ".github/workflows"
	filePerm    = 0o644
)

var (
	// lockRe は宣言の1行。`"<浮動の label>" = "<固定先の label>"` の形。
	lockRe = regexp.MustCompile(`^"([A-Za-z0-9._-]+)"\s*=\s*"([A-Za-z0-9._-]+)"$`)
	// runsOnRe は `runs-on:` の行。値と、行末の注記までを分けて捕まえます。
	// 値に空白を含む形（列・`group:`・`${{ }}`）はここで捕まらず、捕まえられなかったこと
	// 自体を errUnknownRunner で落とします。
	runsOnRe = regexp.MustCompile(`^(\s*runs-on:\s+)([^\s#]+)([ \t]*(?:#.*)?)$`)
)

var lockFormat = lockfile.Format{
	Line: lockRe,
	Header: []string{
		"GitHub-hosted runner の label（SSOT）。",
		"make pin-runners-apply で workflow の runs-on へ反映する。",
	},
	Resolve: "pin-runners-apply",
	Perm:    filePerm,
}

var (
	// errUsage は、サブコマンドが無いか未知の場合のエラー。
	errUsage = xerrors.New("usage: pin-runners <apply|check>")
	// errNoPins は、宣言が1件も読めなかった場合のエラー。**空の宣言で全件を通さない。**
	// 固定先を持たない実行は、固定したことにならない。
	errNoPins = xerrors.New("runner の宣言が1件もありません")
	// errNoRunsOn は、走査した workflow に `runs-on:` が1件も無かった場合のエラー。
	// 検査対象を失ったことと、違反が無かったことを区別する。
	errNoRunsOn = xerrors.New("workflow に runs-on が1件もありません")
	// errUnknownRunner は、宣言のどちらの側にも無い label を見た場合のエラー。
	// 式や列の形もここへ来る —— 読めない入力を取りこぼしとして黙って通さない。
	errUnknownRunner = xerrors.New("宣言にも固定先にも無い runner label です")
	// errRunnerDrift は、check が固定されていない `runs-on:` を見つけた場合のエラー。
	errRunnerDrift = xerrors.New("runs-on が宣言からずれています")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Getwd, os.Stdout); err != nil {
		log.Fatalf("❌ %s: %v", toolName, err)
	}
}

// run は引数を解釈して apply か check を呼びます。main は判断を持ちません
// （scripts/README.md の Test Strategy）。
func run(args []string, wd func() (string, error), out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}
	root, err := wd()
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリの取得")
	}
	switch args[0] {
	case "apply":
		return applyOrCheck(root, false, out)
	case "check":
		return applyOrCheck(root, true, out)
	default:
		return errUsage
	}
}

// applyOrCheck は、宣言を読んで各 workflow の `runs-on:` を突き合わせます。
// dryRun が真なら書き込まず、ずれを errRunnerDrift で報せます。
//
// **1バイトも書く前に全ファイルを検証します。** 途中で未知の label に当たった実行が、
// そこまでの書き換えだけを作業ツリーへ残すと、直し方が固定の適用ではなくなる。
func applyOrCheck(root string, dryRun bool, out io.Writer) error {
	pins, err := lockFormat.Read(filepath.Join(root, pinFile))
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", pinFile)
	}
	if len(pins) == 0 {
		return xerrors.Wrapf(errNoPins, "%s", pinFile)
	}
	pinned := pinnedSet(pins)

	files, err := workflowFiles(root)
	if err != nil {
		return err
	}

	changes := map[string]string{}
	var drifted []string
	total := 0
	for _, rel := range files {
		path := filepath.Join(root, rel)
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", rel)
		}
		updated, seen, err := rewrite(string(source), pins, pinned)
		if err != nil {
			return xerrors.Wrapf(err, "%s", rel)
		}
		total += seen
		if updated == string(source) {
			continue
		}
		changes[path] = updated
		drifted = append(drifted, rel)
	}
	if total == 0 {
		return xerrors.Wrapf(errNoRunsOn, "%s", workflowDir)
	}
	sort.Strings(drifted)

	if dryRun {
		return report(out, drifted, len(files))
	}
	if len(changes) > 0 {
		if err := atomicwrite.Apply(changes, filePerm); err != nil {
			return xerrors.Wrap(err, "workflow への反映")
		}
	}
	_, err = io.WriteString(out, formatApplied(drifted)+"\n")
	return err
}

// pinnedSet は、固定先の label の集合を返します。既に固定されている行を
// ずれと数えないために要ります。
func pinnedSet(pins map[string]string) map[string]bool {
	set := make(map[string]bool, len(pins))
	for _, v := range pins {
		set[v] = true
	}
	return set
}

// workflowFiles は `.github/workflows` 直下の workflow を、root からの相対パスで名前順に返します。
//
// 相対で返すのは、報告にそのまま載る形がこれだからです。絶対パスから毎回引き直すと、
// 引き直しの失敗という到達しない枝が呼び出し側に増えます。
func workflowFiles(root string) ([]string, error) {
	dir := filepath.Join(root, workflowDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の走査", workflowDir)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext != ".yaml" && ext != ".yml" {
			continue
		}
		files = append(files, path.Join(workflowDir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// rewrite は source の `runs-on:` を固定先へ揃え、揃えた内容と見た `runs-on:` の件数を返します。
//
// ブロックスカラーの中身は見ません。`run:` の中の文字列が `runs-on:` を含むだけで
// 書き換わってしまうためで、その判定は yamlblock が1箇所で持っています。
func rewrite(source string, pins map[string]string, pinned map[string]bool) (string, int, error) {
	body := yamlblock.ContentLines(source)
	lines := strings.Split(source, "\n")
	seen := 0
	for i, line := range lines {
		if body[i+1] {
			continue
		}
		m := runsOnRe.FindStringSubmatch(line)
		if m == nil {
			if strings.HasPrefix(strings.TrimSpace(line), "runs-on:") {
				return "", 0, xerrors.Wrapf(errUnknownRunner, "%d 行目: %s", i+1, strings.TrimSpace(line))
			}
			continue
		}
		seen++
		label := m[2]
		switch {
		case pinned[label]:
		case pins[label] != "":
			lines[i] = m[1] + pins[label] + m[3]
		default:
			return "", 0, xerrors.Wrapf(errUnknownRunner, "%d 行目: %s", i+1, label)
		}
	}
	return strings.Join(lines, "\n"), seen, nil
}

// report は check の判定を書き出します。ずれていれば errRunnerDrift を返します。
func report(out io.Writer, drifted []string, files int) error {
	if len(drifted) > 0 {
		return xerrors.Wrapf(errRunnerDrift, "make pin-runners-apply してコミットしてください: %s", strings.Join(drifted, ", "))
	}
	_, err := io.WriteString(out, "✅ "+toolName+": workflow "+strconv.Itoa(files)+" 件の runs-on が宣言通りに固定されています\n")
	return err
}

// formatApplied は apply の結果の1行を組み立てます。
func formatApplied(drifted []string) string {
	if len(drifted) == 0 {
		return "✅ " + toolName + "-apply: 反映するずれはありませんでした"
	}
	return "✅ " + toolName + "-apply: " + strconv.Itoa(len(drifted)) + " ファイルへ反映しました: " + strings.Join(drifted, ", ")
}
