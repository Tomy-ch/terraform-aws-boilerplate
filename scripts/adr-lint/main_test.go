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

// writeADRs はリポジトリの実物ではなく一時ディレクトリへ検査対象を組み立てます。
// 実物を読むテストは、今日の ADR の内容で通ったり落ちたりするようになります。
func writeADRs(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}
	return root
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
		require.NoError(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "ADR 1 件")
	})

	t.Run("ADR が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{"README.md": index()})
		var out bytes.Buffer
		err := run([]string{"-root", root}, &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errNoADR))
	})

	t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", filepath.Join(t.TempDir(), "missing")}, &out))
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
			err := run([]string{"-root", root}, &out)
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
	err := run([]string{"-root", root}, &out)
	require.Error(t, err)
	assert.False(t, xerrors.Is(err, errNoADR))
	assert.Contains(t, out.String(), "ファイル名が")
}

func Test_番号の重複(t *testing.T) {
	t.Parallel()

	// 番号は採番後に再利用しない（ADR-0001 決定4）。同じ番号が2つ在ると、
	// `ADR-0001` という参照がどちらを指すか決まらない。
	root := writeADRs(t, map[string]string{
		"0001-a.md": soundADR,
		"0001-b.md": soundADR,
		"README.md": index(row("0001", "0001-a.md")),
	})
	var out bytes.Buffer
	require.Error(t, run([]string{"-root", root}, &out))
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
			require.Error(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
	})

	t.Run("参照先が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		// 参照先が無いまま supersede を宣言すると、読み手は置き換わった先へ到達できない。
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by repository-wide/ADR-0099"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "参照先 ADR-0099")
	})

	t.Run("scope を伴わない形は落とす", func(t *testing.T) {
		t.Parallel()
		// identity は <scope>/ADR-<number>（ADR-0001 決定11）。scope が無いと、
		// 別 scope の同番号 ADR と区別できない。
		root := writeADRs(t, map[string]string{
			"0001-a.md": superseded("Superseded by ADR-0002"),
			"0002-b.md": soundADRNo("0002"),
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-b.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
	})

	t.Run("特定のユースケースを名指しすれば落ちる", func(t *testing.T) {
		t.Parallel()
		// 依存の向きは root → use-case の一方向に限る（ADR-0001 決定13-15）。
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("`modules/ecs-web-service/main.tf` を参照する。"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "特定ユースケースの path")
	})

	t.Run("違反の行番号は該当行を指す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": body("一行目\n`modules/ecs/x.tf`"),
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
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
		require.Error(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "索引に載っていません")
	})

	t.Run("索引が別のファイルを指していれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-old-name.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "実ファイルは 0001-a.md です")
	})

	t.Run("索引に実体の無い行が残っていれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR,
			"README.md": index(row("0001", "0001-a.md"), row("0002", "0002-gone.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
		assert.Contains(t, out.String(), "対応する実ファイルがありません")
	})

	t.Run("索引が無ければエラー", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{"0001-a.md": soundADR})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
	})

	t.Run("存在しない ADR への参照は落とす", func(t *testing.T) {
		t.Parallel()
		// 詰め直しで参照の更新を落とすと、この形で現れる。
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR + "\nADR-0099 を参照する。\n",
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
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
		require.NoError(t, run([]string{"-root", root}, &out))
	})

	t.Run("違反の行番号は参照している行を指す", func(t *testing.T) {
		t.Parallel()
		root := writeADRs(t, map[string]string{
			"0001-a.md": soundADR + "\nADR-0099\n",
			"README.md": index(row("0001", "0001-a.md")),
		})
		var out bytes.Buffer
		require.Error(t, run([]string{"-root", root}, &out))
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
	require.NoError(t, run([]string{"-root", root}, &out))
}

func soundApexADR(text string) string {
	return soundADR + "\n" + text + "\n"
}
