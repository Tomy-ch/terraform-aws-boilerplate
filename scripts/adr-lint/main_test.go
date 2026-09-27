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

// soundADRNo は番号を差し替えた健全な ADR を返します。見出しが名乗る番号は
// ファイル名の番号と一致していなければならないため、0001 以外のファイルはこれを使います。
func soundADRNo(number string) string {
	return strings.Replace(soundADR, "# ADR-0001:", "# ADR-"+number+":", 1)
}

// writeADRs はリポジトリの実物ではなく一時ディレクトリへ検査対象を組み立て、ADR の置き場所を
// 返します。実物を読むテストは、今日の ADR の内容で通ったり落ちたりするようになります。
//
// ADR を docs/adr/ の下へ置き、その外へ README.md を1つ置くのは、**検査8 が「外から指すリンク」を
// 主題にしている**ためです。外が存在しない木は、この道具が想定する形ではありません。
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

// repoOf は、ADR の置き場所からリポジトリのルートを返します。
func repoOf(root string) string {
	return filepath.Dir(filepath.Dir(root))
}

// args は run へ渡す既定の引数です。-repo を省くと既定の "." が効き、テストが実物の木を読みます。
func args(root string) []string {
	return []string{"-root", root, "-repo", repoOf(root)}
}

// index は、渡した ADR をすべて載せた索引を組み立てます。
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
	return "| [" + num + "](" + file + ") | なにか | Accepted |"
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

// 「ADR が1件も無い」と「全てのファイル名が規約違反で外れた」を区別する。
// 後者を番兵で返すと、直すべき違反が出力されないまま終わる。
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

	superseded := func(status string) string {
		return "# ADR-0001: x\n\n- Status: " + status + "\n- Date: 2026-09-17\n- Scope: repository-wide\n"
	}

	t.Run("参照先が在れば通る", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by repository-wide/ADR-0002"),
			"0002-b.md": soundADRNo("0002"),
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-b.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("参照先が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		// 参照先が無いまま supersede を宣言すると、読み手は置き換わった先へ到達できない。
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by repository-wide/ADR-0099"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "参照先 ADR-0099")
	})

	t.Run("scope を伴わない形は落とす", func(t *testing.T) {
		t.Parallel()
		// identity は <scope>/<slug>（ADR-0001 決定4）。scope が無いと、
		// 別 scope の同番号 ADR と区別できない。
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by ADR-0002"),
			"0002-b.md": soundADRNo("0002"),
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-b.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "形式が")
	})

	t.Run("他 scope の参照先は実在を見ない", func(t *testing.T) {
		t.Parallel()
		// 他 scope の ADR はこのディレクトリに無い。見に行くと、必ず落ちる検査になる。
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by ecs-web-service/ADR-0002"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})
}

func Test_ユースケースへの依存(t *testing.T) {
	t.Parallel()

	body := func(text string) string {
		return soundADR + "\n" + text + "\n"
	}

	t.Run("プレースホルダは許容する", func(t *testing.T) {
		t.Parallel()
		// `modules/<use-case>/` は特定のユースケースを名指ししていない。
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("配置は `modules/<use-case>/docs/adr/` とする。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.NoError(t, run(args(root), &out))
	})

	t.Run("特定のユースケースを名指しすれば落ちる", func(t *testing.T) {
		t.Parallel()
		// 依存の向きは root → use-case の一方向に限る（ADR-0001 決定17-19）。
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("`modules/ecs-web-service/main.tf` を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
		assert.Contains(t, out.String(), "特定ユースケースの path")
	})

	t.Run("違反の行番号は該当行を指す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("一行目\n`modules/ecs/x.tf`"),
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
		// 索引は ADR の一覧が存在する唯一の場所。載っていない決定へ読み手は到達できない。
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

	t.Run("索引が無ければエラー", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{"0001-a.md": soundADR})
		var out bytes.Buffer
		require.Error(t, run(args(root), &out))
	})
}

// 番号が identity ではなく順序になったため、参照の実在は機械で見るほかない（ADR-0001 決定5-9）。
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
		// 詰め直しで参照の更新を落とすと、この形で現れる。
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
		// 見出し（`# ADR-0001: …`）や「本ADR」の言い換えは参照ではない。
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

func Test_相互参照_scopeを伴う参照は見ない(t *testing.T) {
	t.Parallel()

	// identity は `<scope>/<slug>`（ADR-0001 決定4）。scope を書いた参照はこちらの番号体系の
	// 外を指しており、見に行けば必ず落ちる検査になる。
	root := writeADRs(t, map[string]string{
		"0001-a.md": soundApexADR("ecs-web-service/ADR-0099 を参照する。"),
		"README.md": index(row("0001", "0001-a.md")),
	})
	var out bytes.Buffer
	require.NoError(t, run(args(root), &out))
}

func soundApexADR(text string) string {
	return soundADR + "\n" + text + "\n"
}

// writeRepo は、docs/adr/ とその外の文書を持つリポジトリを組み立て、ルートを返します。
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
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := writeRepo(t, adrs, tt.outside)
				findings, refs, err := checkPathReferences(repo, filepath.Join(repo, "docs", "adr"), docs)
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
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := writeRepo(t, adrs, tt.outside)
				findings, _, err := checkPathReferences(repo, filepath.Join(repo, "docs", "adr"), docs)
				require.NoError(t, err)
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, "AGENTS.md", findings[0].File)
				assert.Equal(t, 1, findings[0].Line)
			})
		}

		t.Run("ADR を名指ししているのにリンクを取り出せなければ番兵で返す", func(t *testing.T) {
			t.Parallel()
			// reference-style link は markdownLink が拾えない。解析が対応しない書式へ文書が
			// 寄った日に「参照 0 件」で緑を返さないことを固定する（ADR-0702 決定15）。
			repo := writeRepo(t, adrs, map[string]string{
				"AGENTS.md": "決定は [ADR][ref] を見よ。\n\n[ref]: docs/adr/0001-a.md\n",
			})
			_, _, err := checkPathReferences(repo, filepath.Join(repo, "docs", "adr"), docs)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoReference))
		})

		t.Run("外に Markdown が1件も無ければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			repo := writeRepo(t, adrs, nil)
			_, _, err := checkPathReferences(repo, filepath.Join(repo, "docs", "adr"), docs)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoMarkdown))
		})
	})
}

