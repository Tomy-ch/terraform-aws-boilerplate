package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type result struct {
	dir    string
	code   int
	json   string
	stderr string
}

func setup(t *testing.T, ignore string, results ...result) (string, []string) {
	t.Helper()
	base := t.TempDir()
	var lines []string
	for i, r := range results {
		dir := filepath.Join(base, r.dir)
		js := writeFile(t, base, fmt.Sprintf("tmp/tf-validate/%d.json", i), r.json)
		errf := writeFile(t, base, fmt.Sprintf("tmp/tf-validate/%d.err", i), r.stderr)
		lines = append(lines, fmt.Sprintf("%s\t%d\t%s\t%s", dir, r.code, js, errf))
	}
	manifest := writeFile(t, base, "tmp/tf-validate/manifest.tsv", strings.Join(lines, "\n")+"\n")
	ign := writeFile(t, base, "ignore.yaml", ignore)
	return base, []string{"-base", base, "-manifest", manifest, "-ignore", ign}
}

func Test_run(t *testing.T) {
	t.Parallel()
	const empty = "warnings: []\n"

	t.Run("警告もエラーも無ければ成功し、ディレクトリ数を報告する", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", json: cleanJSON}, result{dir: "usecases/b", json: cleanJSON})
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.Contains(t, out.String(), "✅ tf-validate: 2 ディレクトリを validate し、エラーと抑止されない警告はありません（抑止 0 件）")
	})
	t.Run("警告が1件でもあれば失敗し、位置と本文と現れたディレクトリを出す", func(t *testing.T) {
		t.Parallel()
		base, args := setup(t, empty, result{dir: "modules/a", json: awsWarningJSON})
		var out bytes.Buffer
		err := run(args, &out)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, out.String(), "modules/a/main.tf:14:25: [warning] Argument is deprecated（resource \"aws_iam_role\" \"this\"）")
		assert.Contains(t, out.String(), "\n  │ managed_policy_arns is deprecated.")
		assert.Contains(t, out.String(), "modules/a/main.tf:11:33: [warning] Argument is deprecated（resource \"aws_iam_role\" \"this\"）")
		assert.Contains(t, out.String(), "\n  │ inline_policy is deprecated.")
		assert.Contains(t, out.String(), "現れたディレクトリ: "+filepath.Join(base, "modules/a"))
		assert.Contains(t, out.String(), "抑止されない警告が 2 件")
	})
	t.Run("子 module の警告は呼び出し元にも現れるが1件に数える", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty,
			result{dir: "modules/inner", json: childWarningJSON("main.tf")},
			result{dir: "usecases/b", json: childWarningJSON("../../modules/inner/main.tf")})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errFailed)
		assert.Contains(t, out.String(), "抑止されない警告が 1 件")
	})
	t.Run("宣言に当たる警告は外され、件数が合えば成功する", func(t *testing.T) {
		t.Parallel()
		ign :=
			"warnings:\n" +
				"  - summary: Argument is deprecated\n" +
				"    path: modules/a/main.tf\n" +
				"    block: resource \"aws_iam_role\" \"this\"\n" +
				"    detail: managed_policy_arns is deprecated. Use the aws_iam_role_policy_attachment resource instead.\n" +
				"    occurrences: 1\n" +
				"    reason: 理由\n" +
				"    withdrawal: 撤回の条件\n" +
				"  - summary: Argument is deprecated\n" +
				"    path: modules/a/main.tf\n" +
				"    block: resource \"aws_iam_role\" \"this\"\n" +
				"    detail: inline_policy is deprecated. Use the aws_iam_role_policy resource instead.\n" +
				"    occurrences: 1\n" +
				"    reason: 理由\n" +
				"    withdrawal: 撤回の条件\n"
		_, args := setup(t, ign, result{dir: "modules/a", json: awsWarningJSON})
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.Contains(t, out.String(), "✅ tf-validate: 1 ディレクトリを validate し、エラーと抑止されない警告はありません（抑止 2 件）")
	})
	t.Run("宣言が一部の警告にしか当たらなければ失敗する", func(t *testing.T) {
		t.Parallel()
		ign :=
			"warnings:\n" +
				"  - summary: Argument is deprecated\n" +
				"    path: modules/a/main.tf\n" +
				"    block: resource \"aws_iam_role\" \"this\"\n" +
				"    detail: inline_policy is deprecated. Use the aws_iam_role_policy resource instead.\n" +
				"    occurrences: 1\n" +
				"    reason: 理由\n" +
				"    withdrawal: 撤回の条件\n"
		_, args := setup(t, ign, result{dir: "modules/a", json: awsWarningJSON})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errFailed)
		assert.Contains(t, out.String(), "抑止されない警告が 1 件")
		assert.NotContains(t, out.String(), "件数の合わない抑止")
	})
	t.Run("どの警告にも当たらない宣言は失敗する", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, validIgnore, result{dir: "modules/a", json: cleanJSON})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errFailed)
		assert.Contains(t, out.String(), "当たった警告 0 件、宣言 1 件")
		assert.Contains(t, out.String(), "件数の合わない抑止の宣言が 1 件")
	})
	t.Run("エラーのあるディレクトリは diagnostic と落ちたディレクトリの行を出して失敗する", func(t *testing.T) {
		t.Parallel()
		base, args := setup(t, empty,
			result{dir: "modules/a", code: 1, json: errorJSON},
			result{dir: "usecases/b", json: cleanJSON},
			result{dir: "usecases/c", code: 1, json: errorJSON})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errFailed)
		assert.Contains(t, out.String(), filepath.Join(base, "modules/a")+"/main.tf:2:11: Reference to undeclared input variable")
		assert.Contains(t, out.String(), fmt.Sprintf("❌ tf-validate が落ちたディレクトリ: %s %s",
			filepath.Join(base, "modules/a"), filepath.Join(base, "usecases/c")))
	})
	t.Run("エラーと警告が両方あれば両方を報告して失敗する", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", code: 1, json: errorJSON}, result{dir: "modules/b", json: awsWarningJSON})
		var out bytes.Buffer
		err := run(args, &out)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, out.String(), "❌ tf-validate が落ちたディレクトリ:")
		assert.Contains(t, out.String(), "抑止されない警告が 2 件")
	})
	t.Run("読めない出力は stderr を添えてエラーにする", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", code: 1, json: "", stderr: "chdir failed"})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errUnreadable)
		assert.Contains(t, out.String(), "chdir failed")
	})
	t.Run("通った validate でも stderr が空でなければ出す", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", json: cleanJSON, stderr: "provider の注意"})
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.Contains(t, out.String(), "modules/a の validate の stderr:\n  │ provider の注意\n")
	})
	t.Run("JSON のファイルが無ければエラー", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		manifest := writeFile(t, base, "manifest.tsv", "modules/a\t0\t"+base+"/missing.json\t"+base+"/missing.err\n")
		ign := writeFile(t, base, "ignore.yaml", empty)
		require.ErrorIs(t, run([]string{"-base", base, "-manifest", manifest, "-ignore", ign}, &bytes.Buffer{}), errUnreadable)
	})
	t.Run("基点の外を指す警告はエラー", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", json: childWarningJSON("../../../outside.tf")})
		require.ErrorIs(t, run(args, &bytes.Buffer{}), errOutside)
	})
	t.Run("抑止の宣言が壊れていればエラー", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, "", result{dir: "modules/a", json: cleanJSON})
		require.ErrorIs(t, run(args, &bytes.Buffer{}), errIgnore)
	})
	t.Run("結果の一覧が0件ならエラー", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty)
		require.ErrorIs(t, run(args, &bytes.Buffer{}), errNoTargets)
	})
	for _, missing := range []string{"-base", "-manifest", "-ignore"} {
		t.Run(missing+" が無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, args := setup(t, empty, result{dir: "modules/a", json: cleanJSON})
			for i := range args {
				if args[i] == missing {
					args = append(args[:i:i], args[i+2:]...)
					break
				}
			}
			require.ErrorIs(t, run(args, &bytes.Buffer{}), errUsage)
		})
	}
	t.Run("知らない引数はエラー", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"-unknown"}, &bytes.Buffer{}), errUsage)
	})
	t.Run("本文の各行に印を付け、行頭に workflow command を作らない", func(t *testing.T) {
		t.Parallel()
		data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[
{"severity":"warning","summary":"Deprecated variable got a value","detail":"use new\n::warning::forged",
"range":{"filename":"main.tf","start":{"line":3,"column":12}},"snippet":{"context":"module \"i\""}}]}`
		_, args := setup(t, empty, result{dir: "modules/a", json: data, stderr: "::error::forged"})
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errFailed)
		assert.Contains(t, out.String(), "  │ use new\n  │ ::warning::forged\n")
		assert.Contains(t, out.String(), "  │ ::error::forged\n")
		for _, line := range strings.Split(out.String(), "\n") {
			assert.False(t, strings.HasPrefix(strings.TrimSpace(line), "::"), line)
		}
	})
	t.Run("空白だけの stderr は出さない", func(t *testing.T) {
		t.Parallel()
		_, args := setup(t, empty, result{dir: "modules/a", json: cleanJSON, stderr: " \n\t\n"})
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.NotContains(t, out.String(), "stderr")
	})
	t.Run("stderr のファイルが無くても JSON で判定する", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		js := writeFile(t, base, "a.json", cleanJSON)
		manifest := writeFile(t, base, "manifest.tsv", base+"/modules/a\t0\t"+js+"\t"+base+"/missing.err\n")
		ign := writeFile(t, base, "ignore.yaml", empty)
		var out bytes.Buffer
		require.NoError(t, run([]string{"-base", base, "-manifest", manifest, "-ignore", ign}, &out))
		assert.Contains(t, out.String(), "✅ tf-validate: 1 ディレクトリを validate し")
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"成功は 0", nil, 0},
		{"エラーか警告で落ちたときは 1", xerrors.Wrap(errFailed, "x"), 1},
		{"読めない出力は 2", xerrors.Wrap(errUnreadable, "x"), 2},
		{"壊れた宣言は 2", xerrors.Wrap(errIgnore, "x"), 2},
		{"0件は 2", xerrors.Wrap(errNoTargets, "x"), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

func Test_quote(t *testing.T) {
	t.Parallel()
	t.Run("すべての行に空白でない印を前置し、行頭に workflow command を作らない", func(t *testing.T) {
		t.Parallel()
		got := quote("a\n::warning::x\n")
		assert.Equal(t, "  │ a\n  │ ::warning::x\n  │ ", got)
		for _, line := range strings.Split(got, "\n") {
			assert.False(t, strings.HasPrefix(strings.TrimSpace(line), "::"), line)
		}
	})
}

func Test_oneLine(t *testing.T) {
	t.Parallel()
	t.Run("改行を空白に置き換える", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "a ::warning::x", oneLine("a\n::warning::x"))
	})
}

func Test_readManifest(t *testing.T) {
	t.Parallel()
	t.Run("4つの欄の行を読む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "m.tsv", "modules/a\t0\ta.json\ta.err\n\nusecases/b\t1\tb.json\tb.err\n")
		got, err := readManifest(p)
		require.NoError(t, err)
		assert.Equal(t, []manifestEntry{
			{dir: "modules/a", code: 0, json: "a.json", stderr: "a.err"},
			{dir: "usecases/b", code: 1, json: "b.json", stderr: "b.err"},
		}, got)
	})
	t.Run("無いファイルはエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readManifest(t.TempDir() + "/missing.tsv")
		require.ErrorIs(t, err, errManifest)
	})
	t.Run("読み進められない一覧（ディレクトリ）はエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readManifest(t.TempDir())
		require.ErrorIs(t, err, errManifest)
	})
	broken := []struct{ name, body string }{
		{"欄が3つ", "modules/a\t0\ta.json\n"},
		{"欄が5つ", "modules/a\t0\ta.json\ta.err\tx\n"},
		{"ディレクトリが空", "\t0\ta.json\ta.err\n"},
		{"JSON のパスが空", "modules/a\t0\t\ta.err\n"},
		{"stderr のパスが空", "modules/a\t0\ta.json\t\n"},
		{"終了コードが数でない", "modules/a\tx\ta.json\ta.err\n"},
		{"終了コードが負", "modules/a\t-1\ta.json\ta.err\n"},
	}
	for _, c := range broken {
		t.Run(c.name+"はエラー", func(t *testing.T) {
			t.Parallel()
			_, err := readManifest(writeFile(t, t.TempDir(), "m.tsv", c.body))
			require.ErrorIs(t, err, errManifest)
		})
	}
	for _, body := range []string{"", "\n\n"} {
		t.Run(fmt.Sprintf("行の無い一覧 %q は0件のエラー", body), func(t *testing.T) {
			t.Parallel()
			_, err := readManifest(writeFile(t, t.TempDir(), "m.tsv", body))
			require.ErrorIs(t, err, errNoTargets)
		})
	}
}
