package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// writeWorkflows はリポジトリの実物ではなく一時ディレクトリへ検査対象を組み立てます。
func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return dir
}

const commentingJob = `jobs:
  lint:
    steps:
      - uses: ./.github/actions/upsert-pr-comment
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
`

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("GITHUB_TOKEN だけなら通る", func(t *testing.T) {
		t.Parallel()
		dir := writeWorkflows(t, map[string]string{"a.yaml": commentingJob})
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", dir}, &out))
		assert.Contains(t, out.String(), "コメント投稿 job 1 件")
	})

	t.Run("他の secret を渡していれば落ちる", func(t *testing.T) {
		t.Parallel()
		// マスキングは runner が log へ捕える経路しか覆わない。tee で書いたバイトは通らず、
		// log でマスクされて見える値が public なコメントに生で載る。
		src := commentingJob + "          token: ${{ secrets.SLACK_TOKEN }}\n"
		dir := writeWorkflows(t, map[string]string{"a.yaml": src})
		var out bytes.Buffer
		err := run([]string{"-workflows", dir}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "`secrets.SLACK_TOKEN`")
	})

	t.Run("workflow 全体の env: もコメント投稿 job へ届く", func(t *testing.T) {
		t.Parallel()
		src := "env:\n  T: ${{ secrets.OTHER }}\n\n" + commentingJob
		dir := writeWorkflows(t, map[string]string{"a.yaml": src})
		var out bytes.Buffer
		err := run([]string{"-workflows", dir}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "workflow 全体に及ぶ")
	})

	t.Run("コメント投稿しない job の secret は対象外", func(t *testing.T) {
		t.Parallel()
		src := commentingJob + "  other:\n    steps:\n      - run: echo ${{ secrets.OTHER }}\n"
		dir := writeWorkflows(t, map[string]string{"a.yaml": src})
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", dir}, &out))
	})

	t.Run("コメント投稿 job が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		dir := writeWorkflows(t, map[string]string{"a.yaml": "jobs:\n  x:\n    runs-on: y\n"})
		var out bytes.Buffer
		err := run([]string{"-workflows", dir}, &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errNoCommentingJob))
	})
}

func Test_secretReferences(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text      string
		wantCount int
		wantName  string
	}{
		"属性形":     {text: "x: ${{ secrets.FOO }}", wantCount: 1, wantName: "FOO"},
		"添字形":     {text: `x: ${{ secrets["FOO"] }}`, wantCount: 1, wantName: "FOO"},
		"単引用の添字形": {text: "x: ${{ secrets['FOO'] }}", wantCount: 1, wantName: "FOO"},
		"名前を取れない参照はコンテキスト全体": {text: "x: ${{ toJSON(secrets) }}", wantCount: 1, wantName: ""},
		"GITHUB_TOKEN は許可":   {text: "x: ${{ secrets.GITHUB_TOKEN }}", wantCount: 0},
		"式の外の散文には反応しない":      {text: "# secrets を渡さないこと", wantCount: 0},
		"1行に2つ": {text: "x: ${{ secrets.A }} ${{ secrets.B }}", wantCount: 2, wantName: "A"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := secretReferences(workflow.Line{Number: 7, Text: tt.text})
			require.Len(t, got, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Equal(t, tt.wantName, got[0].name)
				assert.Equal(t, 7, got[0].number)
			}
		})
	}
}

func Test_describeSecret(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "`secrets.FOO`", describeSecret("FOO"))
	assert.Equal(t, "`secrets` コンテキスト全体", describeSecret(""))
}

func Test_scanWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("jobs: を読めなければ found が false", func(t *testing.T) {
		t.Parallel()
		// 検査対象の取り違えを、違反0件と区別する。
		got := scanWorkflow("a.yaml", "on:\n  pull_request:\n")
		assert.False(t, got.found)
		assert.Zero(t, got.commentingJobs)
	})

	t.Run("コメント投稿 job が複数あればすべて数える", func(t *testing.T) {
		t.Parallel()
		src := commentingJob + "  b:\n    steps:\n      - uses: ./.github/actions/upsert-pr-comment\n"
		got := scanWorkflow("a.yaml", src)
		assert.Equal(t, 2, got.commentingJobs)
	})

	t.Run("コメント投稿 job が無ければ preamble を見ない", func(t *testing.T) {
		t.Parallel()
		// 届く先が無いものを違反として報告すると、直しようのない指摘が残る。
		src := "env:\n  T: ${{ secrets.OTHER }}\n\njobs:\n  a:\n    runs-on: x\n"
		got := scanWorkflow("a.yaml", src)
		require.True(t, got.found)
		assert.Empty(t, got.findings)
	})

	t.Run("違反の行番号は元の行を指す", func(t *testing.T) {
		t.Parallel()
		src := commentingJob + "          token: ${{ secrets.OTHER }}\n"
		got := scanWorkflow("a.yaml", src)
		require.Len(t, got.findings, 1)
		assert.Equal(t, 7, got.findings[0].Line)
		assert.Equal(t, "a.yaml", got.findings[0].File)
	})

	t.Run("jobs: の後ろに来たトップレベルキーも preamble として見る", func(t *testing.T) {
		t.Parallel()
		// jobs: より後ろに書かれた env: も、コメント投稿 job へ届く。
		src := commentingJob + "\nenv:\n  T: ${{ secrets.OTHER }}\n"
		got := scanWorkflow("a.yaml", src)
		require.NotEmpty(t, got.findings)
		assert.Contains(t, got.findings[0].Message, "workflow 全体に及ぶ")
	})
}

