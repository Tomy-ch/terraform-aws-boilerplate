package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// miseFixture は実際の mise.toml の形を最小で再現する。`[settings]` と `[env]` を混ぜてあるのは、
// ツール宣言だけを読めていることを確かめるため。
const miseFixture = `min_version = "2026.6.0"

[settings]
experimental = true

[tools]
go = "1.26.5"
sqlc = "1.31.1"
"aqua:golang-migrate/migrate" = "4.19.1"
"go:go.uber.org/mock/mockgen" = "0.6.0"
"npm:@redocly/cli" = "2.31.4"

[env]
OTEL_LGTM_VERSION = "0.28.0"
`

// miseOneAquaTool は、GitHub リリースに紐づく backend のツールを 1 つだけ宣言した mise.toml。
const miseOneAquaTool = `[tools]
"aqua:owner/repo" = "1.2.3"
`

var errRoundTrip = xerrors.New("round trip failed")

// upstreamRedirect は、上流ホスト宛のリクエストをテストサーバへ向け替える RoundTripper。
// 上流の URL は定数から組み立てられ差し替え口が無いため、宛先だけを経路の途中で置き換える。
type upstreamRedirect struct {
	scheme string
	host   string
}

// unreachableUpstream は、上流へ到達できない状況を再現する RoundTripper。実ネットワークへ出ないことも
// 同時に保証するため、リクエストを送らない経路の検証にも使います。
type unreachableUpstream struct{}

func (u upstreamRedirect) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = u.scheme
	clone.URL.Host = u.host
	return http.DefaultTransport.RoundTrip(clone)
}

func (unreachableUpstream) RoundTrip(*http.Request) (*http.Response, error) { return nil, errRoundTrip }

func offlineClient() *http.Client { return &http.Client{Transport: unreachableUpstream{}} }

// fakeUpstream は、上流宛のリクエストを handler が受け取るようにした HTTP クライアントを返します。
func fakeUpstream(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return &http.Client{Transport: upstreamRedirect{scheme: base.Scheme, host: base.Host}}
}

// respondJSON は、path に一致したときだけ body を返し、それ以外は 404 を返すハンドラを組みます。
func respondJSON(path, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

// respondStatus は、常に code を返すハンドラを組みます。
func respondStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// requestedPath は、そのツールの公開時刻を引くために最初に叩かれたパスを返します。
func requestedPath(t *testing.T, target tool) string {
	t.Helper()
	paths := make(chan string, 1)
	client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}
		w.WriteHeader(http.StatusNotFound)
	})

	_, _ = publishedAt(t.Context(), client, target)
	select {
	case p := <-paths:
		return p
	default:
		return ""
	}
}

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fn()
	return buf.String()
}

func writeBypass(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bypass.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func releasedOn(t *testing.T, publishedOn string) *http.Client {
	t.Helper()
	return fakeUpstream(t, respondJSON(
		"/repos/owner/repo/releases/tags/v1.2.3", `{"published_at":"`+publishedOn+`T00:00:00Z"}`,
	))
}

// useMiseWorkTree は mise.toml だけを持つ作業ディレクトリを作り、そこへ移動します。パスを返すのは
// バイパス lockfile や出力先を同じツリーへ置けるようにするため。
func useMiseWorkTree(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, miseFile), []byte(content), 0o600))
	t.Chdir(dir)
	return dir
}

// useMiseGateRepo は base 側 mise.toml を 1 コミット持つリポジトリを作り、作業ツリーの mise.toml を
// current へ差し替えてそこへ移動します。gate の差分は git 越しに取るため実リポジトリが要ります。
func useMiseGateRepo(t *testing.T, base, current string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, miseFile), []byte(base), 0o600))

	git := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}
	for _, args := range [][]string{{"init", "-q"}, {"add", miseFile}, {"commit", "-q", "-m", "init"}} {
		//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
		out, err := exec.CommandContext(t.Context(), "git", append(git, args...)...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, miseFile), []byte(current), 0o600))
	t.Chdir(dir)
}

// useRepoWithoutMise は mise.toml を持たないコミットが 1 つだけあるリポジトリを作り、そこへ
// 移動します。base を取り違えた状況（宣言がまだ無い時点の commit）を再現するために使います。
func useRepoWithoutMise(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# t\n"), 0o600))

	git := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}
	for _, args := range [][]string{{"init", "-q"}, {"add", "README.md"}, {"commit", "-q", "-m", "init"}} {
		//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
		out, err := exec.CommandContext(t.Context(), "git", append(git, args...)...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	t.Chdir(dir)
}

func writeWorkTreeBypass(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(bypassFile)), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, bypassFile), []byte(body), 0o600))
}

func runCaptured(t *testing.T, args []string, client *http.Client, now time.Time) (string, error) {
	t.Helper()
	var err error
	out := captureLog(t, func() { err = run(args, client, now) })
	return out, err
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func Test_parseTools(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("裸のキーと引用符付きのキーの両方を読む", func(t *testing.T) {
			t.Parallel()
			got, err := parseTools([]byte(miseFixture))
			require.NoError(t, err)

			ids := make([]string, 0, len(got))
			for _, tool := range got {
				ids = append(ids, tool.id())
			}
			assert.ElementsMatch(t, []string{
				"go@1.26.5",
				"sqlc@1.31.1",
				"aqua:golang-migrate/migrate@4.19.1",
				"go:go.uber.org/mock/mockgen@0.6.0",
				"npm:@redocly/cli@2.31.4",
			}, ids)
		})

		// `[settings]` の値や `[env]` のバージョン値をツールとして拾うと、解決できない
		// キーが毎回 unresolved として報告され続ける。
		t.Run("tools 以外のセクションからは読まない", func(t *testing.T) {
			t.Parallel()
			got, err := parseTools([]byte(miseFixture))
			require.NoError(t, err)
			for _, tool := range got {
				assert.NotEqual(t, "OTEL_LGTM_VERSION", tool.key)
				assert.NotEqual(t, "min_version", tool.key)
			}
		})

		// mise は tool option 付きの table 形式も受け付ける。形が違うだけでゲートから外れると、
		// 窓の検査が黙って抜ける。
		t.Run("version を持つ table 形式の宣言も読む", func(t *testing.T) {
			t.Parallel()
			got, err := parseTools([]byte("[tools]\n" +
				`node = { version = "24.11.0" }` + "\n" +
				`"npm:markdownlint-cli2" = { version = "0.23.2", trust_policy_excludes = ["fastq@1.20.2"] }` + "\n" +
				`sqlc = "1.31.1"` + "\n"))
			require.NoError(t, err)

			ids := make([]string, 0, len(got))
			for _, tool := range got {
				ids = append(ids, tool.id())
			}
			assert.ElementsMatch(t, []string{"node@24.11.0", "npm:markdownlint-cli2@0.23.2", "sqlc@1.31.1"}, ids)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// **読み飛ばすと、その道具は窓の検査から外れたままゲートが緑を返す**（ADR-0702 決定14）。
		// 版を持たない宣言は、このツールが測れるものを持たないのだから、黙って対象から落とさず
		// 宣言の側を直させる。判定は misetoml が持ち、ここが固定するのは握り潰さないことである。
		t.Run("version を持たない table 形式は読み飛ばさずエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseTools([]byte("[tools]\n" +
				`sqlc = "1.31.1"` + "\n" +
				`"npm:some-tool" = { allow_builds = ["esbuild"] }` + "\n"))
			require.ErrorIs(t, err, misetoml.ErrInvalidLine)
			assert.Contains(t, err.Error(), "npm:some-tool", "直す先の行を報告していない")
		})
	})
}

func Test_parseDeclarations(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言を 1 つの一覧へまとめる", func(t *testing.T) {
			t.Parallel()
			got, err := parseDeclarations([]declaration{
				{path: miseFile, content: []byte(miseOneAquaTool)},
			})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "aqua:owner/repo@1.2.3", got[0].id())
		})

		t.Run("宣言元のパスをツールに持たせる", func(t *testing.T) {
			t.Parallel()
			got, err := parseDeclarations([]declaration{
				{path: miseFile, content: []byte(miseOneAquaTool)},
			})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, miseFile, got[0].file)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("解析に失敗したファイルのパスを添えてエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseDeclarations([]declaration{
				{path: miseFile, content: []byte("[tools]\n" + strings.Repeat("a", bufio.MaxScanTokenSize+1) + "\n")},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), miseFile)
		})
	})
}

func Test_declarationPaths(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 宣言の出所は mise.toml だけである。ここが増えたら、窓の判定も readDeclarations も
		// 追随しているかを確かめること。
		t.Run("mise.toml を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{miseFile}, declarationPaths())
		})
	})
}

func Test_readDeclarations(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("与えられたパスの中身をパスごと返す", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), miseFile)
			require.NoError(t, os.WriteFile(path, []byte(miseOneAquaTool), 0o600))

			got, err := readDeclarations([]string{path})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, path, got[0].path)
			assert.Equal(t, miseOneAquaTool, string(got[0].content))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 読めなかったファイルを空として扱うと、そこの宣言は検査されないまま通る。
		t.Run("読めないパスはエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := readDeclarations([]string{filepath.Join(t.TempDir(), "no-such-file")})
			require.Error(t, err)
		})
	})
}

