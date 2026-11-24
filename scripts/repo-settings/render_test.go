package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ JSON の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const rulesetFixture = `{
  "name": "branch-protection",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/old"
      ]
    }
  },
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"},
    {"type": "pull_request", "parameters": {
      "require_code_owner_review": true,
      "required_review_thread_resolution": true,
      "required_approving_review_count": 1,
      "allowed_merge_methods": ["merge"]
    }},
    {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "go-test", "integration_id": 15368}]}}
  ],
  "bypass_actors": []
}
`

// editorconfig-checker-enable

func Test_locateInclude(t *testing.T) {
	t.Parallel()

	t.Run("include の配列の範囲と字下げを返す", func(t *testing.T) {
		t.Parallel()
		span, err := locateInclude([]byte(rulesetFixture))
		require.NoError(t, err)
		assert.Equal(t, "[\n        \"refs/heads/old\"\n      ]", rulesetFixture[span.start:span.end])
		assert.Equal(t, "      ", span.indent)
	})

	cases := []struct {
		name string
		src  string
	}{
		{"include が無ければ失敗する", `{"conditions": {"ref_name": {"exclude": []}}}`},
		{"別の階層の include は数えない", `{"include": ["x"], "conditions": {"ref_name": {}}}`},
		{"include が配列でなければ失敗する", `{"conditions": {"ref_name": {"include": "x"}}}`},
		{"include の要素が文字列でなければ失敗する", `{"conditions": {"ref_name": {"include": [1]}}}`},
		{"include の要素が object なら失敗する", `{"conditions": {"ref_name": {"include": [{}]}}}`},
		{"同じ階層のキーの重複は失敗する", `{"conditions": {"ref_name": {"include": ["a"], "include": ["b"]}}}`},
		{"深い階層のキーの重複も失敗する", `{"conditions": {"ref_name": {"include": ["a"]}}, "rules": [{"type": "a", "type": "b"}]}`},
		{"値の後の余分な内容は失敗する", `{"conditions": {"ref_name": {"include": ["a"]}}} {}`},
		{"値の後の不正なトークンも失敗する", `{"conditions": {"ref_name": {"include": ["a"]}}} x`},
		{"入れ子の conditions.ref_name の include は数えない", `{"x": {"conditions": {"ref_name": {"include": ["a"]}}}}`},
		{"`.` を含むキー名の include は数えない", `{"conditions.ref_name": {"include": ["a"]}}`},
		{"壊れた JSON は失敗する", `{"conditions": {"ref_name": {"include": ["a"]}`},
		{"include の配列が閉じていなければ失敗する", `{"conditions": {"ref_name": {"include": ["a"`},
		{"空の入力は失敗する", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := locateInclude([]byte(tc.src))
			require.ErrorIs(t, err, errInclude)
		})
	}
}

func Test_walkValue(t *testing.T) {
	t.Parallel()

	t.Run("配列の要素の下の conditions.ref_name は数えない", func(t *testing.T) {
		t.Parallel()
		src := []byte(`[{"conditions": {"ref_name": {"include": ["a"]}}}]`)
		var spans []includeSpan
		require.NoError(t, walkValue(newDecoder(src), src, nil, &spans))
		// 配列の要素の path は "[]" から始まるので、最上位の conditions.ref_name には当たらない。
		assert.Empty(t, spans)
	})

	t.Run("最上位の conditions.ref_name の include を集める", func(t *testing.T) {
		t.Parallel()
		src := []byte(`{"conditions": {"ref_name": {"include": ["a", "b"]}}}`)
		var spans []includeSpan
		require.NoError(t, walkValue(newDecoder(src), src, nil, &spans))
		require.Len(t, spans, 1)
		assert.Equal(t, `["a", "b"]`, string(src[spans[0].start:spans[0].end]))
	})

	t.Run("スカラーは読み飛ばす", func(t *testing.T) {
		t.Parallel()
		src := []byte(`"x"`)
		var spans []includeSpan
		require.NoError(t, walkValue(newDecoder(src), src, nil, &spans))
		assert.Empty(t, spans)
	})
}

func newDecoder(src []byte) *json.Decoder {
	return json.NewDecoder(strings.NewReader(string(src)))
}