func Test_usesCommentAction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want bool
	}{
		"素の指定":        {line: "        uses: ./.github/actions/upsert-pr-comment", want: true},
		"二重引用符":       {line: `        uses: "./.github/actions/upsert-pr-comment"`, want: true},
		"行末コメント":      {line: "        uses: ./.github/actions/upsert-pr-comment # なぜ", want: true},
		"別のアクション":     {line: "        uses: ./.github/actions/notify-detail", want: false},
		"名前が前方一致する別物": {line: "        uses: ./.github/actions/upsert-pr-comment-v2", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			job := workflow.Job{Lines: []workflow.Line{{Number: 1, Text: tt.line}}}
			assert.Equal(t, tt.want, usesCommentAction(job))
		})
	}
}

func Test_secretName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rest string
		want string
	}{
		"属性形":       {rest: ".FOO }}", want: "FOO"},
		"属性形の空白":    {rest: " . FOO }}", want: "FOO"},
		"添字形":       {rest: `["FOO"]`, want: "FOO"},
		"添字形の空白":    {rest: `[ "FOO" ]`, want: "FOO"},
		"ハイフンを含む名前": {rest: ".MY-TOKEN }}", want: "MY-TOKEN"},
		"どちらでもない":   {rest: ") }}", want: ""},
		"空":         {rest: "", want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, secretName(tt.rest))
		})
	}
}

func Test_secretReferences_式の書き方の差(t *testing.T) {
	t.Parallel()

	t.Run("複数行にまたがる式でも中身を見る", func(t *testing.T) {
		t.Parallel()
		// 式は改行を跨げる。行単位で切ってから当てると、跨いだ参照を取り逃がす。
		got := secretReferences(workflow.Line{Number: 1, Text: "x: ${{\n  secrets.FOO\n}}"})
		require.Len(t, got, 1)
		assert.Equal(t, "FOO", got[0].name)
	})

	t.Run("閉じていない式は検査対象にしない", func(t *testing.T) {
		t.Parallel()
		// 構文誤りは actionlint の担当。ここで拾うと、同じ誤りを2箇所が別の言葉で報告する。
		assert.Empty(t, secretReferences(workflow.Line{Number: 1, Text: "x: ${{ secrets.FOO"}))
	})

	t.Run("GITHUB_TOKEN と別 secret が同居しても別 secret だけを返す", func(t *testing.T) {
		t.Parallel()
		got := secretReferences(workflow.Line{Number: 1, Text: "a: ${{ secrets.GITHUB_TOKEN }} b: ${{ secrets.OTHER }}"})
		require.Len(t, got, 1)
		assert.Equal(t, "OTHER", got[0].name)
	})

	t.Run("接頭辞が一致するだけの語を secrets 参照にしない", func(t *testing.T) {
		t.Parallel()
		// `secretsmanager` のような語に反応すると、無関係な式が違反になる。
		assert.Empty(t, secretReferences(workflow.Line{Number: 1, Text: "x: ${{ env.secretsmanager }}"}))
	})

	t.Run("secret を含まない行は空を返す", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, secretReferences(workflow.Line{Number: 1, Text: "x: ${{ github.sha }}"}))
	})
}

func Test_scanWorkflow_許可された参照とenvの切り分け(t *testing.T) {
	t.Parallel()

	t.Run("許可された GITHUB_TOKEN だけなら違反にしない", func(t *testing.T) {
		t.Parallel()
		got := scanWorkflow("a.yaml", commentingJob)
		require.True(t, got.found)
		assert.Empty(t, got.findings)
		assert.Equal(t, 1, got.commentingJobs)
	})

	t.Run("ワークフロー全体の env は別の文言で挙げる", func(t *testing.T) {
		t.Parallel()
		// job 本文の違反と区別できないと、直す場所が分からない。
		got := scanWorkflow("a.yaml", "env:\n  T: ${{ secrets.OTHER }}\n\n"+commentingJob)
		require.Len(t, got.findings, 1)
		assert.Contains(t, got.findings[0].Message, "workflow 全体に及ぶ")
		assert.NotContains(t, got.findings[0].Message, "job `")
	})
}
