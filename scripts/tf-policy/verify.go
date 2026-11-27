package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// verifyEntry は `opa test -f json` の1要素です。duration はナノ秒です。
type verifyEntry struct {
	Package  string          `json:"package"`
	Name     string          `json:"name"`
	Fail     bool            `json:"fail"`
	Error    json.RawMessage `json:"error"`
	Skip     bool            `json:"skip"`
	Duration int64           `json:"duration"`
}

// failure は、落ちたケースなら名前と理由を返します。時間切れは error（eval_cancel_error）として表れます。
func (e verifyEntry) failure() (string, bool) {
	name := strings.TrimPrefix(e.Package, "data.") + "." + e.Name
	if len(e.Error) > 0 && string(e.Error) != "null" {
		var te struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(e.Error, &te) == nil && te.Code != "" {
			return fmt.Sprintf("%s: %s: %s", name, te.Code, te.Message), true
		}

		return fmt.Sprintf("%s: %s", name, e.Error), true
	}
	if e.Fail {
		return name, true
	}

	return "", false
}

func parseVerify(report []byte) ([]verifyEntry, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(report), []byte("[")) {
		return nil, xerrors.Wrap(errRead, "opa test の結果を解釈できません: JSON の配列ではありません")
	}
	var entries []verifyEntry
	if err := json.Unmarshal(report, &entries); err != nil {
		return nil, xerrors.Wrapf(errRead, "opa test の結果を解釈できません: %v", err)
	}
	// null や {} の要素はゼロ値になり、通った1件に数えられてしまう。opa test の要素は package と name を必ず持つ。
	for i, e := range entries {
		if e.Package == "" || e.Name == "" {
			return nil, xerrors.Wrapf(errRead, "opa test の結果の %d 番目の要素が package と name を持ちません", i+1)
		}
	}

	return entries, nil
}

// countTests は、テストの rego の行頭の `test_` を数えます。opa test が走らせたテストの数と独立に得る宣言の側です。
func countTests(files []regoFile) int {
	n := 0
	for _, f := range files {
		if !f.test {
			continue
		}
		for line := range strings.SplitSeq(f.src, "\n") {
			if strings.HasPrefix(line, "test_") {
				n++
			}
		}
	}

	return n
}

// judgeVerify は、opa test の結果を、宣言のテストの数 want と突き合わせます。落ちたテストが1つでもあるか、通った
// 数が want でなければ失敗します。skip したケースは通った数に入れません。
func judgeVerify(report []byte, want int) (int, []string, error) {
	entries, err := parseVerify(report)
	if err != nil {
		return 0, nil, err
	}
	passed := 0
	var failed []string
	for _, e := range entries {
		if f, ok := e.failure(); ok {
			failed = append(failed, f)

			continue
		}
		if !e.Skip {
			passed++
		}
	}
	if len(failed) > 0 {
		return passed, failed, xerrors.Wrapf(errFailed, "Rego のテストが落ちました（%d 件）", len(failed))
	}
	if passed != want {
		return passed, nil, xerrors.Wrapf(errFailed, "通った Rego のテストが %d 件で、宣言された %d 件と一致しません", passed, want)
	}

	return passed, nil, nil
}

// slowest は、時間のかかったケースを長い順に n 件返します。上限との余裕を読むための表示で、判定には使いません。
func slowest(report []byte, n int) []string {
	entries, err := parseVerify(report)
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Duration > entries[j].Duration })
	var out []string
	for _, e := range entries[:min(n, len(entries))] {
		out = append(out, fmt.Sprintf("%s.%s %s", strings.TrimPrefix(e.Package, "data."), e.Name, time.Duration(e.Duration).Round(time.Millisecond)))
	}

	return out
}

func runVerifyResult(args []string, w io.Writer) error {
	fset := flag.NewFlagSet("tf-policy verify-result", flag.ContinueOnError)
	fset.SetOutput(w)
	policy := fset.String("policy", "", "Policy の木（conftest/plan）")
	reportPath := fset.String("report", "", "`opa test -f json` の出力")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *policy == "" || *reportPath == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "tf-policy verify-result -policy <dir> -report <path>")
	}
	files, err := readPolicy(*policy)
	if err != nil {
		return err
	}
	want := countTests(files)
	if want == 0 {
		return xerrors.Wrapf(errNoInput, "Rego のテストが0件です（%s）", *policy)
	}
	raw, err := os.ReadFile(filepath.Clean(*reportPath))
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", *reportPath, err)
	}
	passed, failed, err := judgeVerify(raw, want)
	for _, f := range failed {
		fmt.Fprintf(w, "  %s\n", f)
	}
	if err != nil {
		if len(failed) == 0 && !xerrors.Is(err, errFailed) {
			fmt.Fprintf(w, "%s\n", strings.TrimSpace(string(raw)))
		}

		return err
	}
	fmt.Fprintf(w, "✅ Rego のテスト %d 件\n", passed)
	for _, l := range slowest(raw, 3) {
		fmt.Fprintf(w, "  遅いケース: %s\n", l)
	}

	return nil
}
