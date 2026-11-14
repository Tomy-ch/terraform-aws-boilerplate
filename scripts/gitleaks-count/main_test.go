package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
)

type fixture struct {
	t   *testing.T
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{t: t, dir: t.TempDir()}
	f.git("init", "-q")
	f.write("base", "base\n")
	f.commit("base")

	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()

	cmd := exec.CommandContext(f.t.Context(), "git", append([]string{
		"-C", f.dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
	}, args...)...)
	cmd.Env = gitFreeEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(f.t, err, string(out))

	return strings.TrimSpace(string(out))
}

func (f *fixture) write(name, body string) {
	f.t.Helper()

	require.NoError(f.t, os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o600))
	f.git("add", name)
}

func (f *fixture) commit(msg string, extra ...string) {
	f.t.Helper()

	f.git(append([]string{"commit", "-q", "-m", msg}, extra...)...)
}

func (f *fixture) head() string {
	f.t.Helper()

	return f.git("rev-parse", "HEAD")
}

// numstat は make の GITLEAKS_NUMSTAT と同じ引数で git log を取る。
func (f *fixture) numstat(rev string) []byte {
	f.t.Helper()

	cmd := exec.CommandContext(f.t.Context(), "git", "-C", f.dir, "log", "--no-merges", "-M", "--diff-filter=d",
		"--format=commit %H", "--numstat", "-z", rev)
	cmd.Env = gitFreeEnv()
	out, err := cmd.Output()
	require.NoError(f.t, err)

	return out
}

// gitFreeEnv は、git の hook の中で走ったときに外側のリポジトリを指す GIT_* を外した環境を返す。
func gitFreeEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_")
	})
}

func writeFile(t *testing.T, body string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))

	return p
}

func Test_run(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	from := f.head()
	f.write("a", "a\n")
	f.commit("text")
	f.commit("empty", "--allow-empty")
	ns := writeFile(t, string(f.numstat(from+"..HEAD")))

	ok := map[string][]string{
		"範囲: 空の commit を数えない走査を通す": {"-log", writeFile(t, "INF 1 commits scanned.\n"), "-numstat", ns},
		"全履歴: 下限ちょうど":              {"-log", writeFile(t, "INF 1 commits scanned.\n"), "-min", "1"},
	}
	for name, args := range ok {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, run(args))
		})
	}

	ng := map[string]struct {
		args []string
		want error
	}{
		"範囲: 走査が0件":             {[]string{"-log", writeFile(t, "INF 0 commits scanned.\n"), "-numstat", ns}, errMismatch},
		"範囲: 空の commit まで数えた走査": {[]string{"-log", writeFile(t, "INF 2 commits scanned.\n"), "-numstat", ns}, errMismatch},
		"全履歴: 下限の1つ手前":          {[]string{"-log", writeFile(t, "INF 0 commits scanned.\n"), "-min", "1"}, errMismatch},
		"数を読めない出力":              {[]string{"-log", writeFile(t, "ERR stderr is not empty\n"), "-numstat", ns}, errNoCount},
		"gitleaks の出力が無い":       {[]string{"-log", filepath.Join(t.TempDir(), "absent"), "-min", "1"}, errRead},
		"numstat が無い":           {[]string{"-log", writeFile(t, "INF 1 commits scanned.\n"), "-numstat", filepath.Join(t.TempDir(), "absent")}, errRead},
		"numstat を解釈できない":       {[]string{"-log", writeFile(t, "INF 1 commits scanned.\n"), "-numstat", writeFile(t, "x\x00")}, errNumstat},
		"-log が無い":              {[]string{"-min", "1"}, errUsage},
		"-numstat と -min の両方":   {[]string{"-log", ns, "-numstat", ns, "-min", "1"}, errUsage},
		"-numstat も -min も無い":   {[]string{"-log", ns}, errUsage},
		"-min が0（全履歴が0件で通る）":    {[]string{"-log", ns, "-min", "0"}, errUsage},
		"余分な位置引数":               {[]string{"-log", ns, "-min", "1", "x"}, errUsage},
		"知らない flag":             {[]string{"-x"}, errUsage},
	}
	for name, c := range ng {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, run(c.args), c.want)
		})
	}
}

