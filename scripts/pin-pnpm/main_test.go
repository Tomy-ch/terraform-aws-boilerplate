package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

const (
	shaA = "0a3f8f12f4d894ba5013c7d445136eec5260dcffb63477247ca9b1c23d4fe27f"
	shaB = "57f5f13647270dd55f4c463b5ad8e6e9157fc0463bb1ac62216da7c18531cb5b"
	shaC = "1111111111111111111111111111111111111111111111111111111111111111"
	shaD = "2222222222222222222222222222222222222222222222222222222222222222"
)

const soundMise = `[tools]
node = "26.13.1"
"aqua:pnpm/pnpm" = "12.17.0"
`

// editorconfig-checker-disable -- raw string の fixture は埋め込んだシェルの字下げを持つ。撤回: fixture を外部ファイルへ移したとき

// soundDockerfile は写しを持つ Dockerfile。**レシピの継続行の字下げと行末の `; \` を含める** ——
// 置換で前置きや後置きが落ちる欠陥は、そこにしか現れない。
const soundDockerfile = `# pnpm_version="0.0.0" はコメントであって写しではない
RUN set -eu; \
    pnpm_version="12.17.0"; \
    pnpm_sha256_amd64="` + shaA + `"; \
    pnpm_sha256_arm64="` + shaB + `"; \
    true
`

// editorconfig-checker-enable

func newRepo(t *testing.T, mise, dockerfileBody string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, miseFile), []byte(mise), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docker", "tools"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, dockerfile), []byte(dockerfileBody), 0o600))

	return root
}

func readDockerfile(t *testing.T, root string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, dockerfile))
	require.NoError(t, err)

	return string(got)
}

// requests は、テスト用の API が受けた要求の path と Authorization を記録する。
type requests struct {
	mu    sync.Mutex
	paths []string
	auths []string
}

func (r *requests) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.paths...), append([]string(nil), r.auths...)
}

// releaseServer は、tag の Release だけに assets を返し、受けた要求を記録する。
func releaseServer(t *testing.T, tag string, assets map[string]string) (*httptest.Server, *requests) {
	t.Helper()
	rec := &requests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		if r.URL.Path != "/repos/pnpm/pnpm/releases/tags/"+tag {
			http.NotFound(w, r)

			return
		}
		var parts []string
		for name, digest := range assets {
			parts = append(parts, fmt.Sprintf(`{"name":%q,"digest":%q}`, name, digest))
		}
		_, _ = fmt.Fprintf(w, `{"assets":[%s]}`, strings.Join(parts, ","))
	}))
	t.Cleanup(srv.Close)

	return srv, rec
}

func ghFor(srv *httptest.Server) githubAPI {
	return githubAPI{base: srv.URL, client: srv.Client()}
}

func bothAssets(amd, arm string) map[string]string {
	return map[string]string{
		"pnpm-linux-x64-musl.tar.gz":   "sha256:" + amd,
		"pnpm-linux-arm64-musl.tar.gz": "sha256:" + arm,
		"pnpm-linux-x64.tar.gz":        "sha256:" + shaD,
	}
}

