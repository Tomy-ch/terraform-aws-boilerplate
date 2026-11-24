package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testImage = "example/scan:1.0@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fake は docker と trivy の偽物の振る舞いを置くディレクトリである。偽物は起動ごとに「作業ディレクトリの basename 段」を
// calls.log へ書き、受け取った環境変数を env-<名>-<段>（段の `/` と `,` は `_`）へ控える。basename は最初の `.` より前
// だけを使う（os.MkdirTemp の置き場を名で引くため）。振る舞いは次のファイルで決まる。
//
//	image          イメージの段が出す内容
//	fail-<段>      その段を 1 で落とす（fail-<段>-<名> は作業ディレクトリの basename が名のときだけ）
//	kill-<段>      その段を自身へのシグナルで終わらせる（kill-<段>-<名> は fail と同じ）
//	scenario/<名>/ 名は作業ディレクトリの basename（無ければ default）。rc は up と trivy の終了コード、stdout は trivy の
//	               stdout、out/ は up がコンテナの /tmp/out に残すもの
//
// copy-in は、その時点の compose.yaml を compose-<名>.yaml へ、work/ の下のファイルの一覧を work-<名>.txt へ控える
// （canary の作業ディレクトリは通れば消えるため）。
// up と trivy は、stdout と stderr に印を書く。trivy-convert/sarif は convert-sarif（無ければ ROOTPATH を作業ディレクトリの
// result.json に向けた SARIF）を、ほかの trivy-convert は result.json を `converted <形>:` に続けて stdout へ書く。
type fake struct {
	t   *testing.T
	dir string
}

func newFake(t *testing.T) (*fake, runner) {
	t.Helper()
	f := &fake{t: t, dir: t.TempDir()}
	f.write("image", testImage+"\n")
	script := filepath.Join(f.dir, "tool")
	body := `#!/bin/sh
F='` + f.dir + `'
step="$1"
d=$(basename "$PWD"); d=${d%%.*}
echo "$d $step" >> "$F/calls.log"
env | LC_ALL=C sort > "$F/env-$d-$(printf %s "$step" | tr '/,' '__')"
if [ -e "$F/kill-$step" ] || [ -e "$F/kill-$step-$d" ]; then kill -9 $$; fi
if [ -e "$F/fail-$step" ] || [ -e "$F/fail-$step-$d" ]; then echo "fake: $step failed" >&2; exit 1; fi
s="$F/scenario/$d"; [ -d "$s" ] || s="$F/scenario/default"
rc=$(cat "$s/rc" 2>/dev/null || echo 0)
case "$step" in
image-*) cat "$F/image" ;;
copy-in) cp compose.yaml "$F/compose-$d.yaml"; (cd work && find . -type f | LC_ALL=C sort) > "$F/work-$d.txt"
    [ -d work/src ] || exit 9 ;;
up) echo "up-stdout $d"; echo "up-stderr $d" >&2; rm -rf .fake-out; mkdir .fake-out
    if [ -d "$s/out" ]; then cp -R "$s/out/." .fake-out/; fi; exit "$rc" ;;
copy-out) cp -R .fake-out/. out/ ;;
trivy-config/*) [ -e .trivyignore.yaml ] || exit 8
    if [ -e "$s/stdout" ]; then cat "$s/stdout"; fi; echo "trivy-stderr $d" >&2; exit "$rc" ;;
trivy-convert/sarif) [ -e result.json ] || exit 7
    if [ -e "$F/convert-sarif" ]; then cat "$F/convert-sarif"
    else printf '{"runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"file://%s/result.json/"}},"results":[]}]}' "$PWD"; fi ;;
trivy-convert/*) [ -e result.json ] || exit 7; printf 'converted %s:' "${step#trivy-convert/}"; cat result.json ;;
esac
exit 0
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	// exec せず /bin/sh に読ませる。並行のテストが書いたばかりのスクリプトの exec は ETXTBSY で落ちうる（tooltest.Fake.Tool と同じ）。
	tool := func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), "/bin/sh", script, step)
		c.Dir, c.Env = d, env
		return c
	}
	return f, runner{tool: tool, environ: os.Environ()}
}

func (f *fake) write(rel, body string) {
	f.t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(rel))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(f.t, os.WriteFile(p, []byte(body), 0o600))
}

func (f *fake) fail(step string) { f.write("fail-"+step, "") }

func (f *fake) failIn(step, name string) { f.write("fail-"+step+"-"+name, "") }

func (f *fake) kill(step string) { f.write("kill-"+step, "") }

func (f *fake) killIn(step, name string) { f.write("kill-"+step+"-"+name, "") }

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// out は /tmp/out からの相対パスと中身である。
func (f *fake) scenario(name string, rc int, stdout string, out map[string]string) {
	f.write("scenario/"+name+"/rc", strconv.Itoa(rc))
	if stdout != "" {
		f.write("scenario/"+name+"/stdout", stdout)
	}
	for rel, body := range out {
		f.write("scenario/"+name+"/out/"+rel, body)
	}
	if len(out) == 0 {
		require.NoError(f.t, os.MkdirAll(filepath.Join(f.dir, "scenario", name, "out"), 0o750))
	}
}

func (f *fake) calls() []string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(f.t, err)
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func (f *fake) envOf(name, step string) []string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "env-"+name+"-"+strings.NewReplacer("/", "_", ",", "_").Replace(step)))
	require.NoError(f.t, err)
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func (f *fake) composeOf(name string) string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "compose-"+name+".yaml"))
	require.NoError(f.t, err)
	return string(raw)
}

func (f *fake) workOf(name string) []string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "work-"+name+".txt"))
	require.NoError(f.t, err)
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for rel, body := range files {
		p := filepath.Join(base, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return base
}

func tfTree(t *testing.T) string {
	t.Helper()
	return tree(t, map[string]string{
		"usecases/a/main.tf":                 "",
		"usecases/a/.terraform.lock.hcl":     "",
		"usecases/a/.terraform/x/ignored.tf": "",
		"modules/b/main.tf":                  "",
		"env/c/main.tf":                      "",
	})
}

func cycle(name string) []string {
	return []string{name + " create", name + " copy-in", name + " up", name + " copy-out", name + " down"}
}
