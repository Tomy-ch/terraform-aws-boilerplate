package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// trivyRuleGitHubPAT は、canary の偽の token に当たる Trivy の規則です。
const trivyRuleGitHubPAT = "github-pat"

// trivyConfigs は、Trivy が作業ディレクトリから既定で読む設定（--config と --secret-config の既定値）です。
var trivyConfigs = []string{"trivy.yaml", "trivy-secret.yaml"}

var trivySteps = map[string]string{"table": stepTrivyTable, "json": stepTrivyJSON, "sarif": stepTrivySARIF}

// runTrivy は、作業ツリーの秘密を Trivy で走査します。trivy.mk の脆弱性の走査は `--scanners vuln` で secret を
// 落としているので、秘密の走査はここだけが持ちます。severity では絞りません —— 秘密に「軽微な漏洩」は無い。
//
// Trivy の結果は所見のあるファイルしか載せず、走査した件数を報告しません。走査が対象を失っても「所見なし」と区別
// できないので（ADR-0702 決定13）、本走査の前に、実行時に作った偽の token を canary として検出させます。canary は
// 抑止のファイルを読まず、終了コード 0 で走らせ、結果の RuleID で検出を見ます —— `--exit-code 1` の非0は、検出と
// Trivy 自身の異常終了を区別できない。canary の置き場は本走査の前に消します（本走査は root の tmp/ も読む）。
func runTrivy(args []string, stdout, log io.Writer, r runner) error {
	f := flag.NewFlagSet("secret-scan trivy", flag.ContinueOnError)
	f.SetOutput(log)
	format := f.String("format", "table", "本走査の出力の形（table | json | sarif）")
	if err := f.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if f.NArg() > 0 {
		return xerrors.Wrap(errUsage, "secret-scan trivy は位置引数を取りません")
	}
	step, ok := trivySteps[*format]
	if !ok {
		return xerrors.Wrapf(errUsage, "-format %q は受けない値です（table | json | sarif）", *format)
	}
	env := withoutPrefix(r.environ, "TRIVY_")
	if err := trivyCanary(r, env, log); err != nil {
		return err
	}
	cmd := r.tool(r.root, env, step, "")
	cmd.Stdout, cmd.Stderr = stdout, log
	rc, err := toolrun.Exec(cmd)
	if err != nil {
		return xerrors.Wrapf(err, "trivy の本走査")
	}
	switch rc {
	case 0:
		return nil
	case 1:
		return xerrors.Wrap(errFindings, "trivy")
	default:
		return xerrors.Wrapf(errScan, "trivy が終了コード %d で終わりました", rc)
	}
}

// trivyCanary は、tmp/secret-scan/trivy/canary に偽の token と、root に在る trivyConfigs の写しを置いて canary の段を
// 走らせ、終わりに作業ディレクトリを消します。落ちたときだけ、Trivy の stderr を log へ出します。
func trivyCanary(r runner, env []string, log io.Writer) error {
	dir, err := prepareWork(r.root, "trivy")
	if err != nil {
		return err
	}
	defer removeWork(r.root, "trivy")
	canary := filepath.Join(dir, "canary")
	if err := writeFile(filepath.Join(canary, "canary.txt"), fakeGitHubToken()); err != nil {
		return err
	}
	for _, name := range trivyConfigs {
		_, err := os.Lstat(filepath.Join(r.root, name))
		switch {
		case xerrors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return xerrors.Wrapf(errInput, "%v", err)
		}
		if err := copyInto(r.root, canary, name); err != nil {
			return err
		}
	}
	var out, errOut bytes.Buffer
	cmd := r.tool(canary, env, stepTrivyCanary, "")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	rc, err := toolrun.Exec(cmd)
	if err != nil {
		return xerrors.Wrapf(err, "trivy の canary")
	}
	if err := judgeTrivyCanary(rc, out.Bytes()); err != nil {
		fmt.Fprintf(log, "--- trivy の canary の stderr:\n%s\n", errOut.String())

		return xerrors.Wrapf(err, "trivy の canary")
	}
	fmt.Fprintln(log, "✅ trivy の canary: 埋めた偽の秘密を検出しました")

	return nil
}

// judgeTrivyCanary は、canary の走査が 0 で終わり、結果の Results[].Secrets[] に trivyRuleGitHubPAT があることを
// 確かめます。canary は --exit-code 0 で走るので、0 以外の終了コードと読めない結果は走査器の不全（errScan）、
// 期待の規則が無いのは errCanary です。
func judgeTrivyCanary(rc int, out []byte) error {
	if rc != 0 {
		return xerrors.Wrapf(errScan, "trivy が異常終了しました（終了コード %d）", rc)
	}
	var report struct {
		Results []struct {
			Secrets []struct {
				RuleID string `json:"RuleID"`
			} `json:"Secrets"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return xerrors.Wrapf(errScan, "trivy の結果を読めません: %v", err)
	}
	for _, res := range report.Results {
		for _, s := range res.Secrets {
			if s.RuleID == trivyRuleGitHubPAT {
				return nil
			}
		}
	}

	return xerrors.Wrap(errCanary, "埋めた偽の秘密を検出できませんでした")
}

func withoutPrefix(environ []string, prefix string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}

	return out
}
