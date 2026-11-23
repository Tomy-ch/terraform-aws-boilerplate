package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// undeclared は、宣言が provider_format_version を持たないときに出す値です。
const undeclared = "（宣言に無い）"

// declaredFormat は、Policy の宣言の policy_plan.provider_format_version を返します。
func declaredFormat(decl []byte) (string, error) {
	var d struct {
		PolicyPlan struct {
			ProviderFormatVersion *string `json:"provider_format_version"`
		} `json:"policy_plan"`
	}
	if err := json.Unmarshal(decl, &d); err != nil {
		return "", xerrors.Wrapf(errRead, "Policy の宣言を解釈できません: %v", err)
	}
	if d.PolicyPlan.ProviderFormatVersion == nil {
		return undeclared, nil
	}

	return *d.PolicyPlan.ProviderFormatVersion, nil
}

// extractPlan は、terraform test -json の出力からちょうど1件の test_plan を取り出し、provider_schemas を除いた
// plan を返します。test_plan が1件でないか、provider_format_version が want でなければ失敗します。
func extractPlan(name string, jsonl []byte, want string) ([]byte, error) {
	var plans []map[string]json.RawMessage
	sc := bufio.NewScanner(bytes.NewReader(jsonl))
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
	for sc.Scan() {
		var line struct {
			Type     string                     `json:"type"`
			TestPlan map[string]json.RawMessage `json:"test_plan"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			return nil, xerrors.Wrapf(errFailed, "%s: terraform test の出力を解釈できません: %v", name, err)
		}
		if line.Type == "test_plan" {
			plans = append(plans, line.TestPlan)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, xerrors.Wrapf(errFailed, "%s: %v", name, err)
	}
	if len(plans) != 1 {
		return nil, xerrors.Wrapf(errFailed, "%s: test_plan が %d 件です（1件を期待）", name, len(plans))
	}
	got := "（無い）"
	if raw, ok := plans[0]["provider_format_version"]; ok {
		if err := json.Unmarshal(raw, &got); err != nil {
			return nil, xerrors.Wrapf(errFailed, "%s: provider_format_version を読めません", name)
		}
	}
	if got != want {
		return nil, xerrors.Wrapf(errFailed, "%s: test_plan の provider_format_version が %s で、宣言の %s ではありません（Provider の schema の形が変わった可能性があります）", name, got, want)
	}
	delete(plans[0], "provider_schemas")
	out, err := json.Marshal(plans[0])
	if err != nil {
		return nil, xerrors.Wrapf(errFailed, "%s: %v", name, err)
	}

	return out, nil
}

// extractAll は、plans.txt の各 jsonl から Policy の入力を <out>/inputs/<name>.json に書き、件数を返します。
// 落ちた root は最後まで見てから名指します。入力が0件なら errNoInput です。
func extractAll(out, want string, w io.Writer) (int, error) {
	jsonls, err := readLines(filepath.Join(out, "plans.txt"))
	if err != nil {
		return 0, err
	}
	inputs := filepath.Join(out, "inputs")
	if err := os.MkdirAll(inputs, 0o750); err != nil {
		return 0, xerrors.Wrapf(err, "%s の作成", inputs)
	}
	var failed []string
	n := 0
	for _, p := range jsonls {
		name := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		raw, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return 0, xerrors.Wrapf(errRead, "%s: %v", p, err)
		}
		plan, err := extractPlan(name, raw, want)
		if err != nil {
			fmt.Fprintf(w, "❌ %v\n", err)
			failed = append(failed, name)

			continue
		}
		if err := os.WriteFile(filepath.Join(inputs, name+".json"), plan, 0o600); err != nil {
			return 0, xerrors.Wrapf(err, "%s の書き込み", name)
		}
		n++
	}
	if len(failed) > 0 {
		fmt.Fprintf(w, "❌ tf-policy が落ちたディレクトリ: %s\n", strings.Join(failed, " "))

		return n, xerrors.Wrapf(errFailed, "Policy の入力を取り出せない root %d 件", len(failed))
	}
	if n == 0 {
		return 0, xerrors.Wrap(errNoInput, "Policy の入力が0件です（reference の root から test_plan を1件も得られませんでした）")
	}

	return n, nil
}

func runExtract(args []string, w io.Writer) error {
	fset := flag.NewFlagSet("tf-policy extract", flag.ContinueOnError)
	fset.SetOutput(w)
	out := fset.String("out", "", "tf-policy plan の -out")
	declPath := fset.String("decl", "", "Policy の宣言（provider_format_version を読む）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *out == "" || *declPath == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "tf-policy extract -out <dir> -decl <path>")
	}
	decl, err := os.ReadFile(filepath.Clean(*declPath))
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", *declPath, err)
	}
	want, err := declaredFormat(decl)
	if err != nil {
		return err
	}
	n, err := extractAll(*out, want, w)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "✅ Policy の入力 %d 件（reference の root ごとの plan）\n", n)

	return nil
}