func Test_mentionsADR(t *testing.T) {
	t.Parallel()

	idx := map[int]string{1: "0001-a.md"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			want bool
		}{
			"リンクの外でも実在する ADR への道筋を拾う": {line: "[ref]: docs/adr/0001-a.md", want: true},
			"通常のリンクも拾う":               {line: "[0001](docs/adr/0001-a.md)", want: true},
			"別のディレクトリの同名は拾わない":        {line: "[x](docs/other/0001-a.md)", want: false},
			"実在しない ADR は拾わない":         {line: "[x](docs/adr/9999-absent.md)", want: false},
			"索引は ADR ではない":            {line: "[索引](docs/adr/README.md)", want: false},
			"名指しが無ければ拾わない":            {line: "ただの散文", want: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, mentionsADR(tt.line, "docs/adr", idx))
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
			want    string
			ok      bool
		}{
			"ルートからの参照":      {target: "docs/adr/0001-a.md", fromDir: ".", want: "0001-a.md", ok: true},
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
				got, ok := adrTarget(tt.target, tt.fromDir, "docs/adr")
				assert.Equal(t, tt.ok, ok)
				if tt.ok {
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
			"ルートは入る":                  {rel: ".", want: false},
			"通常のディレクトリは入る":            {rel: "scripts", want: false},
			"ADR の置き場所は入らない":          {rel: "docs/adr", want: true},
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

		// 「1件も無い」と「全件が規約違反で外れた」は、どちらも docs が空になる。
		// ここを空として固定しておかないと、番兵を立てる側（run）が両者を区別できない。
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
			// 同じ番号が2つ在ると、`ADR-0001` という参照がどちらを指すか決まらない。
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
			// 番号は4桁。前後1桁ずれた形は見出しとして認めない。
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

func Test_checkSupersede(t *testing.T) {
	t.Parallel()

	// superseded は Status 行だけを差し替えた ADR を返します。
	superseded := func(status string) string {
		return "# ADR-0001: x\n\n- Status: " + status + "\n- Date: 2026-09-17\n- Scope: repository-wide\n"
	}
	target := doc{path: "0002-b.md", name: "0002-b.md", number: 2, source: soundADRNo("0002")}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]doc{
			"Superseded でない Status は見ない": {
				{path: "0001-a.md", number: 1, source: soundADR},
			},
			"同一 scope の参照先が実在する": {
				{path: "0001-a.md", number: 1, source: superseded("Superseded by repository-wide/ADR-0002")},
				target,
			},
			// 他 scope の ADR はこのディレクトリに無い。見に行けば必ず落ちる検査になる。
			"他 scope の参照先は実在を見ない": {
				{path: "0001-a.md", number: 1, source: superseded("Superseded by ecs-web-service/ADR-0099")},
			},
			// Status の不在は checkMetadata が報告済み。ここで二重に出さない。
			"Status 行そのものが無い": {
				{path: "0001-a.md", number: 1, source: "# ADR-0001: x\n\n- Date: 2026-09-17\n"},
			},
			"ADR が1件もない": nil,
		}

		for name, docs := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, checkSupersede(docs, "docs/adr"))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			docs        []doc
			wantMessage string
		}{
			"同一 scope の参照先が実在しない": {
				docs:        []doc{{path: "0001-a.md", number: 1, source: superseded("Superseded by repository-wide/ADR-0099")}},
				wantMessage: "Superseded by の参照先 ADR-0099 が docs/adr に存在しません",
			},
			"scope を伴わない形": {
				docs:        []doc{{path: "0001-a.md", number: 1, source: superseded("Superseded by ADR-0002")}, target},
				wantMessage: "形式が",
			},
			"番号が3桁": {
				docs:        []doc{{path: "0001-a.md", number: 1, source: superseded("Superseded by repository-wide/ADR-002")}, target},
				wantMessage: "形式が",
			},
			"番号が5桁": {
				docs:        []doc{{path: "0001-a.md", number: 1, source: superseded("Superseded by repository-wide/ADR-00021")}, target},
				wantMessage: "形式が",
			},
			"末尾に余分な語が続く": {
				docs:        []doc{{path: "0001-a.md", number: 1, source: superseded("Superseded by repository-wide/ADR-0002 （理由）")}, target},
				wantMessage: "形式が",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := checkSupersede(tt.docs, "docs/adr")
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
			// `modules/<use-case>/` は特定のユースケースを名指ししていない。
			"プレースホルダは許容する":    "配置は `modules/<use-case>/docs/adr/` とする。",
			"modules を含まない本文": "ここには path が無い。",
			// use-case 名は kebab-case。大文字を含む綴りはユースケースの path ではない。
			"大文字を含む綴りは当たらない": "`modules/EcsWebService/main.tf`",
			"配下を伴わない形は当たらない": "`modules/ecs-web-service/`",
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
			// 依存の向きは root → use-case の一方向に限る。
			d := doc{path: "0001-a.md", number: 1, source: "一行目\n二行目\n`modules/ecs-web-service/main.tf` を参照する。\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, "0001-a.md", findings[0].File)
			assert.Equal(t, 3, findings[0].Line)
			assert.Contains(t, findings[0].Message, "特定ユースケースの path")
			assert.Contains(t, findings[0].Message, "modules/ecs-web-service/main.tf")
		})

		t.Run("同じ行に2箇所あれば2件返す", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "modules/a/main.tf と modules/b/main.tf\n"}
			findings := checkUseCaseDependency([]doc{d})
			require.Len(t, findings, 2)
			assert.Equal(t, 1, findings[0].Line)
			assert.Equal(t, 1, findings[1].Line)
		})

		t.Run("プレースホルダと実名が混在しても実名だけを報告する", func(t *testing.T) {
			t.Parallel()
			d := doc{path: "0001-a.md", number: 1, source: "`modules/<use-case>/main.tf`\n`modules/ecs/main.tf`\n"}
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
			// 見出しや「本ADR」の言い換えは参照ではない。
			"自分自身への言及は参照として見ない": {
				{path: "0001-a.md", number: 1, source: soundADR + "\nADR-0001 は本文書である。\n"},
			},
			// identity は <scope>/<slug>。scope を書いた参照はこちらの番号体系の外を指す。
			"scope を伴う参照は見ない": {
				{path: "0001-a.md", number: 1, source: soundADR + "\necs-web-service/ADR-0099 を参照する。\n"},
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
			// 詰め直しで参照の更新を落とすと、この形で現れる。
			d := doc{path: "0001-a.md", number: 1, source: "一行目\nADR-0099 を参照する。\n"}
			findings := checkCrossReferences([]doc{d})
			require.Len(t, findings, 1)
			assert.Equal(t, "0001-a.md", findings[0].File)
			assert.Equal(t, 2, findings[0].Line)
			assert.Equal(t, "参照先の ADR-0099 が存在しません", findings[0].Message)
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

func Test_checkIndex(t *testing.T) {
	t.Parallel()

	// docsOf は、索引と突き合わせる ADR の一覧を組み立てます。
	docsOf := func(root string, names ...string) []doc {
		out := make([]doc, 0, len(names))
		for i, name := range names {
			out = append(out, doc{path: filepath.Join(root, name), name: name, number: i + 1})
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

		// 索引も ADR も空なら突合する対象が無い。ここを緑のまま終えないための番兵は
		// run が errNoADR で立てており、この関数は空を返すのが正しい。
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
			// 索引は ADR の一覧が存在する唯一の場所。載っていない決定へ読み手は到達できない。
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
			assert.Equal(t, "索引の ADR-0001 が 0001-old-name.md を指していますが、実ファイルは 0001-a.md です", findings[0].Message)
		})

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
