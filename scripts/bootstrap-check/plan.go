package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// mockFile と mockRun は、判定に使う run。mock と override は同じファイルのほかの run と共有する。
	mockFile = "tests/imports.tftest.hcl"
	mockRun  = "check_plan"
)

// kinds は、期待値の1行目の語。件数の要約もこの順で出す。
var kinds = []string{"import", "add", "change", "destroy"}

var (
	headerPattern = regexp.MustCompile(`^commit: [0-9a-f]{40}( \S.*)?$`)

	errInputUnreadable    = xerrors.New("plan のファイルを読めません。plan を検証できません")
	errExpectedUnreadable = xerrors.New("期待値を読めません。plan を検証できません")
	errExpectedEmpty      = xerrors.New("期待値が空です。plan を検証できません")
	errExpectedMalformed  = xerrors.New("期待値の行が「<import|add|change|destroy> <address>」の形ではありません。plan を検証できません")
	errExpectedDuplicate  = xerrors.New("期待値に同じ行が2つあります。plan を検証できません")
	errHeader             = xerrors.New("plan-header の出力を読めません。plan を検証できません")
	errPlanJSON           = xerrors.New("plan の JSON ではありません。plan を検証できません")
	errPlanErrored        = xerrors.New("plan がエラーで終わっています。plan を検証できません")
	errPlanIncomplete     = xerrors.New("plan が完全ではありません（deferred な変更があります）。plan を検証できません")
	errPlanShape          = xerrors.New("plan の JSON の形が想定と違います。plan を検証できません")
	errPlanEmpty          = xerrors.New("plan に管理対象の resource が1件もありません。plan を検証できません")
	errMockUnreadable     = xerrors.New("terraform test の出力を読めません。plan を検証できません")
	errTestFailed         = xerrors.New("terraform test が落ちました。plan を検証できません")
	errMockNotPassed      = xerrors.New("tests/imports.tftest.hcl の run \"check_plan\" が pass で終わっていません。plan を検証できません")
	errMockPlanCount      = xerrors.New("tests/imports.tftest.hcl の run \"check_plan\" の plan が1件ではありません。plan を検証できません")
	errMockVersion        = xerrors.New("plan の形の版が想定と違います。plan を検証できません")
)

func checkPlan(o options, out io.Writer, d deps) error {
	input, err := readInput(o.input, d)
	if err != nil {
		return err
	}

	raw, err := d.readFile(d.expected)
	if err != nil {
		return xerrors.Wrap(errExpectedUnreadable, d.expected)
	}

	if o.mock && d.bootstrapping != "" {
		// 無いことは、立ち上げの中のアカウントが無いことを表す。メンバーの resource が plan に残れば余剰で落ちる。
		extra, err := d.readFile(d.bootstrapping)

		switch {
		case err == nil:
			raw = append(append(raw, '\n'), extra...)
		case !errors.Is(err, fs.ErrNotExist):
			return xerrors.Wrap(errExpectedUnreadable, d.bootstrapping)
		}
	}

	expected, err := parseExpected(raw, d.expected)
	if err != nil {
		return err
	}

	if o.mock {
		actual, err := checkMock(o, input, out, d)
		if err != nil {
			return err
		}

		return compare(out, "mock の plan", expected, actual, false)
	}

	actual, err := parsePlan(input)
	if err != nil {
		return err
	}

	header, err := readHeader(o.headerFile, d)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "# "+header)

	return compare(out, "plan", expected, actual, o.partial)
}

func readInput(path string, d deps) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(d.stdin)
		if err != nil {
			return nil, xerrors.Wrap(errInputUnreadable, "標準入力")
		}

		return data, nil
	}

	data, err := d.readFile(path)
	if err != nil {
		return nil, xerrors.Wrap(errInputUnreadable, path)
	}

	return data, nil
}

// readHeader は、`tf-local plan-header` の出力（commit の1行）を返す。
func readHeader(path string, d deps) (string, error) {
	data, err := d.readFile(path)
	if err != nil {
		return "", xerrors.Wrap(errHeader, path)
	}

	header := strings.TrimSpace(string(data))
	if !headerPattern.MatchString(header) {
		return "", xerrors.Wrap(errHeader, path)
	}

	return header, nil
}

// parseExpected は、期待値のファイルを「<種類> <address>」の行へ読み、整列して返す。
// 形の崩れた行と同じ行の重複は、取りこぼさずに失敗する。
func parseExpected(data []byte, path string) ([]string, error) {
	var lines []string

	seen := map[string]bool{}

	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		kind, address, ok := strings.Cut(line, " ")
		if !ok || !slices.Contains(kinds, kind) || address == "" || address != strings.TrimSpace(address) || strings.ContainsAny(address, "\t\r") {
			return nil, xerrors.Wrapf(errExpectedMalformed, "%s の %d 行目 %q", path, i+1, line)
		}

		if seen[line] {
			return nil, xerrors.Wrapf(errExpectedDuplicate, "%s の %d 行目 %q", path, i+1, line)
		}

		seen[line] = true
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return nil, xerrors.Wrap(errExpectedEmpty, path)
	}

	sort.Strings(lines)

	return lines, nil
}