func Test_diffAdded(t *testing.T) {
	t.Parallel()

	before := []tool{{key: "a", version: "1.0.0"}, {key: "b", version: "1.0.0"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("新規追加のみを返す", func(t *testing.T) {
			t.Parallel()
			got := diffAdded(before, append(before, tool{key: "c", version: "0.1.0"}))
			require.Len(t, got, 1)
			assert.Equal(t, "c@0.1.0", got[0].id())
		})

		t.Run("バージョンを上げたツールも対象にする", func(t *testing.T) {
			t.Parallel()
			got := diffAdded(before, []tool{{key: "a", version: "1.0.0"}, {key: "b", version: "2.0.0"}})
			require.Len(t, got, 1)
			assert.Equal(t, "b@2.0.0", got[0].id())
		})

		t.Run("変化が無ければ空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, diffAdded(before, before))
		})
	})
}

func Test_backendKind(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("aqua は GitHub リリース系に分類する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "github", backendKind("aqua:owner/repo"))
		})

		t.Run("ubi は GitHub リリース系に分類する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "github", backendKind("ubi:owner/repo"))
		})

		t.Run("github は GitHub リリース系に分類する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "github", backendKind("github:owner/repo"))
		})

		t.Run("go は module proxy 系に分類する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "go", backendKind("go:go.uber.org/mock/mockgen"))
		})

		t.Run("npm は npm レジストリ系に分類する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "npm", backendKind("npm:@redocly/cli"))
		})

		t.Run("取得経路を持たない backend は空種別になる", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, backendKind("asdf:someone/plugin"))
		})

		t.Run("backend 名は最初のコロンまでで判定する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "npm", backendKind("npm:@scope/pkg:extra"))
		})
	})
}

func Test_windowFor(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("GitHub リリース系の窓は 14 日", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, releaseWindowDays, windowFor("aqua:owner/repo"))
		})

		t.Run("go の窓は 7 日", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, registryWindowDays, windowFor("go:go.uber.org/mock/mockgen"))
		})

		t.Run("npm の窓は 7 日", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, registryWindowDays, windowFor("npm:@redocly/cli"))
		})

		// 窓を GitHub 側へ倒すと、レジストリ系が本来より長く止まる。既定はレジストリ窓に寄せる。
		t.Run("種別を判定できない backend はレジストリ窓を採る", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, registryWindowDays, windowFor("asdf:someone/plugin"))
		})
	})
}

func Test_escapeModulePath(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 化け方は escapeModulePath の doc コメントが持つ。
		t.Run("大文字を ! + 小文字へ変換する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "github.com/!burnt!sushi/toml", escapeModulePath("github.com/BurntSushi/toml"))
		})

		t.Run("小文字だけのパスは変えない", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "go.uber.org/mock", escapeModulePath("go.uber.org/mock"))
		})
	})
}

func Test_readBypasses(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメントと空行を読み飛ばして 1 エントリを読む", func(t *testing.T) {
			t.Parallel()
			path := writeBypass(t, "# comment\n\n"+
				`"aqua:owner/repo@1.2.3" = { expires = 2026-11-06, issue = 931, reason = "CRITICAL への即応" }`+"\n")
			got, err := readBypasses(path)
			require.NoError(t, err)
			require.Len(t, got, 1)

			b := got["aqua:owner/repo@1.2.3"]
			assert.Equal(t, day("2026-11-06"), b.expires)
			assert.Equal(t, 931, b.issue)
			assert.Equal(t, "CRITICAL への即応", b.reason)
		})

		t.Run("ファイルが無ければ空として扱う", func(t *testing.T) {
			t.Parallel()
			got, err := readBypasses(filepath.Join(t.TempDir(), "absent.toml"))
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("解釈できない行はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := readBypasses(writeBypass(t, "これは TOML ではない\n"))
			require.Error(t, err)
			assert.ErrorIs(t, err, errBypassInvalidLine)
		})

		t.Run("expires を欠いた行はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := readBypasses(writeBypass(t, `"a@1" = { issue = 1, reason = "期限が無い" }`+"\n"))
			require.Error(t, err)
			assert.ErrorIs(t, err, errBypassInvalidLine)
		})

		t.Run("キーの重複はエラーにする", func(t *testing.T) {
			t.Parallel()
			line := `"a@1" = { expires = 2026-11-06, issue = 1, reason = "r" }` + "\n"
			_, err := readBypasses(writeBypass(t, line+line))
			require.Error(t, err)
			assert.ErrorIs(t, err, errBypassDuplicateKey)
		})

		// 不在と読めない状態を同じ「空」に倒すと、壊れた lockfile がバイパス無しとして素通りする。
		t.Run("不在ではなく開けないパスはエラーにする", func(t *testing.T) {
			t.Parallel()
			underFile := filepath.Join(writeBypass(t, ""), "bypass.toml")

			_, err := readBypasses(underFile)
			require.Error(t, err)
			assert.NotErrorIs(t, err, os.ErrNotExist)
		})

		// 形は合っているが日付でない値をゼロ値の期限として通すと、その判断は誰にも気付かれない。
		t.Run("日付として解釈できない expires はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := readBypasses(writeBypass(t,
				`"a@1" = { expires = 2026-13-45, issue = 1, reason = "r" }`+"\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "expires")
		})

		t.Run("整数に収まらない issue はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := readBypasses(writeBypass(t,
				`"a@1" = { expires = 2026-11-06, issue = 99999999999999999999, reason = "r" }`+"\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "issue")
		})
	})
}

func Test_validateBypasses(t *testing.T) {
	t.Parallel()

	today := day("2026-08-06")
	declared := []tool{{key: "aqua:owner/repo", version: "1.2.3"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("期限内かつ宣言にあるエントリは違反にならない", func(t *testing.T) {
			t.Parallel()
			violations, invalid := validateBypasses(map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: day("2026-09-06"), issue: 1, reason: "r", line: 1},
			}, declared, today)
			assert.Empty(t, violations)
			assert.Empty(t, invalid)
		})

		t.Run("期限当日は許す", func(t *testing.T) {
			t.Parallel()
			violations, invalid := validateBypasses(map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: today, issue: 1, reason: "r", line: 1},
			}, declared, today)
			assert.Empty(t, violations)
			assert.Empty(t, invalid)
		})

		t.Run("上限ちょうどの期限は許す", func(t *testing.T) {
			t.Parallel()
			violations, invalid := validateBypasses(map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: today.AddDate(0, maxBypassMonths, 0), issue: 1, reason: "r", line: 1},
			}, declared, today)
			assert.Empty(t, violations)
			assert.Empty(t, invalid)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("期限切れは違反にし効力も失わせる", func(t *testing.T) {
			t.Parallel()
			violations, invalid := validateBypasses(map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: day("2026-08-05"), issue: 1, reason: "r", line: 3},
			}, declared, today)
			require.Len(t, violations, 1)
			assert.Contains(t, violations[0].msg, "が切れています")
			assert.Contains(t, invalid, "aqua:owner/repo@1.2.3")
		})

		t.Run("上限を越えた期限は違反にし効力も失わせる", func(t *testing.T) {
			t.Parallel()
			violations, invalid := validateBypasses(map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: today.AddDate(0, maxBypassMonths, 1), issue: 1, reason: "r", line: 3},
			}, declared, today)
			require.Len(t, violations, 1)
			assert.Contains(t, violations[0].msg, "を越えています")
			assert.Contains(t, invalid, "aqua:owner/repo@1.2.3")
		})

		t.Run("宣言に無いエントリは違反にする", func(t *testing.T) {
			t.Parallel()
			violations, _ := validateBypasses(map[string]bypass{
				"aqua:gone/away@9.9.9": {expires: day("2026-09-06"), issue: 1, reason: "r", line: 3},
			}, declared, today)
			require.Len(t, violations, 1)
			assert.Contains(t, violations[0].msg, "mise.toml に存在しません")
		})
	})
}

func Test_classify(t *testing.T) {
	t.Parallel()

	f := finding{tool: tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"}, ageDays: 1, window: 14}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("gate は窓内をすべて落とす", func(t *testing.T) {
			t.Parallel()
			_, blocked, reported := classify("gate", []finding{f}, nil, nil)
			require.Len(t, blocked, 1)
			assert.Empty(t, reported)
		})

		t.Run("audit は何も落とさない", func(t *testing.T) {
			t.Parallel()
			_, blocked, reported := classify("audit", []finding{f}, nil, nil)
			assert.Empty(t, blocked)
			assert.Len(t, reported, 1)
		})

		t.Run("有効なバイパスは gate を通す", func(t *testing.T) {
			t.Parallel()
			bypassed, blocked, _ := classify("gate", []finding{f}, map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: day("2026-09-06"), issue: 1, reason: "r"},
			}, nil)
			require.Len(t, bypassed, 1)
			assert.Empty(t, blocked)
		})

		// 期限切れのバイパスが素通しするなら、期限そのものが何も担保しないことになる。
		t.Run("無効なバイパスは効力を失い gate が落とす", func(t *testing.T) {
			t.Parallel()
			bypassed, blocked, _ := classify("gate", []finding{f}, map[string]bypass{
				"aqua:owner/repo@1.2.3": {expires: day("2026-01-01"), issue: 1, reason: "r"},
			}, map[string]struct{}{"aqua:owner/repo@1.2.3": {}})
			assert.Empty(t, bypassed)
			require.Len(t, blocked, 1)
		})
	})
}

