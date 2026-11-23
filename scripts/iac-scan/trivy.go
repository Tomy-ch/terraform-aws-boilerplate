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
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// trivyCanaryID は、MEDIUM の誤設定（tag に latest を使う）です。閾値が緩められると、MEDIUM の誤設定は件数0の
// 「所見なし」として通ります。
const (
	trivyCanaryID = "DS-0001"
	trivyCanary   = "FROM alpine:latest\nUSER nobody\nHEALTHCHECK CMD true\n"
)

// runTrivyConfig は、<base>/tmp/trivy-config-canary に MEDIUM の誤設定だけを持つ Dockerfile と <base> の
// .trivyignore.yaml を置いて、本走査と同じ引数（出力は json）で canary を走らせ、DS-0001 で落ちることを確かめてから、
// <base> で本走査を起動します。本走査の出力だけを stdout へ流します。tmp/ は gitignore された作業領域なので走査から
// 外します。
func runTrivyConfig(args []string, stdout, log io.Writer, tool toolFunc) error {
	f := flag.NewFlagSet("iac-scan trivy-config", flag.ContinueOnError)
	f.SetOutput(log)
	base := f.String("base", ".", "走査の基点（.trivyignore.yaml を持つ）")
	severity := f.String("severity", "", "閾値（"+strings.Join(trivySeverities, " | ")+"）")
	format := f.String("format", "table", "本走査の出力の形（"+strings.Join(trivyFormats, " | ")+"）")
	if err := f.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if f.NArg() > 0 {
		return xerrors.Wrapf(errUsage, "iac-scan trivy-config は位置引数を取りません（%s）", strings.Join(f.Args(), " "))
	}
	step, err := trivyStep(*severity, *format)
	if err != nil {
		return err
	}
	canaryStep, err := trivyStep(*severity, "json")
	if err != nil {
		return err
	}
	ignore, err := os.ReadFile(filepath.Join(*base, trivyIgnore))
	if err != nil {
		return xerrors.Wrapf(errInput, "%v", err)
	}
	env := withoutPrefix(os.Environ(), "TRIVY_")

	canary := filepath.Join(*base, "tmp", "trivy-config-canary")
	if err := os.RemoveAll(canary); err != nil {
		return xerrors.Wrapf(err, "%s の削除", canary)
	}
	if err := writeText(filepath.Join(canary, "Dockerfile"), trivyCanary); err != nil {
		return err
	}
	if err := writeText(filepath.Join(canary, trivyIgnore), string(ignore)); err != nil {
		return err
	}
	var out, errOut bytes.Buffer
	cmd := tool(canary, env, canaryStep)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	rc := exitStatus(cmd.Run())
	if rc < 0 {
		return xerrors.Wrap(errStart, "trivy")
	}
	if err := judgeTrivyCanary(rc, out.Bytes()); err != nil {
		fmt.Fprintf(log, "--- trivy-config の canary の出力:\n%s\n", errOut.String())

		return err
	}
	if err := os.RemoveAll(canary); err != nil {
		return xerrors.Wrapf(err, "%s の削除", canary)
	}
	fmt.Fprintln(log, "✅ trivy-config の canary: MEDIUM の誤設定を閾値の内側で検出しました")

	cmd = tool(*base, env, step)
	cmd.Stdout, cmd.Stderr = stdout, log
	switch rc := exitStatus(cmd.Run()); {
	case rc == 0:
		return nil
	case rc < 0:
		return xerrors.Wrap(errStart, "trivy")
	default:
		return xerrors.Wrapf(errFindings, "trivy config が終了コード %d で終わりました", rc)
	}
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