func Test_scanned(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want int
	}{
		"gitleaks の行": {"9:41AM INF 4 commits scanned.\n9:41AM INF no leaks found\n", 4},
		"0件":          {"INF 0 commits scanned.\n", 0},
		"前の文字が数字でない":  {"x12 commits scanned.", 12},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			n, err := scanned(c.out)
			require.NoError(t, err)
			assert.Equal(t, c.want, n)
		})
	}

	for name, out := range map[string]string{
		"数の行が無い": "ERR error=\"stderr is not empty\"\n",
		"空の出力":   "",
		"数の行が2つ": "INF 1 commits scanned.\nINF 2 commits scanned.\n",
		"桁あふれ":   "INF 99999999999999999999 commits scanned.\n",
		"数の無い行":  "INF commits scanned.\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scanned(out)
			require.ErrorIs(t, err, errNoCount)
		})
	}
}

func Test_expected(t *testing.T) {
	t.Parallel()

	t.Run("git の出力", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			build       func(f *fixture)
			must, maybe int
		}{
			"空の範囲":           {func(*fixture) {}, 0, 0},
			"空の commit だけ":   {func(f *fixture) { f.commit("empty", "--allow-empty") }, 0, 0},
			"空の commit を含む":  {func(f *fixture) { f.write("a", "a\n"); f.commit("a"); f.commit("empty", "--allow-empty") }, 1, 0},
			"行を足す":           {func(f *fixture) { f.write("a", "a\n"); f.commit("a") }, 1, 0},
			"行を消すだけの変更":      {func(f *fixture) { f.write("base", ""); f.commit("shrink") }, 1, 0},
			"ファイルの削除だけ":      {func(f *fixture) { f.git("rm", "-q", "base"); f.commit("rm") }, 0, 0},
			"空のファイルの追加":      {func(f *fixture) { f.write("e", ""); f.commit("e") }, 0, 0},
			"mode の変更だけ":     {func(f *fixture) { f.git("update-index", "--chmod=+x", "base"); f.commit("x") }, 0, 0},
			"行の変わらない rename": {func(f *fixture) { f.git("mv", "base", "moved"); f.commit("mv") }, 0, 1},
			"行の変わる rename": {func(f *fixture) {
				f.git("mv", "base", "moved")
				f.write("moved", "base\nmore\n")
				f.commit("mv")
			}, 1, 0},
			"バイナリだけ":    {func(f *fixture) { f.write("b.bin", "\x00\x01\x02"); f.commit("bin") }, 0, 1},
			"バイナリとテキスト": {func(f *fixture) { f.write("b.bin", "\x00\x01"); f.write("a", "a\n"); f.commit("both") }, 1, 0},
			"commit ごとに数える": {func(f *fixture) {
				f.write("a", "a\n")
				f.commit("a")
				f.write("b", "b\n")
				f.write("c", "c\n")
				f.commit("bc")
			}, 2, 0},
			"空白や改行を含む path": {func(f *fixture) { f.write("a b\nc", "x\n"); f.commit("odd") }, 1, 0},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				f := newFixture(t)
				from := f.head()
				c.build(f)

				must, maybe, err := expected(f.numstat(from + "..HEAD"))
				require.NoError(t, err)
				assert.Equal(t, c.must, must, "必ず数える commit")
				assert.Equal(t, c.maybe, maybe, "数えるかを決められない commit")
			})
		}
	})

	t.Run("解釈できない出力", func(t *testing.T) {
		t.Parallel()

		hash := strings.Repeat("a", 40)
		for name, raw := range map[string]string{
			"commit の行より前の numstat": "\n1\t0\ta\x00",
			"object 名でない commit の行": "commit xyz\x00",
			"大文字の object 名":         "commit " + strings.Repeat("A", 40) + "\x00",
			"列が足りない":                "commit " + hash + "\x00\n1\t0\x00",
			"数でない行数":                "commit " + hash + "\x00\nx\t0\ta\x00",
			"片側だけの -":               "commit " + hash + "\x00\n-\t0\ta\x00",
			"rename の path が欠けている":  "commit " + hash + "\x00\n0\t0\t\x00old\x00",
			"途中の空の区切り":              "commit " + hash + "\x00\x00\n1\t0\ta\x00",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, _, err := expected([]byte(raw))
				require.ErrorIs(t, err, errNumstat)
			})
		}
	})

	t.Run("SHA-256 の object 名", func(t *testing.T) {
		t.Parallel()

		must, maybe, err := expected([]byte("commit " + strings.Repeat("b", 64) + "\x00\n1\t0\ta\x00"))
		require.NoError(t, err)
		assert.Equal(t, [2]int{1, 0}, [2]int{must, maybe})
	})

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		entries, err := misetoml.ParseFile(filepath.Join("..", "..", "mise.toml"))
		require.NoError(t, err)

		got, ok := misetoml.Lookup(entries, "aqua:gitleaks/gitleaks")
		require.True(t, ok, "mise.toml に gitleaks の宣言がありません")
		assert.Equal(t, gitleaksVersion, got, "gitleaks を上げたら、数え方を新しい版のソースで確かめ直す")
	})
}

