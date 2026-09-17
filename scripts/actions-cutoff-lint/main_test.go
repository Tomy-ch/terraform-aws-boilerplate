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

func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return dir
}

const sound = `jobs:
  lint:
    timeout-minutes: 10
    steps:
      - name: comment
        if: always() && github.event_name == 'pull_request'
        uses: ./.github/actions/upsert-pr-comment
        with:
          title: "${{ steps.x.outputs.title || '## ⚠️ CUT OFF (no result produced)' }}"
`

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("3点そろっていれば通る", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": sound})}, &out))
		assert.Contains(t, out.String(), "job 1 件")
	})

	t.Run("timeout-minutes が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		// 無いと GitHub 既定の360分まで走り、1つのハングが runner を6時間押さえる。
		src := "jobs:\n  lint:\n    runs-on: x\n"
		var out bytes.Buffer
		err := run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "timeout-minutes がありません")
	})

	t.Run("reusable workflow 呼び出しは timeout の対象外", func(t *testing.T) {
		t.Parallel()
		// 呼び出し job には timeout-minutes を書けない（invalid key）。
		src := "jobs:\n  notify:\n    uses: ./.github/workflows/notify.yaml\n" + sound[len("jobs:\n"):]
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": "jobs:\n  notify:\n    uses: ./x.yaml\n  lint:\n    timeout-minutes: 1\n"})}, &out))
		_ = src
	})

	t.Run("コメント投稿ステップに if: が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		// Actions は status 関数を含まない if: に暗黙の success() を前置するため、
		// 打ち切られた job はコメントステップを skip する。
		src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          title: \"## ⚠️ CUT OFF\"\n"
		var out bytes.Buffer
		err := run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "if: がありません")
	})

	t.Run("if: が打ち切りに到達しなければ落ちる", func(t *testing.T) {
		t.Parallel()
		// failure() は cancelled では false になる。「status 関数を持つか」で書くと取り逃がす。
		src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - if: failure()\n        uses: ./.github/actions/upsert-pr-comment\n        with:\n          title: \"## ⚠️ CUT OFF\"\n"
		var out bytes.Buffer
		err := run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "打ち切りに到達しません")
	})

	t.Run("title: に打ち切り見出しが無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		// tee で書きかけのファイルが残ると、本文の有無では打ち切りを判別できない。
		src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - if: always()\n        uses: ./.github/actions/upsert-pr-comment\n        with:\n          title: \"## 結果\"\n"
		var out bytes.Buffer
		err := run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "打ち切り時の見出しがありません")
	})

	// 退化した入力の pin。
	t.Run("検査対象の job が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := run([]string{"-workflows", t.TempDir()}, &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errNothingChecked))
	})
}

func Test_conditionOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lines []string
		want  string
		wants bool
	}{
		"1行":       {lines: []string{"      - if: always()"}, want: "always()", wants: true},
		"折り畳みスカラー": {lines: []string{"      - if: >-", "          always() &&", "          github.event_name == 'x'"}, want: "always() && github.event_name == 'x'", wants: true},
		"リテラルスカラー": {lines: []string{"      - if: |", "          always()"}, want: "always()", wants: true},
		"if: が無い":  {lines: []string{"      - uses: x"}, wants: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			step := workflow.Step{Number: 1}
			for i, text := range tt.lines {
				step.Lines = append(step.Lines, workflow.Line{Number: i + 1, Text: text})
			}
			got, ok := conditionOf(step)
			require.Equal(t, tt.wants, ok)
			if ok {
				assert.Equal(t, tt.want, got.value)
			}
		})
	}
}

func Test_reachesCancelled(t *testing.T) {
	t.Parallel()
	assert.True(t, reachesCancelled.MatchString("always()"))
	assert.True(t, reachesCancelled.MatchString("cancelled() || failure()"))
	assert.True(t, reachesCancelled.MatchString("always( )"))
	// failure() は cancelled で false になるため、単体では到達しない。
	assert.False(t, reachesCancelled.MatchString("failure()"))
	assert.False(t, reachesCancelled.MatchString("success()"))
}

