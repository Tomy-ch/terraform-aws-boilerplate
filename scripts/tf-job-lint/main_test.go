package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

var jobOK = strings.Join([]string{
	"name: t",
	"on:",
	"  pull_request:",
	"permissions:",
	"  contents: read",
	"jobs:",
	"  tf:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - uses: bullfrogsec/bullfrog@0000000000000000000000000000000000000000 # v0",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000 # v0",
	"        with:",
	"          persist-credentials: false",
	"      - uses: ./.github/actions/setup-mise",
	"        with:",
	"          tools: aqua:hashicorp/terraform",
	"      - run: make tf-fmt-check RUNNER_MODE=host",
	"",
}, "\n")

func writeWorkflow(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", jobOK)
		var out bytes.Buffer
		require.NoError(t, run(dir, &out))
		assert.Contains(t, out.String(), "job 1 件の権限・checkout・uses: を確認しました")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("対象の job が無ければ0件のエラー", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeWorkflow(t, dir, "a.yaml", strings.ReplaceAll(strings.ReplaceAll(jobOK, "aqua:hashicorp/terraform", "go"), "make tf-fmt-check", "make go-test"))
			err := run(dir, &bytes.Buffer{})
			require.ErrorIs(t, err, errNoTarget)
		})

		t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			err := run(filepath.Join(t.TempDir(), "none"), &bytes.Buffer{})
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("違反があれば違反を書いてエラー", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeWorkflow(t, dir, "a.yaml", strings.Replace(jobOK, "      - run: make", "      - uses: actions/cache@0000000000000000000000000000000000000000\n      - run: make", 1))
			var out bytes.Buffer
			require.ErrorIs(t, run(dir, &out), errViolations)
			assert.Contains(t, out.String(), "actions/cache@")
		})

		t.Run("別のファイルに対象があっても、jobs: を読めないファイルの terraform は違反", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeWorkflow(t, dir, "a.yaml", jobOK)
			writeWorkflow(t, dir, "b.yaml", strings.Replace(strings.Replace(jobOK, "jobs:", `"jobs":`, 1), "contents: read\n    steps:", "contents: write\n    steps:", 1))
			var out bytes.Buffer
			require.ErrorIs(t, run(dir, &out), errViolations)
			assert.Contains(t, out.String(), "どの job にも読み取れません")
		})
	})

	t.Run("対象の数はファイルをまたいで足し、YAML でないファイルは読まない", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", jobOK)
		writeWorkflow(t, dir, "b.yml", jobOK)
		writeWorkflow(t, dir, "README.md", jobOK)
		var out bytes.Buffer
		require.NoError(t, run(dir, &out))
		assert.Contains(t, out.String(), "job 2 件")
	})
}

