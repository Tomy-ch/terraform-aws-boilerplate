package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const soundADR = `# ADR-0001: なにか

- Status: Accepted
- Date: 2026-09-17
- Scope: repository-wide

## Context
本文。
`

// 見出しが名乗る番号はファイル名の番号と一致していなければならないため、0001 以外のファイルはこれを使います。
func soundADRNo(number string) string {
	return strings.Replace(soundADR, "# ADR-0001:", "# ADR-"+number+":", 1)
}

// docs/adr/ の外へ README.md を1つ置くのは、外を走査する検査8 が対象0件で止まるのを避けるためです。
func writeADRs(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "docs", "adr")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o600))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}
	return root
}

func repoOf(root string) string {
	return filepath.Dir(filepath.Dir(root))
}

func writeUseCaseADRs(t *testing.T, repo, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(repo, "usecases", name, "docs", "adr")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	for file, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600))
	}
	return dir
}

// -repo を省くと既定の "." が効き、テストが実物の木を読みます。
func args(root string) []string {
	return []string{"-root", root, "-repo", repoOf(root)}
}

func index(rows ...string) string {
	var b strings.Builder
	b.WriteString("# ADR\n\n| ID | タイトル | Status |\n| --- | --- | --- |\n")
	for _, r := range rows {
		b.WriteString(r)
		b.WriteString("\n")
	}
	return b.String()
}

func row(num, file string) string {
	return rowWithStatus(num, file, "Accepted")
}

func rowWithStatus(num, file, status string) string {
	return "| [" + num + "](" + file + ") | なにか | " + status + " |"
}

func withStatus(source, status string) string {
	return strings.Replace(source, "- Status: Accepted", "- Status: "+status, 1)
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("構造が揃っていれば通る", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
		assert.Contains(t, out.String(), "ADR 1 件")
	})

	t.Run("索引と雛形は ADR として数えない", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md":   soundADR,
			"template.md": "# 雛形\n",
			"README.md":   index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
		assert.Contains(t, out.String(), "ADR 1 件")
	})

	t.Run("ADR が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{"README.md": index()})
		var out bytes.Buffer
		err := run(args(root), &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errNoADR))
	})

	t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", filepath.Join(t.TempDir(), "missing"), "-repo", t.TempDir()}, &out))
	})
}

func Test_run_ユースケースのADR(t *testing.T) {
	t.Parallel()

	rootOnly := func(t *testing.T) string {
		t.Helper()
		return writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-a.md")),
		})
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("番号は scope ごとに独立している", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(row("0001", "0001-a.md")),
			})
			var out bytes.Buffer
			require.NoError(t, run(args(root), &out))
			assert.Contains(t, out.String(), "ADR 2 件（2 scope）")
		})

		t.Run("docs/adr を持たないユースケースを出力に挙げる", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			require.NoError(t, os.MkdirAll(filepath.Join(repoOf(root), "usecases", "no-adr"), 0o750))
			var out bytes.Buffer
			require.NoError(t, run(args(root), &out))
			assert.Contains(t, out.String(), "docs/adr を持たないユースケース: no-adr")
		})

		t.Run("use-case ADR は自分のユースケースの path を名指ししてよい", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{
				"0001-a.md": soundADR + "\n`usecases/demo/main.tf` を参照する。\n",
				"README.md": index(row("0001", "0001-a.md")),
			})
			var out bytes.Buffer
			require.NoError(t, run(args(root), &out))
			assert.Contains(t, out.String(), "ADR 2 件（2 scope）")
		})

		t.Run("use-case ADR は scope を前置して root の ADR を指してよい", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{
				"0001-a.md": soundADR + "\nrepository-wide/ADR-0001 を前提とする。\n",
				"README.md": index(row("0001", "0001-a.md")),
			})
			var out bytes.Buffer
			require.NoError(t, run(args(root), &out))
			assert.Contains(t, out.String(), "ADR 2 件（2 scope）の構造と、scope を伴う参照 1 件と、")
			assert.NotContains(t, out.String(), "docs/adr を持たないユースケース")
		})

		t.Run("昇格した use-case ADR は root の slug を宛先にして supersede できる", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			status := "Superseded by repository-wide/a"
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{
				"0001-b.md": withStatus(soundADR, status),
				"README.md": index(rowWithStatus("0001", "0001-b.md", status)),
			})
			var out bytes.Buffer
			require.NoError(t, run(args(root), &out))
			assert.Contains(t, out.String(), "ADR 2 件（2 scope）")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files       map[string]string
			wantMessage string
		}{
			"見出しの番号の食い違いを落とす": {
				files: map[string]string{
					"0001-a.md": soundADRNo("0002"),
					"README.md": index(row("0001", "0001-a.md")),
				},
				wantMessage: "見出しが ADR-0002 を名乗っています",
			},
			"メタデータの欠落を落とす": {
				files: map[string]string{
					"0001-a.md": strings.Replace(soundADR, "- Date: 2026-09-17\n", "", 1),
					"README.md": index(row("0001", "0001-a.md")),
				},
				wantMessage: "先頭メタデータに `Date` がありません",
			},
			"scope の中の slug の重複を落とす": {
				files: map[string]string{
					"0001-a.md": soundADR,
					"0002-a.md": soundADRNo("0002"),
					"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-a.md")),
				},
				wantMessage: "slug a が重複しています",
			},
			"scope の中の番号の重複を落とす": {
				files: map[string]string{
					"0001-a.md": soundADR,
					"0001-b.md": soundADR,
					"README.md": index(row("0001", "0001-a.md")),
				},
				wantMessage: "番号 0001 が重複しています",
			},
			"索引との食い違いを落とす": {
				files: map[string]string{
					"0001-a.md": soundADR,
					"README.md": index(),
				},
				wantMessage: "0001-a.md が索引に載っていません",
			},
			"scope を伴わない番号は自 scope で引く": {
				files: map[string]string{
					"0001-a.md": soundADR + "\nADR-0101 を参照する。\n",
					"README.md": index(row("0001", "0001-a.md")),
				},
				wantMessage: "参照先の ADR-0101 が存在しません",
			},
			"scope を伴う番号は宛先の scope で引く": {
				files: map[string]string{
					"0001-a.md": soundADR + "\nrepository-wide/ADR-0099 を参照する。\n",
					"README.md": index(row("0001", "0001-a.md")),
				},
				wantMessage: "参照先の repository-wide/ADR-0099 が存在しません",
			},
			"root に無い slug への supersede を落とす": {
				files: map[string]string{
					"0001-a.md": withStatus(soundADR, "Superseded by repository-wide/missing"),
					"README.md": index(rowWithStatus("0001", "0001-a.md", "Superseded by repository-wide/missing")),
				},
				wantMessage: "Superseded by の参照先 repository-wide/missing が存在しません",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root := writeADRs(t, map[string]string{
					"0001-a.md": soundADR,
					"0101-b.md": soundADRNo("0101"),
					"README.md": index(row("0001", "0001-a.md"), row("0101", "0101-b.md")),
				})
				writeUseCaseADRs(t, repoOf(root), "demo", tt.files)
				var out bytes.Buffer
				require.Error(t, run(args(root), &out))
				assert.Contains(t, out.String(), filepath.Join("usecases", "demo", "docs", "adr"))
				assert.Contains(t, out.String(), tt.wantMessage)
			})
		}

		sentinels := map[string]map[string]string{
			"ADR ディレクトリが空なら成功で返さない":      nil,
			"ADR ディレクトリに索引しか無ければ成功で返さない": {"README.md": index()},
		}

		for name, files := range sentinels {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root := rootOnly(t)
				writeUseCaseADRs(t, repoOf(root), "demo", files)
				var out bytes.Buffer
				err := run(args(root), &out)
				require.ErrorIs(t, err, errNoADR)
			})
		}

		t.Run("索引が無ければエラー", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{"0001-a.md": soundADR})
			var out bytes.Buffer
			err := run(args(root), &out)
			require.Error(t, err)
			assert.Contains(t, err.Error(), filepath.Join("usecases", "demo", "docs", "adr", "README.md"))
		})

		t.Run("scope の発見に失敗すれば検査せずにエラーを返す", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			require.NoError(t, os.WriteFile(filepath.Join(repoOf(root), "usecases"), nil, 0o600))
			var out bytes.Buffer
			require.Error(t, run(args(root), &out))
			assert.NotContains(t, out.String(), "✅")
		})

		t.Run("use-case の ADR を名指ししているのにリンクを取り出せなければ番兵で返す", func(t *testing.T) {
			t.Parallel()
			root := rootOnly(t)
			writeUseCaseADRs(t, repoOf(root), "demo", map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(row("0001", "0001-a.md")),
			})
			require.NoError(t, os.WriteFile(filepath.Join(repoOf(root), "README.md"),
				[]byte("[0001](docs/adr/0001-a.md)\n\n[ADR][ref]\n\n[ref]: usecases/demo/docs/adr/0001-a.md\n"), 0o600))
			var out bytes.Buffer
			err := run(args(root), &out)
			require.ErrorIs(t, err, errNoReference)
			assert.Contains(t, err.Error(), "usecases/demo/docs/adr")
		})
	})
}