func Test_summary(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("除外件数と窓を先頭に述べる", func(t *testing.T) {
			t.Parallel()
			got := summary("audit", nil, nil, []tool{{key: "go", version: "1.26.5"}}, nil, nil, nil, nil)
			assert.Contains(t, got, "ランタイム除外 1 件")
			assert.Contains(t, got, "窓 GitHub 14 日・レジストリ 7 日")
		})

		t.Run("ブロックした件を見出し付きで並べる", func(t *testing.T) {
			t.Parallel()
			f := finding{
				tool:      tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"},
				published: day("2026-08-01"), ageDays: 5, window: 14,
			}
			got := summary("gate", []finding{f}, nil, nil, nil, nil, nil, []tool{f.tool})
			assert.Contains(t, got, "## cooldown 未達 (1)")
			assert.Contains(t, got, "aqua:owner/repo@1.2.3")
		})

		// audit が窓内のツールを黙って落とすと、棚卸しの結果がサマリに何も残らない。
		t.Run("ブロックしない窓内の件も参考として並べる", func(t *testing.T) {
			t.Parallel()
			f := finding{
				tool:      tool{key: "npm:@redocly/cli", version: "2.31.4", backend: "npm:@redocly/cli"},
				published: day("2026-08-04"), ageDays: 2, window: registryWindowDays,
			}
			got := summary("audit", []finding{f}, nil, nil, nil, nil, nil, []tool{f.tool})
			assert.Contains(t, got, "## 参考: 窓内だがブロックしないもの (1)")
			assert.Contains(t, got, "npm:@redocly/cli@2.31.4")
		})

		t.Run("公開時刻を取得できなかったものを見出し付きで並べる", func(t *testing.T) {
			t.Parallel()
			got := summary("gate", nil, []tool{{key: "asdf:x", version: "1.0.0", backend: "asdf:x"}}, nil, nil, nil, nil, nil)
			assert.Contains(t, got, "## 公開時刻を取得できなかったもの (1)")
			assert.Contains(t, got, "asdf:x@1.0.0（asdf:x）")
		})

		// 値は mise.toml 由来で pull request が中身を決めるため、フェンスは値の側から長さを取る。
		t.Run("値に含まれるバッククォート連より長いフェンスで包む", func(t *testing.T) {
			t.Parallel()
			got := summary("audit", nil, nil, nil, []violation{{file: bypassFile, msg: "``` を含む理由"}}, nil, nil, nil)
			assert.Contains(t, got, "````text")
		})
	})
}

func Test_appendOutput(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("件数を key=value で追記する", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "out")
			require.NoError(t, appendOutput(path, 2, 33, 1))

			body, err := os.ReadFile(path) //nolint:gosec // path は t.TempDir 配下
			require.NoError(t, err)
			assert.Equal(t, "findings=2\naudited=33\nblocking=1\n", string(body))
		})

		t.Run("既存の内容を消さずに追記する", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "out")
			require.NoError(t, os.WriteFile(path, []byte("prior=1\n"), 0o600))
			require.NoError(t, appendOutput(path, 0, 1, 0))

			body, err := os.ReadFile(path) //nolint:gosec // path は t.TempDir 配下
			require.NoError(t, err)
			assert.Equal(t, "prior=1\nfindings=0\naudited=1\nblocking=0\n", string(body))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 書けなかったことを黙って飲むと、後続ステップは前回の値を見て判断し続ける。
		t.Run("存在しないディレクトリ配下へは追記できずエラーを返す", func(t *testing.T) {
			t.Parallel()
			require.Error(t, appendOutput(filepath.Join(t.TempDir(), "absent", "out"), 1, 1, 0))
		})
	})
}

func Test_tool_id(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("キーとバージョンを @ で連結する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "aqua:owner/repo@1.2.3", tool{key: "aqua:owner/repo", version: "1.2.3"}.id())
		})

		// バージョンが識別子から落ちると、diffAdded がバージョン更新を「変化なし」と見なして素通しする。
		t.Run("バージョンだけが違うツールは別の識別子になる", func(t *testing.T) {
			t.Parallel()
			old := tool{key: "aqua:owner/repo", version: "1.2.3"}
			assert.NotEqual(t, old.id(), tool{key: "aqua:owner/repo", version: "1.2.4"}.id())
		})
	})
}

func Test_added(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// base を読まずに現在の宣言をそのまま返すと、gate が毎回全ツールを窓違反として落とす。
		t.Run("base に無い宣言だけを返す", func(t *testing.T) {
			t.Parallel()

			out, err := exec.CommandContext(t.Context(), "git", "show", "HEAD:"+miseFile).Output()
			require.NoError(t, err)
			base, err := parseTools(out)
			require.NoError(t, err)
			require.NotEmpty(t, base)

			got, err := added("HEAD", []string{miseFile}, append(slices.Clone(base), tool{key: "aqua:fake/tool", version: "9.9.9"}))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "aqua:fake/tool@9.9.9", got[0].id())
		})

		t.Run("base に無い宣言ファイルはその時点で空だったものとして扱う", func(t *testing.T) {
			t.Parallel()

			got, err := added("HEAD", []string{"python/no-such-file-for-cooldown-test.in"},
				[]tool{{key: "pypi:example", version: "1.0.0"}})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "pypi:example@1.0.0", got[0].id())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base の ref を解決できなければエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := added("refs/heads/no-such-branch-for-cooldown-test", []string{miseFile}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base を取得できていない可能性があります")
		})
	})
}

//nolint:paralleltest // t.Chdir がプロセス共有の状態を差し替えるため並列化不可
func Test_baseDeclarations(t *testing.T) {
	//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("base 時点の中身をパスごと返す", func(t *testing.T) {
			got, err := baseDeclarations("HEAD", []string{miseFile})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, miseFile, got[0].path)
			assert.Contains(t, string(got[0].content), "[tools]")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("base に無い宣言ファイルは飛ばす", func(t *testing.T) {
			got, err := baseDeclarations("HEAD", []string{"python/no-such-file-for-cooldown-test.in"})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
	t.Run("異常系", func(t *testing.T) {
		// ファイル不在と ref の解決失敗を同じ扱いにすると、base を取り違えたまま gate が通る。
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("base の ref を解決できなければエラーにする", func(t *testing.T) {
			_, err := baseDeclarations("refs/heads/no-such-branch-for-cooldown-test", []string{miseFile})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base を取得できていない可能性があります")
		})

		// 新しい宣言ファイルと同じく空として扱うと、gate は全件を差分として検査し始める。
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("base に mise.toml が無ければエラーにする", func(t *testing.T) {
			useRepoWithoutMise(t)

			_, err := baseDeclarations("HEAD", []string{miseFile})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base を取得できていない可能性があります")
		})
	})
}

func Test_verifyRef(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("commit へ解決できる ref は通す", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, verifyRef(t.Context(), "HEAD"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("解決できない ref はエラーにする", func(t *testing.T) {
			t.Parallel()
			require.Error(t, verifyRef(t.Context(), "refs/heads/no-such-branch-for-cooldown-test"))
		})
	})
}

func Test_addedFrom(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base の宣言に無い組だけを返す", func(t *testing.T) {
			t.Parallel()
			got, err := addedFrom([]declaration{{path: miseFile, content: []byte(miseFixture)}}, []tool{
				{key: "sqlc", version: "1.31.1"},
				{key: "sqlc", version: "1.32.0"},
			})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "sqlc@1.32.0", got[0].id())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base を解析できなければエラーにする", func(t *testing.T) {
			t.Parallel()
			unreadable := "[tools]\n" + strings.Repeat("a", bufio.MaxScanTokenSize+1) + "\n"

			_, err := addedFrom([]declaration{{path: miseFile, content: []byte(unreadable)}},
				[]tool{{key: "sqlc", version: "1.31.1"}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base の宣言")
		})
	})
}