func Test_check(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source  string
		targets int
		want    []string
	}{
		"制約を満たす job は違反なし": {source: jobOK, targets: 1},
		"jobs: が無ければ対象なし":  {source: "name: x\n", targets: 0},
		"steps を読めない対象の job": {
			source:  "jobs:\n  tf:\n    runs-on: x\n    permissions:\n      contents: read\n    env:\n      X: aqua:hashicorp/terraform\n",
			targets: 1,
			want:    []string{"job tf の steps を読み取れません"},
		},
		"steps を読めない job は、他の検査を走らせない": {
			source:  "jobs:\n  tf:\n    runs-on: x\n    permissions:\n      contents: write\n    env:\n      X: aqua:hashicorp/terraform\n",
			targets: 1,
			want:    []string{"job tf の steps を読み取れません"},
		},
		"jobs: を解釈できないファイルの terraform は違反": {
			source:  strings.Replace(jobOK, "jobs:", `"jobs":`, 1),
			targets: 0,
			want:    []string{"terraform を走らせる行のうち 2 行が、どの job にも読み取れません"},
		},
		"job の見出しを解釈できないファイルの terraform は違反": {
			source:  strings.Replace(jobOK, "\n  tf:\n", "\n    tf:\n", 1),
			targets: 0,
			want:    []string{"どの job にも読み取れません"},
		},
		"トップレベルの名前の terraform は数えない": {
			source:  "name: terraform gates\n" + jobOK,
			targets: 1,
		},
		"terraform という名前の job が読めない行を隠さない": {
			source:  strings.Replace(jobOK, "jobs:\n", "jobs:\n  terraform:\n    runs-on: x\n    permissions:\n      contents: read\n    steps:\n      - run: echo\n", 1) + "    terraform plan\n",
			targets: 1,
			want:    nil,
		},
		"フロースタイルの job の見出しは違反": {
			source:  jobOK + "  bad: {permissions: {contents: write}, steps: [{run: terraform plan}]}\n",
			targets: 1,
			want:    []string{"job の見出しを読み取れません"},
		},
		"権限と uses: の両方の違反": {
			source:  strings.Replace(strings.Replace(jobOK, "      contents: read\n    steps:", "      contents: write\n    steps:", 1), "./.github/actions/setup-mise", "./.github/actions/setup-mise-x", 1),
			targets: 1,
			want:    []string{"job tf の permissions は `contents: read` だけにしてください（contents: write）", "./.github/actions/setup-mise-x"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, got := check("a.yaml", tt.source)
			assert.Equal(t, tt.targets, n)
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func jobOf(t *testing.T, source string) workflow.Job {
	t.Helper()
	split := workflow.SplitJobs(source)
	require.True(t, split.Found)
	require.Len(t, split.Jobs, 1)
	return split.Jobs[0]
}

func Test_isTarget(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want bool
	}{
		"setup-mise の tools に terraform":                {body: "      - uses: ./.github/actions/setup-mise\n        with:\n          tools: aqua:hashicorp/terraform\n", want: true},
		"make の tf- ターゲット":                              {body: "      - run: make -k tf-validate tf-test RUNNER_MODE=host\n", want: true},
		"グローバルな flag を挟んだ terraform":                    {body: "      - run: terraform -chdir=x init -backend=false\n", want: true},
		"terraform のどのサブコマンドも":                          {body: "      - run: terraform destroy -auto-approve\n", want: true},
		"terraform を走らせない job":                          {body: "      - run: make go-test\n", want: false},
		"コメントの中の terraform は数えない":                       {body: "      # make tf-test\n      - run: make go-test\n", want: false},
		"id-token を持つ job は対象にしない":                      {body: "      - run: make tf-plan\n    permissions:\n      id-token: write\n", want: false},
		"id-token が write でない job は対象に残す":               {body: "      - run: terraform plan\n    permissions:\n      id-token: none\n", want: true},
		"permissions の外の id-token: write は対象から外す根拠にしない": {body: "      - run: terraform plan\n        env:\n          id-token: write\n", want: true},
		"コメントの id-token: write は対象から外す根拠にしない":           {body: "      - run: terraform plan\n    permissions:\n      # id-token: write\n      contents: read\n", want: true},
		"&& で繋いだ terraform":                             {body: "      - run: cd x && terraform plan\n", want: true},
		"; で繋いだ terraform":                              {body: "      - run: cd x; terraform plan\n", want: true},
		"サブシェルの terraform":                              {body: "      - run: (terraform plan)\n", want: true},
		"mise exec 経由の terraform":                       {body: "      - run: mise exec -- terraform plan\n", want: true},
		"パス付きの terraform":                               {body: "      - run: /usr/local/bin/terraform plan\n", want: true},
		"引用符付きの terraform":                              {body: "      - run: '\"terraform\" plan'\n", want: true},
		"tofu も terraform として扱う":                        {body: "      - run: tofu plan\n", want: true},
		".terraform.lock.hcl の言及は terraform の実行ではない":    {body: "      - run: echo '**/.terraform.lock.hcl'\n", want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isTarget(jobOf(t, "jobs:\n  tf:\n    steps:\n"+tt.body)))
		})
	}
}

func Test_checkPermissions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   string
	}{
		"job の contents: read は通る":       {source: "jobs:\n  tf:\n    permissions:\n      contents: read\n"},
		"空の permissions は通る":             {source: "jobs:\n  tf:\n    permissions: {}\n"},
		"job が無ければ workflow のものを継ぐ":      {source: "permissions:\n  contents: read\njobs:\n  tf:\n    runs-on: x\n"},
		"job の書き込み権限は落ちる":                {source: "jobs:\n  tf:\n    permissions:\n      contents: read\n      pull-requests: write\n", want: "contents: read, pull-requests: write"},
		"1行の read-all は落ちる":              {source: "jobs:\n  tf:\n    permissions: read-all\n", want: "（read-all）"},
		"継いだ workflow の書き込み権限も落ちる":       {source: "permissions:\n  contents: write\njobs:\n  tf:\n    runs-on: x\n", want: "job tf が継ぐ workflow の permissions"},
		"どちらにも permissions が無ければ落ちる":     {source: "jobs:\n  tf:\n    runs-on: x\n", want: "permissions がありません"},
		"コメントは値に数えない":                    {source: "jobs:\n  tf:\n    permissions:\n      # 読むだけ\n      contents: read\n"},
		"値の無い permissions は落ちる":          {source: "jobs:\n  tf:\n    permissions:\n    steps:\n", want: "（）"},
		"フロースタイルの permissions は落ちる":      {source: "jobs:\n  tf:\n    permissions: { contents: read }\n", want: "{ contents: read }"},
		"job の値は workflow の read に救われない": {source: "permissions:\n  contents: read\njobs:\n  tf:\n    permissions:\n      contents: write\n", want: "job tf の permissions"},
		"行末のコメントは値に含めない":                 {source: "jobs:\n  tf:\n    permissions: read-all # 全部\n", want: "（read-all）"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			split := workflow.SplitJobs(tt.source)
			got := checkPermissions("a.yaml", split.Jobs[0], split.Preamble)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Contains(t, got[0].Message, tt.want)
		})
	}
}