func Test_titleOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lines []string
		want  string
		found bool
	}{
		"素の値":        {lines: []string{"          title: \"## 結果\""}, want: `"## 結果"`, found: true},
		"式":          {lines: []string{"          title: ${{ steps.x.outputs.title }}"}, want: "${{ steps.x.outputs.title }}", found: true},
		"前後の空白を落とす":  {lines: []string{"          title:    x   "}, want: "x", found: true},
		"桁が違えば読まない":  {lines: []string{"        title: x"}, found: false},
		"title: が無い": {lines: []string{"          body-file: /tmp/x"}, found: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			step := workflow.Step{Number: 1}
			for i, text := range tt.lines {
				step.Lines = append(step.Lines, workflow.Line{Number: i + 1, Text: text})
			}
			got, ok := titleOf(step)
			require.Equal(t, tt.found, ok)
			if ok {
				assert.Equal(t, tt.want, got.value)
			}
		})
	}
}

func Test_scanWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("jobs: を読めなければ found が false", func(t *testing.T) {
		t.Parallel()
		got := scanWorkflow("a.yaml", "on:\n  pull_request:\n")
		assert.False(t, got.found)
	})

	t.Run("reusable 呼び出しは checkedJobs に数えない", func(t *testing.T) {
		t.Parallel()
		// 呼び出し job には timeout-minutes を書けない（invalid key）ため、検査の分母から外す。
		src := "jobs:\n  notify:\n    uses: ./.github/workflows/notify.yaml\n  lint:\n    timeout-minutes: 1\n"
		got := scanWorkflow("a.yaml", src)
		assert.Equal(t, 1, got.checkedJobs)
		assert.Empty(t, got.findings)
	})

	t.Run("コメント投稿しないステップは checkedSteps に数えない", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - run: echo x\n      - uses: other\n"
		got := scanWorkflow("a.yaml", src)
		assert.Zero(t, got.checkedSteps)
	})

	t.Run("複数 job にまたがって数える", func(t *testing.T) {
		t.Parallel()
		src := sound + "  b:\n    timeout-minutes: 2\n    steps:\n      - run: x\n"
		got := scanWorkflow("a.yaml", src)
		assert.Equal(t, 2, got.checkedJobs)
		assert.Equal(t, 1, got.checkedSteps)
	})

	t.Run("違反の行番号はステップ内の該当行を指す", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - if: failure()\n        uses: ./.github/actions/upsert-pr-comment\n        with:\n          title: \"## ⚠️ CUT OFF\"\n"
		got := scanWorkflow("a.yaml", src)
		require.Len(t, got.findings, 1)
		assert.Equal(t, 5, got.findings[0].Line)
	})
}

func Test_callsReusableWorkflow(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want bool
	}{
		"job 直下の uses":   {line: "    uses: ./.github/workflows/x.yaml", want: true},
		"ステップの uses は違う": {line: "      - uses: actions/checkout@abc", want: false},
		"深い桁の uses も違う":  {line: "        uses: actions/checkout@abc", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			job := workflow.Job{Lines: []workflow.Line{{Number: 1, Text: tt.line}}}
			assert.Equal(t, tt.want, callsReusableWorkflow(job))
		})
	}
}

func Test_cutOffHeading(t *testing.T) {
	t.Parallel()
	assert.True(t, cutOffHeading.MatchString("## ⚠️ X: CUT OFF (no result produced)"))
	assert.True(t, cutOffHeading.MatchString("${{ steps.x.outputs.title || '## CUT OFF' }}"))
	assert.False(t, cutOffHeading.MatchString("## 結果"))
	// 大文字小文字は区別する。表記揺れを許すと、見出しの一致が偶然に頼る。
	assert.False(t, cutOffHeading.MatchString("cut off"))
}

