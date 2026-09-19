package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_rewriteProtection(t *testing.T) {
	t.Parallel()

	const protection = `{
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/old"
      ]
    }
  }
}
`

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("include を宣言の順序どおりに組み直す", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteProtection(protection, []string{"develop", "release/**/*"})
			require.NoError(t, err)
			assert.Contains(t, got, `        "refs/heads/develop",`)
			assert.Contains(t, got, `        "refs/heads/release/**/*"`)
			assert.NotContains(t, got, "refs/heads/old")
		})

		// 末尾にカンマが残ると JSON として壊れ、適用そのものが落ちる。
		t.Run("末尾の要素にカンマを付けない", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteProtection(protection, []string{"a", "b"})
			require.NoError(t, err)
			assert.Contains(t, got, `"refs/heads/b"`+"\n")
			assert.NotContains(t, got, `"refs/heads/b",`)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 書き換え先を見つけられないまま素通りすると、保護対象が古いまま残る。
		t.Run("include が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewriteProtection(`{"conditions": {}}`, []string{"a"})
			require.ErrorIs(t, err, errDrift)
		})
	})
}

func Test_findPushBranches(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("on.push.branches の要素行の範囲を返す", func(t *testing.T) {
			t.Parallel()
			content := "name: x\n\non:\n  push:\n    branches:\n      - develop\n      - production\n    paths:\n      - 'a'\n"
			start, end, found := findPushBranches(content)
			require.True(t, found)
			assert.Equal(t, 5, start)
			assert.Equal(t, 7, end)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// push 側を絞っていない workflow を「見つからない」ではなく0件と読むと、
		// 生成が全 workflow へ空の branches を書き込む。
		t.Run("push に branches が無ければ found=false", func(t *testing.T) {
			t.Parallel()
			_, _, found := findPushBranches("name: x\n\non:\n  push:\n    paths:\n      - 'a'\n")
			assert.False(t, found)
		})

		t.Run("pull_request の branches を push のものと取り違えない", func(t *testing.T) {
			t.Parallel()
			_, _, found := findPushBranches("name: x\n\non:\n  pull_request:\n    branches:\n      - develop\n")
			assert.False(t, found)
		})

		t.Run("on: を持たなければ found=false", func(t *testing.T) {
			t.Parallel()
			_, _, found := findPushBranches("name: x\njobs:\n  a:\n")
			assert.False(t, found)
		})
	})
}

func Test_rewritePushBranches(t *testing.T) {
	t.Parallel()

	const workflow = "name: x\n\non:\n  push:\n    branches:\n      - old\n    paths:\n      - 'a'\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("branches を宣言から組み直し、後続の paths を残す", func(t *testing.T) {
			t.Parallel()
			got, err := rewritePushBranches(workflow, []string{"develop", "production"})
			require.NoError(t, err)
			assert.Equal(t, "name: x\n\non:\n  push:\n    branches:\n      - develop\n      - production\n    paths:\n      - 'a'\n", got)
		})

		// `*` を裸で置くと YAML がエイリアスの指定として読み、workflow が壊れる。
		t.Run("glob を含む値は引用符で囲む", func(t *testing.T) {
			t.Parallel()
			got, err := rewritePushBranches(workflow, []string{"release/*"})
			require.NoError(t, err)
			assert.Contains(t, got, "      - 'release/*'\n")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("on.push.branches が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewritePushBranches("name: x\non:\n  pull_request:\n", []string{"a"})
			require.ErrorIs(t, err, errDrift)
		})
	})
}

func Test_quoteIfNeeded(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct{ in, want string }{
			{in: "develop", want: "develop"},
			{in: "release/*", want: "'release/*'"},
			{in: "release/**/*", want: "'release/**/*'"},
		} {
			assert.Equal(t, tc.want, quoteIfNeeded(tc.in), tc.in)
		}
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("サブコマンドが無ければ使い方を示してエラーにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, run(nil, &out), errUsage)
		})

		t.Run("未知のサブコマンドはエラーにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, run([]string{"no-such"}, &out), errUsage)
		})
	})
}