func Test_readInclude(t *testing.T) {
	t.Parallel()

	t.Run("空の配列も範囲として返す", func(t *testing.T) {
		t.Parallel()
		src := []byte("{\n\t\"include\": []\n}")
		dec := newDecoder(src)
		_, err := dec.Token() // {
		require.NoError(t, err)
		_, err = dec.Token() // "include"
		require.NoError(t, err)
		span, err := readInclude(dec, src)
		require.NoError(t, err)
		assert.Equal(t, "[]", string(src[span.start:span.end]))
		assert.Equal(t, "\t", span.indent)
	})

	t.Run("値が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		src := []byte(`{"include":`)
		dec := newDecoder(src)
		_, _ = dec.Token()
		_, _ = dec.Token()
		_, err := readInclude(dec, src)
		require.ErrorIs(t, err, errInclude)
	})
}

func Test_renderInclude(t *testing.T) {
	t.Parallel()

	t.Run("区間だけを書き換え、区間の外のバイトを変えない", func(t *testing.T) {
		t.Parallel()
		got, err := renderInclude([]byte(rulesetFixture), []string{"refs/heads/a", "refs/heads/b/**/*"})
		require.NoError(t, err)
		want := strings.Replace(rulesetFixture,
			"[\n        \"refs/heads/old\"\n      ]",
			"[\n        \"refs/heads/a\",\n        \"refs/heads/b/**/*\"\n      ]", 1)
		assert.Equal(t, want, string(got))
	})

	t.Run("同じパターンなら入力と同じバイト列を返す", func(t *testing.T) {
		t.Parallel()
		got, err := renderInclude([]byte(rulesetFixture), []string{"refs/heads/old"})
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(got))
	})

	t.Run("区間を定められなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := renderInclude([]byte(`{}`), []string{"refs/heads/a"})
		require.ErrorIs(t, err, errInclude)
	})
}

func Test_verifyInclude(t *testing.T) {
	t.Parallel()

	t.Run("include が一致すれば通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, verifyInclude([]byte(rulesetFixture), []string{"refs/heads/old"}))
	})

	t.Run("include の件数や値が違えば失敗する", func(t *testing.T) {
		t.Parallel()
		err := verifyInclude([]byte(rulesetFixture), []string{"refs/heads/old", "refs/heads/new"})
		require.ErrorIs(t, err, errInclude)
		assert.Contains(t, err.Error(), "1 件で、宣言の 2 件")
	})

	t.Run("JSON でなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, verifyInclude([]byte(`x`), []string{"a"}), errInclude)
	})
}