// stubMise は PATH の先頭へ偽の mise を置きます。
//
// 実物を呼ぶと、テストがこのマシンに mise が入っているかどうかで通ったり落ちたりするようになり、
// ツールについてのテストであることをやめます。境界（外部コマンドの起動）で差し替えることで、
// **ツールが組み立てた引数列そのもの**を検査対象にできます。
func stubMise(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	body := "#!/bin/sh\n" + script + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mise"), []byte(body), 0o700)) //nolint:gosec // テスト専用の実行可能スタブ
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

//nolint:paralleltest // PATH を t.Setenv で差し替えるため並列化できない
func Test_miseRegistry(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("候補が複数あっても先頭だけを backend にする", func(t *testing.T) {
			stubMise(t, `echo "aqua:sqlc-dev/sqlc ubi:sqlc-dev/sqlc"`)
			got, err := miseRegistry(t.Context(), "sqlc")
			require.NoError(t, err)
			assert.Equal(t, "aqua:sqlc-dev/sqlc", got)
			assert.NotContains(t, got, " ")
		})

		t.Run("言語ランタイムは core backend として解決される", func(t *testing.T) {
			stubMise(t, `echo "core:go"`)
			got, err := miseRegistry(t.Context(), "go")
			require.NoError(t, err)
			assert.Equal(t, "core:go", got)
		})

		t.Run("渡した名前がそのまま引数になる", func(t *testing.T) {
			// 引数の組み立てを取り違えると、別の道具の backend を黙って返す。
			stubMise(t, `printf "aqua:x/%s" "$2"`)
			got, err := miseRegistry(t.Context(), "actionlint")
			require.NoError(t, err)
			assert.Equal(t, "aqua:x/actionlint", got)
		})

		t.Run("前後の空白を落とす", func(t *testing.T) {
			stubMise(t, `printf "  core:node  \n"`)
			got, err := miseRegistry(t.Context(), "node")
			require.NoError(t, err)
			assert.Equal(t, "core:node", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("registry が知らない名前はエラーにする", func(t *testing.T) {
			stubMise(t, `exit 1`)
			_, err := miseRegistry(t.Context(), "no-such-tool")
			require.Error(t, err)
		})

		t.Run("空の出力はエラーにする", func(t *testing.T) {
			// 空を通すと、backend が決まらないまま「解決できた」ことになる。
			stubMise(t, `printf ""`)
			_, err := miseRegistry(t.Context(), "x")
			require.Error(t, err)
		})

		t.Run("mise が PATH に無ければエラーにする", func(t *testing.T) {
			// 握り潰すと、version manager が無い環境で検査が「違反なし」に化ける。
			t.Setenv("PATH", t.TempDir())
			_, err := miseRegistry(t.Context(), "go")
			require.Error(t, err)
		})
	})
}

func Test_firstBackend(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("候補が複数あっても先頭だけを採る", func(t *testing.T) {
			t.Parallel()
			got, err := firstBackend([]byte("aqua:sqlc/sqlc ubi:sqlc/sqlc\n"), "sqlc")
			require.NoError(t, err)
			assert.Equal(t, "aqua:sqlc/sqlc", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空の backend を返すと種別が空になり、そのツールは窓を持たないまま検査を通り抜ける。
		t.Run("候補が 1 つも無ければ取得経路なしとしてエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := firstBackend([]byte("  \n"), "sqlc")
			require.ErrorIs(t, err, errUnsupportedBackend)
		})
	})
}

//nolint:paralleltest // PATH を t.Setenv で差し替えるため並列化できない
func Test_resolveBackends(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("コロンを含むキーはそのまま backend にする", func(t *testing.T) {
			// registry を引かないので、mise が無くても解決できる。
			t.Setenv("PATH", t.TempDir())
			targets, skipped, err := resolveBackends(t.Context(), []tool{{key: "aqua:owner/repo", version: "1.2.3"}})
			require.NoError(t, err)
			require.Len(t, targets, 1)
			assert.Equal(t, "aqua:owner/repo", targets[0].backend)
			assert.Empty(t, skipped)
		})

		// 言語ランタイムの除外は明示的に受容したリスクなので、短縮名から core へ解決される経路ごと固定する。
		t.Run("短縮名のランタイムは registry 解決を経て除外される", func(t *testing.T) {
			stubMise(t, `echo "core:go"`)
			targets, skipped, err := resolveBackends(t.Context(), []tool{{key: "go", version: "1.26.5"}})
			require.NoError(t, err)
			assert.Empty(t, targets)
			require.Len(t, skipped, 1)
			assert.Equal(t, "core:go", skipped[0].backend)
		})

		t.Run("core backend を明示したキーも除外する", func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			targets, skipped, err := resolveBackends(t.Context(), []tool{{key: "core:node", version: "24.0.0"}})
			require.NoError(t, err)
			assert.Empty(t, targets)
			assert.Len(t, skipped, 1)
		})

		t.Run("短縮名が core 以外へ解決されれば検査対象に残る", func(t *testing.T) {
			// 除外の条件は「解決先が core であること」であって「短縮名であること」ではない。
			stubMise(t, `echo "aqua:rhysd/actionlint"`)
			targets, skipped, err := resolveBackends(t.Context(), []tool{{key: "actionlint", version: "1.7.12"}})
			require.NoError(t, err)
			require.Len(t, targets, 1)
			assert.Equal(t, "aqua:rhysd/actionlint", targets[0].backend)
			assert.Empty(t, skipped)
		})

		t.Run("検査対象と除外を同時に扱う", func(t *testing.T) {
			stubMise(t, `case "$2" in go) echo "core:go" ;; *) echo "aqua:x/$2" ;; esac`)
			targets, skipped, err := resolveBackends(t.Context(), []tool{
				{key: "go", version: "1.27.1"},
				{key: "actionlint", version: "1.7.12"},
			})
			require.NoError(t, err)
			require.Len(t, targets, 1)
			assert.Equal(t, "aqua:x/actionlint", targets[0].backend)
			require.Len(t, skipped, 1)
			assert.Equal(t, "core:go", skipped[0].backend)
		})

		t.Run("入力が0件なら何も返さない", func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			targets, skipped, err := resolveBackends(t.Context(), nil)
			require.NoError(t, err)
			assert.Empty(t, targets)
			assert.Empty(t, skipped)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		// 解決できないキーを黙って落とすと、そのツールだけ検査されないまま gate が通る。
		t.Run("backend を解決できないキーはエラーにする", func(t *testing.T) {
			stubMise(t, `exit 1`)
			targets, _, err := resolveBackends(t.Context(), []tool{{key: "no-such-tool", version: "1.0.0"}})
			require.Error(t, err)
			assert.Empty(t, targets)
		})

		t.Run("1件でも解決できなければ全体をエラーにする", func(t *testing.T) {
			// 解決できたものだけで続けると、検査範囲が黙って縮む。
			stubMise(t, `case "$2" in actionlint) echo "aqua:x/actionlint" ;; *) exit 1 ;; esac`)
			_, _, err := resolveBackends(t.Context(), []tool{
				{key: "actionlint", version: "1.7.12"},
				{key: "unknown", version: "1.0.0"},
			})
			require.Error(t, err)
		})
	})
}