func Test_discoverScopes(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("usecases が無ければ root だけを返す", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			got, skipped, err := discoverScopes(repo, "docs/adr")
			require.NoError(t, err)
			assert.Equal(t, []scope{{name: rootScope, dir: "docs/adr"}}, got)
			assert.Empty(t, skipped)
		})

		t.Run("docs/adr を持つユースケースを名前の昇順で root の後に返す", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			b := writeUseCaseADRs(t, repo, "b", nil)
			a := writeUseCaseADRs(t, repo, "a", nil)
			got, _, err := discoverScopes(repo, "docs/adr")
			require.NoError(t, err)
			assert.Equal(t, []scope{{name: rootScope, dir: "docs/adr"}, {name: "a", dir: a}, {name: "b", dir: b}}, got)
		})

		t.Run("大文字と数字とアンダースコアを含む名前は scope にする", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeUseCaseADRs(t, repo, "Demo_2", nil)
			scopes, _, err := discoverScopes(repo, "docs/adr")
			require.NoError(t, err)
			require.Len(t, scopes, 2)
			assert.Equal(t, "Demo_2", scopes[1].name)
		})

		t.Run("docs/adr を持たないユースケースは scope にせず名前を返す", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "no-adr", "docs", "adrs"), 0o750))
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "a-no-adr"), 0o750))
			b := writeUseCaseADRs(t, repo, "b", nil)
			require.NoError(t, os.WriteFile(filepath.Join(repo, "usecases", "README.md"), []byte("# x\n"), 0o600))
			got, skipped, err := discoverScopes(repo, "docs/adr")
			require.NoError(t, err)
			assert.Equal(t, []scope{{name: rootScope, dir: "docs/adr"}, {name: "b", dir: b}}, got)
			assert.Equal(t, []string{"a-no-adr", "no-adr"}, skipped)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("docs/adr がディレクトリでなければエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "demo", "docs"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(repo, "usecases", "demo", "docs", "adr"), nil, 0o600))
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errNotDirectory)
			assert.Contains(t, err.Error(), filepath.Join("usecases", "demo", "docs", "adr"))
		})

		// 不在以外の失敗を「docs/adr を持たない」へ倒すと、そのユースケースが黙って外れる。
		t.Run("docs/adr の確認が不在以外で失敗すればエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "demo"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(repo, "usecases", "demo", "docs"), nil, 0o600))
			_, skipped, err := discoverScopes(repo, "docs/adr")
			require.Error(t, err)
			assert.Nil(t, skipped)
		})

		t.Run("usecases がディレクトリでなければエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(repo, "usecases"), nil, 0o600))
			_, _, err := discoverScopes(repo, "docs/adr")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "usecases")
		})

		t.Run("root scope と同じ名前のユースケースはエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeUseCaseADRs(t, repo, rootScope, nil)
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errReservedScope)
		})

		t.Run("scope として使えない文字を含む名前のユースケースは docs/adr が無くてもエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "a.b"), 0o750))
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errScopeName)
		})

		t.Run("scope として使えない文字を含む名前のユースケースはエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeUseCaseADRs(t, repo, "a.b", nil)
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errScopeName)
			assert.Contains(t, err.Error(), "a.b")
		})

		t.Run("シンボリックリンクのユースケースはエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			target := writeUseCaseADRs(t, t.TempDir(), "real", nil)
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases"), 0o750))
			require.NoError(t, os.Symlink(filepath.Dir(filepath.Dir(target)), filepath.Join(repo, "usecases", "link")))
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errSymlink)
			assert.Contains(t, err.Error(), filepath.Join("usecases", "link"))
		})

		t.Run("シンボリックリンクの docs/adr はエラー", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			target := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases", "demo", "docs"), 0o750))
			require.NoError(t, os.Symlink(target, filepath.Join(repo, "usecases", "demo", "docs", "adr")))
			_, _, err := discoverScopes(repo, "docs/adr")
			require.ErrorIs(t, err, errSymlink)
		})
	})
}

func Test_checkScope(t *testing.T) {
	t.Parallel()

	naming := soundADR + "\n`usecases/demo/main.tf`\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("use-case scope ではユースケースへの依存を見ない", func(t *testing.T) {
			t.Parallel()
			dir := writeUseCaseADRs(t, t.TempDir(), "demo", map[string]string{
				"0001-a.md": naming,
				"README.md": index(row("0001", "0001-a.md")),
			})
			docs, findings, err := checkScope(scope{name: "demo", dir: dir})
			require.NoError(t, err)
			assert.Empty(t, findings)
			assert.Len(t, docs, 1)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root scope ではユースケースへの依存を落とす", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": naming,
				"README.md": index(row("0001", "0001-a.md")),
			})
			_, findings, err := checkScope(scope{name: rootScope, dir: root})
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "特定ユースケースの path")
		})

		t.Run("ファイル名がすべて規約違反なら番兵ではなく違反を返す", func(t *testing.T) {
			t.Parallel()
			dir := writeUseCaseADRs(t, t.TempDir(), "demo", map[string]string{
				"bad_name.md": soundADR,
				"README.md":   index(),
			})
			docs, findings, err := checkScope(scope{name: "demo", dir: dir})
			require.NoError(t, err)
			assert.Empty(t, docs)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "ファイル名が")
		})

		t.Run("ADR が0件なら置き場所を添えて番兵で返す", func(t *testing.T) {
			t.Parallel()
			dir := writeUseCaseADRs(t, t.TempDir(), "demo", map[string]string{"README.md": index()})
			_, _, err := checkScope(scope{name: "demo", dir: dir})
			require.ErrorIs(t, err, errNoADR)
			assert.Contains(t, err.Error(), dir)
		})
	})
}

func Test_ファイル名の規約(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		ok   bool
	}{
		"4桁の番号と kebab": {name: "0001-a-b-c.md", ok: true},
		"数字を含む語":       {name: "0001-adr-0159-1.md", ok: true},
		"番号が3桁":        {name: "001-a.md", ok: false},
		"番号が5桁":        {name: "00001-a.md", ok: false},
		"大文字":          {name: "0001-Abc.md", ok: false},
		"アンダースコア":      {name: "0001-a_b.md", ok: false},
		"ハイフンが連続":      {name: "0001-a--b.md", ok: false},
		"末尾がハイフン":      {name: "0001-a-.md", ok: false},
		"題が無い":         {name: "0001.md", ok: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				tt.name:     soundADR,
				"README.md": index(row("0001", tt.name)),
			})
			var out bytes.Buffer
			err := run(args(root), &out)
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, out.String(), "ファイル名が")
		})
	}
}

func Test_ファイル名違反だけのときも違反として報告する(t *testing.T) {
	t.Parallel()

	root := writeADRs(t, map[string]string{
		"bad_name.md": soundADR,
		"README.md":   index(),
	})
	var out bytes.Buffer
	err := run(args(root), &out)
	require.Error(t, err)
	assert.False(t, xerrors.Is(err, errNoADR))
	assert.Contains(t, out.String(), "ファイル名が")
}

func Test_番号の重複(t *testing.T) {
	t.Parallel()

	// 同じ scope の中で番号は重複しない。同じ番号が2つ在ると、
	// `ADR-0001` という参照がどちらを指すか決まらない。
	root := writeADRs(t, map[string]string{
		"0001-a.md": soundADR,
		"0001-b.md": soundADR,
		"README.md": index(row("0001", "0001-a.md")),
	})
	var out bytes.Buffer
	require.Error(t, run(args(root), &out))
	assert.Contains(t, out.String(), "番号 0001 が重複しています")
}

func Test_先頭メタデータ(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want string
	}{
		"Status が無い": {body: "# ADR-0001: x\n\n- Date: 2026-09-17\n- Scope: repository-wide\n", want: "`Status`"},
		"Date が無い":   {body: "# ADR-0001: x\n\n- Status: Accepted\n- Scope: repository-wide\n", want: "`Date`"},
		"Scope が無い":  {body: "# ADR-0001: x\n\n- Status: Accepted\n- Date: 2026-09-17\n", want: "`Scope`"},
		"値が空":        {body: "# ADR-0001: x\n\n- Status:\n- Date: 2026-09-17\n- Scope: repository-wide\n", want: "`Status`"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": tt.body,
				"README.md": index(row("0001", "0001-a.md")),
			})
			var out bytes.Buffer
			require.Error(t, run(args(root), &out))
			assert.Contains(t, out.String(), tt.want)
		})
	}
}

