package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// gitleaksRuleAWS は、canary の偽の AWS キーに当たる gitleaks の規則です。
const gitleaksRuleAWS = "aws-access-token"

// runGitleaks は、gitleaks-range（ranged）と gitleaks-history を走らせます。範囲の走査は起点を解決してから、
// 全履歴の走査はそのまま canary を走らせ、通れば root で本走査を起動します。本走査の出力（gitleaks のログ）は
// 落ちるときも先に log へ流し、そのうえで走査した数を runCount で突き合わせます。範囲は git の数えた範囲、
// 全履歴は1件以上です。
func runGitleaks(args []string, ranged bool, log io.Writer, r runner) error {
	name, work, step := "secret-scan gitleaks-history", "gitleaks-history", stepGitleaksHistory
	if ranged {
		name, work, step = "secret-scan gitleaks-range", "gitleaks-range", stepGitleaksRange
	}
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(log)
	var from *string
	if ranged {
		from = f.String("from", "", "範囲の起点（commit へ解決できる rev）")
	}
	if err := f.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if f.NArg() > 0 {
		return xerrors.Wrapf(errUsage, "%s は位置引数を取りません", name)
	}
	env, err := gitenv.Env(r.environ)
	if err != nil {
		return err
	}
	rev := ""
	if ranged {
		if rev, err = resolveFrom(r, env, r.root, *from); err != nil {
			return err
		}
	}
	dir, err := prepareWork(r.root, work)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := gitleaksCanary(r, env, dir, log); err != nil {
		return err
	}
	minimum := 1
	var ns []byte
	if ranged {
		minimum = 0
		if ns, err = numstat(r, env, r.root, rev); err != nil {
			return err
		}
	}
	var out bytes.Buffer
	cmd := r.tool(r.root, env, step, rev)
	cmd.Stdout, cmd.Stderr = io.Discard, io.MultiWriter(log, &out)
	rc, err := toolrun.Exec(cmd)
	if err != nil {
		return xerrors.Wrapf(err, "gitleaks の本走査")
	}
	if err := runCount(out.String(), ns, minimum); err != nil {
		return xerrors.Wrapf(err, "gitleaks の本走査")
	}

	return gitleaksVerdict(rc)
}

// gitleaksCanary は、dir/canary に偽の AWS キーを commit したリポジトリと、root の設定・抑止の写しを置き、本走査と
// 同じ段で範囲と全履歴の両方を走らせて、どちらも検出することを確かめます。走査した数も本走査と同じく突き合わせます。
// 落ちたときだけ、gitleaks のログを log へ出します。
func gitleaksCanary(r runner, env []string, dir string, log io.Writer) error {
	repo := filepath.Join(dir, "canary")
	from, err := buildCanaryRepo(r, env, repo, fakeAWSCredentials())
	if err != nil {
		return err
	}
	for _, name := range []string{gitleaksConfig, gitleaksIgnore} {
		if err := copyInto(r.root, repo, name); err != nil {
			return err
		}
	}
	for _, c := range []struct{ label, step, rev string }{
		{"範囲", stepGitleaksRange, from},
		{"全履歴", stepGitleaksHistory, ""},
	} {
		ns, err := numstat(r, env, repo, c.rev)
		if err != nil {
			return err
		}
		var report, out bytes.Buffer
		cmd := r.tool(repo, env, c.step, c.rev)
		cmd.Stdout, cmd.Stderr = &report, &out
		rc, err := toolrun.Exec(cmd)
		if err != nil {
			return xerrors.Wrapf(err, "gitleaks の canary（%s）", c.label)
		}
		if err := judgeGitleaksCanary(rc, report.Bytes(), runCount(out.String(), ns, 0)); err != nil {
			fmt.Fprintf(log, "--- gitleaks の canary（%s）の出力:\n%s\n", c.label, out.String())

			return xerrors.Wrapf(err, "gitleaks の canary（%s）", c.label)
		}
	}
	fmt.Fprintln(log, "✅ gitleaks の canary: 埋めた偽の AWS キーを範囲と全履歴の両方で検出しました")

	return nil
}

// judgeGitleaksCanary は、canary の走査が終了コード 1 で終わり、JSON の報告に gitleaksRuleAWS の所見があり、
// 走査した数の突き合わせ countErr が通ったことを確かめます。gitleaks は検出でも異常終了でも 1 を返すので、終了コードと
// 報告の RuleID の両方で見ます。検出しない（0）・数が合わない・期待の所見が無いのは errCanary、0・1 以外の終了コードと
// 読めない報告は走査器の不全（errScan）です。数の行が無い・numstat を解釈できないときは countErr をそのまま返します。
func judgeGitleaksCanary(rc int, report []byte, countErr error) error {
	switch {
	case rc == 0:
		return xerrors.Wrap(errCanary, "埋めた偽の AWS キーを検出できませんでした（抑止・検出規則・--log-opts の変更を疑う）")
	case rc != 1:
		return xerrors.Wrapf(errScan, "gitleaks が異常終了しました（終了コード %d）", rc)
	case xerrors.Is(countErr, errMismatch):
		return xerrors.Wrapf(errCanary, "%v", countErr)
	case countErr != nil:
		return countErr
	}
	var findings []struct {
		RuleID string `json:"RuleID"`
	}
	if err := json.Unmarshal(report, &findings); err != nil {
		return xerrors.Wrapf(errScan, "gitleaks の報告を読めません: %v", err)
	}
	for _, f := range findings {
		if f.RuleID == gitleaksRuleAWS {
			return nil
		}
	}

	return xerrors.Wrap(errCanary, "終了コード 1 ですが、偽の AWS キーの所見がありません。gitleaks の異常終了を疑う")
}

func gitleaksVerdict(rc int) error {
	switch rc {
	case 0:
		return nil
	case 1:
		return xerrors.Wrap(errFindings, "gitleaks")
	default:
		return xerrors.Wrapf(errScan, "gitleaks が終了コード %d で終わりました", rc)
	}
}