func Test_shellVarRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("継続行の値を掴む", func(t *testing.T) {
			t.Parallel()
			m := shellVarRe("pnpm_version").FindAllStringSubmatch(soundDockerfile, -1)
			require.Len(t, m, 1)
			assert.Equal(t, "12.17.0", m[0][2])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメント行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, shellVarRe("pnpm_version").FindAllString(`# pnpm_version="1.0.0"`, -1))
		})

		// 名前の前方が一致するだけの変数を掴むと、別の値を版として読む。
		t.Run("名前を末尾に含む別の変数を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, shellVarRe("pnpm_version").FindAllString(`    old_pnpm_version="1.0.0"; \`, -1))
		})
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("check は一致していれば通り、何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile)
			var out bytes.Buffer
			require.NoError(t, run(context.Background(), []string{"check"}, root, githubAPI{base: "http://unused.invalid", client: http.DefaultClient}, &out))
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
			assert.Contains(t, out.String(), "12.17.0")
		})

		t.Run("resolve は宣言の版の Release から版と照合値を書く", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, strings.Replace(soundMise, "12.17.0", "12.18.0", 1), soundDockerfile)
			srv, rec := releaseServer(t, "v12.18.0", bothAssets(shaC, shaD))
			var out bytes.Buffer

			require.NoError(t, run(context.Background(), []string{"resolve"}, root, ghFor(srv), &out))
			want := strings.NewReplacer(`"12.17.0"`, `"12.18.0"`, shaA, shaC, shaB, shaD).Replace(soundDockerfile)
			assert.Equal(t, want, readDockerfile(t, root))
			paths, _ := rec.snapshot()
			assert.Equal(t, []string{"/repos/pnpm/pnpm/releases/tags/v12.18.0"}, paths)
		})
	})

	t.Run("正常系（照合値が未記入）", func(t *testing.T) {
		t.Parallel()

		// 照合値がまだ形を成していない Dockerfile は、比べる相手が無い（detectReplaced）。
		t.Run("resolve は同じ版のまま空の照合値を埋める", func(t *testing.T) {
			t.Parallel()
			empty := strings.NewReplacer(shaA, "", shaB, "").Replace(soundDockerfile)
			root := newRepo(t, soundMise, empty)
			srv, _ := releaseServer(t, "v12.17.0", bothAssets(shaA, shaB))
			require.NoError(t, run(context.Background(), []string{"resolve"}, root, ghFor(srv), &bytes.Buffer{}))
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("引数が無ければ usage", func(t *testing.T) {
			t.Parallel()
			err := run(context.Background(), nil, t.TempDir(), githubAPI{client: http.DefaultClient}, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("未知のサブコマンドは usage", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile)
			err := run(context.Background(), []string{"apply"}, root, githubAPI{client: http.DefaultClient}, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("check は版のずれを errDrift で返し、何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, strings.Replace(soundMise, "12.17.0", "12.18.0", 1), soundDockerfile)
			var out bytes.Buffer
			err := run(context.Background(), []string{"check"}, root, githubAPI{client: http.DefaultClient}, &out)
			require.ErrorIs(t, err, errDrift)
			assert.Contains(t, out.String(), "pnpm_version")
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
		})

		t.Run("Dockerfile が読めなければエラー", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, miseFile), []byte(soundMise), 0o600))
			err := run(context.Background(), []string{"check"}, root, githubAPI{client: http.DefaultClient}, &bytes.Buffer{})
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("resolve は Release を取れなければ何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, strings.Replace(soundMise, "12.17.0", "12.18.0", 1), soundDockerfile)
			srv, _ := releaseServer(t, "v99.0.0", nil)
			err := run(context.Background(), []string{"resolve"}, root, ghFor(srv), &bytes.Buffer{})
			require.ErrorIs(t, err, errRelease)
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
		})

		t.Run("resolve は同じ版の digest が変わっていれば何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile)
			srv, _ := releaseServer(t, "v12.17.0", bothAssets(shaC, shaB))
			err := run(context.Background(), []string{"resolve"}, root, ghFor(srv), &bytes.Buffer{})
			require.ErrorIs(t, err, errReplaced)
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
		})
	})
}