func Test_supersedeの参照先(t *testing.T) {
	t.Parallel()

	superseded := func(status string) map[string]string {
		return map[string]string{
			"0001-a.md": withStatus(soundADR, status),
			"0002-b.md": soundADRNo("0002"),
			"README.md": index(rowWithStatus("0001", "0001-a.md", status), row("0002", "0002-b.md")),
		}
	}

	t.Run("参照先の slug が在れば通る", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, superseded("Superseded by repository-wide/b"))
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	tests := map[string]struct {
		status      string
		wantMessage string
	}{
		"参照先が無ければ落ちる": {
			status:      "Superseded by repository-wide/missing",
			wantMessage: "参照先 repository-wide/missing が存在しません",
		},
		// identity は <scope>/<slug>（ADR-0001 決定4）。番号は詰められるので宛先にならない。
		"番号で書いた宛先は落とす": {
			status:      "Superseded by repository-wide/ADR-0002",
			wantMessage: "形式が",
		},
		"scope を伴わない形は落とす": {
			status:      "Superseded by b",
			wantMessage: "形式が",
		},
		"どの scope にも当たらない宛先は落とす": {
			status:      "Superseded by ecs-web-service/b",
			wantMessage: "宛先の scope ecs-web-service が見つかりません",
		},
		"自分自身を指せば落とす": {
			status:      "Superseded by repository-wide/a",
			wantMessage: "自分自身を指しています",
		},
		"by を伴わない Superseded は落とす": {
			status:      "Superseded",
			wantMessage: "いずれでもありません",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, superseded(tt.status))
			var out bytes.Buffer
			require.Error(t, run(args(root), &out))
			assert.Contains(t, out.String(), tt.wantMessage)
		})
	}
}

func Test_ユースケースへの依存(t *testing.T) {
	t.Parallel()

	body := func(text string) string {
		return soundADR + "\n" + text + "\n"
	}

	t.Run("プレースホルダは許容する", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("配置は `usecases/<use-case>/docs/adr/` とする。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("特定のユースケースを名指しすれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("`usecases/ecs-web-service/main.tf` を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "特定ユースケースの path")
	})

	t.Run("違反の行番号は該当行を指す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("一行目\n`usecases/ecs/x.tf`"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), ":11")
	})
}

func Test_索引との突合(t *testing.T) {
	t.Parallel()

	t.Run("索引に載っていなければ落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "索引に載っていません")
	})

	t.Run("索引が別のファイルを指していれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-old-name.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "実ファイルは 0001-a.md です")
	})

	t.Run("索引に実体の無い行が残っていれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-gone.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "対応する実ファイルがありません")
	})

	t.Run("索引の Status が本文と食い違えば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(rowWithStatus("0001", "0001-a.md", "Proposed")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), `索引の ADR-0001 の Status は "Proposed" ですが、本文は "Accepted" です`)
	})

	t.Run("索引が無ければエラー", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{"0001-a.md": soundADR})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
	})
}

func Test_相互参照の実在(t *testing.T) {
	t.Parallel()

	t.Run("実在する ADR への参照は通る", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR + "\nADR-0002 を参照する。\n",
			"0002-b.md": soundADRNo("0002"),
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-b.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("存在しない ADR への参照は落とす", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR + "\nADR-0099 を参照する。\n",
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "参照先の ADR-0099 が存在しません")
	})

	t.Run("自分自身への言及は参照として見ない", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": "# ADR-0001: なにか\n\n- Status: Accepted\n- Date: 2026-09-17\n- Scope: repository-wide\n\nADR-0001 は本文書である。\n",
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("違反の行番号は参照している行を指す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR + "\nADR-0099\n",
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), ":10")
	})
}

func Test_相互参照_scopeを伴う参照(t *testing.T) {
	t.Parallel()

	t.Run("宛先の scope に実在すれば通る", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundApexADR("repository-wide/ADR-0001 を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("どの scope にも当たらない宛先は落とす", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundApexADR("ecs-web-service/ADR-0099 を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "scope ecs-web-service が見つかりません")
	})

	t.Run("参照を含む行から取り出しきれなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundApexADR("a.b/ADR-0001 を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.ErrorIs(t, run(args(root), &out), errNoScopedRef)
		assert.NotContains(t, out.String(), "✅")
	})
}

func soundApexADR(text string) string {
	return soundADR + "\n" + text + "\n"
}

func writeRepo(t *testing.T, adrs, outside map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "docs", "adr")
	require.NoError(t, os.MkdirAll(root, 0o750))
	for name, body := range adrs {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}
	for name, body := range outside {
		path := filepath.Join(repo, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
	return repo
}

func Test_checkPathReferences(t *testing.T) {
	t.Parallel()

	adrs := map[string]string{
		"0001-a.md": soundADR,
		"README.md": index(row("0001", "0001-a.md")),
	}
	docs := []doc{{path: "0001-a.md", name: "0001-a.md", number: 1}}
	scoped := func(repo string) []scopeDocs {
		return []scopeDocs{
			{scope: scope{name: rootScope, dir: filepath.Join(repo, "docs", "adr")}, docs: docs},
			{scope: scope{name: "demo", dir: filepath.Join(repo, "usecases", "demo", "docs", "adr")}, docs: docs},
		}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			outside  map[string]string
			wantRefs int
		}{
			"ルートからの参照を数える": {
				outside:  map[string]string{"AGENTS.md": "[0001](docs/adr/0001-a.md) を見よ\n"},
				wantRefs: 1,
			},
			"入れ子からの相対参照も解決する": {
				outside:  map[string]string{"scripts/README.md": "[0001](../docs/adr/0001-a.md)\n"},
				wantRefs: 1,
			},
			"アンカー付きでも解決する": {
				outside:  map[string]string{"AGENTS.md": "[0001](docs/adr/0001-a.md#決定1)\n"},
				wantRefs: 1,
			},
			"ADR- を伴う文言も突き合わせる": {
				outside:  map[string]string{"AGENTS.md": "[ADR-0001](docs/adr/0001-a.md)\n"},
				wantRefs: 1,
			},
			"外部 URL は番号体系が違うので見ない": {
				outside:  map[string]string{"AGENTS.md": "[ADR-0109](https://example.com/docs/adr/0109-x.md)\n"},
				wantRefs: 0,
			},
			"サイトのルートからの絶対パスは作業ツリーを指していない": {
				outside:  map[string]string{"AGENTS.md": "[9999](/docs/adr/9999-absent.md)\n"},
				wantRefs: 0,
			},
			"フェンスの中の例示は参照として数えない": {
				outside:  map[string]string{"AGENTS.md": "```md\n[0001](docs/adr/9999-absent.md)\n```\n"},
				wantRefs: 0,
			},
			"docs/adr 以外を指すリンクは見ない": {
				outside:  map[string]string{"AGENTS.md": "[x](docs/other/0001-a.md)\n"},
				wantRefs: 0,
			},
			"索引と雛形へのリンクは ADR ではない": {
				outside:  map[string]string{"AGENTS.md": "[索引](docs/adr/README.md) [雛形](docs/adr/template.md)\n"},
				wantRefs: 0,
			},
			"同じ ADR ディレクトリの中のリンクは外からの参照として数えない": {
				outside: map[string]string{
					"usecases/demo/docs/adr/README.md": "| [0001](0001-gone.md) | x | Accepted |\n",
					"AGENTS.md":                        "[0001](usecases/demo/docs/adr/0001-a.md)\n",
				},
				wantRefs: 1,
			},
			"use-case ADR から root の ADR への相対リンクも突き合わせる": {
				outside:  map[string]string{"usecases/demo/docs/adr/0001-a.md": "[0001](../../../../docs/adr/0001-a.md)\n"},
				wantRefs: 1,
			},
			"サイトのルートからの絶対パスは実在する ADR の名前でも数えない": {
				outside:  map[string]string{"AGENTS.md": "[0001](/docs/adr/0001-a.md)\n"},
				wantRefs: 0,
			},
			"use-case の ADR へのリンクはその scope の ADR と突き合わせる": {
				outside:  map[string]string{"AGENTS.md": "[0001](usecases/demo/docs/adr/0001-a.md)\n"},
				wantRefs: 1,
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := writeRepo(t, adrs, tt.outside)
				findings, refs, err := checkPathReferences(repo, scoped(repo))
				require.NoError(t, err)
				assert.Empty(t, findings)
				assert.Equal(t, tt.wantRefs, refs)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			outside     map[string]string
			wantMessage string
		}{
			"番号が実在しなければ落とす": {
				outside:     map[string]string{"AGENTS.md": "[9999](docs/adr/9999-absent.md)\n"},
				wantMessage: "参照先の ADR-9999 が存在しません",
			},
			"番号は在るが slug が違えば落とす": {
				outside:     map[string]string{"AGENTS.md": "[0001](docs/adr/0001-renamed.md)\n"},
				wantMessage: "参照先が実在しません",
			},
			"文言の番号が指し先と食い違えば落とす": {
				outside:     map[string]string{"AGENTS.md": "[0002](docs/adr/0001-a.md)\n"},
				wantMessage: "リンクの文言",
			},
			"ファイル名の形でなければ落とす": {
				outside:     map[string]string{"AGENTS.md": "[x](docs/adr/notes.md)\n"},
				wantMessage: "ADR のファイル名の形ではありません",
			},
			"どの scope にも当たらない use-case の ADR の置き場所を指せば落とす": {
				outside:     map[string]string{"AGENTS.md": "[0001](usecases/typo/docs/adr/0001-a.md)\n"},
				wantMessage: "どの scope にも当たらない",
			},
			"use-case の ADR を指す slug が違えば落とす": {
				outside:     map[string]string{"AGENTS.md": "[0001](usecases/demo/docs/adr/0001-renamed.md)\n"},
				wantMessage: "参照先が実在しません",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := writeRepo(t, adrs, tt.outside)
				findings, _, err := checkPathReferences(repo, scoped(repo))
				require.NoError(t, err)
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, "AGENTS.md", findings[0].File)
				assert.Equal(t, 1, findings[0].Line)
			})
		}

		t.Run("use-case ADR から root の ADR へのリンクの文言が食い違えば落とす", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, map[string]string{
				"usecases/demo/docs/adr/0001-a.md": "[0002](../../../../docs/adr/0001-a.md)\n",
			})
			findings, _, err := checkPathReferences(repo, scoped(repo))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Equal(t, "usecases/demo/docs/adr/0001-a.md", findings[0].File)
			assert.Contains(t, findings[0].Message, "リンクの文言")
		})

		t.Run("ADR を名指ししているのにリンクを取り出せなければ番兵で返す", func(t *testing.T) {
			t.Parallel()
			// reference-style link は markdownLink が拾えない書式の例。突合の意図は checkPathReferences を見よ。
			repo := writeRepo(t, adrs, map[string]string{
				"AGENTS.md": "決定は [ADR][ref] を見よ。\n\n[ref]: docs/adr/0001-a.md\n",
			})
			_, _, err := checkPathReferences(repo, scoped(repo))
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoReference))
		})

		t.Run("別の scope へのリンクが取れても、取りこぼした scope は番兵で返す", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, map[string]string{
				"AGENTS.md": "[0001](usecases/demo/docs/adr/0001-a.md)\n\n決定は [ADR][ref] を見よ。\n\n[ref]: docs/adr/0001-a.md\n",
			})
			_, _, err := checkPathReferences(repo, scoped(repo))
			require.ErrorIs(t, err, errNoReference)
			assert.Contains(t, err.Error(), "docs/adr")
		})

		t.Run("root へのリンクが取れても、取りこぼした use-case scope は番兵で返す", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, map[string]string{
				"AGENTS.md": "[0001](docs/adr/0001-a.md)\n\n[ADR][ref]\n\n[ref]: usecases/demo/docs/adr/0001-a.md\n",
			})
			_, _, err := checkPathReferences(repo, scoped(repo))
			require.ErrorIs(t, err, errNoReference)
			assert.Contains(t, err.Error(), "usecases/demo/docs/adr")
		})

		t.Run("読めない Markdown があれば黙って飛ばさずエラー", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, map[string]string{"README.md": "# x\n"})
			require.NoError(t, os.Symlink("absent", filepath.Join(repo, "AGENTS.md")))
			_, _, err := checkPathReferences(repo, scoped(repo))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "AGENTS.md")
		})

		t.Run("scope の置き場所を repo からの相対で表せなければエラー", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, map[string]string{"README.md": "# x\n"})
			_, _, err := checkPathReferences(repo, []scopeDocs{{scope: scope{name: rootScope, dir: "docs/adr"}, docs: docs}})
			require.Error(t, err)
		})

		t.Run("外に Markdown が1件も無ければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, nil)
			_, _, err := checkPathReferences(repo, scoped(repo))
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoMarkdown))
		})
	})
}