func Test_terraformLines(t *testing.T) {
	t.Parallel()

	lines := jobsRegion("jobs:\n  a:\n    steps:\n      - run: terraform plan\n      # terraform apply\n      - run: make tf-test\n      - run: echo .terraform.lock.hcl\n      - run: /usr/local/bin/terraform plan\n      - run: '\"terraform\" plan'\n")
	assert.Equal(t, 4, terraformLines(lines))
	assert.Equal(t, 0, terraformLines(nil))
}

func Test_jobBodies(t *testing.T) {
	t.Parallel()

	split := workflow.SplitJobs("jobs:\n  terraform:\n    steps:\n      - run: terraform plan\n  b:\n    runs-on: x")
	var texts []string
	for _, l := range jobBodies(split) {
		texts = append(texts, l.Text)
	}
	assert.Equal(t, []string{"    steps:", "      - run: terraform plan", "    runs-on: x"}, texts)
}

func Test_jobsRegion(t *testing.T) {
	t.Parallel()

	t.Run("jobs: の下だけを返す", func(t *testing.T) {
		t.Parallel()
		got := jobsRegion("name: terraform gates\njobs:\n  a:\n    x: 1\nenv:\n  B: 2\n")
		require.Len(t, got, 2)
		assert.Equal(t, workflow.Line{Number: 3, Text: "  a:"}, got[0])
		assert.Equal(t, workflow.Line{Number: 4, Text: "    x: 1"}, got[1])
	})

	t.Run("jobs: が無ければ全行を返す", func(t *testing.T) {
		t.Parallel()
		assert.Len(t, jobsRegion("a\nb"), 2)
	})
}

func Test_permissionsBlock(t *testing.T) {
	t.Parallel()

	lines := []workflow.Line{
		{Number: 1, Text: "  permissions:"},
		{Number: 2, Text: "    permissions:"},
		{Number: 3, Text: "      contents: read"},
		{Number: 4, Text: ""},
		{Number: 5, Text: "      actions: read"},
		{Number: 6, Text: "    steps:"},
	}
	got, at, ok := permissionsBlock(lines, "    ")
	require.True(t, ok)
	assert.Equal(t, 2, at)
	assert.Equal(t, []string{"contents: read", "actions: read"}, got)

	_, _, ok = permissionsBlock(lines, "      ")
	assert.False(t, ok)
}

func Test_checkCheckout(t *testing.T) {
	t.Parallel()

	job := jobOf(t, "jobs:\n  tf:\n    steps:\n"+
		"      - uses: actions/checkout@abc\n        with:\n          persist-credentials: false\n"+
		"      - uses: actions/checkout@abc\n        with:\n          fetch-depth: 0\n"+
		"      - uses: actions/checkout@abc\n        # persist-credentials: false\n"+
		"      - uses: actions/checkout@abc\n        with:\n          persist-credentials: true\n"+
		"      - uses: actions/checkout@abc\n        with:\n          persist-credentials: \"false\" # 引用符\n"+
		"      - uses: actions/checkout@abc\n        env:\n          persist-credentials: false\n"+
		"      - uses: actions/checkout@abc\n        with:\n          persist-credentials: false\n          persist-credentials: true\n"+
		"      - run: make tf-test\n")
	got := checkCheckout("a.yaml", job)
	require.Len(t, got, 5)
	assert.Equal(t, 7, got[0].Line)
	assert.Equal(t, 10, got[1].Line)
	assert.Equal(t, 12, got[2].Line)
	assert.Equal(t, 18, got[3].Line)
	assert.Equal(t, 21, got[4].Line)
	assert.Contains(t, got[0].Message, "persist-credentials: false がありません")
}

func Test_checkUses(t *testing.T) {
	t.Parallel()

	job := jobOf(t, "jobs:\n  tf:\n    steps:\n"+
		"      - uses: bullfrogsec/bullfrog@abc\n"+
		"      - name: x\n        uses: \"actions/checkout@abc\" # v7\n"+
		"      - uses: ./.github/actions/setup-mise\n"+
		"      - uses: actions/cache@abc\n"+
		"      - uses: ./.github/actions/setup-mise/../other\n"+
		"      - { uses: actions/cache@abc }\n"+
		"      # - uses: evil/x@abc\n"+
		"      - run: echo \"see uses: pin\"\n")
	got := checkUses("a.yaml", job)
	require.Len(t, got, 3)
	assert.Equal(t, 8, got[0].Line)
	assert.Contains(t, got[0].Message, "actions/cache@abc")
	assert.Equal(t, 9, got[1].Line)
	assert.Contains(t, got[1].Message, "./.github/actions/setup-mise/../other")
	assert.Equal(t, 10, got[2].Line)
	assert.Contains(t, got[2].Message, "uses: を読み取れません")
}