func Test_declaredVersion(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), miseFile)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		got, err := declaredVersion(write(t, soundMise))
		require.NoError(t, err)
		assert.Equal(t, "12.17.0", got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言が無ければ errShape", func(t *testing.T) {
			t.Parallel()
			_, err := declaredVersion(write(t, "[tools]\nnode = \"26.13.1\"\n"))
			require.ErrorIs(t, err, errShape)
		})

		// 理由は declaredVersion の版の形の検査が持つ。
		for _, v := range []string{"12", "12.17", "latest", "12.17.0-rc.1"} {
			t.Run("版が x.y.z でなければ errShape: "+v, func(t *testing.T) {
				t.Parallel()
				_, err := declaredVersion(write(t, "[tools]\n\"aqua:pnpm/pnpm\" = \""+v+"\"\n"))
				require.ErrorIs(t, err, errShape)
			})
		}

		t.Run("mise.toml が読めなければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := declaredVersion(filepath.Join(t.TempDir(), miseFile))
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("宣言が重複していれば先頭を採らずに落とす", func(t *testing.T) {
			t.Parallel()
			_, err := declaredVersion(write(t, soundMise+"\"aqua:pnpm/pnpm\" = \"12.18.0\"\n"))
			require.ErrorIs(t, err, misetoml.ErrDuplicateKey)
		})

		t.Run("解釈できない行があれば落とす", func(t *testing.T) {
			t.Parallel()
			_, err := declaredVersion(write(t, soundMise+"pnpm 12.17.0\n"))
			require.ErrorIs(t, err, misetoml.ErrInvalidLine)
		})
	})
}

func Test_readPins(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		got, err := readPins(soundDockerfile)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"pnpm_version": "12.17.0", "pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB,
		}, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 照合値の行が消えたまま「一致」と報告しない。
		t.Run("変数が欠けていれば errShape", func(t *testing.T) {
			t.Parallel()
			_, err := readPins(strings.Replace(soundDockerfile, "pnpm_sha256_arm64", "removed", 1))
			require.ErrorIs(t, err, errShape)
		})

		t.Run("変数が2件あれば errShape", func(t *testing.T) {
			t.Parallel()
			_, err := readPins(soundDockerfile + "    pnpm_version=\"1.0.0\"; \\\n")
			require.ErrorIs(t, err, errShape)
		})

		// 写しが ARG になると `--build-arg` で差し替えられる。シェル変数の形でなければ数えない。
		for name, src := range map[string]string{
			"単引用符の写し":     strings.Replace(soundDockerfile, `pnpm_version="12.17.0"`, `pnpm_version='12.17.0'`, 1),
			"ARG の写し":     strings.Replace(soundDockerfile, `    pnpm_version="12.17.0"; \`, `ARG pnpm_version=12.17.0`, 1),
			"コメントの中にしか無い": strings.Replace(soundDockerfile, `    pnpm_version="12.17.0"; \`, `#   pnpm_version="12.17.0"; \`, 1),
		} {
			t.Run(name+"は数えず errShape", func(t *testing.T) {
				t.Parallel()
				require.NotEqual(t, soundDockerfile, src, "書き換えられていない")
				_, err := readPins(src)
				require.ErrorIs(t, err, errShape)
			})
		}

		t.Run("空の Dockerfile は errShape", func(t *testing.T) {
			t.Parallel()
			_, err := readPins("")
			require.ErrorIs(t, err, errShape)
		})
	})
}

func Test_check(t *testing.T) {
	t.Parallel()

	sound := map[string]string{"pnpm_version": "12.17.0", "pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range sound {
			m[kk] = vv
		}
		m[k] = v

		return m
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, check("12.17.0", sound, &out))
		assert.Contains(t, out.String(), "✅")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 出力はずれた変数を名指す。arm64 と amd64 の取り違えも、ここで見える。
		tests := map[string]struct {
			key, value string
		}{
			"版が宣言と違う":    {"pnpm_version", "12.16.0"},
			"照合値が空":      {"pnpm_sha256_amd64", ""},
			"照合値が63桁":    {"pnpm_sha256_arm64", shaB[:63]},
			"照合値が65桁":    {"pnpm_sha256_arm64", shaB + "0"},
			"照合値が大文字":    {"pnpm_sha256_arm64", strings.ToUpper(shaB)},
			"照合値に接頭辞が残る": {"pnpm_sha256_amd64", "sha256:" + shaA},
		}
		for name, tc := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				require.ErrorIs(t, check("12.17.0", with(tc.key, tc.value), &out), errDrift)
				assert.Contains(t, out.String(), tc.key)
			})
		}

		t.Run("複数のずれをすべて名指す", func(t *testing.T) {
			t.Parallel()
			current := with("pnpm_version", "12.16.0")
			current["pnpm_sha256_arm64"] = ""
			var out bytes.Buffer
			require.ErrorIs(t, check("12.17.0", current, &out), errDrift)
			assert.Contains(t, out.String(), "pnpm_version")
			assert.Contains(t, out.String(), "pnpm_sha256_arm64")
			assert.NotContains(t, out.String(), "pnpm_sha256_amd64")
		})
	})
}