func Test_mentionedDirs(t *testing.T) {
	t.Parallel()

	indexes := map[string]map[int]string{
		"docs/adr":               {1: "0001-a.md"},
		"usecases/demo/docs/adr": {1: "0001-a.md"},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			from string
			want map[string]bool
		}{
			"root の ADR への道筋": {line: "[0001](docs/adr/0001-a.md)", from: ".", want: map[string]bool{"docs/adr": true}},
			"use-case の道筋は root の言及に数えない": {
				line: "[0001](usecases/demo/docs/adr/0001-a.md)", from: ".",
				want: map[string]bool{"usecases/demo/docs/adr": true},
			},
			"自分の置き場所は外す":  {line: "docs/adr/0001-a.md", from: "docs/adr", want: map[string]bool{}},
			"リンクの外の道筋も拾う": {line: "[ref]: docs/adr/0001-a.md", from: ".", want: map[string]bool{"docs/adr": true}},
			"use-case への相対の道筋を拾う": {
				line: "[0001](../demo/docs/adr/0001-a.md)", from: "usecases/other",
				want: map[string]bool{"usecases/demo/docs/adr": true},
			},
			"入れ子の文書からリポジトリのルート起点で書いた道筋も拾う": {
				line: "`docs/adr/0001-a.md`", from: "scripts", want: map[string]bool{"docs/adr": true},
			},
			"指し先の scope に無い ADR は同名でも拾わない": {
				line: "[0001](usecases/x/docs/adr/0001-a.md)", from: ".", want: map[string]bool{},
			},
			"外部 URL の中の同名は拾わない": {
				line: "https://example.com/o/r/blob/main/docs/adr/0001-a.md", from: ".", want: map[string]bool{},
			},
			"別のディレクトリの同名は拾わない":     {line: "[x](docs/other/0001-a.md)", from: ".", want: map[string]bool{}},
			"番号は在るが slug が違えば拾わない": {line: "docs/adr/0001-renamed.md", from: ".", want: map[string]bool{}},
			"サイトのルートからの絶対パスは拾わない":  {line: "[0001](/docs/adr/0001-a.md)", from: ".", want: map[string]bool{}},
			"索引は ADR ではない":         {line: "[索引](docs/adr/README.md)", from: ".", want: map[string]bool{}},
			"名指しが無ければ空":            {line: "ただの散文", from: ".", want: map[string]bool{}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, mentionedDirs(tt.line, tt.from, indexes))
			})
		}
	})
}

func Test_unknownScopeDir(t *testing.T) {
	t.Parallel()

	dirs := map[string]bool{"docs/adr": true, "usecases/demo/docs/adr": true}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			target  string
			fromDir string
			want    string
			ok      bool
		}{
			"scope に無い use-case の ADR の置き場所": {
				target: "usecases/typo/docs/adr/0001-a.md", fromDir: ".", want: "usecases/typo/docs/adr", ok: true,
			},
			"相対で書いても解決して当てる": {
				target: "../typo/docs/adr/0001-a.md#x", fromDir: "usecases/demo", want: "usecases/typo/docs/adr", ok: true,
			},
			"scope の置き場所は当たらない":        {target: "usecases/demo/docs/adr/0001-a.md", fromDir: ".", ok: false},
			"root の置き場所は当たらない":         {target: "docs/adr/0001-a.md", fromDir: ".", ok: false},
			"ADR の置き場所でない path は当たらない": {target: "usecases/typo/README.md", fromDir: ".", ok: false},
			"外部 URL は見ない":              {target: "https://example.com/usecases/typo/docs/adr/0001-a.md", fromDir: ".", ok: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := unknownScopeDir(tt.target, tt.fromDir, dirs)
				assert.Equal(t, tt.ok, ok)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

func Test_adrTarget(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			target  string
			fromDir string
			wantDir string
			want    string
			ok      bool
		}{
			"ルートからの参照": {target: "docs/adr/0001-a.md", fromDir: ".", want: "0001-a.md", ok: true},
			"use-case の ADR ディレクトリはその dir を返す": {
				target: "../../usecases/demo/docs/adr/0001-a.md", fromDir: "docs/adr",
				wantDir: "usecases/demo/docs/adr", want: "0001-a.md", ok: true,
			},
			"入れ子からの相対参照":    {target: "../docs/adr/0001-a.md", fromDir: "scripts", want: "0001-a.md", ok: true},
			"アンカーを落とす":      {target: "docs/adr/0001-a.md#x", fromDir: ".", want: "0001-a.md", ok: true},
			"scheme 付きは見ない": {target: "https://example.com/docs/adr/0001-a.md", fromDir: ".", ok: false},
			"scheme 無しの //": {target: "//docs/adr/0001-a.md", fromDir: ".", ok: false},
			"ルートからの絶対パス":    {target: "/docs/adr/0001-a.md", fromDir: ".", ok: false},
			"別ディレクトリは見ない":   {target: "docs/other/0001-a.md", fromDir: ".", ok: false},
			"より深い階層は見ない":    {target: "docs/adr/sub/0001-a.md", fromDir: ".", ok: false},
			"索引は ADR ではない":  {target: "docs/adr/README.md", fromDir: ".", ok: false},
			"雛形は ADR ではない":  {target: "docs/adr/template.md", fromDir: ".", ok: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dirs := map[string]bool{"docs/adr": true, "usecases/demo/docs/adr": true}
				dir, got, ok := adrTarget(tt.target, tt.fromDir, dirs)
				assert.Equal(t, tt.ok, ok)
				if tt.ok {
					wantDir := tt.wantDir
					if wantDir == "" {
						wantDir = "docs/adr"
					}
					assert.Equal(t, wantDir, dir)
					assert.Equal(t, tt.want, got)
				}
			})
		}
	})
}