func Test_inspect(t *testing.T) {
	t.Parallel()

	now := day("2026-08-06")

	released := func(t *testing.T, repo, version, publishedOn string) *http.Client {
		t.Helper()
		return fakeUpstream(t, respondJSON(
			"/repos/"+repo+"/releases/tags/v"+version, `{"published_at":"`+publishedOn+`T00:00:00Z"}`,
		))
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("公開時刻を取得できない backend は unresolved として返す", func(t *testing.T) {
			t.Parallel()
			findings, unresolved := inspect(t.Context(), offlineClient(),
				[]tool{{key: "asdf:x", version: "1.0.0", backend: "asdf:x"}}, now)
			assert.Empty(t, findings)
			require.Len(t, unresolved, 1)
			assert.Equal(t, "asdf:x@1.0.0", unresolved[0].id())
		})

		// 取得は並行で走るため、並べ直さないと同じ入力でも報告の順序が実行ごとに変わる。
		t.Run("unresolved は識別子順に並べる", func(t *testing.T) {
			t.Parallel()
			_, unresolved := inspect(t.Context(), offlineClient(), []tool{
				{key: "asdf:c", version: "1.0.0", backend: "asdf:c"},
				{key: "asdf:a", version: "1.0.0", backend: "asdf:a"},
				{key: "asdf:b", version: "1.0.0", backend: "asdf:b"},
			}, now)

			ids := make([]string, 0, len(unresolved))
			for _, u := range unresolved {
				ids = append(ids, u.id())
			}
			assert.Equal(t, []string{"asdf:a@1.0.0", "asdf:b@1.0.0", "asdf:c@1.0.0"}, ids)
		})

		t.Run("対象が無ければ何も返さない", func(t *testing.T) {
			t.Parallel()
			findings, unresolved := inspect(t.Context(), offlineClient(), nil, now)
			assert.Empty(t, findings)
			assert.Empty(t, unresolved)
		})

		// 窓の内側を見逃すと、このツールは何も検出しないまま常に緑になる。
		t.Run("窓に 1 日足りない版は経過日数と窓を添えて finding にする", func(t *testing.T) {
			t.Parallel()
			target := tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"}

			findings, unresolved := inspect(t.Context(),
				released(t, "owner/repo", "1.2.3", "2026-07-24"), []tool{target}, now)
			require.Len(t, findings, 1)
			assert.Equal(t, releaseWindowDays-1, findings[0].ageDays)
			assert.Equal(t, releaseWindowDays, findings[0].window)
			assert.Empty(t, unresolved)
		})

		// 境界を含める向きに間違えると、窓を満たした版まで恒久的にブロックされる。
		t.Run("窓ちょうど経過した版は finding にしない", func(t *testing.T) {
			t.Parallel()
			target := tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"}

			findings, unresolved := inspect(t.Context(),
				released(t, "owner/repo", "1.2.3", "2026-07-23"), []tool{target}, now)
			assert.Empty(t, findings)
			assert.Empty(t, unresolved)
		})

		// 並べ直す理由は "unresolved は識別子順に並べる" と同じ。
		t.Run("findings は経過日数の短い順に並べる", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/fresh/releases/tags/v1.0.0":
					_, _ = io.WriteString(w, `{"published_at":"2026-08-05T00:00:00Z"}`)
				case "/repos/owner/older/releases/tags/v1.0.0":
					_, _ = io.WriteString(w, `{"published_at":"2026-07-30T00:00:00Z"}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			findings, _ := inspect(t.Context(), client, []tool{
				{key: "aqua:owner/older", version: "1.0.0", backend: "aqua:owner/older"},
				{key: "aqua:owner/fresh", version: "1.0.0", backend: "aqua:owner/fresh"},
			}, now)
			require.Len(t, findings, 2)
			assert.Equal(t, 1, findings[0].ageDays)
			assert.Equal(t, 7, findings[1].ageDays)
		})
	})
}

//nolint:paralleltest // GITHUB_TOKEN を t.Setenv で差し替えるため並列化できない
func Test_getJSON(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("応答の JSON を復号して格納する", func(t *testing.T) {
			client := fakeUpstream(t, respondJSON("/pkg", `{"name":"ok"}`))

			var body map[string]any
			require.NoError(t, getJSON(t.Context(), client, npmBase+"pkg", &body))
			assert.Equal(t, "ok", body["name"])
		})

		// 理由は fetchBody のコメントと同じ。
		t.Run("GitHub API にはトークンを Authorization ヘッダで載せる", func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "test-token")
			auth := make(chan string, 1)
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				auth <- r.Header.Get("Authorization")
				_, _ = io.WriteString(w, "{}")
			})

			var body map[string]any
			require.NoError(t, getJSON(t.Context(), client, githubAPI+"owner/repo/releases/tags/v1.2.3", &body))
			assert.Equal(t, "Bearer test-token", <-auth)
		})

		// GitHub 以外の上流へトークンを送ると、資格情報を無関係な第三者へ渡すことになる。
		t.Run("GitHub API 以外の上流にはトークンを載せない", func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "test-token")
			auth := make(chan string, 1)
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				auth <- r.Header.Get("Authorization")
				_, _ = io.WriteString(w, "{}")
			})

			var body map[string]any
			require.NoError(t, getJSON(t.Context(), client, npmBase+"pkg", &body))
			assert.Empty(t, <-auth)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("404 は errNotFound にする", func(t *testing.T) {
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			var body map[string]any
			require.ErrorIs(t, getJSON(t.Context(), client, npmBase+"pkg", &body), errNotFound)
		})

		t.Run("410 は errNotFound にする", func(t *testing.T) {
			client := fakeUpstream(t, respondStatus(http.StatusGone))

			var body map[string]any
			require.ErrorIs(t, getJSON(t.Context(), client, npmBase+"pkg", &body), errNotFound)
		})

		// 存在しない版と上流の不調を同じ扱いにすると、後者を「解決済み」と読み違える。
		t.Run("想定外のステータスは errUpstreamStatus にする", func(t *testing.T) {
			client := fakeUpstream(t, respondStatus(http.StatusInternalServerError))

			var body map[string]any
			require.ErrorIs(t, getJSON(t.Context(), client, npmBase+"pkg", &body), errUpstreamStatus)
		})

		t.Run("URL として組み立てられない宛先はエラーにする", func(t *testing.T) {
			var body map[string]any
			err := getJSON(t.Context(), offlineClient(), npmBase+"pkg\x7f", &body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "new request")
		})

		// 到達できなかったことをエラーにしないと、上流の不通が全ツール「窓明け」に化ける。
		t.Run("上流へ到達できなければエラーにする", func(t *testing.T) {
			var body map[string]any
			err := getJSON(t.Context(), offlineClient(), npmBase+"pkg", &body)
			require.ErrorIs(t, err, errRoundTrip)
		})
	})
}

func Test_githubReleaseAt(t *testing.T) {
	t.Parallel()

	const published = `{"published_at":"2026-08-01T00:00:00Z"}`

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("v 付きタグの published_at を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/owner/repo/releases/tags/v1.2.3", published))

			at, err := githubReleaseAt(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "2026-08-01T00:00:00Z", at.Format(time.RFC3339))
		})

		// v を付けない流儀のリポジトリで諦めると、そのツールは恒久的に unresolved になる。
		t.Run("v 付きが無ければ v なしタグへ切り替える", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/owner/repo/releases/tags/1.2.3", published))

			at, err := githubReleaseAt(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "2026-08-01T00:00:00Z", at.Format(time.RFC3339))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("どちらのタグも無ければエラーを返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := githubReleaseAt(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errNotFound)
		})
	})
}

func Test_goModuleAt(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("パッケージパスから遡ってモジュールパスで解決する", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/go.uber.org/mock/@v/v0.6.0.info", `{"Time":"2026-07-20T00:00:00Z"}`))

			at, err := goModuleAt(t.Context(), client, "go.uber.org/mock/mockgen", "0.6.0")
			require.NoError(t, err)
			assert.Equal(t, "2026-07-20T00:00:00Z", at.Format(time.RFC3339))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("どの階層でも解決できなければエラーを返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := goModuleAt(t.Context(), client, "go.uber.org/mock/mockgen", "0.6.0")
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("遡れるモジュールパスが無ければ問い合わせずにエラーを返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			at, err := goModuleAt(t.Context(), client, "mockgen", "0.6.0")
			require.ErrorIs(t, err, errNotFound)
			assert.True(t, at.IsZero())
		})
	})
}

func Test_npmPackageAt(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("time から該当バージョンの公開時刻を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/@redocly/cli", `{"time":{"2.31.4":"2026-07-01T00:00:00Z"}}`))

			at, err := npmPackageAt(t.Context(), client, "@redocly/cli", "2.31.4")
			require.NoError(t, err)
			assert.Equal(t, "2026-07-01T00:00:00Z", at.Format(time.RFC3339))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 理由は publishedAt のコメントと同じ。
		t.Run("time に載っていないバージョンはエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/@redocly/cli", `{"time":{"2.31.3":"2026-07-01T00:00:00Z"}}`))

			_, err := npmPackageAt(t.Context(), client, "@redocly/cli", "2.31.4")
			require.ErrorIs(t, err, errNotFound)
		})
	})
}

func Test_publishedAt(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("GitHub リリース系はリリース API を引く", func(t *testing.T) {
			t.Parallel()
			got := requestedPath(t, tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"})
			assert.Equal(t, "/repos/owner/repo/releases/tags/v1.2.3", got)
		})

		// Release を出さない上流では、ここで諦めるとそのツールが恒久的に unresolved になる。
		t.Run("GitHub リリースが無い aqua は配布物の Last-Modified へ退く", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case aquaRegistryPath(t, "owner/repo"):
					_, _ = io.WriteString(w, aquaHTTPRegistry)
				case "/tool-linux-x86_64-1.2.3.zip":
					w.Header().Set("Last-Modified", "Fri, 04 Sep 2026 19:13:50 GMT")
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			at, err := publishedAt(t.Context(), client,
				tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"})
			require.NoError(t, err)
			assert.Equal(t, "2026-09-04T19:13:50Z", at.Format(time.RFC3339))
		})

		t.Run("go は module proxy を引く", func(t *testing.T) {
			t.Parallel()
			got := requestedPath(t, tool{key: "go:go.uber.org/mock/mockgen", version: "0.6.0", backend: "go:go.uber.org/mock/mockgen"})
			assert.Equal(t, "/go.uber.org/mock/mockgen/@v/v0.6.0.info", got)
		})

		t.Run("npm は npm レジストリを引く", func(t *testing.T) {
			t.Parallel()
			got := requestedPath(t, tool{key: "npm:@redocly/cli", version: "2.31.4", backend: "npm:@redocly/cli"})
			assert.Equal(t, "/@redocly/cli", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// レート制限や 500 を「リリース無し」と取り違えて配布物へ退くと、窓の基準時刻が
		// 静かにすり替わる。退く条件が errNotFound であることを、誤りの側からも固定する。
		t.Run("aqua でも 404 以外のエラーでは配布物へ退かない", func(t *testing.T) {
			t.Parallel()

			var registryHit atomic.Bool
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "aqua-registry") {
					registryHit.Store(true)
				}
				w.WriteHeader(http.StatusInternalServerError)
			})

			_, err := publishedAt(t.Context(), client,
				tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo"})
			require.ErrorIs(t, err, errUpstreamStatus)
			assert.False(t, registryHit.Load(), "レジストリへ退いている")
		})

		// aqua 以外の GitHub 系 backend は配布物の所在を辿る手立てを持たない。退かずに落とす。
		t.Run("aqua 以外はリリースが無い時点でエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := publishedAt(t.Context(), client,
				tool{key: "ubi:owner/repo", version: "1.2.3", backend: "ubi:owner/repo"})
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("取得経路を持たない backend はエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := publishedAt(t.Context(), client, tool{key: "asdf:x", version: "1.0.0", backend: "asdf:x"})
			require.ErrorIs(t, err, errUnsupportedBackend)
		})
	})
}

// aquaHTTPRegistry は、配布物を GitHub 以外から取る aqua パッケージの定義。aws-cli がこの形。
const aquaHTTPRegistry = `packages:
  - type: http
    repo_owner: owner
    repo_name: repo
    version_source: github_tag
    url: https://cdn.example.com/tool-{{.OS}}-{{.Arch}}-{{.Version}}.zip
    replacements:
      amd64: x86_64
`

// aquaRegistryPath は、その repo の定義が置かれる取得元のパスを返します。**固定値を書き写さない**
// —— 取得元を別の ref へ動かしたとき、写しは黙って一致しなくなる。
func aquaRegistryPath(t *testing.T, repo string) string {
	t.Helper()
	u, err := url.Parse(aquaRegistryBase + repo + "/registry.yaml")
	require.NoError(t, err)

	return u.Path
}

// respondLastModified は、path に一致したときだけ Last-Modified 付きの 200 を返します。
func respondLastModified(path, lastModified string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if lastModified != "" {
			w.Header().Set("Last-Modified", lastModified)
		}
		w.WriteHeader(http.StatusOK)
	}
}

// respondText は、path に一致したときだけ body を返します。
func respondText(path, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	}
}

func Test_renderAquaURL(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 理由は renderAquaURL の doc コメントが持つ。
		t.Run("replacements を適用してから展開する", func(t *testing.T) {
			t.Parallel()

			got, err := renderAquaURL(
				"https://cdn.example.com/tool-{{.OS}}-{{.Arch}}-{{.Version}}.zip",
				map[string]string{"amd64": "x86_64"},
				"1.2.3",
			)
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/tool-linux-x86_64-1.2.3.zip", got)
		})

		// 理由は renderAquaURL の trimV についてのコメントが持つ。
		t.Run("aqua の trimV を展開できる", func(t *testing.T) {
			t.Parallel()

			got, err := renderAquaURL("https://cdn.example.com/t_{{trimV .Version}}.zip", nil, "v1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/t_1.2.3.zip", got)
		})

		t.Run("replacements に無い名前はそのまま使う", func(t *testing.T) {
			t.Parallel()

			got, err := renderAquaURL("https://cdn.example.com/{{.OS}}/{{.Arch}}/{{.Version}}", nil, "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/linux/amd64/1.2.3", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 当てずっぽうの URL を叩いて得た時刻は、窓の判定を黙って狂わせる。
		t.Run("渡していないフィールドを参照するテンプレートはエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := renderAquaURL("https://cdn.example.com/{{.Format}}/{{.Version}}", nil, "1.2.3")
			require.ErrorIs(t, err, errUnsupportedPackage)
		})

		// 対応範囲は renderAquaURL の trimV についてのコメントが持つ。素通しではなく失敗へ
		// 倒れることを、ここで固定する。
		t.Run("未対応のテンプレート関数を使う定義はエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := renderAquaURL(`https://cdn.example.com/{{trimPrefix "go" .Version}}.zip`, nil, "1.2.3")
			require.ErrorIs(t, err, errUnsupportedPackage)
		})

		t.Run("壊れたテンプレートはエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := renderAquaURL("https://cdn.example.com/{{.Version", nil, "1.2.3")
			require.Error(t, err)
		})

		// 理由は renderAquaURL の doc コメントが持つ。
		t.Run("版を指さない URL は errUnversionedArtifact を返す", func(t *testing.T) {
			t.Parallel()

			// path 以外へ版を置いた形は、サーバがそれを無視すれば固定 URL と同じものを指す。
			for name, tmpl := range map[string]string{
				"版がどこにも無い":   "https://cdn.example.com/installer.sh",
				"版がクエリにだけ在る": "https://cdn.example.com/latest/installer.sh?v={{.Version}}",
				"版が素片にだけ在る":  "https://cdn.example.com/latest/installer.sh#{{.Version}}",
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					_, err := renderAquaURL(tmpl, nil, "1.2.3")
					require.ErrorIs(t, err, errUnversionedArtifact)
				})
			}
		})

		// 空の版に対しては strings.Contains が常に真を返し、検査が無条件通過に化ける。
		t.Run("版が空なら errUnversionedArtifact を返す", func(t *testing.T) {
			t.Parallel()

			_, err := renderAquaURL("https://cdn.example.com/t.zip", nil, "")
			require.ErrorIs(t, err, errUnversionedArtifact)
		})

		// 理由は hostIsLiteral の doc コメントが持つ。
		t.Run("ホストが補間で作られる URL はエラーにする", func(t *testing.T) {
			t.Parallel()

			for name, tmpl := range map[string]string{
				"ホストごと補間":  "https://{{.OS}}.example.com/{{.Version}}.zip",
				"スキームから補間": "{{.OS}}://cdn.example.com/{{.Version}}.zip",
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					_, err := renderAquaURL(tmpl, nil, "1.2.3")
					require.ErrorIs(t, err, errUnsupportedPackage)
				})
			}
		})

		// hostIsLiteral は展開を含む非 https を先に弾くため、ここを通すには展開を含まない形が要る。
		// 展開入りの非 https で書くと、名乗りと違う分岐を踏んだまま緑になる。
		t.Run("展開後の URL が叩けない形ならエラーにする", func(t *testing.T) {
			t.Parallel()

			for name, tmpl := range map[string]string{
				"https でない":    "http://cdn.example.com/1.2.3.zip",
				"ホストが空":        "https:///{{.Version}}.zip",
				"userinfo を含む": "https://user:pass@cdn.example.com/{{.Version}}.zip",
				"ホストが偽装されている":  "https://trusted.example.com@evil.example.com/{{.Version}}.zip",
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					_, err := renderAquaURL(tmpl, nil, "1.2.3")
					require.ErrorIs(t, err, errUnsupportedPackage)
				})
			}
		})
	})
}