func Test_fetchDigests(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("musl の配布物の digest を変数名で返す", func(t *testing.T) {
			t.Parallel()
			srv, _ := releaseServer(t, "v12.17.0", bothAssets(shaA, shaB))
			got, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB}, got)
		})

		t.Run("トークンがあれば Bearer で送る", func(t *testing.T) {
			t.Parallel()
			srv, rec := releaseServer(t, "v12.17.0", bothAssets(shaA, shaB))
			gh := ghFor(srv)
			gh.token = "tkn"
			_, err := fetchDigests(context.Background(), gh, "12.17.0")
			require.NoError(t, err)
			_, auths := rec.snapshot()
			assert.Equal(t, []string{"Bearer tkn"}, auths)
		})

		t.Run("トークンが空なら Authorization を送らない", func(t *testing.T) {
			t.Parallel()
			srv, rec := releaseServer(t, "v12.17.0", bothAssets(shaA, shaB))
			_, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
			require.NoError(t, err)
			_, auths := rec.snapshot()
			assert.Equal(t, []string{""}, auths)
		})

		t.Run("API の base の末尾の / を許す", func(t *testing.T) {
			t.Parallel()
			srv, _ := releaseServer(t, "v12.17.0", bothAssets(shaA, shaB))
			_, err := fetchDigests(context.Background(), githubAPI{base: srv.URL + "/", client: srv.Client()}, "12.17.0")
			require.NoError(t, err)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("Release が無ければ errRelease", func(t *testing.T) {
			t.Parallel()
			srv, _ := releaseServer(t, "v1.0.0", nil)
			_, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
			require.ErrorIs(t, err, errRelease)
		})

		assets := map[string]map[string]string{
			"配布物が欠けている":  {"pnpm-linux-x64-musl.tar.gz": "sha256:" + shaA},
			"配布物が1件も無い":  {},
			"digest が無い": {"pnpm-linux-x64-musl.tar.gz": "", "pnpm-linux-arm64-musl.tar.gz": "sha256:" + shaB},
			"digest が別の算法": {
				"pnpm-linux-x64-musl.tar.gz": "sha512:" + shaA, "pnpm-linux-arm64-musl.tar.gz": "sha256:" + shaB,
			},
			"digest の桁が足りない": {
				"pnpm-linux-x64-musl.tar.gz": "sha256:" + shaA[:10], "pnpm-linux-arm64-musl.tar.gz": "sha256:" + shaB,
			},
		}
		for name, a := range assets {
			t.Run(name+"なら errRelease", func(t *testing.T) {
				t.Parallel()
				srv, _ := releaseServer(t, "v12.17.0", a)
				_, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
				require.ErrorIs(t, err, errRelease)
			})
		}

		t.Run("200 以外なら errRelease", func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"assets":[]}`))
			}))
			t.Cleanup(srv.Close)
			_, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
			require.ErrorIs(t, err, errRelease)
		})

		t.Run("JSON でなければ errRelease", func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			}))
			t.Cleanup(srv.Close)
			_, err := fetchDigests(context.Background(), ghFor(srv), "12.17.0")
			require.ErrorIs(t, err, errRelease)
		})

		t.Run("接続できなければ errRelease", func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.NotFoundHandler())
			url := srv.URL
			srv.Close()
			_, err := fetchDigests(context.Background(), githubAPI{base: url, client: http.DefaultClient}, "12.17.0")
			require.ErrorIs(t, err, errRelease)
		})
	})
}

func Test_detectReplaced(t *testing.T) {
	t.Parallel()

	current := map[string]string{"pnpm_version": "12.17.0", "pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("同じ版で同じ digest なら通る", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, detectReplaced(current, current))
		})

		t.Run("版が変われば digest が変わってよい", func(t *testing.T) {
			t.Parallel()
			want := map[string]string{"pnpm_version": "12.18.0", "pnpm_sha256_amd64": shaC, "pnpm_sha256_arm64": shaD}
			require.NoError(t, detectReplaced(current, want))
		})

		// 照合値がまだ書かれていない初回は、比べる相手が無い。
		t.Run("固定済みの値が sha256 の形でなければ比べない", func(t *testing.T) {
			t.Parallel()
			empty := map[string]string{"pnpm_version": "12.17.0", "pnpm_sha256_amd64": "", "pnpm_sha256_arm64": ""}
			require.NoError(t, detectReplaced(empty, current))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"pnpm_sha256_amd64", "pnpm_sha256_arm64"} {
			t.Run("同じ版で "+name+" が変われば errReplaced", func(t *testing.T) {
				t.Parallel()
				want := map[string]string{}
				for k, v := range current {
					want[k] = v
				}
				want[name] = shaC
				err := detectReplaced(current, want)
				require.ErrorIs(t, err, errReplaced)
				assert.Contains(t, err.Error(), shaC)
			})
		}
	})
}

func Test_write(t *testing.T) {
	t.Parallel()

	current := map[string]string{"pnpm_version": "12.17.0", "pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("変化が無ければ書かない", func(t *testing.T) {
			t.Parallel()
			// 書き込めないディレクトリに置くので、書こうとすればエラーになる。
			path := filepath.Join(t.TempDir(), "missing", "Dockerfile")
			var out bytes.Buffer
			require.NoError(t, write(path, soundDockerfile, current, current, &out))
			assert.Contains(t, out.String(), "固定済み")
		})

		// 値を連結しない理由は write の置換の注記が持つ。
		t.Run("値の $ を後方参照として読まない", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile)
			path := filepath.Join(root, dockerfile)
			want := map[string]string{"pnpm_version": "1.2.3$1", "pnpm_sha256_amd64": shaA, "pnpm_sha256_arm64": shaB}
			require.NoError(t, write(path, soundDockerfile, current, want, &bytes.Buffer{}))
			assert.Contains(t, readDockerfile(t, root), `pnpm_version="1.2.3$1"; \`)
		})

		t.Run("変わった変数だけを名乗る", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile)
			want := map[string]string{"pnpm_version": "12.17.0", "pnpm_sha256_amd64": shaC, "pnpm_sha256_arm64": shaB}
			var out bytes.Buffer
			require.NoError(t, write(filepath.Join(root, dockerfile), soundDockerfile, current, want, &out))
			assert.Contains(t, out.String(), "pnpm_sha256_amd64")
			assert.NotContains(t, out.String(), "pnpm_sha256_arm64")
			assert.Equal(t, strings.Replace(soundDockerfile, shaA, shaC, 1), readDockerfile(t, root))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// rename はファイルの権限を見ないので、親ディレクトリを読み取り専用にする（Test Strategy）。
		t.Run("書き込めなければエラーにし、元の内容を残す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "root は読み取り専用のディレクトリにも書ける")
			root := newRepo(t, soundMise, soundDockerfile)
			dir := filepath.Join(root, "docker", "tools")
			require.NoError(t, os.Chmod(dir, 0o500))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			want := map[string]string{"pnpm_version": "12.18.0", "pnpm_sha256_amd64": shaC, "pnpm_sha256_arm64": shaD}
			require.Error(t, write(filepath.Join(root, dockerfile), soundDockerfile, current, want, &bytes.Buffer{}))
			assert.Equal(t, soundDockerfile, readDockerfile(t, root))
		})
	})
}