// ここから下は輸入した検査項目。実運用で踏んだ形から足されたケースを、こちらの実装へ当て直す。

func Test_hasJobTimeout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want bool
	}{
		"job 直下": {line: "    timeout-minutes: 10", want: true},
		"ステップの桁は job のものと誤認しない": {line: "        timeout-minutes: 10", want: false},
		"ステップ先頭キーの桁も誤認しない":      {line: "      - timeout-minutes: 10", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			job := workflow.Job{Lines: []workflow.Line{{Number: 1, Text: tt.line}}}
			assert.Equal(t, tt.want, hasJobTimeout(job))
		})
	}
}

func Test_callsCommentAction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want bool
	}{
		"ステップ先頭キー": {line: "      - uses: ./.github/actions/upsert-pr-comment", want: true},
		"先頭でない位置":  {line: "        uses: ./.github/actions/upsert-pr-comment", want: true},
		"桁が浅すぎる":   {line: "    uses: ./.github/actions/upsert-pr-comment", want: false},
		"別のアクション":  {line: "        uses: ./.github/actions/notify-detail", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			step := workflow.Step{Lines: []workflow.Line{{Number: 1, Text: tt.line}}}
			assert.Equal(t, tt.want, callsCommentAction(step))
		})
	}
}

func Test_conditionOf_続き行の打ち切り(t *testing.T) {
	t.Parallel()

	// 次のキーで続き行の取り込みを止める。止めないと、後続のキーまで条件式に混ざる。
	step := workflow.Step{Number: 1, Lines: []workflow.Line{
		{Number: 1, Text: "      - if: >-"},
		{Number: 2, Text: "          always()"},
		{Number: 3, Text: "        uses: x"},
	}}
	got, ok := conditionOf(step)
	require.True(t, ok)
	assert.Equal(t, "always()", got.value)
	assert.NotContains(t, got.value, "uses")
}

func Test_reachesCancelled_輸入したケース(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cond string
		want bool
	}{
		"always()":                      {cond: "always()", want: true},
		"cancelled()":                   {cond: "cancelled()", want: true},
		"空白入りの呼び出し":                     {cond: "always( )", want: true},
		"failure() は cancelled で false": {cond: "failure()", want: false},
		"success() だけ":                  {cond: "success()", want: false},
		"条件を書かない（暗黙の success()）": {cond: "", want: false},
		// 接尾辞が一致するだけの識別子を関数呼び出しと見なさない。
		// 見なすと、無関係な式が「打ち切りに到達する」ことになり検査が沈黙する。
		"接尾辞一致の識別子": {cond: "myalways()", want: false},
		"接頭辞に語が続く":  {cond: "notcancelled()", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, reachesCancelled.MatchString(tt.cond))
		})
	}
}

func Test_scanWorkflow_同じステップの複数欠落(t *testing.T) {
	t.Parallel()

	// if: と title: は別々の問いなので、片方の欠落でもう片方の検査を止めない。
	src := "jobs:\n  lint:\n    timeout-minutes: 1\n    steps:\n      - uses: ./.github/actions/upsert-pr-comment\n"
	got := scanWorkflow("a.yaml", src)
	require.Len(t, got.findings, 2)
	assert.Contains(t, got.findings[0].Message, "if: がありません")
	assert.Contains(t, got.findings[1].Message, "title: がありません")
	// どちらもステップ先頭の行を指す。
	assert.Equal(t, 5, got.findings[0].Line)
	assert.Equal(t, 5, got.findings[1].Line)
}

func Test_scanWorkflow_timeout違反の行番号(t *testing.T) {
	t.Parallel()

	// job 見出しの行を指す。違反を直しに行く先がそこだから。
	src := "jobs:\n  a:\n    runs-on: x\n  b:\n    timeout-minutes: 1\n"
	got := scanWorkflow("a.yaml", src)
	require.Len(t, got.findings, 1)
	assert.Equal(t, 2, got.findings[0].Line)
}
