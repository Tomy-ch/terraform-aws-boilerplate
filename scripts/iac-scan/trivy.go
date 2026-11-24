package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// trivyCanaryID は、MEDIUM の誤設定（tag に latest を使う）です。閾値が緩められると、MEDIUM の誤設定は件数0の
// 「所見なし」として通ります。
const (
	trivyCanaryID = "DS-0001"
	trivyCanary   = "FROM alpine:latest\nUSER nobody\nHEALTHCHECK CMD true\n"
)

// runTrivyConfig は、<base>/tmp/trivy-config-canary.* に MEDIUM の誤設定だけを持つ Dockerfile と <base> の
// .trivyignore.yaml を置いて、本走査と同じ引数で canary を走らせ、DS-0001 で落ちることを確かめてから、<base> で本走査を
// 起動します。走査は常に json で走らせ、走査の root ごとに Terraform の対象が1件以上あることを確かめます。json 以外の形は
// その結果から trivy convert で作り、要求された形の結果だけを stdout へ書きます。tmp/ は gitignore された作業領域なので
// 走査から外します。
func runTrivyConfig(args []string, stdout, log io.Writer, r runner) error {
	f := flag.NewFlagSet("iac-scan trivy-config", flag.ContinueOnError)
	f.SetOutput(log)
	base := f.String("base", ".", "走査の基点（.trivyignore.yaml を持つ）")
	severity := f.String("severity", "", "閾値（"+strings.Join(trivySeverities, " | ")+"）")
	format := f.String("format", "table", "本走査の出力の形（"+strings.Join(trivyFormats, " | ")+"）")
	if err := f.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if f.NArg() == 0 {
		return xerrors.Wrap(errUsage, "iac-scan trivy-config は走査の root が要ります")
	}
	scanStep, convertStep, err := trivyStep(*severity, *format)
	if err != nil {
		return err
	}
	roots, err := checkRoots(*base, f.Args())
	if err != nil {
		return err
	}
	ignore, err := os.ReadFile(filepath.Join(*base, trivyIgnore))
	if err != nil {
		return xerrors.Wrapf(errInput, "%v", err)
	}
	env := withoutPrefix(r.environ, "TRIVY_")
	tmp := filepath.Join(*base, "tmp")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return xerrors.Wrapf(errInput, "%s の作成: %v", tmp, err)
	}

	canary, err := os.MkdirTemp(tmp, "trivy-config-canary.")
	if err != nil {
		return xerrors.Wrapf(errInput, "canary の置き場: %v", err)
	}
	defer func() { _ = os.RemoveAll(canary) }()
	if err := writeText(filepath.Join(canary, "Dockerfile"), trivyCanary); err != nil {
		return err
	}
	if err := writeText(filepath.Join(canary, trivyIgnore), string(ignore)); err != nil {
		return err
	}
	var out, errOut bytes.Buffer
	cmd := r.tool(canary, env, scanStep)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	rc := exitStatus(cmd.Run())
	if rc < 0 {
		return xerrors.Wrap(errStart, "trivy")
	}
	if err := judgeTrivyCanary(rc, out.Bytes()); err != nil {
		fmt.Fprintf(log, "--- trivy-config の canary の出力:\n%s\n", errOut.String())

		return err
	}
	fmt.Fprintln(log, "✅ trivy-config の canary: MEDIUM の誤設定を閾値の内側で検出しました")

	out.Reset()
	cmd = r.tool(*base, env, scanStep)
	cmd.Stdout, cmd.Stderr = &out, log
	rc = exitStatus(cmd.Run())
	if rc < 0 {
		return xerrors.Wrap(errStart, "trivy")
	}
	counts, err := trivyTargets(out.Bytes(), roots)
	if err != nil {
		return err
	}
	if err := writeTrivyReport(out.Bytes(), *base, tmp, convertStep, env, r, stdout, log); err != nil {
		return err
	}
	for _, root := range roots {
		if counts[root] == 0 {
			return xerrors.Wrapf(errNoTargets, "trivy-config: %s の下に Terraform の走査の対象がありません", root)
		}
	}
	if rc != 0 {
		return xerrors.Wrapf(errFindings, "trivy config が終了コード %d で終わりました", rc)
	}

	return nil
}

