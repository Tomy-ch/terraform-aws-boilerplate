package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 抑止は path の正規表現で置きます（trufflehog は fingerprint の抑止を持たない）。各エントリの理由はファイルが
// 持ちます。履歴にだけ残る検出の抑止は全履歴の走査にだけ足します（ADR-0302 決定9）。抑止のファイルを足すときは、
// scripts/secret-suppression-lint の対象にも足します。
const (
	trufflehogExclude        = ".trufflehog-exclude-paths.txt"
	trufflehogExcludeHistory = ".trufflehog-exclude-paths-history.txt"
)

// trufflehogFound は、trufflehog が所見を見つけたときの終了コードです（--fail）。
const trufflehogFound = 183

// trufflehogDetectorAWS は、canary の偽の AWS キーに当たる trufflehog の検出器です。
const trufflehogDetectorAWS = "AWS"

var trufflehogFormats = []string{"text", "json"}

// trufflehogSteps は、出力の形ごとの、範囲・全履歴・作業ツリーの段です。
var trufflehogSteps = map[string][3]string{
	"text": {stepTrufflehogRange, stepTrufflehogHistory, stepTrufflehogFilesystem},
	"json": {stepTrufflehogRangeJSON, stepTrufflehogHistoryJSON, stepTrufflehogFilesystemJSON},
}

// trufflehogExcludeFile は、git の段が --exclude-paths で読む、作業ディレクトリからの相対パスです。
func trufflehogExcludeFile(step string) string {
	if step == stepTrufflehogRange || step == stepTrufflehogRangeJSON {
		return trufflehogRangeExclude
	}

	return trufflehogHistoryExclude
}

// trufflehogExcludes は、root の抑止のファイルを読み、範囲と作業ツリーの走査が使うものと、全履歴の走査が使う
// 和集合を返します。どちらかのファイルが無ければ errInput です。
func trufflehogExcludes(root string) (base, union string, err error) {
	if base, err = readFile(filepath.Join(root, trufflehogExclude)); err != nil {
		return "", "", err
	}
	history, err := readFile(filepath.Join(root, trufflehogExcludeHistory))
	if err != nil {
		return "", "", err
	}
	if base != "" && !strings.HasSuffix(base, "\n") {
		return base, base + "\n" + history, nil
	}

	return base, base + history, nil
}

// parseTrufflehogFlags は、-format（と、ranged なら -from）を読みます。
func parseTrufflehogFlags(name string, args []string, ranged bool, log io.Writer) (from, format string, err error) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(log)
	fromFlag := new(string)
	if ranged {
		fromFlag = f.String("from", "", "範囲の起点（commit へ解決できる rev）")
	}
	formatFlag := f.String("format", "text", "本走査の出力の形（"+strings.Join(trufflehogFormats, " | ")+"）")
	if err := f.Parse(args); err != nil {
		return "", "", xerrors.Wrapf(errUsage, "%v", err)
	}
	if f.NArg() > 0 {
		return "", "", xerrors.Wrapf(errUsage, "%s は位置引数を取りません", name)
	}
	if !slices.Contains(trufflehogFormats, *formatFlag) {
		return "", "", xerrors.Wrapf(errUsage, "-format %q は受けない値です（%s）", *formatFlag, strings.Join(trufflehogFormats, " | "))
	}

	return *fromFlag, *formatFlag, nil
}

// runTrufflehogGit は、trufflehog-range（ranged）と trufflehog-history を走らせます。canary が通れば、抑止の写しを
// 固定名のファイルへ置いて、root で本走査を起動します。本走査の stdout は要求された形のまま stdout へ書きます。
func runTrufflehogGit(args []string, ranged bool, stdout, log io.Writer, r runner) error {
	name, work, idx := "secret-scan trufflehog-history", "trufflehog-history", 1
	if ranged {
		name, work, idx = "secret-scan trufflehog-range", "trufflehog-range", 0
	}
	from, format, err := parseTrufflehogFlags(name, args, ranged, log)
	if err != nil {
		return err
	}
	env, err := gitenv.Env(r.environ)
	if err != nil {
		return err
	}
	rev := ""
	if ranged {
		if rev, err = resolveFrom(r, env, r.root, from); err != nil {
			return err
		}
	}
	base, union, err := trufflehogExcludes(r.root)
	if err != nil {
		return err
	}
	dir, err := prepareWork(r.root, work)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := trufflehogCanary(r, env, dir, trufflehogSteps["json"][idx], union, log); err != nil {
		return err
	}
	step := trufflehogSteps[format][idx]
	exclude := base
	if !ranged {
		exclude = union
	}
	if err := writeFile(filepath.Join(r.root, filepath.FromSlash(trufflehogExcludeFile(step))), exclude); err != nil {
		return err
	}

	return trufflehogMain(r.tool(r.root, env, step, rev), stdout, log)
}