func Test_matchReference(t *testing.T) {
	t.Parallel()

	idx := map[int]string{1: "0001-a.md"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct{ text, name string }{
			"番号の文言と指し先が一致":       {text: "0001", name: "0001-a.md"},
			"ADR- を伴う文言":         {text: "ADR-0001", name: "0001-a.md"},
			"番号を名乗らない文言は突き合わせない": {text: "アーキテクチャ原則", name: "0001-a.md"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, matchReference("AGENTS.md", 1, tt.text, tt.name, idx))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			text, name  string
			wantMessage string
		}{
			"ファイル名の形でない": {text: "x", name: "notes.md", wantMessage: "ファイル名の形ではありません"},
			"番号が索引に無い":   {text: "9999", name: "9999-absent.md", wantMessage: "存在しません"},
			"slug が違う":   {text: "0001", name: "0001-renamed.md", wantMessage: "参照先が実在しません"},
			"文言の番号が食い違う": {text: "0002", name: "0001-a.md", wantMessage: "リンクの文言"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := matchReference("AGENTS.md", 7, tt.text, tt.name, idx)
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, 7, findings[0].Line)
			})
		}
	})
}

func Test_collectMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("adr の外の Markdown だけを相対パスで並べ替えて集める", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t,
				map[string]string{"0001-a.md": soundADR},
				map[string]string{
					"README.md":                         "",
					"scripts/README.md":                 "",
					"scripts/main.go":                   "",
					"tmp/note.md":                       "",
					".claude/worktrees/other/AGENTS.md": "",
				})
			files, err := collectMarkdown(repo, "docs/adr")
			require.NoError(t, err)
			assert.Equal(t, []string{"README.md", "scripts/README.md"}, files)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("走査先が無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := collectMarkdown(filepath.Join(t.TempDir(), "absent"), "docs/adr")
			require.Error(t, err)
		})
	})
}

func Test_skipDir(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			rel  string
			want bool
		}{
			"ルートは入る":         {rel: ".", want: false},
			"通常のディレクトリは入る":   {rel: "scripts", want: false},
			"ADR の置き場所は入らない": {rel: "docs/adr", want: true},
			// use-case ADR から root の ADR へのリンクは、ここを読まなければ検査されない。
			"use-case の ADR の置き場所は入る": {rel: "usecases/demo/docs/adr", want: false},
			".git は入らない":              {rel: ".git", want: true},
			"作業場所は入らない":               {rel: "tmp", want: true},
			"別の作業の worktree は入らない":    {rel: ".claude/worktrees", want: true},
			"入れ子の node_modules も入らない": {rel: "scripts/node_modules", want: true},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, skipDir(tt.rel, "docs/adr"))
			})
		}
	})
}

func Test_collect(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ADR をファイル名の昇順で返す", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0002-b.md": soundADRNo("0002"),
				"0001-a.md": soundADR,
			})
			docs, findings, err := collect(root)
			require.NoError(t, err)
			assert.Empty(t, findings)
			require.Len(t, docs, 2)
			assert.Equal(t, []string{"0001-a.md", "0002-b.md"}, []string{docs[0].name, docs[1].name})
			assert.Equal(t, []int{1, 2}, []int{docs[0].number, docs[1].number})
			assert.Equal(t, []string{"a", "b"}, []string{docs[0].slug, docs[1].slug})
			assert.Equal(t, filepath.Join(root, "0001-a.md"), docs[0].path)
			assert.Equal(t, soundADR, docs[0].source)
		})

		t.Run("索引と雛形と Markdown 以外は ADR として読まない", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md":   soundADR,
				"README.md":   index(row("0001", "0001-a.md")),
				"template.md": "# 雛形\n",
				"notes.txt":   "ADR ではない\n",
			})
			docs, findings, err := collect(root)
			require.NoError(t, err)
			assert.Empty(t, findings)
			require.Len(t, docs, 1)
			assert.Equal(t, "0001-a.md", docs[0].name)
		})

		t.Run("ADR の名前をしたディレクトリは読まない", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{"0001-a.md": soundADR})
			require.NoError(t, os.MkdirAll(filepath.Join(root, "0002-dir.md"), 0o750))
			docs, findings, err := collect(root)
			require.NoError(t, err)
			assert.Empty(t, findings)
			require.Len(t, docs, 1)
			assert.Equal(t, "0001-a.md", docs[0].name)
		})

		t.Run("ADR が1件も無ければ違反も無い空を返す", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{"README.md": index()})
			docs, findings, err := collect(root)
			require.NoError(t, err)
			assert.Empty(t, docs)
			assert.Empty(t, findings)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイル名が規約に適合しなければ違反として返し、対象から外す", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"bad_name.md": soundADR,
				"0001-a.md":   soundADR,
			})
			docs, findings, err := collect(root)
			require.NoError(t, err)
			require.Len(t, docs, 1)
			assert.Equal(t, "0001-a.md", docs[0].name)
			require.Len(t, findings, 1)
			assert.Equal(t, filepath.Join(root, "bad_name.md"), findings[0].File)
			assert.Equal(t, 1, findings[0].Line)
			assert.Contains(t, findings[0].Message, "ファイル名が")
		})

		t.Run("読めない ADR があれば黙って飛ばさずエラー", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, nil)
			require.NoError(t, os.Symlink("absent", filepath.Join(root, "0001-a.md")))
			docs, _, err := collect(root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "0001-a.md")
			assert.Nil(t, docs)
		})

		t.Run("ディレクトリが存在しなければエラー", func(t *testing.T) {
			t.Parallel()
			missing := filepath.Join(t.TempDir(), "absent")
			docs, findings, err := collect(missing)
			require.Error(t, err)
			assert.Contains(t, err.Error(), missing)
			assert.Nil(t, docs)
			assert.Nil(t, findings)
		})
	})
}

func Test_checkNumbers(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]doc{
			"番号が重複していない": {
				{name: "0001-a.md", number: 1},
				{name: "0002-b.md", number: 2},
			},
			"ADR が1件もない": nil,
			"ADR が1件だけ": {
				{name: "0001-a.md", number: 1},
			},
		}

		for name, docs := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkNumbers(docs, "docs/adr"))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("同じ番号を名乗るファイルを列挙して報告する", func(t *testing.T) {
			t.Parallel()
			docs := []doc{
				{name: "0001-a.md", number: 1},
				{name: "0001-b.md", number: 1},
				{name: "0001-c.md", number: 1},
			}
			findings := checkNumbers(docs, "docs/adr")
			require.Len(t, findings, 1)
			assert.Equal(t, "docs/adr", findings[0].File)
			assert.Equal(t, 1, findings[0].Line)
			assert.Equal(t, "番号 0001 が重複しています（0001-a.md / 0001-b.md / 0001-c.md）", findings[0].Message)
		})

		t.Run("重複が複数あれば番号の昇順で並べる", func(t *testing.T) {
			t.Parallel()
			docs := []doc{
				{name: "0009-x.md", number: 9},
				{name: "0009-y.md", number: 9},
				{name: "0002-p.md", number: 2},
				{name: "0002-q.md", number: 2},
			}
			findings := checkNumbers(docs, "docs/adr")
			require.Len(t, findings, 2)
			assert.Contains(t, findings[0].Message, "番号 0002")
			assert.Contains(t, findings[1].Message, "番号 0009")
		})
	})
}

func Test_checkSlugs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]doc{
			"slug が重複していない": {
				{name: "0001-a.md", slug: "a"},
				{name: "0002-b.md", slug: "b"},
			},
			"ADR が1件もない": nil,
		}

		for name, docs := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkSlugs(docs, "docs/adr"))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("番号だけが違う同じ slug を列挙して報告する", func(t *testing.T) {
			t.Parallel()
			docs := []doc{
				{name: "0001-a.md", slug: "a"},
				{name: "0002-a.md", slug: "a"},
			}
			findings := checkSlugs(docs, "docs/adr")
			require.Len(t, findings, 1)
			assert.Equal(t, "docs/adr", findings[0].File)
			assert.Equal(t, 1, findings[0].Line)
			assert.Equal(t, "slug a が重複しています（0001-a.md / 0002-a.md）", findings[0].Message)
		})

		t.Run("重複が複数あれば slug の昇順で並べる", func(t *testing.T) {
			t.Parallel()
			docs := []doc{
				{name: "0001-z.md", slug: "z"},
				{name: "0002-z.md", slug: "z"},
				{name: "0003-b.md", slug: "b"},
				{name: "0004-b.md", slug: "b"},
			}
			findings := checkSlugs(docs, "docs/adr")
			require.Len(t, findings, 2)
			assert.Contains(t, findings[0].Message, "slug b")
			assert.Contains(t, findings[1].Message, "slug z")
		})
	})
}

func Test_checkHeading(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]doc{
			"見出しの番号がファイル名と一致する":  {path: "0001-a.md", number: 1, source: soundADR},
			"見出しが本文の途中に在っても見つける": {path: "0002-b.md", number: 2, source: "前書き\n\n# ADR-0002: なにか\n"},
		}

		for name, d := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkHeading([]doc{d}))
			})
		}

		t.Run("ADR が1件もなければ違反も出ない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkHeading(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			d           doc
			wantMessage string
		}{
			"見出しが無い": {
				d:           doc{path: "0001-a.md", number: 1, source: "- Status: Accepted\n"},
				wantMessage: "見出しがありません",
			},
			"見出しが別の番号を名乗る": {
				d:           doc{path: "0001-a.md", number: 1, source: "# ADR-0002: なにか\n"},
				wantMessage: "見出しが ADR-0002 を名乗っていますが、ファイル名の番号は 0001 です",
			},
			"番号が3桁": {
				d:           doc{path: "0001-a.md", number: 1, source: "# ADR-001: なにか\n"},
				wantMessage: "見出しがありません",
			},
			"番号が5桁": {
				d:           doc{path: "0001-a.md", number: 1, source: "# ADR-00011: なにか\n"},
				wantMessage: "見出しがありません",
			},
			"見出しが行頭にない": {
				d:           doc{path: "0001-a.md", number: 1, source: "  # ADR-0001: なにか\n"},
				wantMessage: "見出しがありません",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := checkHeading([]doc{tt.d})
				require.Len(t, findings, 1)
				assert.Equal(t, tt.d.path, findings[0].File)
				assert.Equal(t, 1, findings[0].Line)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
			})
		}
	})
}