func Test_aquaRegistryBase(t *testing.T) {
	t.Parallel()

	// 取得元のパスはテストが定数から導くため、この定数の構造が壊れても経路の一致は保たれる。
	// ref は pin の更新で動くので、ref に依らない部分だけを独立に固定する。
	t.Run("aqua の公式レジストリの pkgs を指す", func(t *testing.T) {
		t.Parallel()

		assert.True(t, strings.HasPrefix(aquaRegistryBase,
			"https://raw.githubusercontent.com/aquaproj/aqua-registry/"), aquaRegistryBase)
		assert.True(t, strings.HasSuffix(aquaRegistryBase, "/pkgs/"), aquaRegistryBase)
		assert.Contains(t, aquaRegistryBase, aquaRegistryRef)
	})
}

func Test_hostIsLiteral(t *testing.T) {
	t.Parallel()

	// 理由は hostIsLiteral の doc コメントが持つ。renderAquaURL 経由では踏めない枝があるため、
	// ここで直接固定する。
	for name, tc := range map[string]struct {
		tmpl string
		want bool
	}{
		"展開を1つも含まない":     {"https://cdn.example.com/t-1.2.3.zip", true},
		"展開が path にだけ在る": {"https://cdn.example.com/{{.Version}}.zip", true},
		"ホストが補間で作られる":    {"https://{{.OS}}.example.com/{{.Version}}.zip", false},
		"スキームが補間で作られる":   {"{{.OS}}://cdn.example.com/{{.Version}}.zip", false},
		"https で始まらない":   {"http://cdn.example.com/{{.Version}}.zip", false},
		"ホストの終わりを決められない": {"https://cdn.example.com{{.Version}}", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, hostIsLiteral(tc.tmpl))
		})
	}
}

func Test_lastModified(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("Last-Modified を UTC の時刻として返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondLastModified("/a.zip", "Fri, 04 Sep 2026 19:13:50 GMT"))

			at, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.NoError(t, err)
			assert.Equal(t, "2026-09-04T19:13:50Z", at.Format(time.RFC3339))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("配布物が無ければ errNotFound を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("想定外のステータスは errUpstreamStatus を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusInternalServerError))

			_, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.ErrorIs(t, err, errUpstreamStatus)
		})

		// 理由は publishedAt のコメントと同じ。
		t.Run("Last-Modified が無ければ errNoLastModified を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondLastModified("/a.zip", ""))

			_, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.ErrorIs(t, err, errNoLastModified)
		})

		// 追えば、検証を通した宛先とは別のホストが公開時刻を名乗れる。CheckRedirect を外しても
		// 落ちないままだと、この1行が守っているものを誰も見ていないことになる。
		t.Run("リダイレクトを追わず、その先も叩かない", func(t *testing.T) {
			t.Parallel()

			var followed atomic.Bool
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere.zip" {
					followed.Store(true)
					w.Header().Set("Last-Modified", "Mon, 01 Jan 2001 00:00:00 GMT")
					w.WriteHeader(http.StatusOK)

					return
				}
				w.Header().Set("Location", "https://evil.example.com/elsewhere.zip")
				w.WriteHeader(http.StatusFound)
			})

			_, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.ErrorIs(t, err, errUpstreamStatus)
			assert.False(t, followed.Load(), "リダイレクト先を叩いている")
		})

		t.Run("解釈できない Last-Modified はエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondLastModified("/a.zip", "yesterday"))

			_, err := lastModified(t.Context(), client, "https://cdn.example.com/a.zip")
			require.Error(t, err)
		})
	})
}

func Test_aquaArtifactURL(t *testing.T) {
	t.Parallel()

	path := aquaRegistryPath(t, "owner/repo")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 理由は aquaArtifactURL の Overrides フィールドのコメントが持つ。
		t.Run("probe の goos に一致する override を base より優先する", func(t *testing.T) {
			t.Parallel()
			body := aquaHTTPRegistry + "    overrides:\n" +
				"      - goos: " + probeOS + "\n" +
				"        url: https://cdn.example.com/only-{{.OS}}-{{.Version}}.tar.gz\n" +
				"      - goos: darwin\n" +
				"        url: https://cdn.example.com/mac-{{.Version}}.pkg\n"
			client := fakeUpstream(t, respondText(path, body))

			got, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/only-linux-1.2.3.tar.gz", got)
		})

		// 実在のレジストリに goos + goarch で分岐する override が在る（aws/session-manager-plugin
		// など）。goarch を見ないと、別の環境向けの url を掴む。
		t.Run("goos が一致しても goarch が違う override は採らない", func(t *testing.T) {
			t.Parallel()
			body := aquaHTTPRegistry + "    overrides:\n" +
				"      - goos: " + probeOS + "\n" +
				"        goarch: mips\n" +
				"        url: https://cdn.example.com/mips-{{.Version}}.zip\n"
			client := fakeUpstream(t, respondText(path, body))

			got, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/tool-linux-x86_64-1.2.3.zip", got)
		})

		t.Run("goarch を書かない override は その goos のすべてに掛かる", func(t *testing.T) {
			t.Parallel()
			body := aquaHTTPRegistry + "    overrides:\n" +
				"      - goos: " + probeOS + "\n" +
				"        url: https://cdn.example.com/any-{{.Version}}.zip\n"
			client := fakeUpstream(t, respondText(path, body))

			got, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/any-1.2.3.zip", got)
		})

		t.Run("probe の goos に一致する override が無ければ base を使う", func(t *testing.T) {
			t.Parallel()
			body := aquaHTTPRegistry + "    overrides:\n" +
				"      - goos: darwin\n" +
				"        url: https://cdn.example.com/mac-{{.Version}}.pkg\n"
			client := fakeUpstream(t, respondText(path, body))

			got, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/tool-linux-x86_64-1.2.3.zip", got)
		})

		t.Run("type http の定義から配布物の URL を組む", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondText(path, aquaHTTPRegistry))

			got, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/tool-linux-x86_64-1.2.3.zip", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("定義を取れなければエラーを返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("解釈できない定義はエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondText(path, "\tpackages: [[["))

			_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.Error(t, err)
		})

		// 決められない理由は aquaArtifactURL の当該分岐が持つ。
		t.Run("packages が 1 件でなければ errUnsupportedPackage を返す", func(t *testing.T) {
			t.Parallel()

			for name, body := range map[string]string{
				"0 件": "packages: []\n",
				"2 件": aquaHTTPRegistry + "  - type: http\n    url: https://cdn.example.com/other\n",
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					client := fakeUpstream(t, respondText(path, body))

					_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
					require.ErrorIs(t, err, errUnsupportedPackage)
				})
			}
		})

		// GitHub Release から取る型は、そもそもこの経路へ来ない。来たなら定義の読み違いである。
		t.Run("type が http でなければ errUnsupportedPackage を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondText(path, "packages:\n  - type: github_release\n    url: https://x/y\n"))

			_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errUnsupportedPackage)
		})

		// 理由は aquaArtifactURL の override ループのコメントが持つ。
		t.Run("該当 goos の override の url が空なら base へ退かない", func(t *testing.T) {
			t.Parallel()
			body := aquaHTTPRegistry + "    overrides:\n" +
				"      - goos: " + probeOS + "\n" +
				"        url: \"\"\n"
			client := fakeUpstream(t, respondText(path, body))

			_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errUnsupportedPackage)
			// 同じセンチネルを後段の URL 検査も返すため、早期のガードを消しても緑のままになる。
			// どちらが落としたかをメッセージで区別する（report が利用者へそのまま出す）。
			assert.Contains(t, err.Error(), "この環境向けの url が無い")
		})

		t.Run("base にも override にも url が無ければ errUnsupportedPackage を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondText(path, "packages:\n  - type: http\n"))

			_, err := aquaArtifactURL(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errUnsupportedPackage)
		})
	})
}