type resourceChange struct {
	Address string `json:"address"`
	Mode    string `json:"mode"`
	Change  struct {
		Actions   []string        `json:"actions"`
		Importing json.RawMessage `json:"importing"`
	} `json:"change"`
}

// parsePlan は、`terraform show -json` の plan から「<種類> <address>」の行を整列して返す。
func parsePlan(data []byte) ([]string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return nil, errPlanJSON
	}

	version := top["format_version"]
	if !isString(version) {
		return nil, errPlanJSON
	}

	if !hasMajorOne(version) {
		return nil, errMockVersion
	}

	errored, err := boolOr(top, "errored", false)
	if err != nil {
		return nil, err
	}

	complete, err := boolOr(top, "complete", true)
	if err != nil {
		return nil, err
	}

	if errored {
		return nil, errPlanErrored
	}

	if !complete {
		return nil, errPlanIncomplete
	}

	return changeLines(top["resource_changes"])
}

// boolOr は、キーが無ければ def を、あれば JSON の真偽値を返す。真偽値でなければ形の誤りとして失敗する。
func boolOr(top map[string]json.RawMessage, key string, def bool) (bool, error) {
	raw, ok := top[key]
	if !ok {
		return def, nil
	}

	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errPlanShape
	}
}

// hasMajorOne は、JSON の文字列の版が "1." で始まるかを返す。
func hasMajorOne(raw json.RawMessage) bool {
	return strings.HasPrefix(strings.Trim(string(bytes.TrimSpace(raw)), `"`), "1.")
}

func isString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)

	return len(trimmed) > 0 && trimmed[0] == '"'
}

// changeLines は、resource_changes の管理対象の resource から「<種類> <address>」の行を整列して返す。
// 種類の数え方は terraform plan の要約行と同じで、置き換えは add と destroy の両方に数える。
// mode は managed か data のどちらかで、それ以外（欠けている、空、知らない値）は形の誤りとして失敗する。
// 知らない actions は種類ごと行にして、差分として現れるようにする。resource が1件も無い plan は、
// 何も検査していないので成功させない。
func changeLines(raw json.RawMessage) ([]string, error) {
	var changes []resourceChange
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &changes); err != nil {
			return nil, errPlanShape
		}
	}

	var lines []string

	resources := 0

	for _, rc := range changes {
		switch rc.Mode {
		case "managed":
		case "data":
			continue
		default:
			return nil, errPlanShape
		}

		if rc.Address == "" {
			return nil, errPlanShape
		}

		resources++

		if imp := bytes.TrimSpace(rc.Change.Importing); len(imp) > 0 && string(imp) != "null" {
			lines = append(lines, "import "+rc.Address)
		}

		lines = append(lines, actionLines(rc.Change.Actions, rc.Address)...)
	}

	if resources == 0 {
		return nil, errPlanEmpty
	}

	sort.Strings(lines)

	return lines, nil
}

func actionLines(actions []string, address string) []string {
	switch {
	case slices.Equal(actions, []string{"no-op"}):
		return nil
	case slices.Equal(actions, []string{"create"}):
		return []string{"add " + address}
	case slices.Equal(actions, []string{"update"}):
		return []string{"change " + address}
	case slices.Equal(actions, []string{"delete"}):
		return []string{"destroy " + address}
	case slices.Equal(actions, []string{"delete", "create"}), slices.Equal(actions, []string{"create", "delete"}):
		return []string{"add " + address, "destroy " + address}
	default:
		return []string{"unknown(" + strings.Join(actions, ",") + ") " + address}
	}
}

type testPlan struct {
	FormatVersion   json.RawMessage `json:"plan_format_version"`
	ResourceChanges json.RawMessage `json:"resource_changes"`
}

type testEvent struct {
	Type    string `json:"type"`
	Level   string `json:"@level"`
	Message string `json:"@message"`
	File    string `json:"@testfile"`
	Run     string `json:"@testrun"`
	TestRun struct {
		Status string `json:"status"`
	} `json:"test_run"`
	TestPlan testPlan `json:"test_plan"`
}

// testOutput は、`terraform test -json -verbose` の出力から読んだ、判定に要る分。
type testOutput struct {
	passed int
	failed bool
	plans  []testPlan
	errors []string
}

