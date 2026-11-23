package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// verifyEntry は `conftest verify -o json` の1要素です。
type verifyEntry struct {
	Filename  string `json:"filename"`
	Successes int    `json:"successes"`
	Failures  []struct {
		Msg string `json:"msg"`
	} `json:"failures"`
}

// countTests は、テストの rego の行頭の `test_` を数えます。Conftest が走らせたテストの数と独立に得る宣言の側です。
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

// judgeVerify は、conftest verify の結果を、宣言のテストの数 want と突き合わせます。落ちたテストが1つでも
// あるか、通った数が want でなければ失敗します。
func judgeVerify(report []byte, want int) (int, []string, error) {
	var entries []verifyEntry
	if err := json.Unmarshal(report, &entries); err != nil {
		return 0, nil, xerrors.Wrapf(errRead, "conftest verify の結果を解釈できません: %v", err)
	}
	passed := 0
	var failed []string
	for _, e := range entries {
		passed += e.Successes
		for _, f := range e.Failures {
			failed = append(failed, fmt.Sprintf("%s: %s", e.Filename, f.Msg))
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

func runVerifyResult(args []string, w io.Writer) error {
	fset := flag.NewFlagSet("tf-policy verify-result", flag.ContinueOnError)
	fset.SetOutput(w)
	policy := fset.String("policy", "", "Policy の木（conftest/plan）")
	reportPath := fset.String("report", "", "`conftest verify -o json` の出力")
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

	return nil
}
