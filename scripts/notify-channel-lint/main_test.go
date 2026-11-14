package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lockfile"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

// fixture はリポジトリの実物ではなく一時ディレクトリへ、workflow と宣言を組み立てます。
func fixture(t *testing.T, decl string, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	wf := filepath.Join(dir, "workflows")
	require.NoError(t, os.Mkdir(wf, 0o700))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(wf, name), []byte(body), 0o600))
	}
	path := filepath.Join(dir, "notify-channels.toml")
	require.NoError(t, os.WriteFile(path, []byte(decl), 0o600))
	return wf, path
}

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const channels = `"channel:security" = "SLACK_WEBHOOK_URL_SECURITY"
"channel:deploy-prod" = "SLACK_WEBHOOK_URL_DEPLOY_PROD"
`

func caller(job, secretLine string) string {
	return "jobs:\n  " + job + ":\n    uses: ./.github/workflows/notify.yaml\n    with:\n      failed-job: x\n    secrets:\n" + secretLine
}

const prodSecret = "      slack-webhook-url: ${{ secrets.SLACK_WEBHOOK_URL_DEPLOY_PROD }}\n"

// editorconfig-checker-enable

func lint(t *testing.T, decl string, files map[string]string) (string, error) {
	t.Helper()
	wf, path := fixture(t, decl, files)
	var out bytes.Buffer
	err := run([]string{"-workflows", wf, "-decl", path}, &out)
	return out.String(), err
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("宣言した系統の secret を渡していれば通る", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", prodSecret)})
		require.NoError(t, err)
		assert.Contains(t, out, "job 1 件の系統を確認しました")
	})

	t.Run("別の系統の secret を渡していれば落ちる", func(t *testing.T) {
		t.Parallel()
		// 本番の通知が別のチャンネルへ届く取り違えを、この検査のほかに止めるものは無い。
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", "      slack-webhook-url: ${{ secrets.SLACK_WEBHOOK_URL_SECURITY }}\n")})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "secrets.SLACK_WEBHOOK_URL_DEPLOY_PROD を渡してください（secrets.SLACK_WEBHOOK_URL_SECURITY を渡しています）")
		assert.Contains(t, out, "1 件の違反")
		assert.Contains(t, out, "\n    :7  job a.yaml:n", "違反は secret を渡した workflow の行で報告する")
	})

	t.Run("宣言に無い呼び出しは落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels, map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "job a.yaml:n の系統が")
	})

	t.Run("実物に無い宣言の job は落ちる", func(t *testing.T) {
		t.Parallel()
		decl := channels + `"job:a.yaml:n" = "deploy-prod"` + "\n" + `"job:a.yaml:gone" = "deploy-prod"` + "\n"
		out, err := lint(t, decl, map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "job a.yaml:gone は notify.yaml を呼ぶ job として見つかりません")
	})

	t.Run("宣言に無い系統を名指す job は落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-nonprod"`+"\n",
			map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "job a.yaml:n の系統 deploy-nonprod は宣言されていません")
	})

	t.Run("2つの系統が同じ secret を持てば落ちる", func(t *testing.T) {
		t.Parallel()
		decl := channels + `"channel:deploy-nonprod" = "SLACK_WEBHOOK_URL_DEPLOY_PROD"` + "\n" + `"job:a.yaml:n" = "deploy-prod"` + "\n"
		out, err := lint(t, decl, map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "同じ secret SLACK_WEBHOOK_URL_DEPLOY_PROD を持ちます")
	})

	t.Run("接頭辞の無い鍵は落ちる", func(t *testing.T) {
		t.Parallel()
		decl := channels + `"job:a.yaml:n" = "deploy-prod"` + "\n" + `"a.yaml:n" = "deploy-prod"` + "\n"
		out, err := lint(t, decl, map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, `鍵 "a.yaml:n" は`)
	})

	t.Run("secret を式で組めば落ちる", func(t *testing.T) {
		t.Parallel()
		// どの secret が渡るかを字面から決められない形は、取りこぼさずに違反にする。
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", "      slack-webhook-url: ${{ secrets.A || secrets.SLACK_WEBHOOK_URL_DEPLOY_PROD }}\n")})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "の形で書いてください")
	})

	t.Run("secrets: inherit は落ちる", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  n:\n    uses: ./.github/workflows/notify.yaml\n    secrets: inherit\n"
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", map[string]string{"a.yaml": src})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "secrets: inherit")
	})

	t.Run("slack-webhook-url を渡していなければ落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", "")})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "slack-webhook-url を渡していません")
	})

	t.Run("notify.yaml を呼ばない job は対象外", func(t *testing.T) {
		t.Parallel()
		src := caller("n", prodSecret) + "  other:\n    runs-on: x\n    steps:\n      - run: echo ${{ secrets.SLACK_WEBHOOK_URL_SECURITY }}\n"
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", map[string]string{"a.yaml": src})
		require.NoError(t, err)
		assert.Contains(t, out, "job 1 件の系統を確認しました")
	})

	// notifyUse が読めない形の呼び出しは、uses と notify.yaml を持つ行の数え直しと合わず違反になる。
	// これが無いと、正規の呼び出しが1件でもある workflow で、形の違う呼び出しが黙って検査から外れる。
	unread := map[string]string{
		"1行のフロー記法":    "  evil: {uses: \"./.github/workflows/notify.yaml\", secrets: inherit}\n",
		".. を含む path": "  other:\n    uses: ./.github/workflows/../workflows/notify.yaml\n",
		"@ref 付き":     "  other:\n    uses: ./.github/workflows/notify.yaml@main\n",
	}
	for name, extra := range unread {
		t.Run(name+"の呼び出しは件数が合わず落ちる", func(t *testing.T) {
			t.Parallel()
			out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
				map[string]string{"a.yaml": caller("n", prodSecret) + extra})
			require.ErrorIs(t, err, errViolations)
			assert.Contains(t, out, "notify.yaml を uses: で指す行が 2 行あるのに、job として読めた呼び出しは 1 件です")
		})
	}

	t.Run("引用符付きの uses: も呼び出しとして数える", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  n:\n    uses: \"./.github/workflows/notify.yaml\"\n    secrets:\n" + prodSecret
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", map[string]string{"a.yaml": src})
		require.NoError(t, err, out)
		assert.Contains(t, out, "job 1 件の系統を確認しました")
	})

	t.Run("読めない workflow があれば落ちる", func(t *testing.T) {
		t.Parallel()
		wf, path := fixture(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", map[string]string{"a.yaml": caller("n", prodSecret)})
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "none"), filepath.Join(wf, "b.yaml")))
		var out bytes.Buffer
		err := run([]string{"-workflows", wf, "-decl", path}, &out)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run(".yaml の名前のディレクトリは読み飛ばす", func(t *testing.T) {
		t.Parallel()
		wf, path := fixture(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", map[string]string{"a.yaml": caller("n", prodSecret)})
		require.NoError(t, os.Mkdir(filepath.Join(wf, "d.yaml"), 0o700))
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", wf, "-decl", path}, &out))
		assert.Contains(t, out.String(), "job 1 件の系統を確認しました")
	})

	t.Run("宣言が空でも呼び出しがあれば落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, "", map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "job a.yaml:n の系統が")
	})

	t.Run("コメントアウトした slack-webhook-url の行は渡し方として読めず落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", prodSecret+"      # slack-webhook-url: ${{ secrets.X }}\n")})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "の形で書いてください")
	})

	t.Run("系統ごとに別の secret を渡す job が複数あれば全て通る", func(t *testing.T) {
		t.Parallel()
		decl := channels + `"job:a.yaml:n" = "deploy-prod"` + "\n" + `"job:b.yaml:s" = "security"` + "\n"
		out, err := lint(t, decl, map[string]string{
			"a.yaml": caller("n", prodSecret),
			"b.yaml": caller("s", "      slack-webhook-url: ${{ secrets.SLACK_WEBHOOK_URL_SECURITY }}\n"),
		})
		require.NoError(t, err, out)
		assert.Contains(t, out, "job 2 件の系統を確認しました")
	})

	t.Run("notify.yaml を呼ぶ job が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		_, err := lint(t, channels, map[string]string{"a.yaml": "jobs:\n  x:\n    runs-on: y\n"})
		require.ErrorIs(t, err, errNoCaller)
	})

	t.Run("jobs: の無い workflow は落ちる", func(t *testing.T) {
		t.Parallel()
		out, err := lint(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n",
			map[string]string{"a.yaml": caller("n", prodSecret), "b.yaml": "name: b\n"})
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out, "jobs: が見つかりません")
	})

	t.Run("宣言の解釈できない行は落ちる", func(t *testing.T) {
		t.Parallel()
		_, err := lint(t, channels+"job:a.yaml:n = deploy-prod\n", map[string]string{"a.yaml": caller("n", prodSecret)})
		require.ErrorIs(t, err, lockfile.ErrInvalidLine)
	})

	t.Run("宣言が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		wf, _ := fixture(t, channels, map[string]string{"a.yaml": caller("n", prodSecret)})
		var out bytes.Buffer
		err := run([]string{"-workflows", wf, "-decl", filepath.Join(t.TempDir(), "none.toml")}, &out)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("workflow のディレクトリが無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		_, path := fixture(t, channels, nil)
		var out bytes.Buffer
		err := run([]string{"-workflows", filepath.Join(t.TempDir(), "none"), "-decl", path}, &out)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("未知のフラグは落ちる", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.Error(t, run([]string{"-unknown"}, &out))
	})
}

func Test_readDecl(t *testing.T) {
	t.Parallel()

	t.Run("系統と job を分けて読む", func(t *testing.T) {
		t.Parallel()
		_, path := fixture(t, channels+`"job:a.yaml:n" = "deploy-prod"`+"\n", nil)
		d, findings, err := readDecl(path)
		require.NoError(t, err)
		assert.Empty(t, findings)
		assert.Equal(t, map[string]string{"security": "SLACK_WEBHOOK_URL_SECURITY", "deploy-prod": "SLACK_WEBHOOK_URL_DEPLOY_PROD"}, d.channels)
		assert.Equal(t, map[string]string{"a.yaml:n": "deploy-prod"}, d.jobs)
	})

	t.Run("鍵の重複は落ちる", func(t *testing.T) {
		t.Parallel()
		_, path := fixture(t, channels+`"channel:security" = "X"`+"\n", nil)
		_, _, err := readDecl(path)
		require.ErrorIs(t, err, lockfile.ErrDuplicateKey)
	})
}

func Test_countMentions(t *testing.T) {
	t.Parallel()

	t.Run("コメント行を除き、uses と notify.yaml を持つ行を表記を問わず数える", func(t *testing.T) {
		t.Parallel()
		src := "    uses: ./.github/workflows/notify.yaml\n" +
			"  x: {uses: \"./.github/workflows/notify.yaml\"}\n" +
			"    # uses: ./.github/workflows/notify.yaml\n" +
			"    uses: ./.github/workflows/other.yaml\n" +
			"      run: echo notify.yaml\n"
		assert.Equal(t, 2, countMentions(src))
	})
}

func Test_callsNotify(t *testing.T) {
	t.Parallel()

	t.Run("notify.yaml を uses: で呼ぶ job を見分ける", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs(caller("n", prodSecret) + "  other:\n    uses: ./.github/workflows/other.yaml\n")
		require.True(t, split.Found)
		require.Len(t, split.Jobs, 2)
		assert.True(t, callsNotify(split.Jobs[0]))
		assert.False(t, callsNotify(split.Jobs[1]))
	})
}

func Test_checkCaller(t *testing.T) {
	t.Parallel()

	d := decl{channels: map[string]string{"deploy-prod": "SLACK_WEBHOOK_URL_DEPLOY_PROD"}, jobs: map[string]string{"a.yaml:n": "deploy-prod"}}

	t.Run("系統の secret を1回渡していれば違反は無い", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs(caller("n", prodSecret))
		assert.Empty(t, checkCaller("a.yaml", "a.yaml:n", split.Jobs[0], d))
	})

	t.Run("slack-webhook-url を2回渡せば落ちる", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs(caller("n", prodSecret+prodSecret))
		findings := checkCaller("a.yaml", "a.yaml:n", split.Jobs[0], d)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Message, "2 回渡しています")
	})

	t.Run("宣言に無い系統の job は readDecl に任せて指摘を重ねない", func(t *testing.T) {
		t.Parallel()
		typo := decl{channels: d.channels, jobs: map[string]string{"a.yaml:n": "typo"}}
		split := workflow.SplitJobs(caller("n", prodSecret))
		assert.Empty(t, checkCaller("a.yaml", "a.yaml:n", split.Jobs[0], typo))
	})

	t.Run("行末のコメントは渡し方を変えない", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs(caller("n", "      slack-webhook-url: ${{ secrets.SLACK_WEBHOOK_URL_DEPLOY_PROD }} # prod\n"))
		assert.Empty(t, checkCaller("a.yaml", "a.yaml:n", split.Jobs[0], d))
	})
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("鍵を昇順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]string{"c": "", "a": "", "b": ""}))
	})
}