// readTestOutput は、`terraform test -json -verbose` の出力（1行1JSON）を読む。JSON として読めない行があれば失敗する。
func readTestOutput(data []byte) (testOutput, error) {
	var out testOutput

	dec := json.NewDecoder(bytes.NewReader(data))

	for {
		var ev testEvent

		err := dec.Decode(&ev)
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			return testOutput{}, errMockUnreadable
		}

		if ev.Level == "error" {
			out.errors = append(out.errors, ev.Message)
		}

		switch ev.Type {
		case "test_run":
			if ev.TestRun.Status == "fail" || ev.TestRun.Status == "error" {
				out.failed = true
			}

			if ev.File == mockFile && ev.Run == mockRun && ev.TestRun.Status == "pass" {
				out.passed++
			}
		case "test_plan":
			if ev.File == mockFile && ev.Run == mockRun {
				out.plans = append(out.plans, ev.TestPlan)
			}
		}
	}
}

// checkMock は、terraform test の出力から run "check_plan" の plan を1つだけ取り出し、行にして返す。
// terraform test が落ちていたか、run が落ちていたか、pass で終わっていなければ、出力の error の文を出して失敗する。
func checkMock(o options, data []byte, out io.Writer, d deps) ([]string, error) {
	test, err := readTestOutput(data)
	if err != nil {
		return nil, err
	}

	exitFailed, err := testExitFailed(o.testExitFile, d)
	if err != nil {
		return nil, err
	}

	if exitFailed || test.failed || test.passed != 1 {
		for _, msg := range test.errors {
			fmt.Fprintln(out, msg)
		}

		if exitFailed || test.failed {
			return nil, errTestFailed
		}

		return nil, errMockNotPassed
	}

	if len(test.plans) != 1 {
		return nil, xerrors.Wrapf(errMockPlanCount, "%d 件。1件を期待", len(test.plans))
	}

	version := test.plans[0].FormatVersion
	if !isString(version) || !hasMajorOne(version) {
		return nil, errMockVersion
	}

	return changeLines(test.plans[0].ResourceChanges)
}

// testExitFailed は、terraform test の終了コードを書いたファイルが 0 以外を示すかを返す。
func testExitFailed(path string, d deps) (bool, error) {
	data, err := d.readFile(path)
	if err != nil {
		return false, xerrors.Wrap(errInputUnreadable, path)
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n < 0 {
		return false, xerrors.Wrap(errInputUnreadable, path)
	}

	return n != 0, nil
}

// diffLines は、整列した2つの行の集まりを、重複を数えて比べる。missing は expected にだけ在る行、
// extra は actual にだけ在る行。
func diffLines(expected, actual []string) (missing, extra []string) {
	i, j := 0, 0

	for i < len(expected) && j < len(actual) {
		switch {
		case expected[i] == actual[j]:
			i++
			j++
		case expected[i] < actual[j]:
			missing = append(missing, expected[i])
			i++
		default:
			extra = append(extra, actual[j])
			j++
		}
	}

	missing = append(missing, expected[i:]...)
	extra = append(extra, actual[j:]...)

	return missing, extra
}

// summary は、種類ごとの件数の要約の1行。知らない actions の行はどの種類にも数えない。
func summary(label string, lines []string) string {
	parts := make([]string, 0, len(kinds))

	for _, kind := range kinds {
		n := 0

		for _, line := range lines {
			if strings.HasPrefix(line, kind+" ") {
				n++
			}
		}

		parts = append(parts, fmt.Sprintf("%s %d", kind, n))
	}

	return "# " + label + ": " + strings.Join(parts, " / ")
}

// compare は、期待値と plan の差を出力し、差分があれば errMismatch を返す。partial では、期待値に在って
// plan に無いものを「済み」とし、余剰が無いことだけを見る。
func compare(out io.Writer, label string, expected, actual []string, partial bool) error {
	fmt.Fprintln(out, summary("期待値", expected))
	fmt.Fprintln(out, summary(label, actual))

	missing, extra := diffLines(expected, actual)

	for _, line := range extra {
		fmt.Fprintln(out, "NG  余剰  "+line)
	}

	if partial {
		for _, line := range missing {
			fmt.Fprintln(out, "済み  "+line)
		}

		fmt.Fprintf(out, "# 結果（--partial）: 済み %d 件、余剰 %d 件\n", len(missing), len(extra))

		if len(extra) > 0 {
			return errMismatch
		}

		return nil
	}

	for _, line := range missing {
		fmt.Fprintln(out, "NG  不足  "+line)
	}

	fmt.Fprintf(out, "# 結果: 不足 %d 件、余剰 %d 件\n", len(missing), len(extra))

	if len(missing) > 0 || len(extra) > 0 {
		return errMismatch
	}

	return nil
}