func Test_aquaArtifactAt(t *testing.T) {
	t.Parallel()

	registryPath := aquaRegistryPath(t, "owner/repo")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("定義を引いてから配布物の Last-Modified を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case registryPath:
					_, _ = io.WriteString(w, aquaHTTPRegistry)
				case "/tool-linux-x86_64-1.2.3.zip":
					w.Header().Set("Last-Modified", "Fri, 04 Sep 2026 19:13:50 GMT")
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			at, err := aquaArtifactAt(t.Context(), client, "owner/repo", "1.2.3")
			require.NoError(t, err)
			assert.Equal(t, "2026-09-04T19:13:50Z", at.Format(time.RFC3339))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 定義は読めたが、その版の配布物がまだ（あるいはもう）無い場合。
		t.Run("配布物が無ければ errNotFound を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondText(registryPath, aquaHTTPRegistry))

			_, err := aquaArtifactAt(t.Context(), client, "owner/repo", "1.2.3")
			require.ErrorIs(t, err, errNotFound)
		})
	})
}

func Test_hasBypass(t *testing.T) {
	t.Parallel()

	target := tool{key: "aqua:owner/repo", version: "1.2.3"}
	bypasses := map[string]bypass{"aqua:owner/repo@1.2.3": {expires: day("2026-09-06"), issue: 1, reason: "r"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録されたバイパスは有効と判定する", func(t *testing.T) {
			t.Parallel()
			assert.True(t, hasBypass(bypasses, map[string]struct{}{}, target))
		})

		t.Run("登録が無ければ無効と判定する", func(t *testing.T) {
			t.Parallel()
			assert.False(t, hasBypass(map[string]bypass{}, map[string]struct{}{}, target))
		})

		// 理由は Test_classify の同種ケースと同じ。
		t.Run("無効化されたキーは登録があっても無効と判定する", func(t *testing.T) {
			t.Parallel()
			assert.False(t, hasBypass(bypasses, map[string]struct{}{"aqua:owner/repo@1.2.3": {}}, target))
		})
	})
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// map の反復順のままだと、同じ入力でも違反の報告順が実行ごとに変わる。
		t.Run("キーを辞書順に並べる", func(t *testing.T) {
			t.Parallel()
			got := sortedKeys(map[string]bypass{"c@1": {line: 3}, "a@1": {line: 1}, "b@1": {line: 2}})
			assert.Equal(t, []string{"a@1", "b@1", "c@1"}, got)
		})

		t.Run("空のマップでは空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, sortedKeys(map[string]bypass{}))
		})
	})
}

//nolint:paralleltest // log の出力先を差し替えて検証するため並列化できない
func Test_report(t *testing.T) {
	blocked := finding{
		tool:    tool{key: "aqua:owner/repo", version: "1.2.3", backend: "aqua:owner/repo", file: miseFile},
		ageDays: 1, window: releaseWindowDays,
	}
	unfetched := []tool{{key: "asdf:x", version: "1.0.0", backend: "asdf:x", file: miseFile}}

	t.Run("正常系", func(t *testing.T) {
		t.Run("gate は窓内のツールをブロック件数に数える", func(t *testing.T) {
			var count int
			out := captureLog(t, func() {
				count = report("gate", []finding{blocked}, nil, nil, nil, nil, nil, nil, false)
			})
			assert.Equal(t, 1, count)
			assert.Contains(t, out, "cooldown 14 日を満たしていません")
		})

		t.Run("audit は窓内のツールをブロック件数に数えない", func(t *testing.T) {
			var count int
			out := captureLog(t, func() {
				count = report("audit", []finding{blocked}, nil, nil, nil, nil, nil, nil, false)
			})
			assert.Equal(t, 0, count)
			assert.Contains(t, out, "⚠️")
		})

		t.Run("バイパス設定の違反は audit でもブロックする", func(t *testing.T) {
			var count int
			out := captureLog(t, func() {
				count = report("audit", nil, nil, nil, []violation{{file: bypassFile, msg: "期限が切れています"}}, nil, nil, nil, false)
			})
			assert.Equal(t, 1, count)
			assert.Contains(t, out, "期限が切れています")
		})

		t.Run("gate は公開時刻を取得できなかったツールもブロックする", func(t *testing.T) {
			var count int
			captureLog(t, func() {
				count = report("gate", nil, unfetched, nil, nil, nil, nil, nil, false)
			})
			assert.Equal(t, 1, count)
		})

		t.Run("audit は公開時刻を取得できなかったツールを警告に留める", func(t *testing.T) {
			var count int
			out := captureLog(t, func() {
				count = report("audit", nil, unfetched, nil, nil, nil, nil, nil, false)
			})
			assert.Equal(t, 0, count)
			assert.Contains(t, out, "取得できませんでした")
		})

		t.Run("有効なバイパスはブロックせず期限と理由を示す", func(t *testing.T) {
			var count int
			out := captureLog(t, func() {
				count = report("gate", []finding{blocked}, nil, nil, nil, map[string]bypass{
					"aqua:owner/repo@1.2.3": {expires: day("2026-09-06"), issue: 931, reason: "CRITICAL への即応"},
				}, nil, nil, false)
			})
			assert.Equal(t, 0, count)
			assert.Contains(t, out, "#931 のバイパスで通します")
		})

		// アノテーションが消えると、違反は差分ビューに出ずログの奥に埋もれる。
		t.Run("github 指定時は違反をアノテーションとして出す", func(t *testing.T) {
			out := captureLog(t, func() {
				_ = report("gate", []finding{blocked}, nil, nil, nil, nil, nil, nil, true)
			})
			assert.Contains(t, out, "::error file=mise.toml::")
		})

		// バイパス設定の違反は lockfile 側の問題なので、注釈も lockfile へ向ける。
		t.Run("github 指定時はバイパス設定の違反を lockfile へ注釈する", func(t *testing.T) {
			out := captureLog(t, func() {
				_ = report("audit", nil, nil, nil, []violation{{file: bypassFile, msg: "期限が切れています"}}, nil, nil, nil, true)
			})
			assert.Contains(t, out, "::error file="+bypassFile+"::期限が切れています")
		})

		t.Run("github 指定時は取得できなかったツールもアノテーションとして出す", func(t *testing.T) {
			out := captureLog(t, func() {
				_ = report("gate", nil, unfetched, nil, nil, nil, nil, nil, true)
			})
			assert.Contains(t, out, "::error file="+miseFile+"::asdf:x@1.0.0")
		})
	})
}

func Test_parseArgs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("フラグを省略すれば既定値のまま返す", func(t *testing.T) {
			t.Parallel()

			sub, opt, err := parseArgs([]string{"audit"})

			require.NoError(t, err)
			assert.Equal(t, "audit", sub)
			assert.Equal(t, options{}, opt)
		})

		t.Run("与えられたフラグを options へ載せる", func(t *testing.T) {
			t.Parallel()

			sub, opt, err := parseArgs([]string{"gate", "--base=origin/main", "--summary-out=s.md", "--github"})

			require.NoError(t, err)
			assert.Equal(t, "gate", sub)
			assert.Equal(t, options{base: "origin/main", summaryOut: "s.md", github: true}, opt)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("サブコマンドが無ければ使い方を示す", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs(nil)

			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, err.Error(), "usage: tool-cooldown <gate|audit|outdated>")
		})

		t.Run("未知のサブコマンドはエラーにする", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs([]string{"inspect"})

			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, err.Error(), "未知のサブコマンド")
		})

		// base 無しの gate を通すと、比較対象が無いまま全ツールを窓違反として落とす。
		t.Run("gate に base が無ければエラーにする", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs([]string{"gate"})

			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, err.Error(), "--base")
		})

		t.Run("ヘルプ要求は flag.ErrHelp のまま返す", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs([]string{"audit", "-h"})

			require.ErrorIs(t, err, flag.ErrHelp)
		})

		t.Run("解釈できないフラグはエラーにする", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs([]string{"audit", "--no-such-flag"})

			require.Error(t, err)
			require.NotErrorIs(t, err, flag.ErrHelp)
			assert.Contains(t, err.Error(), "フラグを解釈できません")
		})
	})
}