func Test_checkMetadata(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"3つとも在る":     soundADR,
			"区切りがタブでも在る": "# ADR-0001: x\n\n- Status:\tAccepted\n- Date:\t2026-09-17\n- Scope:\trepository-wide\n",
			"値の形は問わない":   "# ADR-0001: x\n\n- Status: なんでも\n- Date: いつか\n- Scope: どこか\n",
		}

		for name, source := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkMetadata([]doc{{path: "0001-a.md", number: 1, source: source}}))
			})
		}

		t.Run("ADR が1件もなければ違反も出ない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkMetadata(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			source   string
			wantKeys []string
		}{
			"Status が無い": {
				source:   "# ADR-0001: x\n\n- Date: 2026-09-17\n- Scope: repository-wide\n",
				wantKeys: []string{"`Status`"},
			},
			"Date が無い": {
				source:   "# ADR-0001: x\n\n- Status: Accepted\n- Scope: repository-wide\n",
				wantKeys: []string{"`Date`"},
			},
			"Scope が無い": {
				source:   "# ADR-0001: x\n\n- Status: Accepted\n- Date: 2026-09-17\n",
				wantKeys: []string{"`Scope`"},
			},
			"値が空": {
				source:   "# ADR-0001: x\n\n- Status:\n- Date: 2026-09-17\n- Scope: repository-wide\n",
				wantKeys: []string{"`Status`"},
			},
			"値が空白だけ": {
				source:   "# ADR-0001: x\n\n- Status:   \n- Date: 2026-09-17\n- Scope: repository-wide\n",
				wantKeys: []string{"`Status`"},
			},
			"行頭から始まっていない": {
				source:   "# ADR-0001: x\n\n  - Status: Accepted\n- Date: 2026-09-17\n- Scope: repository-wide\n",
				wantKeys: []string{"`Status`"},
			},
			"3つとも無い": {
				source:   "# ADR-0001: x\n\n本文だけ。\n",
				wantKeys: []string{"`Status`", "`Date`", "`Scope`"},
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := checkMetadata([]doc{{path: "0001-a.md", number: 1, source: tt.source}})
				require.Len(t, findings, len(tt.wantKeys))
				for i, key := range tt.wantKeys {
					assert.Equal(t, "0001-a.md", findings[i].File)
					assert.Equal(t, 1, findings[i].Line)
					assert.Contains(t, findings[i].Message, key)
				}
			})
		}
	})
}

func Test_statusOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   string
		wantOK bool
	}{
		"値の前後の空白を除く":  {source: "- Status:   Accepted  \n", want: "Accepted", wantOK: true},
		"Status 行が無い": {source: "- Date: 2026-09-17\n"},
		"値が空":         {source: "- Status:\n"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := statusOf(doc{source: tt.source})
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_metadataLine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   int
	}{
		"先頭メタデータの Status 行": {source: "# ADR-0001: x\n\n- Status: Accepted\n", want: 2},
		"同じ形の行が続いても最初の行":    {source: "- Status: Accepted\n本文\n- Status: demo/ADR-0001\n", want: 0},
		"Status 行が無い":       {source: "# ADR-0001: x\n", want: -1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, metadataLine(tt.source))
		})
	}
}

func Test_checkStatusValue(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, status := range []string{"Proposed", "Accepted", "Deprecated", "Superseded by repository-wide/a"} {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkStatusValue([]doc{{path: "0001-a.md", source: withStatus(soundADR, status)}}))
			})
		}

		t.Run("Superseded by の形式違反は見ない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkStatusValue([]doc{{path: "0001-a.md", source: withStatus(soundADR, "Superseded by ADR-0002")}}))
		})

		t.Run("Status 行そのものが無い", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkStatusValue([]doc{{path: "0001-a.md", source: "# ADR-0001: x\n\n- Date: 2026-09-17\n"}}))
		})

		t.Run("ADR が1件もない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkStatusValue(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, status := range []string{"Superseded", "Superseded-by repository-wide/a", "Acceptd", "accepted", "Accepted （暫定）"} {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				findings := checkStatusValue([]doc{{path: "0001-a.md", source: withStatus(soundADR, status)}})
				require.Len(t, findings, 1)
				assert.Equal(t, "0001-a.md", findings[0].File)
				assert.Equal(t, 1, findings[0].Line)
				assert.Contains(t, findings[0].Message, "いずれでもありません（"+status+"）")
			})
		}
	})
}

func Test_checkSupersede(t *testing.T) {
	t.Parallel()

	superseded := func(status string) string {
		return withStatus(soundADR, status)
	}
	adr := func(slug, source string) doc {
		return doc{path: "0001-" + slug + ".md", name: "0001-" + slug + ".md", slug: slug, source: source}
	}
	scopes := func(root, demo []doc) []scopeDocs {
		return []scopeDocs{
			{scope: scope{name: rootScope, dir: "docs/adr"}, docs: root},
			{scope: scope{name: "demo", dir: "usecases/demo/docs/adr"}, docs: demo},
		}
	}
	target := adr("b", soundADR)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]scopeDocs{
			"Superseded でない Status は見ない": scopes([]doc{adr("a", soundADR)}, nil),
			"同一 scope の参照先が実在する": scopes(
				[]doc{adr("a", superseded("Superseded by repository-wide/b")), target}, nil),
			"use-case scope から root の参照先を引く": scopes(
				[]doc{target}, []doc{adr("a", superseded("Superseded by repository-wide/b"))}),
			// 宛先の slug が自分と同じでも、scope が違えば別の決定である。
			"他 scope の同じ slug は自分自身ではない": scopes(
				[]doc{target}, []doc{adr("b", superseded("Superseded by repository-wide/b"))}),
			"Status 行そのものが無い": scopes([]doc{adr("a", "# ADR-0001: x\n\n- Date: 2026-09-17\n")}, nil),
			"ADR が1件もない":      nil,
		}

		for name, in := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkSupersede(in))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			in          []scopeDocs
			wantMessage string
		}{
			"同一 scope の参照先が実在しない": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/missing"))}, nil),
				wantMessage: "Superseded by の参照先 repository-wide/missing が存在しません",
			},
			"use-case scope の参照先が root に実在しない": {
				in:          scopes([]doc{target}, []doc{adr("a", superseded("Superseded by repository-wide/missing"))}),
				wantMessage: "Superseded by の参照先 repository-wide/missing が存在しません",
			},
			"宛先の scope がどれにも当たらない": {
				in:          scopes([]doc{adr("a", superseded("Superseded by ecs-web-service/b"))}, nil),
				wantMessage: "Superseded by の宛先の scope ecs-web-service が見つかりません",
			},
			"自分自身を指す": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/a"))}, nil),
				wantMessage: "Superseded by が自分自身を指しています",
			},
			"番号で書いた宛先": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/ADR-0002")), target}, nil),
				wantMessage: "形式が `Superseded by <scope>/<slug>` ではありません",
			},
			"番号を伴うファイル名で書いた宛先": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/0001-b")), target}, nil),
				wantMessage: "参照先 repository-wide/0001-b が存在しません",
			},
			"scope を伴わない形": {
				in:          scopes([]doc{adr("a", superseded("Superseded by b")), target}, nil),
				wantMessage: "形式が",
			},
			"slug が大文字を含む": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/B")), target}, nil),
				wantMessage: "形式が",
			},
			"slug が連続するハイフンを含む": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/b--c")), target}, nil),
				wantMessage: "形式が",
			},
			"宛先が無い": {
				in:          scopes([]doc{adr("a", superseded("Superseded by"))}, nil),
				wantMessage: "形式が",
			},
			"末尾に余分な語が続く": {
				in:          scopes([]doc{adr("a", superseded("Superseded by repository-wide/b （理由）")), target}, nil),
				wantMessage: "形式が",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := checkSupersede(tt.in)
				require.Len(t, findings, 1)
				assert.Equal(t, "0001-a.md", findings[0].File)
				assert.Equal(t, 1, findings[0].Line)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
			})
		}
	})
}