func Test_checkRuleset(t *testing.T) {
	t.Parallel()

	t.Run("ADR-0603 の項目をすべて持つ宣言は通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkRuleset([]byte(rulesetFixture)))
	})

	cases := []struct {
		name    string
		from    string
		to      string
		message string
	}{
		{"bypass の列挙は拒む", `"bypass_actors": []`, `"bypass_actors": [{"actor_id": 5}]`, "bypass_actors"},
		{"bypass_actors の省略は拒む", ",\n  \"bypass_actors\": []", ``, "bypass_actors"},
		{"削除の禁止が無ければ拒む", `{"type": "deletion"},`, ``, "deletion"},
		{"fast-forward でない更新の禁止が無ければ拒む", `{"type": "non_fast_forward"},`, ``, "non_fast_forward"},
		{"required status check が無ければ拒む", `"type": "required_status_checks"`, `"type": "other"`, "required_status_checks"},
		{"スレッドの解決が無ければ拒む", `"required_review_thread_resolution": true`, `"required_review_thread_resolution": false`, "required_review_thread_resolution"},
		{"code owner の review が無ければ拒む", `"require_code_owner_review": true`, `"require_code_owner_review": false`, "require_code_owner_review"},
		{"承認者数が0なら拒む", `"required_approving_review_count": 1`, `"required_approving_review_count": 0`, "required_approving_review_count"},
		{"承認者数が無ければ拒む", `"required_approving_review_count": 1,`, ``, "required_approving_review_count"},
		{"承認者数が整数でなければ拒む", `"required_approving_review_count": 1`, `"required_approving_review_count": 1.5`, "required_approving_review_count"},
		{"承認者数が数でなければ拒む", `"required_approving_review_count": 1`, `"required_approving_review_count": "1"`, "required_approving_review_count"},
		{"merge 方式が merge だけでなければ拒む", `["merge"]`, `["merge", "squash"]`, "allowed_merge_methods"},
		{"name が無ければ拒む", `"name": "branch-protection",`, ``, "name"},
		{"target が branch でなければ拒む", `"target": "branch"`, `"target": "tag"`, "target"},
		{"enforcement が active でなければ拒む", `"enforcement": "active"`, `"enforcement": "disabled"`, "enforcement"},
		{"exclude に値があれば拒む", `"exclude": []`, `"exclude": ["refs/heads/production"]`, "exclude"},
		{"同じ type のルールが2件あれば拒む", `{"type": "deletion"},`, `{"type": "deletion"}, {"type": "deletion"},`, "deletion が2件以上"},
		{"required status check の context が0件なら拒む", `[{"context": "go-test", "integration_id": 15368}]`, `[]`, "context が1件も"},
		{"required status check の parameters が無ければ拒む", `{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "go-test", "integration_id": 15368}]}}`, `{"type": "required_status_checks"}`, "context が1件も"},
		// 報告元を限らない context は、書き込み権限で投稿できる commit status でも満たせる（ADR-0603 決定34）。
		{"required status check の報告元が無ければ拒む", `{"context": "go-test", "integration_id": 15368}`, `{"context": "go-test"}`, "go-test の integration_id"},
		{"required status check の報告元が GitHub Actions でなければ拒む", `"integration_id": 15368`, `"integration_id": 1`, "go-test の integration_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(rulesetFixture, tc.from, tc.to, 1)
			require.NotEqual(t, rulesetFixture, src)
			err := checkRuleset([]byte(src))
			require.ErrorIs(t, err, errRulesetPolicy)
			assert.Contains(t, err.Error(), tc.message)
		})
	}

	t.Run("承認者数が2以上でも通る", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(rulesetFixture, `"required_approving_review_count": 1`, `"required_approving_review_count": 2`, 1)
		require.NotEqual(t, rulesetFixture, src)
		require.NoError(t, checkRuleset([]byte(src)))
	})

	t.Run("pull_request のルールが無ければ拒む", func(t *testing.T) {
		t.Parallel()
		err := checkRuleset([]byte(`{"bypass_actors": [], "rules": [{"type": "deletion"}, {"type": "non_fast_forward"}, {"type": "required_status_checks"}]}`))
		require.ErrorIs(t, err, errRulesetPolicy)
		assert.Contains(t, err.Error(), "pull_request")
	})

	t.Run("JSON でなければ拒む", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkRuleset([]byte(`x`)), errRulesetPolicy)
	})
}

func Test_foreignReporters(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		checks []any
		want   []string
	}{
		"GitHub Actions の報告元は報せない": {[]any{map[string]any{"context": "a", "integration_id": float64(githubActionsAppID)}}, nil},
		"integration_id が無ければ報せる":  {[]any{map[string]any{"context": "b"}}, []string{"b の integration_id"}},
		"別の報告元は報せる":                {[]any{map[string]any{"context": "c", "integration_id": float64(1)}}, []string{"c の integration_id"}},
		"文字列の integration_id は報せる": {[]any{map[string]any{"context": "d", "integration_id": "15368"}}, []string{"d の integration_id"}},
		"object でない要素は報せる":         {[]any{map[string]any{"context": "a", "integration_id": float64(githubActionsAppID)}, "e"}, []string{"2 番目の要素が object ではありません"}},
		"報せるものだけを順に返す": {[]any{
			map[string]any{"context": "a", "integration_id": float64(githubActionsAppID)},
			map[string]any{"context": "b"},
			map[string]any{"context": "c", "integration_id": float64(1)},
		}, []string{"b の integration_id", "c の integration_id"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := foreignReporters(tc.checks)
			require.Len(t, got, len(tc.want))
			for i, w := range tc.want {
				assert.Contains(t, got[i], w)
			}
		})
	}
}