//nolint:paralleltest // t.Chdir・captureLog・t.Setenv がプロセス共有の状態を差し替えるため並列化不可
func Test_run(t *testing.T) {
	now := day("2026-08-06")
	const (
		fresh = "2026-08-05"
		aged  = "2020-01-01"
	)

	// 実行環境の GITHUB_OUTPUT を持ち込まないよう空にする。設定済みのランナー上で走らせると
	// 検査とは無関係なファイルへ追記してしまう。
	t.Setenv("GITHUB_OUTPUT", "")

	//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("audit は窓内のツールを報告しつつ正常終了する", func(t *testing.T) {
			useMiseWorkTree(t, miseOneAquaTool)

			out, err := runCaptured(t, []string{"audit"}, releasedOn(t, fresh), now)

			require.NoError(t, err)
			assert.Contains(t, out, "aqua:owner/repo@1.2.3（aqua:owner/repo）は公開 1 日で cooldown 14 日")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("gate は base に無いツールでも窓を越えていれば通す", func(t *testing.T) {
			useMiseGateRepo(t, "[tools]\n", miseOneAquaTool)

			out, err := runCaptured(t, []string{"gate", "--base=HEAD"}, releasedOn(t, aged), now)

			require.NoError(t, err)
			assert.Contains(t, out, "違反なし")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("summary-out 指定でサマリを書き出す", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)
			path := filepath.Join(dir, "summary.md")

			_, err := runCaptured(t, []string{"audit", "--summary-out=" + path}, releasedOn(t, fresh), now)

			require.NoError(t, err)
			body, readErr := os.ReadFile(path) //nolint:gosec // テスト内の一時ファイル
			require.NoError(t, readErr)
			assert.Contains(t, string(body), "aqua:owner/repo@1.2.3")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("GITHUB_OUTPUT があれば件数を追記する", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)
			path := filepath.Join(dir, "github-output")
			t.Setenv("GITHUB_OUTPUT", path)

			_, err := runCaptured(t, []string{"audit"}, releasedOn(t, fresh), now)

			require.NoError(t, err)
			body, readErr := os.ReadFile(path) //nolint:gosec // テスト内の一時ファイル
			require.NoError(t, readErr)
			assert.Contains(t, string(body), "findings=1")
			assert.Contains(t, string(body), "audited=1")
			assert.Contains(t, string(body), "blocking=0")
		})

		// ヘルプ要求を解析失敗と同じ扱いにすると、`-h` が異常終了に化ける。
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("ヘルプ要求は失敗にしない", func(t *testing.T) {
			_, err := runCaptured(t, []string{"audit", "-h"}, offlineClient(), now)

			require.NoError(t, err)
		})
	})

	//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("gate は base に無い窓内のツールを失敗にする", func(t *testing.T) {
			useMiseGateRepo(t, "[tools]\n", miseOneAquaTool)

			out, err := runCaptured(t, []string{"gate", "--base=HEAD"}, releasedOn(t, fresh), now)

			require.ErrorIs(t, err, errBlocking)
			assert.Contains(t, out, "窓明けを待つか")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("github 指定のときだけアノテーションを出す", func(t *testing.T) {
			useMiseGateRepo(t, "[tools]\n", miseOneAquaTool)

			out, err := runCaptured(t, []string{"gate", "--base=HEAD", "--github"}, releasedOn(t, fresh), now)

			require.ErrorIs(t, err, errBlocking)
			assert.Contains(t, out, "::error file="+miseFile+"::aqua:owner/repo@1.2.3")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("期限切れのバイパスは audit でも失敗にする", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)
			writeWorkTreeBypass(t, dir,
				`"aqua:owner/repo@1.2.3" = { expires = 2000-01-01, issue = 1, reason = "済んだ緊急対応" }`+"\n")

			_, err := runCaptured(t, []string{"audit"}, releasedOn(t, aged), now)

			require.ErrorIs(t, err, errBlocking)
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("使い方の誤りはそのまま失敗にする", func(t *testing.T) {
			_, err := runCaptured(t, nil, offlineClient(), now)

			require.ErrorIs(t, err, errUsage)
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("mise.toml が無ければ失敗にする", func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := runCaptured(t, []string{"audit"}, offlineClient(), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "宣言ファイルの読み取り: "+miseFile)
		})

		// 読み切れなかった宣言を空として扱うと、そのツールは検査されないまま通る。
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("mise.toml を読み切れなければ失敗にする", func(t *testing.T) {
			useMiseWorkTree(t, "[tools]\n"+strings.Repeat("a", bufio.MaxScanTokenSize+1)+"\n")

			_, err := runCaptured(t, []string{"audit"}, offlineClient(), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "token too long")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("バイパス lockfile を解釈できなければ失敗にする", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)
			writeWorkTreeBypass(t, dir, "これは行として解釈できない\n")

			_, err := runCaptured(t, []string{"audit"}, offlineClient(), now)

			require.ErrorIs(t, err, errBypassInvalidLine)
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("gate の base を解決できなければ失敗にする", func(t *testing.T) {
			useMiseWorkTree(t, miseOneAquaTool)

			_, err := runCaptured(t, []string{"gate", "--base=HEAD"}, offlineClient(), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "base との差分")
		})

		// 理由は Test_resolveBackends と同じ。
		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("backend を解決できないキーがあれば失敗にする", func(t *testing.T) {
			useMiseWorkTree(t, "[tools]\nno-such-tool-for-cooldown-test = \"1.0.0\"\n")

			_, err := runCaptured(t, []string{"audit"}, offlineClient(), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "backend の解決")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("サマリを書き出せなければ失敗にする", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)

			_, err := runCaptured(t, []string{"audit", "--summary-out=" + dir}, releasedOn(t, aged), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "write summary")
		})

		//nolint:paralleltest // 親がプロセス共有の状態を差し替えるため並列化不可
		t.Run("GITHUB_OUTPUT へ追記できなければ失敗にする", func(t *testing.T) {
			dir := useMiseWorkTree(t, miseOneAquaTool)
			t.Setenv("GITHUB_OUTPUT", dir)

			_, err := runCaptured(t, []string{"audit"}, releasedOn(t, aged), now)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "write GITHUB_OUTPUT")
		})
	})
}

//nolint:paralleltest // GITHUB_TOKEN を t.Setenv で差し替えるため並列化できない
func Test_fetchBody(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("200 なら本文を読める形で返す", func(t *testing.T) {
			client := fakeUpstream(t, respondJSON("/pkg", `{"name":"ok"}`))

			body, err := fetchBody(t.Context(), client, npmBase+"pkg")

			require.NoError(t, err)
			defer func() { _ = body.Close() }()
			out, readErr := io.ReadAll(body)
			require.NoError(t, readErr)
			assert.JSONEq(t, `{"name":"ok"}`, string(out))
		})

		// 理由は Test_getJSON と同じ。
		t.Run("GitHub API にだけトークンを載せる", func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "test-token")
			auth := make(chan string, 2)
			client := fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				auth <- r.Header.Get("Authorization")
				_, _ = io.WriteString(w, "{}")
			})

			for _, url := range []string{githubAPI + "owner/repo", npmBase + "pkg"} {
				body, err := fetchBody(t.Context(), client, url)
				require.NoError(t, err)
				_ = body.Close()
			}

			assert.Equal(t, "Bearer test-token", <-auth)
			assert.Empty(t, <-auth)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("404 と 410 は未検出として返す", func(t *testing.T) {
			for _, status := range []int{http.StatusNotFound, http.StatusGone} {
				client := fakeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
				})

				_, err := fetchBody(t.Context(), client, npmBase+"pkg")

				require.ErrorIs(t, err, errNotFound)
			}
		})

		t.Run("その他の異常なステータスは状態エラーとして返す", func(t *testing.T) {
			client := fakeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})

			_, err := fetchBody(t.Context(), client, npmBase+"pkg")

			require.ErrorIs(t, err, errUpstreamStatus)
		})

		t.Run("要求を組み立てられなければエラーを返す", func(t *testing.T) {
			_, err := fetchBody(t.Context(), offlineClient(), npmBase+"pkg\x7f")

			require.Error(t, err)
		})

		t.Run("上流へ届かなければエラーを返す", func(t *testing.T) {
			_, err := fetchBody(t.Context(), offlineClient(), npmBase+"pkg")

			require.Error(t, err)
		})
	})
}

func Test_getText(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("本文をそのまま文字列で返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "v1.1.0\nv1.2.0\n")
			})

			body, err := getText(t.Context(), client, goProxyBase+"example.com/mod/@v/list")

			require.NoError(t, err)
			assert.Equal(t, "v1.1.0\nv1.2.0\n", body, "JSON ではない応答をそのまま扱えること")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("上流が応答しなければエラーを返す", func(t *testing.T) {
			t.Parallel()

			_, err := getText(t.Context(), offlineClient(), goProxyBase+"example.com/mod/@v/list")

			require.Error(t, err)
		})
	})
}