func Test_checkUseCaseDependency(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"プレースホルダは許容する":            "配置は `usecases/<use-case>/docs/adr/` とする。",
			"プレースホルダのディレクトリだけの形も許容する": "`usecases/<use-case>/` に置く。",
			"usecases を含まない本文":        "ここには path が無い。",
			"大文字を含む綴りは当たらない":          "`usecases/EcsWebService/main.tf`",
			// 名前の終端を `/` で確かめない限り、`usecases/ecs_web` が `usecases/ecs` として当たる。
			"末尾の / を伴わない名前だけの形は当たらない": "`usecases/ecs-web-service`",
			"語の途中から始まる綴りは当たらない":       "`myusecases/foo/bar`",
			"kebab の接頭辞を持つ綴りは当たらない":   "`old-usecases/foo/bar`",
			// 一般 module（ADR-0102 決定12）はユースケースではない。特定ユースケースへの依存を見る検査の外にある。
			"一般 module の path は当たらない":   "`modules/s3/main.tf`",
			"旧い modules/<名前>/ の形は当たらない": "`modules/ecs-web-service/main.tf`",
			"snake_case の名前は当たらない":      "`usecases/ecs_web_service/main.tf`",
			"glob だけで名前が無い形は当たらない":      "`usecases/*/main.tf`",
		}

		for name, text := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkUseCaseDependency([]doc{{path: "0001-a.md", number: 1, source: soundADR + "\n" + text + "\n"}}))
			})
		}

		t.Run("ADR が1件もなければ違反も出ない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, checkUseCaseDependency(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("特定のユースケースの path を該当行で報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "一行目\n二行目\n`usecases/ecs-web-service/main.tf` を参照する。\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, "0001-a.md", findings[0].File)
			assert.Equal(t, 3, findings[0].Line)
			assert.Contains(t, findings[0].Message, "特定ユースケースの path")
			assert.Contains(t, findings[0].Message, "usecases/ecs-web-service/main.tf")
		})

		t.Run("同じ行に2箇所あれば2件返す", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "usecases/a/main.tf と usecases/b/main.tf\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 2)
			assert.Equal(t, 1, findings[0].Line)
			assert.Equal(t, 1, findings[1].Line)
			assert.Contains(t, findings[0].Message, "（usecases/a/main.tf）")
			assert.Contains(t, findings[1].Message, "（usecases/b/main.tf）")
		})

		t.Run("バッククォートの無い path は path だけを報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "usecases/ecs/x.tf を参照する。\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "（usecases/ecs/x.tf）")
		})

		t.Run("ユースケースのディレクトリだけを名指ししても報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "`usecases/ecs-web-service/` を参照する。\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "（usecases/ecs-web-service/）")
		})

		t.Run("相対パスで書いた path は境界の文字を含めずに報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "[x](../../usecases/ecs/main.tf)\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "（usecases/ecs/main.tf）")
		})

		t.Run("括弧の直後の path も報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "(usecases/ecs/main.tf)\n"}
			require.Len(t, checkUseCaseDependency([]doc{d}), 1)
		})

		t.Run("プレースホルダと実名が混在しても実名だけを報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "`usecases/<use-case>/main.tf`\n`usecases/ecs/main.tf`\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, 2, findings[0].Line)
		})
	})
}

func Test_checkCrossReferences(t *testing.T) {
	t.Parallel()

	other := doc{path: "0002-b.md", number: 2, source: soundADRNo("0002")}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]doc{
			"実在する ADR への参照": {
				{path: "0001-a.md", number: 1, source: soundADR + "\nADR-0002 を参照する。\n"},
				other,
			},
			"自分自身への言及は参照として見ない": {
				{path: "0001-a.md", number: 1, source: soundADR + "\nADR-0001 は本文書である。\n"},
			},
			"scope を伴う参照は見ない": {
				{path: "0001-a.md", number: 1, source: soundADR + "\necs-web-service/ADR-0099 を参照する。\n"},
			},
			"Status 行の宛先は見ない": {
				{path: "0001-a.md", number: 1, source: withStatus(soundADR, "Superseded by ADR-0099")},
			},
			"語の途中に現れる形は参照ではない": {
				{path: "0001-a.md", number: 1, source: soundADR + "\nxADR-0099\n"},
			},
			"ADR が1件もない": nil,
		}

		for name, docs := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkCrossReferences(docs))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("存在しない参照先を該当行で報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "一行目\nADR-0099 を参照する。\n"}
			findings := checkCrossReferences([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, "0001-a.md", findings[0].File)
			assert.Equal(t, 2, findings[0].Line)
			assert.Equal(t, "参照先の ADR-0099 が存在しません", findings[0].Message)
		})

		t.Run("メタデータの後に続く Status 形の行は本文として見る", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: soundADR + "\n- Status: ADR-0099\n"}
			findings := checkCrossReferences([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, strings.Count(soundADR, "\n")+2, findings[0].Line)
		})

		t.Run("行頭の参照も見る", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "ADR-0099\n"}
			findings := checkCrossReferences([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, 1, findings[0].Line)
		})

		t.Run("同じ行に複数あればすべて返す", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "ADR-0098 と ADR-0099\n"}
			findings := checkCrossReferences([]doc{d})
			require.Len(t, findings, 2)
			assert.Contains(t, findings[0].Message, "ADR-0098")
			assert.Contains(t, findings[1].Message, "ADR-0099")
		})

		t.Run("他の ADR が実在すれば通り、しなければ落ちる", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "ADR-0002\n"}
			assert.Empty(t, checkCrossReferences([]doc{d, other}))
			assert.Len(t, checkCrossReferences([]doc{d}), 1)
		})
	})
}

func Test_checkScopedReferences(t *testing.T) {
	t.Parallel()

	scopes := func(root, demo []doc) []scopeDocs {
		return []scopeDocs{
			{scope: scope{name: rootScope, dir: "docs/adr"}, docs: root},
			{scope: scope{name: "demo", dir: "usecases/demo/docs/adr"}, docs: demo},
		}
	}
	rootADR := doc{path: "0001-a.md", number: 1, source: soundADR}
	rootOnlyADR := doc{path: "0002-b.md", number: 2, source: soundADRNo("0002")}
	body := func(text string) []doc {
		return []doc{{path: "0001-x.md", number: 1, source: "本文\n" + text + "\n"}}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]scopeDocs{
			"他 scope に実在する":      scopes([]doc{rootADR}, body("repository-wide/ADR-0001 を前提とする。")),
			"同じ scope を前置して実在する": scopes([]doc{rootADR}, body("demo/ADR-0001")),
			// 番号が宛先の scope にだけ在る。自 scope で引けば落ちる。
			"宛先の scope にだけ在る番号を引く": scopes([]doc{rootADR, rootOnlyADR}, body("repository-wide/ADR-0002")),
			"scope を伴わない参照は見ない":    scopes([]doc{rootADR}, body("ADR-0099")),
			"メタデータの Status 行は読まない": scopes([]doc{rootADR}, []doc{{path: "0001-x.md", number: 1, source: withStatus(soundADR, "Superseded by nowhere/ADR-0099")}}),
			"ADR が1件もない":           nil,
		}

		for name, in := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings, _, err := checkScopedReferences(in)
				require.NoError(t, err)
				assert.Empty(t, findings)
			})
		}

		t.Run("取り出した参照の件数を返す", func(t *testing.T) {
			t.Parallel()
			_, refs, err := checkScopedReferences(scopes([]doc{rootADR}, body("repository-wide/ADR-0001 と demo/ADR-0001")))
			require.NoError(t, err)
			assert.Equal(t, 2, refs)
		})

		// scope を伴う参照が0件の木は正常である。番兵は抽出との食い違いにだけ立つ。
		t.Run("参照を含む行が無ければ0件で通る", func(t *testing.T) {
			t.Parallel()
			_, refs, err := checkScopedReferences(scopes([]doc{rootADR}, body("ADR-0001")))
			require.NoError(t, err)
			assert.Zero(t, refs)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			in          []scopeDocs
			wantMessage string
		}{
			"メタデータの後に続く Status 形の行は本文として見る": {
				in:          scopes([]doc{rootADR}, []doc{{path: "0001-x.md", number: 1, source: "- Status: Accepted\n- Status: repository-wide/ADR-0099\n"}}),
				wantMessage: "参照先の repository-wide/ADR-0099 が存在しません",
			},
			"宛先の scope に番号が無い": {
				in:          scopes([]doc{rootADR}, body("repository-wide/ADR-0099")),
				wantMessage: "参照先の repository-wide/ADR-0099 が存在しません",
			},
			"宛先の scope がどれにも当たらない": {
				in:          scopes([]doc{rootADR}, body("ecs-web-service/ADR-0001")),
				wantMessage: "参照先 ecs-web-service/ADR-0001 の scope ecs-web-service が見つかりません",
			},
			"全角括弧の直後の参照も取り出す": {
				in:          scopes([]doc{rootADR}, body("（repository-wide/ADR-0099）")),
				wantMessage: "参照先の repository-wide/ADR-0099 が存在しません",
			},
			// 番号が別の scope にだけ在る。宛先でなく全 scope で引けば通ってしまう。
			"宛先の scope に無い番号は他 scope に在っても落とす": {
				in:          scopes([]doc{rootADR, rootOnlyADR}, body("demo/ADR-0002")),
				wantMessage: "参照先の demo/ADR-0002 が存在しません",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings, _, err := checkScopedReferences(tt.in)
				require.NoError(t, err)
				require.Len(t, findings, 1)
				assert.Equal(t, "0001-x.md", findings[0].File)
				assert.Equal(t, 2, findings[0].Line)
				assert.Equal(t, tt.wantMessage, findings[0].Message)
			})
		}
	})

	t.Run("参照を含む行から取り出しきれなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		for _, text := range []string{"a.b/ADR-0099", "x.repository-wide/ADR-0001", "demo/ADR-0001 と a.b/ADR-0001"} {
			_, _, err := checkScopedReferences(scopes([]doc{rootADR}, body(text)))
			require.ErrorIs(t, err, errNoScopedRef, text)
		}
	})

	t.Run("path と URL と雛形は参照として数えない", func(t *testing.T) {
		t.Parallel()
		for _, text := range []string{"docs/adr/ADR-0099", "https://example.com/foo/ADR-0099", "<scope>/ADR-0099", "*demo*/ADR-0001"} {
			_, refs, err := checkScopedReferences(scopes([]doc{rootADR}, body(text)))
			require.NoError(t, err, text)
			assert.Zero(t, refs, text)
		}
	})

	t.Run("取り出せる参照が別の行に在っても、取り出せない行があれば番兵で返す", func(t *testing.T) {
		t.Parallel()
		_, _, err := checkScopedReferences(scopes([]doc{rootADR}, body("repository-wide/ADR-0001\na.b/ADR-0001")))
		require.ErrorIs(t, err, errNoScopedRef)
		assert.Contains(t, err.Error(), "0001-x.md:3")
	})

	t.Run("adrRef と同じ出現に当たらない", func(t *testing.T) {
		t.Parallel()
		line := "ADR-0001 と repository-wide/ADR-0002"
		plain := adrRef.FindAllStringSubmatch(line, -1)
		scoped := scopedRef.FindAllStringSubmatch(line, -1)
		require.Len(t, plain, 1)
		require.Len(t, scoped, 1)
		assert.Equal(t, "0001", plain[0][2])
		assert.Equal(t, "0002", scoped[0][2])
	})
}