func trufflehogMain(cmd *exec.Cmd, stdout, log io.Writer) error {
	cmd.Stdout, cmd.Stderr = stdout, log
	rc, err := toolrun.Exec(cmd)
	if err != nil {
		return xerrors.Wrapf(err, "trufflehog の本走査")
	}

	return trufflehogVerdict(rc)
}

// trufflehogCanary は、偽の AWS キーを、git の段 gitStep と作業ツリーの段の両経路で、検証なしに検出することを
// 確かめます。git の経路は dir/canary-git に建てたリポジトリで、本走査と同じ段と抑止の和集合（union）を使います。
// 作業ツリーの経路は、偽のキーのファイルを union で選び、dir/canary-fs へ置いて走らせます —— 全 path に当たる
// 抑止が混ざると、ここが落ちます。出力は stdout へ出さず、落ちたときだけ trufflehog の stderr の末尾を log へ出します。
func trufflehogCanary(r runner, env []string, dir, gitStep, union string, log io.Writer) error {
	repo := filepath.Join(dir, "canary-git")
	from, err := buildCanaryRepo(r, env, repo, fakeAWSCredentials())
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(repo, filepath.FromSlash(trufflehogExcludeFile(gitStep))), union); err != nil {
		return err
	}
	rev := ""
	if gitStep == stepTrufflehogRangeJSON {
		rev = from
	}
	if err := trufflehogCanaryRun(r.tool(repo, env, gitStep, rev), "git", log); err != nil {
		return err
	}

	patterns, err := parseExcludes(union)
	if err != nil {
		return err
	}
	fs := filepath.Join(dir, "canary-fs")
	if err := writeFile(filepath.Join(fs, canaryCredentials), fakeAWSCredentials()); err != nil {
		return err
	}
	if excluded(canaryCredentials, patterns) {
		return xerrors.Wrapf(errCanary, "trufflehog の canary（filesystem）: 抑止が偽のキーのファイル %s に当たります", canaryCredentials)
	}
	if err := trufflehogCanaryRun(r.tool(fs, env, stepTrufflehogFilesystemJSON, ""), "filesystem", log); err != nil {
		return err
	}
	fmt.Fprintln(log, "✅ trufflehog の canary: 埋めた偽の AWS キーを git と filesystem の両経路で検証なしに検出しました")

	return nil
}

func trufflehogCanaryRun(cmd *exec.Cmd, label string, log io.Writer) error {
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	rc, err := toolrun.Exec(cmd)
	if err != nil {
		return xerrors.Wrapf(err, "trufflehog の canary（%s）", label)
	}
	if err := judgeTrufflehogCanary(rc, out.String()); err != nil {
		fmt.Fprintf(log, "--- trufflehog の canary（%s）の stderr の末尾:\n%s\n", label, tail(errOut.String(), 5))

		return xerrors.Wrapf(err, "trufflehog の canary（%s）", label)
	}

	return nil
}

// judgeTrufflehogCanary は、canary の走査が所見の終了コードで終わり、NDJSON の stdout に AWS の検出器の所見が
// あることを確かめます。JSON として読めない行があれば落とします。
func judgeTrufflehogCanary(rc int, stdout string) error {
	switch rc {
	case 0:
		return xerrors.Wrap(errCanary, "埋めた偽の AWS キーを検出できませんでした（検出器・閾値・--results の変更を疑う）")
	case trufflehogFound:
	default:
		return xerrors.Wrapf(errCanary, "trufflehog が異常終了しました（終了コード %d）", rc)
	}
	found := false
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var res struct {
			DetectorName string `json:"DetectorName"`
		}
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			return xerrors.Wrapf(errCanary, "stdout に JSON でない行があります: %v", err)
		}
		found = found || res.DetectorName == trufflehogDetectorAWS
	}
	if !found {
		return xerrors.Wrap(errCanary, "AWS 以外の所見だけを返しました")
	}

	return nil
}

func trufflehogVerdict(rc int) error {
	switch rc {
	case 0:
		return nil
	case trufflehogFound:
		return xerrors.Wrap(errFindings, "trufflehog")
	default:
		return xerrors.Wrapf(errScan, "trufflehog が終了コード %d で終わりました", rc)
	}
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
