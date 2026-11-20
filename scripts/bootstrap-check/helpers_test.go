package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	commitSHA    = "0123456789abcdef0123456789abcdef01234567"
	headerClean  = "commit: " + commitSHA
	headerDirty  = "commit: " + commitSHA + " （作業ツリーに未コミットの変更あり）"
	expectedBody = "# コメント\n\nimport b.y\nadd a.x\n"
)

// write は、dir の下に name の内容のファイルを作り、そのパスを返す。
func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// rc は、plan の resource_changes の1件（管理対象）。
func rc(address, actions string, importing bool) string {
	imp := "null"
	if importing {
		imp = `{"id":"i"}`
	}

	return fmt.Sprintf(`{"address":%q,"mode":"managed","change":{"actions":%s,"importing":%s}}`, address, actions, imp)
}

// dataRC は、data source の resource_changes の1件。
func dataRC(address string) string {
	return fmt.Sprintf(`{"address":%q,"mode":"data","change":{"actions":["read"],"importing":null}}`, address)
}

// matchingChanges は、expectedBody に当たる変更。
func matchingChanges() []string {
	return []string{rc("a.x", `["create"]`, false), rc("b.y", `["no-op"]`, true), dataRC("data.c")}
}

func planJSON(changes ...string) string {
	return `{"format_version":"1.2","complete":true,"errored":false,"resource_changes":[` + strings.Join(changes, ",") + `]}`
}

func runEvent(file, run, status string) string {
	return fmt.Sprintf(`{"type":"test_run","@testfile":%q,"@testrun":%q,"test_run":{"status":%q}}`, file, run, status)
}

// planEvent の version は JSON の値そのもの（`"1.2"` や `2`）。
func planEvent(file, run, version string, changes ...string) string {
	return fmt.Sprintf(`{"type":"test_plan","@testfile":%q,"@testrun":%q,"test_plan":{"plan_format_version":%s,"resource_changes":[%s],"provider_schemas":{"x":{}}}}`,
		file, run, version, strings.Join(changes, ","))
}

func errorEvent(message string) string {
	return fmt.Sprintf(`{"type":"diagnostic","@level":"error","@message":%q}`, message)
}

func testOutputOf(events ...string) string {
	return strings.Join(events, "\n") + "\n"
}

// passingTestOutput は、run "check_plan" が pass で終わり、plan を1件出した terraform test の出力。
func passingTestOutput(changes ...string) string {
	return testOutputOf(
		runEvent(mockFile, mockRun, "pass"),
		planEvent(mockFile, "other", `"1.2"`),
		planEvent(mockFile, mockRun, `"1.2"`, changes...),
	)
}

// osDeps は、実際のファイルを読み、標準入力に stdin を渡す依存。
func osDeps(stdin string) deps {
	return deps{readFile: os.ReadFile, stdin: strings.NewReader(stdin)}
}

// osDepsExpected は、osDeps に期待値のパスを足した依存。
func osDepsExpected(stdin, expected string) deps {
	d := osDeps(stdin)
	d.expected = expected

	return d
}

// errReader は、読むたびに失敗する io.Reader。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, os.ErrClosed }

// errWriter は、書くたびに失敗する io.Writer。
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// partialWriter は、先頭の半分だけ書いて失敗する io.Writer。
type partialWriter struct{}

func (partialWriter) Write(p []byte) (int, error) { return len(p) / 2, os.ErrClosed }