func Test_scopedMentions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want int
	}{
		"scope を伴う参照":        {line: "repository-wide/ADR-0001 を前提とする。", want: 1},
		"同じ行の複数の参照":          {line: "demo/ADR-0001 と a.b/ADR-0002", want: 2},
		"左が . の名前も数える":       {line: "a.b/ADR-0001", want: 1},
		"scope を伴わない参照":      {line: "ADR-0001", want: 0},
		"path の一部は数えない":      {line: "docs/adr/ADR-0001", want: 0},
		"URL の path は数えない":   {line: "https://example.com/foo/ADR-0001", want: 0},
		"雛形の scope は数えない":    {line: "<scope>/ADR-0001", want: 0},
		"行頭の /ADR- は数えない":    {line: "/ADR-0001", want: 0},
		"番号が4桁に満たない":         {line: "a/ADR-001", want: 0},
		"4桁目が数字でない":          {line: "a/ADR-001x", want: 0},
		"番号の代わりに雛形の記法":       {line: "a/ADR-NNNN", want: 0},
		"数えない出現の後ろに在る参照も数える": {line: "docs/adr/ADR-0001 と a/ADR-0002", want: 1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, scopedMentions(tt.line))
		})
	}
}

func Test_isScopeChar(t *testing.T) {
	t.Parallel()

	for _, b := range []byte("azAZ09_-") {
		t.Run("受理する "+string(b), func(t *testing.T) {
			t.Parallel()
			assert.True(t, isScopeChar(b))
		})
	}
	for _, b := range []byte("./<>* ") {
		t.Run("受理しない "+string(b), func(t *testing.T) {
			t.Parallel()
			assert.False(t, isScopeChar(b))
		})
	}
}

func Test_checkIndex(t *testing.T) {
	t.Parallel()

	docsOf := func(root string, names ...string) []doc {
		out := make([]doc, 0, len(names))
		for i, name := range names {
			out = append(out, doc{path: filepath.Join(root, name), name: name, number: i + 1, source: soundADR})
		}
		return out
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("索引が実ファイルと一致していれば違反は無い", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"0002-b.md": soundADRNo("0002"),
				"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-b.md")),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md", "0002-b.md"))
			require.NoError(t, err)
			assert.Empty(t, findings)
		})

		t.Run("タイトルに `](` や `|` を含んでも Status 列を取り違えない", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index("| [0001](0001-a.md) | [x](y) と a \\| b | Accepted |"),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			assert.Empty(t, findings)
		})

		t.Run("改行が CRLF の索引でも Status 列を読む", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": strings.ReplaceAll(index(row("0001", "0001-a.md")), "\n", "\r\n"),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			assert.Empty(t, findings)
		})

		t.Run("本文に Status が無ければ索引の Status を比べない", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(rowWithStatus("0001", "0001-a.md", "Proposed")),
			})
			docs := []doc{{path: filepath.Join(root, "0001-a.md"), name: "0001-a.md", number: 1, source: "# ADR-0001: x\n"}}
			findings, err := checkIndex(root, docs)
			require.NoError(t, err)
			assert.Empty(t, findings)
		})

		t.Run("索引も ADR も空なら違反は無い", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{"README.md": index()})
			findings, err := checkIndex(root, nil)
			require.NoError(t, err)
			assert.Empty(t, findings)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("索引に載っていなければ落とす", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Equal(t, filepath.Join(root, "README.md"), findings[0].File)
			assert.Equal(t, 1, findings[0].Line)
			assert.Equal(t, "0001-a.md が索引に載っていません", findings[0].Message)
		})

		t.Run("索引が別のファイルを指していれば落とす", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(row("0001", "0001-old-name.md")),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Equal(t, 5, findings[0].Line)
			assert.Equal(t, "索引の ADR-0001 が 0001-old-name.md を指していますが、実ファイルは 0001-a.md です", findings[0].Message)
		})

		t.Run("ファイル名が食い違う行では Status を比べない", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(rowWithStatus("0001", "0001-old-name.md", "Proposed")),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "実ファイルは 0001-a.md です")
		})

		t.Run("ファイル名が食い違う行は Status 列が読めなくてもファイル名だけを報告する", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index("| [0001](0001-old-name.md) | なにか | |"),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "実ファイルは 0001-a.md です")
		})

		t.Run("Status が食い違えば行の行番号で落とす", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"0002-b.md": soundADRNo("0002"),
				"README.md": index(row("0001", "0001-a.md"), rowWithStatus("0002", "0002-b.md", "Proposed")),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md", "0002-b.md"))
			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Equal(t, filepath.Join(root, "README.md"), findings[0].File)
			assert.Equal(t, 6, findings[0].Line)
			assert.Equal(t, `索引の ADR-0002 の Status は "Proposed" ですが、本文は "Accepted" です`, findings[0].Message)
		})

		t.Run("同じ番号の行が重複すれば後の行を落とし、最初の行を突き合わせる", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(rowWithStatus("0001", "0001-a.md", "Proposed"), row("0001", "0001-a.md")),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 2)
			assert.Equal(t, 6, findings[0].Line)
			assert.Equal(t, "索引に ADR-0001 の行が重複しています（最初の行は 5 行目）", findings[0].Message)
			assert.Equal(t, 5, findings[1].Line)
			assert.Contains(t, findings[1].Message, `Status は "Proposed"`)
		})

		unreadable := map[string]string{
			"リンクのセルしか無い":         "| [0001](0001-a.md) |",
			"Status 列を持たない2セルの行": "| [0001](0001-a.md) | なにか |",
			"Status のセルが空":       "| [0001](0001-a.md) | なにか | |",
			"行末の | が無い":          "| [0001](0001-a.md) | なにか | Accepted",
		}

		for name, r := range unreadable {
			t.Run(name+"なら Status 列を読めない行として落とす", func(t *testing.T) {
				t.Parallel()
				root := writeADRs(t, map[string]string{
					"0001-a.md": soundADR,
					"README.md": index(r),
				})
				findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
				require.NoError(t, err)
				require.Len(t, findings, 1)
				assert.Equal(t, 5, findings[0].Line)
				assert.Equal(t, "索引の ADR-0001 の行から Status 列を読めません", findings[0].Message)
			})
		}

		t.Run("実体の無い行は番号の昇順で残らず報告する", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{
				"0001-a.md": soundADR,
				"README.md": index(
					row("0001", "0001-a.md"),
					row("0009", "0009-gone.md"),
					row("0003", "0003-gone.md"),
				),
			})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.NoError(t, err)
			require.Len(t, findings, 2)
			assert.Equal(t, 7, findings[0].Line)
			assert.Equal(t, 6, findings[1].Line)
			assert.Equal(t, "索引の ADR-0003（0003-gone.md）に対応する実ファイルがありません", findings[0].Message)
			assert.Equal(t, "索引の ADR-0009（0009-gone.md）に対応する実ファイルがありません", findings[1].Message)
		})

		t.Run("索引が読めなければエラー", func(t *testing.T) {
			t.Parallel()
			root := writeADRs(t, map[string]string{"0001-a.md": soundADR})
			findings, err := checkIndex(root, docsOf(root, "0001-a.md"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "README.md")
			assert.Nil(t, findings)
		})
	})
}