func Test_lineCounts(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		added, deleted  string
		changed, binary bool
	}{
		"行が変わる":   {"1", "0", true, false},
		"行を消すだけ":  {"0", "3", true, false},
		"行が変わらない": {"0", "0", false, false},
		"バイナリ":    {"-", "-", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			changed, binary, err := lineCounts(c.added, c.deleted)
			require.NoError(t, err)
			assert.Equal(t, c.changed, changed)
			assert.Equal(t, c.binary, binary)
		})
	}

	for name, c := range map[string][2]string{
		"負の数":     {"-1", "0"},
		"数でない":    {"a", "0"},
		"空":       {"", ""},
		"片側だけの -": {"0", "-"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := lineCounts(c[0], c[1])
			require.ErrorIs(t, err, errNumstat)
		})
	}
}

func Test_isHash(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]bool{
		strings.Repeat("0", 40): true,
		strings.Repeat("f", 64): true,
		strings.Repeat("0", 39): false,
		strings.Repeat("0", 41): false,
		strings.Repeat("g", 40): false,
		strings.Repeat("F", 40): false,
		"":                      false,
	} {
		t.Run(fmt.Sprintf("%q", s), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, isHash(s))
		})
	}
}

// fakeGitleaks は gitleaks git の偽物。走査した数は gitleaksVersion の数え方（削除されていないファイルの
// hunk を持つ commit）で数える。範囲の走査（位置引数の無い起動）に限り、FAKE_GITLEAKS_SCANNED で数を
// 差し替える。追加した行に AKIA があれば所見として終了コード 1 を返す。
const fakeGitleaks = `#!/bin/sh
set -eu
src= opts= report=
while [ $# -gt 0 ]; do
	case "$1" in
	--log-opts=*) opts=${1#--log-opts=} ;;
	--report-path) report=$2; shift ;;
	--report-format|--config|--gitleaks-ignore-path) shift ;;
	-*|git) ;;
	*) src=$1 ;;
	esac
	shift
done
if [ -n "$src" ]; then
	g() { env -u GIT_DIR -u GIT_WORK_TREE git -C "$src" "$@"; }
else
	g() { git "$@"; }
fi
if [ -n "$opts" ]; then set -- $opts; else set -- --full-history --all --diff-filter=tuxdb; fi
patch=$(g log -p -U0 --format='commit %H' "$@")
n=$(printf '%s\n' "$patch" | awk '/^commit /{c=$2; next} /^diff --git /{del=0} /^deleted file mode/{del=1} /^@@/ && !del && !(c in s){s[c]=1; k++} END{print k+0}')
if [ -z "$src" ] && [ -n "${FAKE_GITLEAKS_SCANNED:-}" ]; then n=$FAKE_GITLEAKS_SCANNED; fi
echo "INF $n commits scanned." >&2
if printf '%s\n' "$patch" | grep -q '^+.*AKIA'; then
	if [ -n "$report" ]; then printf '[{"RuleID": "aws-access-token"}]\n' > "$report"; fi
	echo "WRN leaks found: 1" >&2
	exit 1
fi
if [ -n "$report" ]; then printf '[]\n' > "$report"; fi
echo "INF no leaks found" >&2
`

