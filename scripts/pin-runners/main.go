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
	//
	// **注記は空白を挟んだ `#` に限ります。** YAML では空白の無い `#` は注記を始めないので、
	// `ubuntu-24.04#x` の `#x` を注記として切り落とすと、GitHub が受け取る label と
	// この検査が見る label が食い違います。
	runsOnRe = regexp.MustCompile(`^([ \t]*runs-on:[ \t]+)(\S+)((?:[ \t]+#.*)?[ \t]*)$`)
	// looseRunsOnRe は `runs-on` を名指ししているように見える行。**厳密な runsOnRe で
	// 解釈できなかったものを、取りこぼしではなくエラーへ回すための門です。** YAML は
	// `runs-on : x` / `"runs-on": x` / `one: {runs-on: x}` をどれも受けるので、
	// 行頭からの一致だけを見る門はそれらを黙って通します。
	looseRunsOnRe = regexp.MustCompile(`(^|[^A-Za-z0-9_-])["']?runs-on["']?[ \t]*:`)
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
	// errPinNotTerminal は、固定先が別の宣言のキーでもある場合のエラー。
	// 自己写像・連鎖・値の誤記がここへ来ます。**宣言1行でゲートを無効化できないようにする門です**
	// —— 固定先の集合に浮動 label が入ると、その label は「固定済み」として素通りします。
	errPinNotTerminal = xerrors.New("固定先が別の宣言のキーでもあります")
	// errPinOrphan は、どの `runs-on:` にも当たらない宣言がある場合のエラー。
	// 対象を失った宣言は、固定しているように読めて何も固定していません。
	errPinOrphan = xerrors.New("どの runs-on にも当たらない宣言があります")
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
	if err := checkTerminal(pins); err != nil {
		return xerrors.Wrapf(err, "%s", pinFile)
	}
	pinned := pinnedSet(pins)

	files, err := workflowFiles(root)
	if err != nil {
		return err
	}

	changes := map[string]string{}
	var drifted []string
	used := map[string]bool{}
	total := 0
	for _, rel := range files {
		path := filepath.Join(root, rel)
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", rel)
		}
		updated, labels, err := rewrite(string(source), pins, pinned)
		if err != nil {
			return xerrors.Wrapf(err, "%s", rel)
		}
		total += len(labels)
		for _, l := range labels {
			used[l] = true
		}
		if updated == string(source) {
			continue
		}
		changes[path] = updated
		drifted = append(drifted, rel)
	}
	if total == 0 {
		return xerrors.Wrapf(errNoRunsOn, "%s", workflowDir)
	}
	if err := checkOrphan(pins, used); err != nil {
		return xerrors.Wrapf(err, "%s", pinFile)
	}
	sort.Strings(drifted)

	if dryRun {
		return report(out, drifted, len(files), total)
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
func rewrite(source string, pins map[string]string, pinned map[string]bool) (string, []string, error) {
	body := yamlblock.ContentLines(source)
	lines := strings.Split(source, "\n")
	var labels []string
	for i, line := range lines {
		if body[i+1] {
			continue
		}
		m := runsOnRe.FindStringSubmatch(line)
		if m == nil {
			// **名指ししているのに厳密な形で読めなかったものは、取りこぼしではなくエラーである。**
			// 行頭からの一致だけを見ると、YAML が受ける別記法（`runs-on : x` / `"runs-on": x` /
			// フロー写像）が黙って通る。注記は名指しではないので対象から外す。
			if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") && looseRunsOnRe.MatchString(line) {
				return "", nil, xerrors.Wrapf(errUnknownRunner, "%d 行目: %q", i+1, line)
			}
			continue
		}
		label := m[2]
		labels = append(labels, label)
		switch {
		case pins[label] != "":
			lines[i] = m[1] + pins[label] + m[3]
		case pinned[label]:
		default:
			return "", nil, xerrors.Wrapf(errUnknownRunner, "%d 行目: %q", i+1, label)
		}
	}
	return strings.Join(lines, "\n"), labels, nil
}

// checkTerminal は、固定先が別の宣言のキーでもある対を拒みます。
//
// 固定先の集合は「もう書き換えなくてよい label」の集合として使われるので、そこへ浮動 label が
// 入ると、その label はどこに現れても素通りします。自己写像・連鎖・値の誤記がこれに当たり、
// **宣言1行でゲートが無効になる**。キーと値の集合が素であることを先に確かめます。
func checkTerminal(pins map[string]string) error {
	for _, k := range sortedKeys(pins) {
		if _, isKey := pins[pins[k]]; isKey {
			return xerrors.Wrapf(errPinNotTerminal, "%q = %q", k, pins[k])
		}
	}
	return nil
}

// checkOrphan は、どの `runs-on:` にも当たらなかった宣言を拒みます。
//
// 対象を失った宣言は、固定しているように読めて何も固定していません。キーとして当たれば
// 書き換えの対象があり、値として当たれば固定済みの対象がある —— どちらも無いものが孤児です。
func checkOrphan(pins map[string]string, used map[string]bool) error {
	for _, k := range sortedKeys(pins) {
		if !used[k] && !used[pins[k]] {
			return xerrors.Wrapf(errPinOrphan, "%q = %q", k, pins[k])
		}
	}
	return nil
}

// sortedKeys は宣言のキーを昇順で返します。map の走査順は実行ごとに変わるので、
// どの対で落ちたかを報せるエラーが実行ごとに入れ替わらないようにします。
func sortedKeys(pins map[string]string) []string {
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// report は check の判定を書き出します。ずれていれば errRunnerDrift を返します。
func report(out io.Writer, drifted []string, files, runsOn int) error {
	if len(drifted) > 0 {
		return xerrors.Wrapf(errRunnerDrift, "make pin-runners-apply してコミットしてください: %s", strings.Join(drifted, ", "))
	}
	// **両方の件数を出す。** 片方だけだと、抽出が壊れて対象が減っても数字が動かない
	// （ADR-0702 決定15）。ファイル数は走査の広さ、runs-on 件数は抽出の結果である。
	_, err := io.WriteString(out, "✅ "+toolName+": workflow "+strconv.Itoa(files)+" 件の runs-on "+strconv.Itoa(runsOn)+" 件が宣言通りに固定されています\n")
	return err
}

// formatApplied は apply の結果の1行を組み立てます。
func formatApplied(drifted []string) string {
	if len(drifted) == 0 {
		return "✅ " + toolName + "-apply: 反映するずれはありませんでした"
	}
	return "✅ " + toolName + "-apply: " + strconv.Itoa(len(drifted)) + " ファイルへ反映しました: " + strings.Join(drifted, ", ")
}