// trivyTargets は、本走査の json の Results のうち Type が terraform のものを、Target が下にある root ごとに数えます。
// Target は base からの `/` 区切りの相対パス（root module のディレクトリかファイル）です。
func trivyTargets(raw []byte, roots []string) (map[string]int, error) {
	var report struct {
		Results *[]struct {
			Target string `json:"Target"`
			Type   string `json:"Type"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, xerrors.Wrapf(errResult, "trivy-config: 本走査の結果を読めません: %v", err)
	}
	if report.Results == nil {
		return nil, xerrors.Wrap(errResult, "trivy-config: 本走査の結果に Results がありません")
	}
	counts := make(map[string]int, len(roots))
	for _, res := range *report.Results {
		if res.Type != "terraform" {
			continue
		}
		for _, root := range roots {
			if res.Target == root || strings.HasPrefix(res.Target, root+"/") {
				counts[root]++
			}
		}
	}

	return counts, nil
}

// writeTrivyReport は、本走査の json を stdout へ書きます。convertStep が空でなければ、tmp の下の一時のディレクトリに
// 置いた json を trivy convert でその形へ変えたものを書きます。sarif は ROOTPATH を base へ向け直してから書きます。
func writeTrivyReport(raw []byte, base, tmp, convertStep string, env []string, r runner, stdout, log io.Writer) error {
	if convertStep == "" {
		if _, err := stdout.Write(raw); err != nil {
			return xerrors.Wrapf(errOutput, "trivy-config: %v", err)
		}

		return nil
	}
	dir, err := os.MkdirTemp(tmp, "trivy-config-convert.")
	if err != nil {
		return xerrors.Wrapf(errInput, "trivy-config: 変換の置き場: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := writeText(filepath.Join(dir, trivyResult), string(raw)); err != nil {
		return err
	}
	var out bytes.Buffer
	cmd := r.tool(dir, env, convertStep)
	cmd.Stdout, cmd.Stderr = &out, log
	switch rc := exitStatus(cmd.Run()); {
	case rc < 0:
		return xerrors.Wrap(errStart, "trivy convert")
	case rc != 0:
		return xerrors.Wrapf(errResult, "trivy convert が終了コード %d で終わりました", rc)
	}
	report := out.Bytes()
	if convertStep == stepTrivyConvertSARIF {
		if report, err = setSARIFRoot(report, base); err != nil {
			return err
		}
	}
	if _, err := stdout.Write(report); err != nil {
		return xerrors.Wrapf(errOutput, "trivy-config: %v", err)
	}

	return nil
}

// setSARIFRoot は、SARIF の各 run の originalUriBaseIds.ROOTPATH.uri を base の絶対パスの file URI（末尾に `/`）に
// 替えます。trivy convert は入力の json のパスをそこへ書くためです。ROOTPATH を持たない run と、JSON の object として
// 読めない入力は errResult です。
func setSARIFRoot(raw []byte, base string) ([]byte, error) {
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, xerrors.Wrapf(errInput, "%s: %v", base, err)
	}
	root := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs) + "/"}).String()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, xerrors.Wrapf(errResult, "trivy convert の SARIF を読めません: %v", err)
	}
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		return nil, xerrors.Wrap(errResult, "trivy convert の SARIF に runs がありません")
	}
	for i, r := range runs {
		run, _ := r.(map[string]any)
		ids, _ := run["originalUriBaseIds"].(map[string]any)
		rootPath, ok := ids["ROOTPATH"].(map[string]any)
		if !ok {
			return nil, xerrors.Wrapf(errResult, "trivy convert の SARIF の runs[%d] に ROOTPATH がありません", i)
		}
		rootPath["uri"] = root
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, xerrors.Wrapf(errResult, "SARIF の書き出し: %v", err)
	}

	return out.Bytes(), nil
}

// exitStatus は、Run の結果を終了コードにします。起動できなかったときは -1 です。
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if xerrors.As(err, &exit) {
		return exit.ExitCode()
	}

	return -1
}

// judgeTrivyCanary は、canary が非0で終わり、結果の Misconfigurations に DS-0001 があることを確かめます。非0の終了は
// 検出と trivy の異常終了を区別できないので、ID と合わせて見ます。
func judgeTrivyCanary(rc int, out []byte) error {
	if rc == 0 {
		return xerrors.Wrapf(errCanary, "trivy-config: MEDIUM の誤設定（%s）を持つ構成でゲートが落ちませんでした", trivyCanaryID)
	}
	var report struct {
		Results []struct {
			Misconfigurations []struct {
				ID string `json:"ID"`
			} `json:"Misconfigurations"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return xerrors.Wrapf(errCanary, "trivy-config: canary の結果を読めません（終了コード %d）: %v", rc, err)
	}
	for _, r := range report.Results {
		for _, m := range r.Misconfigurations {
			if m.ID == trivyCanaryID {
				return nil
			}
		}
	}

	return xerrors.Wrapf(errCanary, "trivy-config: trivy が %s を報告せずに終了しました（終了コード %d。異常終了か、%s の ID・重大度の変更）",
		trivyCanaryID, rc, trivyCanaryID)
}