// recipe は gitleaks.mk のレシピ target を、偽の gitleaks と f のリポジトリに対して走らせる。
// gitleaks-count は実物の GITLEAKS_COUNT（RUN_SCRIPT）がビルドして起動する。
func recipe(t *testing.T, f *fixture, target string, vars []string, env ...string) (string, error) {
	t.Helper()

	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gitleaks"), []byte(fakeGitleaks), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない

	root := filepath.Join("..", "..")
	scriptBin, err := os.MkdirTemp(filepath.Join(root, "tmp"), "gitleaks-count-test.")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(scriptBin) })
	rel, err := filepath.Rel(root, scriptBin)
	require.NoError(t, err)

	args := append([]string{"-C", root, "-s", "--no-print-directory", target, "RUNNER_MODE=host", "GO_TOOL=", "SCRIPT_BIN=" + rel}, vars...)
	cmd := exec.CommandContext(t.Context(), "make", args...)
	cmd.Env = append(slices.DeleteFunc(gitFreeEnv(), func(kv string) bool {
		return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
			strings.HasPrefix(kv, "GITHUB_") || strings.HasPrefix(kv, "FAKE_GITLEAKS_") || strings.HasPrefix(kv, "PATH=")
	}), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_DIR="+filepath.Join(f.dir, ".git"), "GIT_WORK_TREE="+f.dir)
	cmd.Env = append(cmd.Env, env...)

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// TestGitleaksRecipes は、gitleaks.mk のレシピが範囲の数え方を gitleaks-count へ渡し、その失敗を
// 握り潰さないことを、偽の gitleaks で実際に走らせて見る。判定そのものは Test_run と Test_expected が持つ。
func TestGitleaksRecipes(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(filepath.Join("..", "..", "tmp")); err != nil {
		require.NoError(t, os.MkdirAll(filepath.Join("..", "..", "tmp"), 0o750))
	}

	type scenario struct {
		build    func(f *fixture)
		fromHead bool
		env      []string
		fail     bool
	}
	addText := func(f *fixture) { f.write("a", "a\n"); f.commit("a") }

	for name, c := range map[string]scenario{
		"空の commit を含む範囲を通す": {build: func(f *fixture) { addText(f); f.commit("empty", "--allow-empty") }},
		"空の範囲を通す":            {fromHead: true},
		"空の commit だけの範囲を通す": {build: func(f *fixture) { f.commit("empty", "--allow-empty") }},
		"削除・mode・rename だけの commit を含む範囲を通す": {build: func(f *fixture) {
			addText(f)
			f.git("rm", "-q", "a")
			f.commit("rm")
			f.git("update-index", "--chmod=+x", "base")
			f.commit("x")
			f.git("mv", "base", "moved")
			f.commit("mv")
		}},
		"走査が0件なら落とす":   {build: addText, env: []string{"FAKE_GITLEAKS_SCANNED=0"}, fail: true},
		"範囲より多い走査を落とす": {build: addText, env: []string{"FAKE_GITLEAKS_SCANNED=2"}, fail: true},
	} {
		t.Run("secret-scan: "+name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			from := f.head()
			if c.build != nil {
				c.build(f)
			}
			if c.fromHead {
				from = f.head()
			}

			out, err := recipe(t, f, "secret-scan", []string{"COMMITLINT_FROM=" + from}, c.env...)
			if !c.fail {
				require.NoError(t, err, out)
				assert.Contains(t, out, "commits scanned", "gitleaks が走っていません")
				return
			}
			require.Error(t, err, out)
			assert.Contains(t, out, "❌ gitleaks-count:", "突き合わせ以外で落ちています")
		})
	}

	t.Run("secret-scan-history: 走査が0件なら落とす", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out, err := recipe(t, f, "secret-scan-history", nil, "FAKE_GITLEAKS_SCANNED=0")
		require.Error(t, err, out)
		assert.Contains(t, out, "❌ gitleaks-count:")
	})

	t.Run("secret-scan-history: 走査が1件以上なら通す", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out, err := recipe(t, f, "secret-scan-history", nil)
		require.NoError(t, err, out)
	})
}
