package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/harden"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML / シェルの字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const testSSOT = `# comment
[class.base]
hosts = ["b1.test", "b2.test"]
ips = ["127.0.0.100"]

[class.mise]
hosts = [
  "m1.test",
  "m2.test",
]

[class.image]
includes = ["mise"]
hosts = ["i1.test"]

[job."a.yaml:one"]
classes = ["image"]
extra = ["x1.test"]

[job."b.yaml:two"]
classes = []

[job."b.yaml:audited"]
egress_policy = "audit"

[root."env/t/identity"]
jobs = ["a.yaml:one"]

[type.aws_x_]
hosts = ["i1.test"]

[type.aws_none]
hosts = []
`

// editorconfig-checker-enable

const workflowHead = "name: T\n\njobs:\n"

var (
	errWD = xerrors.New("getwd failed")
	// oneHosts は testSSOT が a.yaml:one へ展開する許可リスト。
	oneHosts = []string{"b1.test", "b2.test", "m1.test", "m2.test", "i1.test", "x1.test"}
	testIPs  = []string{"127.0.0.100"}
)

// testTerraform は testSSOT の root "env/t/identity" に置く設定。型は testSSOT の 2 つの type に 1 つずつ当たる。
const testTerraform = "resource \"aws_x_thing\" \"a\" {}\ndata \"aws_none\" \"b\" {}\n"

// testCIMake は testSSOT の root を実行する CI の入口。testWorkflow の one のジョブが呼ぶ。
var testCIMake = ciMakeFixture([3]string{cientry.Plan, "t", "identity"})

// ciMakeFixture は cientry.Parse が読める ci.mk の本文を組む。entries は {種別, env, root}。
// 登録簿に無い種別の role は plan にする。
func ciMakeFixture(entries ...[3]string) string {
	var b strings.Builder
	var defined []string
	for _, e := range entries {
		if !slices.Contains(defined, e[0]) {
			defined = append(defined, e[0])
			b.WriteString("define CI_" + strings.ToUpper(e[0]) + "\nx\nendef\n")
		}
	}
	for _, e := range entries {
		target := "ci-" + e[0] + "-" + e[1] + "-" + e[2]
		role := "plan"
		if k, ok := cientry.Lookup(e[0]); ok {
			role = k.Role(cientry.Root(e[2]))
		}
		b.WriteString(".PHONY: " + target + "\n" +
			target + ": SHELL := /bin/bash\n" +
			target + ": TF_LOCAL_PROFILE := $(call CI_PROFILE," + e[1] + "," + role + ")\n" +
			target + ":\n\t@$(call CI_" + strings.ToUpper(e[0]) + "," + e[1] + "," + e[2] + ")\n")
	}
	return b.String()
}

// testWorkflow は、再試行の無い bullfrog の step（name と uses:）に tail を続け、その後ろへ checkout の step を置いた
// 1 ジョブの workflow。one のジョブは、さらに testSSOT の root を CI の入口で実行する。
func testWorkflow(jobID string, tail ...string) string {
	return jobWorkflow(jobID, append([]string{nameLine, usesLine}, tail...))
}

// jobWorkflow は、block を先頭の step の行として置き、その後ろへ checkout の step を置いた 1 ジョブの workflow。
func jobWorkflow(jobID string, block []string) string {
	var b strings.Builder
	b.WriteString(workflowHead + "  " + jobID + ":\n    steps:\n")
	for _, l := range block {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n      - name: Checkout\n")
	if jobID == "one" {
		b.WriteString("\n      - name: Plan\n        run: make ci-plan-t-identity\n")
	}
	return b.String()
}

func staleWorkflow(jobID string) string {
	return testWorkflow(jobID, "        with:", "          egress-policy: block",
		"          allowed-endpoints: >", "            stale:443")
}

func syncedWorkflow(jobID, policy string, hosts ...string) string {
	return jobWorkflow(jobID, harden.Render(usesLine, renderWith(policy, testIPs, hosts)))
}

func syncedOne() string {
	return syncedWorkflow("one", policyBlock, oneHosts...)
}

// bWorkflow は testSSOT が宣言する b.yaml 側の 2 ジョブ（block と audit）を SSOT どおりに持つ workflow。
func bWorkflow() string {
	return syncedWorkflow("two", policyBlock, "b1.test", "b2.test") +
		strings.TrimPrefix(syncedWorkflow("audited", policyAudit), workflowHead)
}

func newTestRepo(t *testing.T, workflows map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ssotFile), []byte(testSSOT), filePerm))
	writeFile(t, root, "env/t/identity/main.tf", testTerraform)
	writeFile(t, root, "usecases/u/reference/main.tf", testTerraform)
	writeFile(t, root, cientry.Makefile, testCIMake)
	for name, body := range workflows {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "workflows", name), []byte(body), filePerm))
	}
	return root
}

// unlockTestRepo は newTestRepo に、b.yaml の two のジョブが run で CI の入口を呼び、testSSOT の root がそのジョブも
// 名指す宣言を足したリポジトリ。two の許可リストは base だけで、型の宛先 i1.test を持たない。
func unlockTestRepo(t *testing.T, run string) string {
	t.Helper()
	b := syncedWorkflow("two", policyBlock, "b1.test", "b2.test") + "\n      - name: Unlock\n        run: " + run + "\n" +
		strings.TrimPrefix(syncedWorkflow("audited", policyAudit), workflowHead)
	root := newTestRepo(t, map[string]string{"a.yaml": syncedWorkflow("one", policyBlock, oneHosts...), "b.yaml": b})
	writeFile(t, root, cientry.Makefile, ciMakeFixture(
		[3]string{cientry.Plan, "t", "identity"}, [3]string{cientry.Apply, "t", "identity"}, [3]string{cientry.Unlock, "t", "identity"}))
	ssot := strings.Replace(testSSOT, `jobs = ["a.yaml:one"]`, `jobs = ["a.yaml:one", "b.yaml:two"]`, 1)
	require.NotEqual(t, testSSOT, ssot)
	writeFile(t, root, ssotFile, ssot)
	return root
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), filePerm))
}

func newSSOT() *ssot {
	return &ssot{
		classes: map[string]*class{}, jobs: map[string]*jobSpec{},
		roots: map[string]*rootSpec{}, types: map[string]*typeSpec{}, regionSets: map[string]*regionSetSpec{},
	}
}

func mustParse(t *testing.T, body string) *ssot {
	t.Helper()
	s, err := parseSSOT(body)
	require.NoError(t, err)
	return s
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("apply はブロックを SSOT の内容へ書き換える", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			require.NoError(t, run([]string{"apply"}, func() (string, error) { return root, nil }))
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, syncedWorkflow("one", policyBlock, oneHosts...), string(got))
		})

		t.Run(".yml の workflow も走査対象にする", func(t *testing.T) {
			t.Parallel()
			// GitHub は .yaml と .yml をどちらも workflow として実行する。片方だけを
			// 走査していると、SSOT が知らない job が黙って通る。SSOT は c.yml を
			// 知らないので、走査していれば errJobMissing で落ちる。
			root := newTestRepo(t, map[string]string{
				"a.yaml": syncedWorkflow("one", policyBlock, oneHosts...),
				"b.yaml": bWorkflow(),
				"c.yml":  syncedWorkflow("three", policyBlock, "b1.test", "b2.test"),
			})
			require.ErrorIs(t, run([]string{"check"}, func() (string, error) { return root, nil }), errJobMissing)
		})

		t.Run("check はドリフトが無ければ成功する", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": syncedWorkflow("one", policyBlock, oneHosts...),
				"b.yaml": bWorkflow(),
			})
			assert.NoError(t, run([]string{"check"}, func() (string, error) { return root, nil }))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("サブコマンドが無ければ usage を返す", func(t *testing.T) {
			t.Parallel()
			err := run(nil, func() (string, error) { return "", nil })
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("未知のサブコマンドは usage を返す", func(t *testing.T) {
			t.Parallel()
			err := run([]string{"resolve"}, func() (string, error) { return "", nil })
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("check はドリフトを報告し、ファイルを書き換えない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			require.ErrorIs(t, run([]string{"check"}, func() (string, error) { return root, nil }), errEgressDrift)
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("bullfrog の step が 1 件も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": workflowHead + "  one:\n    uses: ./.github/workflows/notify.yaml\n",
			})
			require.ErrorIs(t, run([]string{"check"}, func() (string, error) { return root, nil }), errNoTargets)
		})

		t.Run("apply は 1 ファイルでも失敗すれば何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
				"c.yaml": staleWorkflow("unknown"),
			})
			require.ErrorIs(t, run([]string{"apply"}, func() (string, error) { return root, nil }), errJobMissing)
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("型が呼ぶ宛先がジョブに無ければ、apply は何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			writeFile(t, root, "env/t/identity/kms.tf", "resource \"aws_x_key\" \"k\" {}\nresource \"aws_y_key\" \"k\" {}\n")
			require.ErrorIs(t, run([]string{"apply"}, func() (string, error) { return root, nil }), errTypeUnmapped)
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("作業ディレクトリの取得失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			err := run([]string{"check"}, func() (string, error) { return "", errWD })
			require.ErrorIs(t, err, errWD)
		})
	})
}

func Test_parseSSOT(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("クラスとジョブを宣言順で読む", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.Len(t, s.classes, 3)
			assert.Equal(t, []string{"a.yaml:one", "b.yaml:two", "b.yaml:audited"}, s.jobOrder)
			assert.Equal(t, []string{"b1.test", "b2.test"}, s.baseHosts)
			assert.Equal(t, []string{"127.0.0.100"}, s.baseIPs)
			assert.Equal(t, []string{"mise"}, s.classes["image"].includes)
			assert.Equal(t, policyAudit, s.jobs["b.yaml:audited"].policy)
			assert.Equal(t, []string{"env/t/identity"}, s.rootOrder)
			assert.Equal(t, []string{"a.yaml:one"}, s.roots["env/t/identity"].jobs)
			assert.Equal(t, []string{"aws_x_", "aws_none"}, s.typeOrder)
			assert.Equal(t, []string{"i1.test"}, s.types["aws_x_"].hosts)
			assert.Empty(t, s.types["aws_none"].hosts)
		})

		t.Run("region_set の集合で、宛先の {<set>} を展開する", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT+"\n[region_set.d]\nregions = [\"us-east-1\", \"eu-west-1\"]\n\n[type.aws_r_]\nhosts = [\"r.{d}.amazonaws.com\"]\n")
			assert.Equal(t, []string{"r.us-east-1.amazonaws.com", "r.eu-west-1.amazonaws.com"}, s.types["aws_r_"].hosts)
		})

		t.Run("egress_policy 未指定は block とみなす", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.Equal(t, policyBlock, s.jobs["a.yaml:one"].policy)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("region_set の regions の無い集合を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT(testSSOT + "\n[region_set.d]\n\n[type.aws_r_]\nhosts = [\"r.{d}.amazonaws.com\"]\n")
			require.ErrorIs(t, err, errSSOTRegionSet)
		})

		t.Run("region_set の展開の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT(testSSOT + "\n[region_set.d]\nregions = [\"us-east-1\"]\n")
			require.ErrorIs(t, err, errSSOTRegionSet)
		})

		t.Run("セクションの外の値を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT(`hosts = ["x.test"]`)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("解釈できない行を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.base]\nhosts\n")
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("同一セクション内のキー重複を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.base]\nhosts = [\"a.test\"]\nhosts = [\"b.test\"]\n")
			require.ErrorIs(t, err, errSSOTDuplicate)
		})
	})
}

func Test_stripComment(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("行末コメントを落とす", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, `hosts = ["a.test"] `, stripComment(`hosts = ["a.test"] # note`))
		})

		t.Run("コメントが無い行はそのまま返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "[class.base]", stripComment("[class.base]"))
		})
	})
}

func Test_parseAssignment(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("スカラーを読む", func(t *testing.T) {
			t.Parallel()
			key, values, next, err := parseAssignment([]string{`egress_policy = "audit"`}, 0)
			require.NoError(t, err)
			assert.Equal(t, "egress_policy", key)
			assert.Equal(t, []string{"audit"}, values)
			assert.Equal(t, 0, next)
		})

		t.Run("1 行の配列を読む", func(t *testing.T) {
			t.Parallel()
			key, values, next, err := parseAssignment([]string{`hosts = ["a.test", "b.test"]`}, 0)
			require.NoError(t, err)
			assert.Equal(t, "hosts", key)
			assert.Equal(t, []string{"a.test", "b.test"}, values)
			assert.Equal(t, 0, next)
		})

		t.Run("複数行の配列は閉じ括弧まで取り込む", func(t *testing.T) {
			t.Parallel()
			lines := []string{"hosts = [", `  "a.test",`, `  "b.test", # note`, "]"}
			key, values, next, err := parseAssignment(lines, 0)
			require.NoError(t, err)
			assert.Equal(t, "hosts", key)
			assert.Equal(t, []string{"a.test", "b.test"}, values)
			assert.Equal(t, 3, next)
		})

		t.Run("空配列を読む", func(t *testing.T) {
			t.Parallel()
			key, values, _, err := parseAssignment([]string{"classes = []"}, 0)
			require.NoError(t, err)
			assert.Equal(t, "classes", key)
			assert.Empty(t, values)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("代入として読めない行を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{"hosts:"}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("単一引用符の要素を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{`hosts = ['a.test', "b.test"]`}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("引用符の無い要素を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{"hosts = [", "  a.test,", "]"}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		for name, lines := range map[string][]string{
			"閉じ括弧の後ろに別の配列が続く": {`hosts = ["a.test"] ["b.test"]`},
			"閉じ括弧の後ろに要素が続く":   {`hosts = ["a.test"] "b.test"`},
			"要素の間のカンマが無い":     {`hosts = ["a.test" "b.test"]`},
			"カンマが続く":          {`hosts = ["a.test",, "b.test"]`},
			"引用符の無い Q を含む":    {`hosts = [Q]`},
			"複数行で要素の間のカンマが無い": {"hosts = [", `  "a.test"`, `  "b.test",`, "]"},
		} {
			t.Run(name+"配列を弾く", func(t *testing.T) {
				t.Parallel()
				_, _, _, err := parseAssignment(lines, 0)
				require.ErrorIs(t, err, errSSOTSyntax)
			})
		}

		t.Run("配列のキーへのスカラーを弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{`hosts = "a.test"`}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("スカラーのキーへの配列を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{`egress_policy = ["audit"]`}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("閉じ括弧の後ろの余りを弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{`hosts = ["a.test"] x`}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})

		t.Run("閉じられていない配列を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseAssignment([]string{"hosts = [", `  "a.test",`}, 0)
			require.ErrorIs(t, err, errSSOTSyntax)
		})
	})
}

func Test_assign(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("クラスのキーを載せる", func(t *testing.T) {
			t.Parallel()
			c := &class{}
			require.NoError(t, assign(section{class: c}, "includes", []string{"mise"}))
			require.NoError(t, assign(section{class: c}, "hosts", []string{"a.test"}))
			require.NoError(t, assign(section{class: c}, "ips", []string{"127.0.0.100"}))
			assert.Equal(t, []string{"mise"}, c.includes)
			assert.Equal(t, []string{"a.test"}, c.hosts)
			assert.Equal(t, []string{"127.0.0.100"}, c.ips)
		})

		t.Run("ジョブのキーを載せる", func(t *testing.T) {
			t.Parallel()
			j := &jobSpec{}
			require.NoError(t, assign(section{job: j}, "classes", []string{"image"}))
			require.NoError(t, assign(section{job: j}, "extra", []string{"a.test"}))
			require.NoError(t, assign(section{job: j}, "egress_policy", []string{policyAudit}))
			assert.Equal(t, []string{"image"}, j.classes)
			assert.Equal(t, []string{"a.test"}, j.extra)
			assert.Equal(t, policyAudit, j.policy)
		})

		t.Run("root のキーを載せる", func(t *testing.T) {
			t.Parallel()
			r := &rootSpec{}
			require.NoError(t, assign(section{root: r}, "jobs", []string{"a.yaml:one"}))
			assert.Equal(t, []string{"a.yaml:one"}, r.jobs)
		})

		t.Run("type のキーを載せる", func(t *testing.T) {
			t.Parallel()
			ty := &typeSpec{}
			require.NoError(t, assign(section{typ: ty}, "hosts", []string{"a.test"}))
			assert.Equal(t, []string{"a.test"}, ty.hosts)
		})

		t.Run("region_set のキーを載せる", func(t *testing.T) {
			t.Parallel()
			rs := &regionSetSpec{}
			require.NoError(t, assign(section{regionSet: rs}, "regions", []string{"us-east-1"}))
			assert.Equal(t, []string{"us-east-1"}, rs.regions)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未知のキーを弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{class: &class{}}, "endpoints", []string{"a.test"}), errSSOTSyntax)
		})

		t.Run("クラスのキーをジョブへ書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{job: &jobSpec{}}, "includes", []string{"mise"}), errSSOTSyntax)
		})

		t.Run("ips をジョブへ書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{job: &jobSpec{}}, "ips", []string{"127.0.0.100"}), errSSOTSyntax)
		})

		t.Run("root へ hosts を書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{root: &rootSpec{}}, "hosts", []string{"a.test"}), errSSOTSyntax)
		})

		t.Run("region_set へ hosts を書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{regionSet: &regionSetSpec{}}, "hosts", []string{"a.test"}), errSSOTSyntax)
		})

		t.Run("type へ jobs を書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{typ: &typeSpec{}}, "jobs", []string{"a.yaml:one"}), errSSOTSyntax)
		})

		t.Run("リスト内の重複を弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(section{class: &class{}}, "hosts", []string{"a.test", "a.test"}), errSSOTDuplicate)
		})
	})
}

func Test_requireUnique(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("重複が無ければ成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, requireUnique([]string{"a", "b"}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("重複を弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, requireUnique([]string{"a", "b", "a"}), errSSOTDuplicate)
		})
	})
}

func Test_ssot_openSection(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("各セクションを登録し、代入の宛先を返す", func(t *testing.T) {
			t.Parallel()
			s := newSSOT()
			c, err := s.openSection("class", "base")
			require.NoError(t, err)
			j, err := s.openSection("job", "a.yaml:one")
			require.NoError(t, err)
			r, err := s.openSection("root", "tf/root")
			require.NoError(t, err)
			ty, err := s.openSection("type", "aws_x_")
			require.NoError(t, err)
			rs, err := s.openSection("region_set", "d")
			require.NoError(t, err)
			assert.Equal(t, section{regionSet: s.regionSets["d"]}, rs)
			assert.Equal(t, section{class: s.classes["base"]}, c)
			assert.Equal(t, section{job: s.jobs["a.yaml:one"]}, j)
			assert.Equal(t, section{root: s.roots["tf/root"]}, r)
			assert.Equal(t, section{typ: s.types["aws_x_"]}, ty)
			assert.Equal(t, []string{"a.yaml:one"}, s.jobOrder)
			assert.Equal(t, []string{"tf/root"}, s.rootOrder)
			assert.Equal(t, []string{"aws_x_"}, s.typeOrder)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for kind, name := range map[string]string{
			"class": "base", "job": "a.yaml:one", "root": "tf/root", "type": "aws_x_", "region_set": "d",
		} {
			t.Run(kind+"の重複を弾く", func(t *testing.T) {
				t.Parallel()
				s := newSSOT()
				_, err := s.openSection(kind, name)
				require.NoError(t, err)
				_, err = s.openSection(kind, name)
				require.ErrorIs(t, err, errSSOTDuplicate)
			})
		}
	})
}

func Test_ssot_expandRegionSets(t *testing.T) {
	t.Parallel()

	withSets := func(sets map[string][]string) *ssot {
		s := newSSOT()
		for name, rs := range sets {
			s.regionSets[name] = &regionSetSpec{regions: rs}
		}
		return s
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("class・job の extra・type の宛先を、集合の宣言順に展開する", func(t *testing.T) {
			t.Parallel()
			s := withSets(map[string][]string{"d": {"us-east-1", "eu-west-1"}})
			s.classes["c"] = &class{hosts: []string{"a.test", "c.{d}.amazonaws.com"}}
			s.jobs["a.yaml:one"] = &jobSpec{extra: []string{"j.{d}.amazonaws.com"}}
			s.jobOrder = []string{"a.yaml:one"}
			s.types["aws_r_"] = &typeSpec{hosts: []string{"r-{d}.amazonaws.com"}}
			s.typeOrder = []string{"aws_r_"}
			require.NoError(t, s.expandRegionSets())
			assert.Equal(t, []string{"a.test", "c.us-east-1.amazonaws.com", "c.eu-west-1.amazonaws.com"}, s.classes["c"].hosts)
			assert.Equal(t, []string{"j.us-east-1.amazonaws.com", "j.eu-west-1.amazonaws.com"}, s.jobs["a.yaml:one"].extra)
			assert.Equal(t, []string{"r-us-east-1.amazonaws.com", "r-eu-west-1.amazonaws.com"}, s.types["aws_r_"].hosts)
		})

		t.Run("region_set が無ければ宛先を変えない", func(t *testing.T) {
			t.Parallel()
			s := newSSOT()
			s.classes["c"] = &class{hosts: []string{"a.test"}}
			require.NoError(t, s.expandRegionSets())
			assert.Equal(t, []string{"a.test"}, s.classes["c"].hosts)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			sets  map[string][]string
			hosts []string
		}{
			"未知の集合":           {map[string][]string{"d": {"us-east-1"}}, []string{"a.{d}.test", "b.{x}.test"}},
			"使われない集合":         {map[string][]string{"d": {"us-east-1"}, "u": {"us-east-1"}}, []string{"a.{d}.test"}},
			"空の集合":            {map[string][]string{"d": {}}, []string{"a.{d}.test"}},
			"region の形でない要素":  {map[string][]string{"d": {"global"}}, []string{"a.{d}.test"}},
			"名前の無い {}":        {map[string][]string{"d": {"us-east-1"}}, []string{"a.{d}.test", "b.{}.test"}},
			"1 つの宛先に 2 つの {}": {map[string][]string{"d": {"us-east-1"}}, []string{"{d}.{d}.test"}},
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				s := withSets(tc.sets)
				s.types["aws_r_"] = &typeSpec{hosts: tc.hosts}
				s.typeOrder = []string{"aws_r_"}
				require.ErrorIs(t, s.expandRegionSets(), errSSOTRegionSet)
			})
		}

		t.Run("展開した宛先と、同じリストのリテラルの宛先の重複を弾く", func(t *testing.T) {
			t.Parallel()
			s := withSets(map[string][]string{"d": {"us-east-1"}})
			s.types["aws_r_"] = &typeSpec{hosts: []string{"a.{d}.test", "a.us-east-1.test"}}
			s.typeOrder = []string{"aws_r_"}
			require.ErrorIs(t, s.expandRegionSets(), errSSOTDuplicate)
		})

		t.Run("job の extra の未知の集合を弾く", func(t *testing.T) {
			t.Parallel()
			s := newSSOT()
			s.jobs["a.yaml:one"] = &jobSpec{extra: []string{"j.{d}.test"}}
			s.jobOrder = []string{"a.yaml:one"}
			require.ErrorIs(t, s.expandRegionSets(), errSSOTRegionSet)
		})

		t.Run("class の未知の集合を弾く", func(t *testing.T) {
			t.Parallel()
			s := newSSOT()
			s.classes["c"] = &class{hosts: []string{"c.{d}.test"}}
			require.ErrorIs(t, s.expandRegionSets(), errSSOTRegionSet)
		})
	})
}

func Test_ssot_validate(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base クラスの hosts を暗黙適用分として取り出す", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.Equal(t, []string{"b1.test", "b2.test"}, s.baseHosts)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未定義クラスの includes を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.image]\nincludes = [\"nope\"]\n")
			require.ErrorIs(t, err, errSSOTUnknownClass)
		})

		t.Run("base 以外のクラスの ips を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.mise]\nips = [\"127.0.0.100\"]\n")
			require.ErrorIs(t, err, errSSOTIPScope)
		})

		t.Run("IPv4 でない ips を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.base]\nips = [\"127.0.0.100:53\"]\n")
			require.ErrorIs(t, err, errSSOTIP)
		})

		t.Run("クラスの port 付きの宛先を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[class.base]\nhosts = [\"github.com:443\"]\n")
			require.ErrorIs(t, err, errSSOTDomain)
		})

		t.Run("クラスの循環継承を弾く", func(t *testing.T) {
			t.Parallel()
			body := "[class.a]\nincludes = [\"b\"]\n\n[class.b]\nincludes = [\"a\"]\n\n" +
				"[job.\"w.yaml:j\"]\nclasses = [\"a\"]\n"
			_, err := parseSSOT(body)
			require.ErrorIs(t, err, errSSOTClassCycle)
		})

		for name, body := range map[string]string{
			"大文字を含む":   "[type.AWS_x_]\nhosts = []\n",
			"_ が続く":    "[type.aws__x]\nhosts = []\n",
			"_ で始まる":   "[type._aws]\nhosts = []\n",
			"ハイフンを含む":  "[type.aws-x]\nhosts = []\n",
			"1 要素の接頭辞": "[type.aws_]\nhosts = [\"x.test\"]\n",
		} {
			t.Run("type の名前が"+name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := parseSSOT(body)
				require.ErrorIs(t, err, errSSOTTypeName)
			})
		}

		t.Run("宛先の無い接頭辞の type を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[type.aws_x_]\nhosts = []\n")
			require.ErrorIs(t, err, errSSOTTypeHosts)
		})

		t.Run("宛先の無い型名の type は受ける", func(t *testing.T) {
			t.Parallel()
			s, err := parseSSOT("[type.aws_x]\nhosts = []\n")
			require.NoError(t, err)
			assert.Empty(t, s.types["aws_x"].hosts)
		})

		t.Run("1 要素の型名の type は受ける", func(t *testing.T) {
			t.Parallel()
			s, err := parseSSOT("[type.http]\nhosts = [\"example.test\"]\n")
			require.NoError(t, err)
			assert.Equal(t, []string{"example.test"}, s.types["http"].hosts)
		})

		t.Run("type の port 付きの宛先を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[type.aws_x_]\nhosts = [\"kms.test:443\"]\n")
			require.ErrorIs(t, err, errSSOTDomain)
		})

		t.Run("root の不正を validateRoot から伝播する", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[root.\"tf/root\"]\njobs = [\"w.yaml:nope\"]\n")
			require.ErrorIs(t, err, errSSOTUnknownJob)
		})
	})
}

func Test_ssot_validateRoot(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言済みのジョブを持つ相対パスを受ける", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.NoError(t, s.validateRoot("env/admin/identity", &rootSpec{jobs: []string{"a.yaml:one"}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, root := range map[string]string{
			"絶対パス":        "/env/admin",
			"親を指す":        "../env",
			"途中で親へ戻る":     "env/../../x",
			"正規化されていない":   "env//admin",
			"末尾にスラッシュがある": "env/admin/",
			"現在のディレクトリ":   ".",
			"バックスラッシュを含む": `env\admin`,
		} {
			t.Run(name+"の root を弾く", func(t *testing.T) {
				t.Parallel()
				s := mustParse(t, testSSOT)
				require.ErrorIs(t, s.validateRoot(root, &rootSpec{}), errSSOTRootPath)
			})
		}

		t.Run("未定義のジョブを弾く", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			require.ErrorIs(t, s.validateRoot("tf/root", &rootSpec{jobs: []string{"w.yaml:nope"}}), errSSOTUnknownJob)
		})
	})
}

func Test_ssot_validateJob(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未知の egress_policy を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[job.\"w.yaml:j\"]\negress_policy = \"warn\"\n")
			require.ErrorIs(t, err, errSSOTPolicy)
		})

		t.Run("audit ジョブがクラスを持つことを弾く", func(t *testing.T) {
			t.Parallel()
			body := "[class.x]\nhosts = [\"x.test\"]\n\n[job.\"w.yaml:j\"]\negress_policy = \"audit\"\nclasses = [\"x\"]\n"
			_, err := parseSSOT(body)
			require.ErrorIs(t, err, errSSOTPolicy)
		})

		t.Run("audit ジョブが許可リストを持つことを弾く", func(t *testing.T) {
			t.Parallel()
			body := "[job.\"w.yaml:j\"]\negress_policy = \"audit\"\nextra = [\"a.test\"]\n"
			_, err := parseSSOT(body)
			require.ErrorIs(t, err, errSSOTPolicy)
		})

		t.Run("base の明示宣言を弾く", func(t *testing.T) {
			t.Parallel()
			body := "[class.base]\nhosts = [\"b.test\"]\n\n[job.\"w.yaml:j\"]\nclasses = [\"base\"]\n"
			_, err := parseSSOT(body)
			require.ErrorIs(t, err, errSSOTBaseClass)
		})

		t.Run("未定義クラスの宣言を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[job.\"w.yaml:j\"]\nclasses = [\"nope\"]\n")
			require.ErrorIs(t, err, errSSOTUnknownClass)
		})

		t.Run("extra の port 付きの宛先を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSSOT("[job.\"w.yaml:j\"]\nextra = [\"hooks.slack.com:443\"]\n")
			require.ErrorIs(t, err, errSSOTDomain)
		})
	})
}

func Test_ssot_hostsFor(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base → クラス（継承込み）→ extra の順で並べる", func(t *testing.T) {
			t.Parallel()
			hosts, err := mustParse(t, testSSOT).hostsFor("a.yaml:one")
			require.NoError(t, err)
			assert.Equal(t, []string{"b1.test", "b2.test", "m1.test", "m2.test", "i1.test", "x1.test"}, hosts)
		})

		t.Run("クラス宣言が無いジョブは base だけを持つ", func(t *testing.T) {
			t.Parallel()
			hosts, err := mustParse(t, testSSOT).hostsFor("b.yaml:two")
			require.NoError(t, err)
			assert.Equal(t, []string{"b1.test", "b2.test"}, hosts)
		})

		t.Run("audit ジョブは許可リストを持たない", func(t *testing.T) {
			t.Parallel()
			hosts, err := mustParse(t, testSSOT).hostsFor("b.yaml:audited")
			require.NoError(t, err)
			assert.Empty(t, hosts)
		})

		// 循環の検出が訪問済みを消し忘れると、正しい菱形の継承を循環と取り違える。
		t.Run("共通の継承元を 2 経路で持つクラスは循環と見なさない", func(t *testing.T) {
			t.Parallel()
			body := "[class.a]\nhosts = [\"a.test\"]\n\n[class.b]\nincludes = [\"a\"]\n\n[class.c]\nincludes = [\"a\"]\n\n" +
				"[job.\"w.yaml:j\"]\nclasses = [\"b\", \"c\"]\n"
			hosts, err := mustParse(t, body).hostsFor("w.yaml:j")
			require.NoError(t, err)
			assert.Equal(t, []string{"a.test"}, hosts)
		})

		t.Run("クラス間で重複するホストは初出だけを残す", func(t *testing.T) {
			t.Parallel()
			body := "[class.base]\nhosts = [\"b.test\"]\n\n[class.x]\nhosts = [\"b.test\", \"x.test\"]\n\n" +
				"[job.\"w.yaml:j\"]\nclasses = [\"x\"]\nextra = [\"x.test\"]\n"
			hosts, err := mustParse(t, body).hostsFor("w.yaml:j")
			require.NoError(t, err)
			assert.Equal(t, []string{"b.test", "x.test"}, hosts)
		})
	})
}

func Test_ssot_expandClass(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("継承元を先に展開する", func(t *testing.T) {
			t.Parallel()
			out := &orderedHosts{seen: map[string]bool{}}
			require.NoError(t, mustParse(t, testSSOT).expandClass("image", out, map[string]bool{}))
			assert.Equal(t, []string{"m1.test", "m2.test", "i1.test"}, out.list)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未定義クラスを弾く", func(t *testing.T) {
			t.Parallel()
			out := &orderedHosts{seen: map[string]bool{}}
			err := mustParse(t, testSSOT).expandClass("nope", out, map[string]bool{})
			require.ErrorIs(t, err, errSSOTUnknownClass)
		})

		t.Run("循環を弾く", func(t *testing.T) {
			t.Parallel()
			s := &ssot{classes: map[string]*class{"a": {includes: []string{"a"}}}, jobs: map[string]*jobSpec{}}
			out := &orderedHosts{seen: map[string]bool{}}
			require.ErrorIs(t, s.expandClass("a", out, map[string]bool{}), errSSOTClassCycle)
		})
	})
}

func Test_orderedHosts_add(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("初出順を保ったまま重複を落とす", func(t *testing.T) {
			t.Parallel()
			o := &orderedHosts{seen: map[string]bool{}}
			o.add([]string{"a", "b"})
			o.add([]string{"b", "c"})
			assert.Equal(t, []string{"a", "b", "c"}, o.list)
		})
	})
}

func Test_validateDomain(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		for name, d := range map[string]string{
			"ドメイン名":      "github.com",
			"先頭のワイルドカード": "*.blob.core.windows.net",
			"ハイフンを含むラベル": "mise-versions.jdx.dev",
		} {
			t.Run(name+"を通す", func(t *testing.T) {
				t.Parallel()
				assert.NoError(t, validateDomain(d))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()
		for name, d := range map[string]string{
			"port 付き":       "github.com:443",
			"単独のワイルドカード":    "*",
			"途中のワイルドカード":    "a.*.b",
			"ラベルの無いワイルドカード": "*.",
			"空文字":      "",
			"先頭のハイフン":  "-a.com",
			"末尾のハイフン":  "a-.com",
			"空のラベル":    "a..com",
			"空白":       "a .com",
			"ドットの無い名前": "localhost",
			"アンダースコア":  "_srv.example.com",
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, validateDomain(d), errSSOTDomain)
			})
		}
	})
}

func Test_validateIP(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		for name, v := range map[string]string{
			"loopback のアドレス":    "127.0.0.100",
			"loopback の下端":      "127.0.0.0",
			"loopback の上端":      "127.255.255.255",
			"loopback 全体の CIDR": "127.0.0.0/8",
			"単一アドレスの CIDR":      "127.0.0.100/32",
		} {
			t.Run(name+"を通す", func(t *testing.T) {
				t.Parallel()
				assert.NoError(t, validateIP(v))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()
		for name, v := range map[string]string{
			"全 IPv4 の CIDR":      "0.0.0.0/0",
			"loopback より広い CIDR": "127.0.0.0/7",
			"loopback の直下の外":     "126.255.255.255",
			"loopback の直上の外":     "128.0.0.0",
			"loopback の外の CIDR":  "128.0.0.0/8",
			"private の CIDR":     "10.0.0.0/8",
			"外部のアドレス":            "8.8.8.8",
			"IPv6":               "::1",
			"IPv6 の CIDR":        "::1/128",
			"IPv4 射影の IPv6":      "::ffff:127.0.0.1",
			"欠けたアドレス":            "1.2.3",
			"port 付き":            "127.0.0.100:53",
			"範囲外の prefix 長":      "127.0.0.0/33",
			"空文字":                "",
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, validateIP(v), errSSOTIP)
			})
		}
	})
}

func Test_unclaimedLine(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべての行が割り当てられていれば無しを返す", func(t *testing.T) {
			t.Parallel()
			body := "# head\n" + staleWorkflow("one")
			_, ok := unclaimedLine(body, workflow.SplitJobs(body))
			assert.False(t, ok)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("最初の見出しより前の本文を返す", func(t *testing.T) {
			t.Parallel()
			body := workflowHead + "  bad.job:\n    steps:\n" + strings.TrimPrefix(staleWorkflow("one"), workflowHead)
			l, ok := unclaimedLine(body, workflow.SplitJobs(body))
			require.True(t, ok)
			assert.Equal(t, workflow.Line{Number: 4, Text: "  bad.job:"}, l)
		})
	})
}

func Test_scanJobs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("再試行の無いジョブは本体の step の範囲を返す", func(t *testing.T) {
			t.Parallel()
			targets, err := scanJobs("a.yaml", staleWorkflow("one"))
			require.NoError(t, err)
			// 0 起点で name: は 5 行目、uses: は 6 行目、with: の4行は 7-10 行目。
			assert.Equal(t, []target{{jobKey: "a.yaml:one", start: 5, end: 11, uses: usesLine, mentions: 1}}, targets)
		})

		t.Run("生成した対は本体の頭から再試行の末尾までを範囲にする", func(t *testing.T) {
			t.Parallel()
			targets, err := scanJobs("a.yaml", syncedOne())
			require.NoError(t, err)
			pair := harden.Render(usesLine, renderWith(policyBlock, testIPs, oneHosts))
			assert.Equal(t, []target{{jobKey: "a.yaml:one", start: 5, end: 5 + len(pair), uses: usesLine, mentions: 2}}, targets)
		})

		// `*` の直後が英数字でないワイルドカードと、run: の && はエイリアスやアンカーではない。
		t.Run("ワイルドカードと && をアンカーとして扱わない", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(syncedOne(), "      - name: Checkout\n", "      - name: Checkout\n        run: a && b '*.yaml'\n", 1)
			require.NotEqual(t, syncedOne(), body)
			_, err := scanJobs("a.yaml", body)
			assert.NoError(t, err)
		})

		t.Run("三つ組の後ろで Guard で始まる always() の step を通す", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(syncedOne(), "      - name: Checkout\n", "      - name: Checkout\n        if: "+harden.Guard+" && always()\n", 1)
			_, err := scanJobs("a.yaml", body)
			assert.NoError(t, err)
		})

		t.Run("複数ジョブを job id で識別する", func(t *testing.T) {
			t.Parallel()
			targets, err := scanJobs("b.yaml", bWorkflow())
			require.NoError(t, err)
			require.Len(t, targets, 2)
			assert.Equal(t, "b.yaml:two", targets[0].jobKey)
			assert.Equal(t, "b.yaml:audited", targets[1].jobKey)
		})

		t.Run("引用符付きの job id を読む", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(staleWorkflow("one"), "  one:", `  "one":`, 1)
			targets, err := scanJobs("a.yaml", body)
			require.NoError(t, err)
			require.Len(t, targets, 1)
			assert.Equal(t, "a.yaml:one", targets[0].jobKey)
		})

		t.Run("steps を持たないジョブは対象外にする", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "\n  notify-failure:\n    uses: ./.github/workflows/notify.yaml\n"
			targets, err := scanJobs("a.yaml", body)
			require.NoError(t, err)
			assert.Len(t, targets, 1)
		})

		t.Run("コメント行の bullfrog は数えない", func(t *testing.T) {
			t.Parallel()
			body := "# bullfrogsec/bullfrog の説明\n" + staleWorkflow("one")
			_, err := scanJobs("a.yaml", body)
			assert.NoError(t, err)
		})

		// 取り込まれれば bullfrog の step に未知のキーが載り、形の検査が落ちる。
		t.Run("steps の後ろのジョブレベルのキーは step に含めない", func(t *testing.T) {
			t.Parallel()
			body := testWorkflow("one", "        with:", "          egress-policy: block")
			body = strings.TrimSuffix(body, "\n      - name: Checkout\n") + "    timeout-minutes: 5\n"
			targets, err := scanJobs("a.yaml", body)
			require.NoError(t, err)
			assert.Equal(t, []target{{jobKey: "a.yaml:one", start: 5, end: 9, uses: usesLine, mentions: 1}}, targets)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("steps があるのに bullfrog が無いジョブを弾く", func(t *testing.T) {
			t.Parallel()
			body := workflowHead + "  one:\n    steps:\n      - name: Checkout\n        uses: actions/checkout@x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepMissing)
		})

		t.Run("bullfrog が最初の step でなければ弾く", func(t *testing.T) {
			t.Parallel()
			body := workflowHead + "  one:\n    steps:\n      - name: Checkout\n        uses: actions/checkout@x\n" +
				"      - name: Harden the runner\n        uses: bullfrogsec/bullfrog@x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepNotFirst)
		})

		// 本体と再試行の間に別の step があると、その step は hardening の前に走る。
		t.Run("2つ目の bullfrog が本体の直後にも3番目にも無ければ弾く", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "      - name: Again\n        uses: bullfrogsec/bullfrog@x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepNotFirst)
		})

		// 間の step は書き直す範囲に入るので、生成した回復の step でなければ apply が黙って消す。
		recovery := strings.Join(harden.Recovery(), "\n") + "\n"
		for name, other := range map[string]string{
			"別の run: の step":              "      - name: Other\n        run: make x\n",
			"run: を変えた回復の step":           strings.Replace(recovery, harden.RecoveryRun, "sleep 30 && curl -s x | sh", 1),
			"timeout-minutes の無い回復の step": strings.Replace(recovery, "        timeout-minutes: 2\n", "", 1),
			"後ろに空行のある回復の step":            recovery + "\n",
		} {
			t.Run("本体と再試行の間が"+name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				body := strings.Replace(syncedOne(), recovery, other, 1)
				require.NotEqual(t, syncedOne(), body)
				_, err := scanJobs("a.yaml", body)
				require.ErrorIs(t, err, errStepMalformed)
			})
		}

		t.Run("1 ジョブに 3 つの bullfrog を弾く", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(syncedOne(), "      - name: Checkout\n", "      - name: Again\n        uses: bullfrogsec/bullfrog@x\n      - name: Checkout\n", 1)
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepDuplicate)
		})

		// 本体の後ろの行は書き直す範囲に入るので、apply が黙って消す。
		for name, between := range map[string]string{
			"コメント": "      # 再試行の説明\n",
			"空行":   "\n",
		} {
			t.Run("本体と再試行の間の"+name+"を弾く", func(t *testing.T) {
				t.Parallel()
				body := strings.Replace(syncedOne(), "      - name: Harden the runner (retry)\n", between+"      - name: Harden the runner (retry)\n", 1)
				_, err := scanJobs("a.yaml", body)
				require.ErrorIs(t, err, errStepMalformed)
			})
		}

		// status の関数は暗黙の success() を外すので、bullfrog が2つとも落ちた job で制限の無いまま走る。
		t.Run("三つ組の後ろで Guard の無い always() の step を弾く", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(syncedOne(), "      - name: Checkout\n", "      - name: Checkout\n        if: always()\n", 1)
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepGuard)
		})

		// 三つ組へ書き直す前の形でも、その後ろの step は同じ検査を受ける。
		oldPair := func() []string {
			with := renderWith(policyBlock, testIPs, oneHosts)
			var pair []string
			pair = append(pair, harden.Head()...)
			pair = append(pair, usesLine)
			pair = append(pair, with...)
			pair = append(pair, "      - name: Harden the runner (retry)", "        if: "+harden.RetryIf, usesLine)
			return append(pair, with...)
		}
		for name, body := range map[string]string{
			"再試行の無い本体": testWorkflow("one", renderWith(policyBlock, testIPs, oneHosts)...),
			"回復の無い古い対": jobWorkflow("one", oldPair()),
		} {
			t.Run(name+"の後ろで Guard の無い always() の step を弾く", func(t *testing.T) {
				t.Parallel()
				broken := strings.Replace(body, "      - name: Checkout\n", "      - name: Checkout\n        if: always()\n", 1)
				require.NotEqual(t, body, broken)
				_, err := scanJobs("a.yaml", broken)
				require.ErrorIs(t, err, errStepGuard)
			})
		}

		// 書き直す範囲は本体だけなので、残った回復の step と生成した回復の step が2つ並ぶ。
		t.Run("再試行の無い本体のすぐ後ろの回復の step を弾く", func(t *testing.T) {
			t.Parallel()
			block := append(append(append(harden.Head(), usesLine), renderWith(policyBlock, testIPs, oneHosts)...), harden.Recovery()...)
			_, err := scanJobs("a.yaml", jobWorkflow("one", block))
			require.ErrorIs(t, err, errStepMalformed)
		})

		// エイリアスの値は別の行から持ち込まれるので、if: の行を読む検査はその値を見ない。
		for name, c := range map[string]struct{ from, to string }{
			"job の env: のアンカーと step の if: のエイリアス": {"    steps:\n", "    env:\n      X: &g ${{ always() }}\n    steps:\n"},
			"マージキー": {"      - name: Checkout\n", "      - name: Checkout\n        <<: *m\n"},
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				body := strings.Replace(syncedOne(), c.from, c.to, 1)
				require.NotEqual(t, syncedOne(), body)
				_, err := scanJobs("a.yaml", body)
				require.ErrorIs(t, err, errYAMLAlias)
			})
		}

		t.Run("step の外で bullfrog を名指しすると弾く", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "  two:\n    uses: bullfrogsec/bullfrog/.github/workflows/x.yaml@x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepMalformed)
		})

		t.Run("jobs: を持たない workflow を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanJobs("a.yaml", "name: T\non: push\n")
			require.ErrorIs(t, err, errWorkflowNoJobs)
		})

		// 切り出しが job を 1 つも読めなければ、bullfrog の無い job も 0 件として緑になる。
		t.Run("jobs: の下に読める job が無い workflow を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanJobs("a.yaml", workflowHead+"    one:\n        steps:\n            - run: x\n")
			require.ErrorIs(t, err, errWorkflowNoJobs)
		})

		// 切り出しが step を読めない job は、bullfrog の有無を判定できないまま通ってしまう。
		for name, body := range map[string]string{
			"字下げの無い sequence":  "    steps:\n    - run: x\n",
			"flow style":       "    steps: [{run: x}]\n",
			"引用符付きの steps":     "    \"steps\":\n      - run: x\n",
			"steps も uses も無い": "    runs-on: ubuntu-24.04\n",
		} {
			t.Run(name+"のジョブを弾く", func(t *testing.T) {
				t.Parallel()
				_, err := scanJobs("a.yaml", workflowHead+"  one:\n"+body)
				require.ErrorIs(t, err, errJobUnparsed)
			})
		}

		// 読めない見出しの job は直前の job の本文に吸収され、bullfrog の有無を誰も見なくなる。
		t.Run("読めない見出しの job を弾く", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "  two job:\n    steps:\n      - run: x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errJobUnparsed)
		})

		t.Run("最初の job の見出しが読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			body := workflowHead + "  bad.job:\n    steps:\n      - run: x\n" + strings.TrimPrefix(staleWorkflow("one"), workflowHead)
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errJobUnparsed)
		})

		t.Run("タブで字下げした job を弾く", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "\ttwo:\n\t\tsteps:\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errJobUnparsed)
		})

		t.Run("CRLF の workflow を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanJobs("a.yaml", strings.ReplaceAll(staleWorkflow("one"), "\n", "\r\n"))
			require.Error(t, err)
		})

		t.Run("jobs: の外で bullfrog を名指しすると弾く", func(t *testing.T) {
			t.Parallel()
			body := "env:\n  X: bullfrogsec/bullfrog\n" + staleWorkflow("one")
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepMalformed)
		})

		t.Run("steps を持つジョブのジョブレベルで bullfrog を名指しすると弾く", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(staleWorkflow("one"), "    steps:\n", "    env:\n      X: bullfrogsec/bullfrog\n    steps:\n", 1)
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepMalformed)
		})

		t.Run("空の steps のジョブを弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanJobs("a.yaml", workflowHead+"  one:\n    steps: []\n")
			require.ErrorIs(t, err, errJobUnparsed)
		})

		t.Run("形の崩れた step を伝播する", func(t *testing.T) {
			t.Parallel()
			_, err := scanJobs("a.yaml", testWorkflow("one", "        if: always()"))
			require.ErrorIs(t, err, errStepMalformed)
		})
	})
}

func Test_checkGuards(t *testing.T) {
	t.Parallel()

	guarded := harden.Guard + " && "
	stepsOf := func(t *testing.T, ifs ...string) []workflow.Step {
		t.Helper()
		var b strings.Builder
		b.WriteString(workflowHead + "  one:\n    steps:\n")
		for i, c := range ifs {
			b.WriteString("      - name: S" + strconv.Itoa(i) + "\n        if: " + c + "\n        run: make x\n")
		}
		split := workflow.SplitJobs(b.String())
		require.Len(t, split.Jobs, 1)
		return workflow.SplitSteps(split.Jobs[0])
	}
	rawSteps := func(t *testing.T, body string) []workflow.Step {
		t.Helper()
		split := workflow.SplitJobs(workflowHead + "  one:\n    steps:\n" + body)
		require.Len(t, split.Jobs, 1)
		return workflow.SplitSteps(split.Jobs[0])
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		for name, cond := range map[string]string{
			"status の関数を持たない if:":                 "github.event_name == 'pull_request'",
			"Guard で始まる always()":                 guarded + "always()",
			"Guard で始まり残りが括弧の中の ||":               guarded + "always() && (a == 'x' || b == 'y')",
			"${{ }} で囲んだ Guard で始まる !cancelled()": "${{ " + guarded + "!cancelled() }}",
			"Guard で始まる failure()":                guarded + "failure()",
			"Guard の後ろの文字列の中の ||":                 guarded + "always() && x == 'a||b'",
		} {
			t.Run(name+"を通す", func(t *testing.T) {
				t.Parallel()
				assert.NoError(t, checkGuards(stepsOf(t, cond)))
			})
		}
		for name, body := range map[string]string{
			"block scalar で Guard から始まる always()": "      - name: S\n        if: >-\n          " + guarded + "\n          always()\n        run: make x\n",
			"字下げの指示子を持つ block scalar の Guard 付き式": "      - name: S\n        if: |2\n          " + guarded + "always()\n        run: make x\n",
			"コメントの行の always()":                    "      - name: S\n        # always() は書かない\n        run: make x\n",
		} {
			t.Run(name+"を通す", func(t *testing.T) {
				t.Parallel()
				assert.NoError(t, checkGuards(rawSteps(t, body)))
			})
		}
		t.Run("if: の無い step を通す", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, checkGuards(rawSteps(t, "      - name: S\n        run: make x\n")))
		})
		t.Run("step が0件なら通す", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, checkGuards(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()
		for name, cond := range map[string]string{
			"Guard の無い always()":            "always()",
			"Guard の無い failure()":           "failure()",
			"Guard の無い !cancelled()":        "!cancelled()",
			"Guard の無い大文字の Always()":        "Always()",
			"Guard の無い大文字の CANCELLED()":     "!CANCELLED()",
			"括弧の前に空白を置いた always ()":         "always ()",
			"Guard が後ろにある always()":         "always() && " + harden.Guard,
			"Guard の後の括弧の外の ||":             guarded + "always() || a == 'x'",
			"Guard を || でつないだ always()":     harden.Guard + " || always()",
			"Guard の id が違う always()":       strings.Replace(guarded, harden.RetryID, "retry", 1) + "always()",
			"${{ }} の中で Guard の無い always()": "${{ always() && github.event_name == 'pull_request' }}",
			"文字列の中の閉じ括弧の後の ||":              guarded + "always() && contains(x, ')') || true",
			"文字列の中の開き括弧の後の ||":              guarded + "always() && x == '(' || true",
			"開いていない閉じ括弧の後の ||":              guarded + "always()) || (true",
			"閉じていない文字列":                     guarded + "always() && x == 'a",
			"閉じていない ${{":                    "${{ " + guarded + "always()",
			"引用符で囲んだ Guard 付きの式":            "'" + guarded + "always()'",
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, checkGuards(stepsOf(t, cond)), errStepGuard)
			})
		}
		// 読めない書き方の status の関数は、if: の式の外の行として数え直しで落ちる。
		for name, body := range map[string]string{
			"先頭のキーを if: にした step":             "      - if: always()\n        run: make x\n",
			"引用符で囲んだ if のキー":                  "      - name: S\n        \"if\": always()\n        run: make x\n",
			"フロースタイルの step":                   "      - {name: S, if: always(), run: make x}\n",
			"run: の中の always()":               "      - name: S\n        run: echo 'always()'\n",
			"Guard の次の行で || を継ぐ plain scalar": "      - name: S\n        if: " + guarded + "always()\n          || true\n        run: make x\n",
			"次の行に always() を継ぐ plain scalar":  "      - name: S\n        if: github.event_name == 'push'\n          && always()\n        run: make x\n",
			"次の行へ続く ${{":                      "      - name: S\n        if: ${{ github.event_name == 'push' &&\n          always() }}\n        run: make x\n",
			"引用符のキーで関数名と括弧を別の行に割った式":          "      - name: S\n        \"if\": always\n          ()\n        run: make x\n",
			"フロースタイルで関数名と括弧を別の行に割った式":         "      - {name: S, if: always\n          (), run: make x}\n",
			"9桁の字下げの block scalar":            "      - name: S\n        if: |\n         always()\n        run: make x\n",
			"空行の後ろに || を継ぐ block scalar":      "      - name: S\n        if: >-\n          " + guarded + "always()\n\n          || true\n        run: make x\n",
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, checkGuards(rawSteps(t, body)), errStepGuard)
			})
		}
		t.Run("2つ目の step だけが Guard を欠いても弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, checkGuards(stepsOf(t, guarded+"always()", "always()")), errStepGuard)
		})
	})
}

func Test_stepIf(t *testing.T) {
	t.Parallel()

	stepOf := func(lines ...string) workflow.Step {
		st := workflow.Step{}
		for i, l := range lines {
			st.Lines = append(st.Lines, workflow.Line{Number: i + 1, Text: l})
		}
		return st
	}
	t.Run("1行の if: の式と、その行だけを返す", func(t *testing.T) {
		t.Parallel()
		l, cond, covered, ok := stepIf(stepOf("      - name: S", "        if: always() && x", "        run: y"))
		require.True(t, ok)
		assert.Equal(t, 2, l.Number)
		assert.Equal(t, "always() && x", cond)
		assert.Equal(t, map[int]bool{2: true}, covered)
	})
	t.Run("${{ }} を外す", func(t *testing.T) {
		t.Parallel()
		_, cond, _, ok := stepIf(stepOf("      - name: S", "        if: ${{ always() }}"))
		require.True(t, ok)
		assert.Equal(t, "always()", cond)
	})
	t.Run("閉じていない ${{ は外さない", func(t *testing.T) {
		t.Parallel()
		_, cond, _, ok := stepIf(stepOf("      - name: S", "        if: ${{ always()"))
		require.True(t, ok)
		assert.Equal(t, "${{ always()", cond)
	})
	t.Run("block scalar の続きの行を、空行を挟んでも1つの式に繋ぐ", func(t *testing.T) {
		t.Parallel()
		_, cond, covered, ok := stepIf(stepOf("      - name: S", "        if: >-", "          always() &&", "", "          x", "        run: y"))
		require.True(t, ok)
		assert.Equal(t, "always() && x", cond)
		assert.Equal(t, map[int]bool{2: true, 3: true, 4: true, 5: true}, covered)
	})
	t.Run("plain scalar の折り返しを繋ぐ", func(t *testing.T) {
		t.Parallel()
		_, cond, _, ok := stepIf(stepOf("      - name: S", "        if: a &&", "         always()", "        run: y"))
		require.True(t, ok)
		assert.Equal(t, "a && always()", cond)
	})
	t.Run("if: が無ければ偽", func(t *testing.T) {
		t.Parallel()
		_, _, covered, ok := stepIf(stepOf("      - name: S", "        run: y"))
		assert.False(t, ok)
		assert.Empty(t, covered)
	})
	t.Run("with: の下の深い if: は step の if: にしない", func(t *testing.T) {
		t.Parallel()
		_, _, _, ok := stepIf(stepOf("      - name: S", "        with:", "          if: always()"))
		assert.False(t, ok)
	})
}

func Test_orOutsideParens(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		expr string
		want bool
	}{
		"括弧の外の || は真":          {"a || b", true},
		"括弧の中の || は偽":          {"(a || b)", false},
		"&& の後の括弧の中の || は偽":    {"x && (a || b)", false},
		"閉じた括弧の後の || は真":       {"(a) || b", true},
		"|| が無ければ偽":            {"a && b", false},
		"単独の | は || にしない":      {"a | b", false},
		"末尾の単独の | は偽":          {"a |", false},
		"空の式は偽":                {"", false},
		"入れ子の括弧の中の || は偽":      {"((a || b) && c)", false},
		"括弧で閉じた項の間の || は真":     {"(a && b) || (c)", true},
		"文字列の中の || は数えない":      {"x == 'a||b'", false},
		"文字列の中の括弧は数えない":        {"contains(x, ')') || y", true},
		"文字列の中の引用符の後も文字列として読む": {"x == 'it''s (' || y", true},
		"開いていない閉じ括弧は真":         {"a) && (b", true},
		"閉じていない文字列は真":          {"x == 'a", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, orOutsideParens(c.expr))
		})
	}
}

func Test_mentionsBullfrog(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメント行を除いて名指しする行を数える", func(t *testing.T) {
			t.Parallel()
			lines := []workflow.Line{
				{Number: 1, Text: "        uses: bullfrogsec/bullfrog@x"},
				{Number: 2, Text: "        # bullfrogsec/bullfrog の説明"},
				{Number: 3, Text: "        uses: actions/checkout@x"},
				{Number: 4, Text: `        uses: "bullfrogsec/bullfrog@x"`},
			}
			assert.Equal(t, 2, mentionsBullfrog(lines))
		})

		t.Run("空なら 0 を返す", func(t *testing.T) {
			t.Parallel()
			assert.Zero(t, mentionsBullfrog(nil))
		})
	})
}

func stepOf(texts ...string) workflow.Step {
	st := workflow.Step{Number: 1}
	for i, tx := range texts {
		st.Lines = append(st.Lines, workflow.Line{Number: i + 1, Text: tx})
	}
	return st
}

const (
	nameLine = "      - name: Harden the runner"
	usesLine = "        uses: bullfrogsec/bullfrog@x"
)

func Test_checkHarden(t *testing.T) {
	t.Parallel()

	with := []string{"        with:", "          egress-policy: block"}
	t.Run("本体だけなら本体の範囲と uses: を返す", func(t *testing.T) {
		t.Parallel()
		got, err := checkHarden([]workflow.Step{stepOf(append([]string{nameLine, usesLine}, with...)...)})
		require.NoError(t, err)
		assert.Equal(t, target{start: 0, end: 4, uses: usesLine, mentions: 1}, got)
	})
	t.Run("対なら再試行の末尾までを範囲にし、uses: は本体のものを返す", func(t *testing.T) {
		t.Parallel()
		body := append(append([]string{}, harden.Head()...), append([]string{usesLine}, with...)...)
		retry := append(append([]string{}, harden.RetryHead()...), append([]string{"        uses: bullfrogsec/bullfrog@other"}, with...)...)
		second := stepOf(retry...)
		for i := range second.Lines {
			second.Lines[i].Number += len(body)
		}
		second.Number = len(body) + 1
		got, err := checkHarden([]workflow.Step{stepOf(body...), second})
		require.NoError(t, err)
		assert.Equal(t, target{start: 0, end: len(body) + len(retry), uses: usesLine, mentions: 2}, got)
	})
	t.Run("どちらかの step の形の崩れを伝播する", func(t *testing.T) {
		t.Parallel()
		_, err := checkHarden([]workflow.Step{stepOf(nameLine, usesLine), stepOf(nameLine, "        env:", usesLine)})
		require.ErrorIs(t, err, errStepMalformed)
	})
}

func Test_checkStepShape(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("uses: 行と with: の終わりを返す", func(t *testing.T) {
			t.Parallel()
			uses, end, err := checkStepShape(stepOf(nameLine, usesLine, "        with:", "          egress-policy: block",
				"          allowed-domains: |", "            a.test"), true)
			require.NoError(t, err)
			assert.Equal(t, usesLine, uses)
			assert.Equal(t, 6, end)
		})

		t.Run("with: が無ければ uses: の行で終わる", func(t *testing.T) {
			t.Parallel()
			_, end, err := checkStepShape(stepOf(nameLine, usesLine), true)
			require.NoError(t, err)
			assert.Equal(t, 2, end)
		})

		t.Run("中身の無い with: は with: の行で終わる", func(t *testing.T) {
			t.Parallel()
			_, end, err := checkStepShape(stepOf(nameLine, usesLine, "        with:"), true)
			require.NoError(t, err)
			assert.Equal(t, 3, end)
		})

		t.Run("pin 済みの uses: を受け付ける", func(t *testing.T) {
			t.Parallel()
			pinned := "        uses: bullfrogsec/bullfrog@7dee337d4575320b6d8cbe9a56d48d2fb765963a # v0.11.1"
			uses, _, err := checkStepShape(stepOf(nameLine, pinned), true)
			require.NoError(t, err)
			assert.Equal(t, pinned, uses)
		})

		// 値は生成し直して比べるので、ここでは形だけを見る。
		t.Run("uses: の前の id・continue-on-error・if を受け付ける", func(t *testing.T) {
			t.Parallel()
			_, end, err := checkStepShape(stepOf(nameLine, "        id: x", "        continue-on-error: false", "        if: always()", usesLine), true)
			require.NoError(t, err)
			assert.Equal(t, 5, end)
		})

		t.Run("最後の step の後ろの空行とコメントは範囲に含めない", func(t *testing.T) {
			t.Parallel()
			_, end, err := checkStepShape(stepOf(nameLine, usesLine, "        with:", "          egress-policy: block",
				"", "      # 次の step の説明"), true)
			require.NoError(t, err)
			assert.Equal(t, 4, end)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()
		tests := map[string]workflow.Step{
			"uses: の後の if: を弾く":                stepOf(nameLine, usesLine, "        if: always()"),
			"uses: の後の continue-on-error: を弾く": stepOf(nameLine, usesLine, "        continue-on-error: true"),
			"uses: の前の env: を弾く":               stepOf(nameLine, "        env:", usesLine),
			"with: の後ろの env: を弾く":              stepOf(nameLine, usesLine, "        with:", "          egress-policy: block", "        env:"),
			"name: が先頭に無い step を弾く":            stepOf("      - uses: bullfrogsec/bullfrog@x", "        name: Harden the runner"),
			"1 行形の uses: を弾く":                  stepOf("      - uses: bullfrogsec/bullfrog@x"),
			"name: の後に uses: が無ければ弾く":          stepOf(nameLine),
			"キーだけで uses: が無ければ弾く":              stepOf(nameLine, "        id: x"),
			"name: と uses: の間のコメントを弾く":         stepOf(nameLine, "        # 説明", usesLine),
			"@ の無い uses: を弾く":                  stepOf(nameLine, "        uses: bullfrogsec/bullfrog"),
			"引用符付きの uses: を弾く":                 stepOf(nameLine, `        uses: "bullfrogsec/bullfrog@x"`),
			"with: の中の空行を弾く":                   stepOf(nameLine, usesLine, "        with:", "          egress-policy: block", "", "          enable-sudo: true"),
		}
		for name, st := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, _, err := checkStepShape(st, true)
				require.ErrorIs(t, err, errStepMalformed)
			})
		}
		t.Run("後ろに再試行が続く step の後ろの空行を弾く", func(t *testing.T) {
			t.Parallel()
			_, _, err := checkStepShape(stepOf(nameLine, usesLine, ""), false)
			require.ErrorIs(t, err, errStepMalformed)
		})
	})
}

func Test_renderWith(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("block は名前解決の制限・sudo の除去・IP・ドメインを書く", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"        with:",
				"          egress-policy: block",
				"          dns-policy: allowed-domains-only",
				"          enable-sudo: false",
				"          allowed-ips: |",
				"            127.0.0.100",
				"          allowed-domains: |",
				"            a.test",
				"            b.test",
			}, renderWith(policyBlock, []string{"127.0.0.100"}, []string{"a.test", "b.test"}))
		})

		t.Run("IP が無ければ allowed-ips を書かない", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"        with:",
				"          egress-policy: block",
				"          dns-policy: allowed-domains-only",
				"          enable-sudo: false",
				"          allowed-domains: |",
				"            a.test",
			}, renderWith(policyBlock, nil, []string{"a.test"}))
		})

		t.Run("audit は許可リストを書かず、sudo は外す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"        with:",
				"          egress-policy: audit",
				"          enable-sudo: false",
			}, renderWith(policyAudit, []string{"127.0.0.100"}, nil))
		})
	})
}

func Test_ssot_rewriteFile(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("with: を SSOT 由来の内容へ差し替える", func(t *testing.T) {
			t.Parallel()
			used := map[string]bool{}
			out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", staleWorkflow("one"), used)
			require.NoError(t, err)
			assert.Equal(t, syncedWorkflow("one", policyBlock, oneHosts...), out)
			assert.True(t, used["a.yaml:one"])
		})

		t.Run("with: の無い step へ挿入する", func(t *testing.T) {
			t.Parallel()
			out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", testWorkflow("one"), map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, syncedWorkflow("one", policyBlock, oneHosts...), out)
		})

		// 生成器が with: を丸ごと持つので、手で足した enable-sudo: true や api-token: は残らない。
		t.Run("手で足した入力を消す", func(t *testing.T) {
			t.Parallel()
			body := testWorkflow("one", append(renderWith(policyBlock, testIPs, oneHosts), "          api-token: x")...)
			out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", body, map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, syncedWorkflow("one", policyBlock, oneHosts...), out)
		})

		synced := syncedOne()
		retryID := "        id: " + harden.RetryID + "\n"
		retryIfLine := retryID + "        if: " + harden.RetryIf
		for name, body := range map[string]string{
			"再試行の if: が無い":                   strings.Replace(synced, retryIfLine+"\n", retryID, 1),
			"再試行の if: が failure()":           strings.Replace(synced, retryIfLine, retryID+"        if: failure()", 1),
			"再試行の if: が always()":            strings.Replace(synced, retryIfLine, retryID+"        if: always()", 1),
			"再試行の if: が outcome == failure":  strings.Replace(synced, retryIfLine, retryID+"        if: steps.harden-runner.outcome == 'failure'", 1),
			"再試行の if: の id が違う":              strings.Replace(synced, retryIfLine, retryID+"        if: steps.harden.outcome != 'success'", 1),
			"再試行の id: が無い":                   strings.Replace(synced, retryID, "", 1),
			"再試行の id: が違う":                   strings.Replace(synced, retryID, "        id: retry\n", 1),
			"本体の id: が無い":                    strings.Replace(synced, "        id: harden-runner\n", "", 1),
			"本体の id: が違う":                    strings.Replace(synced, "        id: harden-runner", "        id: harden", 1),
			"本体の continue-on-error: が無い":     strings.Replace(synced, "        continue-on-error: true\n", "", 1),
			"本体の continue-on-error: が false": strings.Replace(synced, "        continue-on-error: true", "        continue-on-error: false", 1),
			"再試行に continue-on-error: がある":    strings.Replace(synced, retryIfLine+"\n", retryIfLine+"\n        continue-on-error: true\n", 1),
			"再試行の uses: が本体と違う":              replaceNth(synced, usesLine, "        uses: bullfrogsec/bullfrog@other", 2),
			"再試行の with: が本体と違う":              replaceNth(synced, "          enable-sudo: false", "          enable-sudo: true", 2),
			"再試行の名前が違う":                      strings.Replace(synced, "(retry)", "(again)", 1),
			"本体の id: が2回ある":                  strings.Replace(synced, "        id: harden-runner\n", "        id: harden-runner\n        id: harden-runner\n", 1),
		} {
			t.Run(name+"なら生成した対に戻す", func(t *testing.T) {
				t.Parallel()
				require.NotEqual(t, synced, body)
				out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", body, map[string]bool{})
				require.NoError(t, err)
				assert.Equal(t, synced, out)
			})
		}

		t.Run("再試行の無い本体に回復と再試行を足す", func(t *testing.T) {
			t.Parallel()
			body := testWorkflow("one", renderWith(policyBlock, testIPs, oneHosts)...)
			out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", body, map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, synced, out)
		})

		t.Run("回復の無い対を三つ組へ書き直す", func(t *testing.T) {
			t.Parallel()
			with := renderWith(policyBlock, testIPs, oneHosts)
			var pair []string
			pair = append(pair, harden.Head()...)
			pair = append(pair, usesLine)
			pair = append(pair, with...)
			pair = append(pair, "      - name: Harden the runner (retry)", "        if: "+harden.RetryIf, usesLine)
			pair = append(pair, with...)
			body := jobWorkflow("one", pair)
			require.NotEqual(t, synced, body)
			out, err := mustParse(t, testSSOT).rewriteFile("a.yaml", body, map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, synced, out)
		})

		t.Run("ずれた with: と無い with: を直した結果に、もう一度適用しても変わらない", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			stale := staleWorkflow("two") + strings.TrimPrefix(testWorkflow("audited"), workflowHead)
			once, err := s.rewriteFile("b.yaml", stale, map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, bWorkflow(), once)
			twice, err := s.rewriteFile("b.yaml", once, map[string]bool{})
			require.NoError(t, err)
			assert.Equal(t, once, twice)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("SSOT に未登録のジョブを弾く", func(t *testing.T) {
			t.Parallel()
			_, err := mustParse(t, testSSOT).rewriteFile("z.yaml", staleWorkflow("nope"), map[string]bool{})
			require.ErrorIs(t, err, errJobMissing)
		})

		t.Run("走査の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			_, err := mustParse(t, testSSOT).rewriteFile("a.yaml", "name: T\n", map[string]bool{})
			require.ErrorIs(t, err, errWorkflowNoJobs)
		})
	})
}

func Test_ssot_checkJob(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録済みのジョブを通す", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, mustParse(t, testSSOT).checkJob("a.yaml:one"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未登録のジョブを弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, mustParse(t, testSSOT).checkJob("z.yaml:none"), errJobMissing)
		})
	})
}

func Test_ssot_planFiles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("差分のあるファイルだけを返す", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": syncedWorkflow("two", policyBlock, "b1.test", "b2.test"),
			})
			files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yaml"))
			require.NoError(t, err)
			changes, used, err := mustParse(t, testSSOT).planFiles(files)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{
				filepath.Join(root, ".github", "workflows", "a.yaml"): syncedWorkflow("one", policyBlock, oneHosts...),
			}, changes)
			assert.True(t, used["a.yaml:one"])
			assert.True(t, used["b.yaml:two"])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読めないファイルを弾く", func(t *testing.T) {
			t.Parallel()
			_, _, err := mustParse(t, testSSOT).planFiles([]string{filepath.Join(t.TempDir(), "missing.yaml")})
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	})
}

func Test_orphanJobs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("参照されなかったジョブキーを返す", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.Equal(t, []string{"b.yaml:audited", "b.yaml:two"}, orphanJobs(s, map[string]bool{"a.yaml:one": true}))
		})

		t.Run("全て参照済みなら空を返す", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			used := map[string]bool{"a.yaml:one": true, "b.yaml:two": true, "b.yaml:audited": true}
			assert.Empty(t, orphanJobs(s, used))
		})
	})
}

func Test_applyOrCheck(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("apply は workflow を書き換える", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			require.NoError(t, applyOrCheck(root, false))
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, syncedWorkflow("one", policyBlock, oneHosts...), string(got))
			assert.NoError(t, applyOrCheck(root, true))
		})

		t.Run("lock の解除だけで root に触れるジョブは、型が呼ぶ宛先を持たなくても通る", func(t *testing.T) {
			t.Parallel()
			root := unlockTestRepo(t, "make ci-unlock-t-identity")
			assert.NoError(t, applyOrCheck(root, true))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, run := range []string{"make ci-unlock-t-identity ci-plan-t-identity", "make ci-apply-t-identity"} {
			t.Run("lock の解除のジョブが "+run+" を呼べば弾く", func(t *testing.T) {
				t.Parallel()
				root := unlockTestRepo(t, run)
				err := applyOrCheck(root, true)
				require.ErrorIs(t, err, errEndpointMissing)
				assert.Contains(t, err.Error(), "i1.test → b.yaml:two")
			})
		}

		t.Run("SSOT が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, applyOrCheck(t.TempDir(), true), os.ErrNotExist)
		})

		// 再試行が本体の失敗で走らない形は、hardening の無いまま job が進む形になる。
		t.Run("check は再試行の if: が欠けた対をドリフトとして報告する", func(t *testing.T) {
			t.Parallel()
			retryID := "        id: " + harden.RetryID + "\n"
			broken := strings.Replace(syncedOne(), retryID+"        if: "+harden.RetryIf+"\n", retryID, 1)
			root := newTestRepo(t, map[string]string{"a.yaml": broken, "b.yaml": bWorkflow()})
			require.ErrorIs(t, applyOrCheck(root, true), errEgressDrift)
		})

		t.Run("check は生成した対どおりの workflow を通す", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{"a.yaml": syncedOne(), "b.yaml": bWorkflow()})
			require.NoError(t, applyOrCheck(root, true))
		})

		t.Run("check はドリフトを非ゼロ終了で報告する", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			require.ErrorIs(t, applyOrCheck(root, true), errEgressDrift)
		})

		t.Run("workflow に現れないジョブ宣言を弾き、apply でも何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{"a.yaml": staleWorkflow("one")})
			err := applyOrCheck(root, false)
			require.ErrorIs(t, err, errJobOrphan)
			assert.Contains(t, err.Error(), "b.yaml:audited, b.yaml:two")
			got, readErr := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, readErr)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("workflow が 1 本も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, applyOrCheck(newTestRepo(t, nil), false), errNoTargets)
		})

		t.Run(".yml の workflow のジョブが実行する root も突き合わせる", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": syncedWorkflow("one", policyBlock, oneHosts...),
				"b.yaml": bWorkflow(),
			})
			writeFile(t, root, cientry.Makefile, ciMakeFixture([3]string{cientry.Plan, "t", "identity"}, [3]string{cientry.Plan, "t", "workload"}))
			writeFile(t, root, ".github/workflows/b.yml", syncedWorkflow("three", policyBlock, "b1.test", "b2.test")+
				"\n      - name: Workload\n        run: make ci-plan-t-workload\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, ssotFile), []byte(testSSOT+"\n[job.\"b.yml:three\"]\nclasses = []\n"), filePerm))
			err := applyOrCheck(root, true)
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "b.yml:three")
		})

		t.Run("宣言に無い root を実行するジョブを弾き、apply でも何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			writeFile(t, root, cientry.Makefile, ciMakeFixture([3]string{cientry.Plan, "t", "identity"}, [3]string{cientry.Plan, "t", "workload"}))
			stale := staleWorkflow("one") + "\n      - name: Workload\n        run: make ci-plan-t-workload\n"
			writeFile(t, root, ".github/workflows/a.yaml", stale)
			err := applyOrCheck(root, false)
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "env/t ")
			got, readErr := openRoot(t, root).ReadFile(".github/workflows/a.yaml")
			require.NoError(t, readErr)
			assert.Equal(t, stale, string(got))
		})

		t.Run("reference の root で region を解けなければ弾き、apply でも何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{"a.yaml": staleWorkflow("one"), "b.yaml": bWorkflow()})
			writeFile(t, root, "usecases/u/reference/main.tf", "resource \"aws_x_a\" \"a\" {\n  for_each = data.aws_regions.r.names\n  region   = each.key\n}\n")
			require.ErrorIs(t, applyOrCheck(root, false), errRegionSet)
			got, readErr := openRoot(t, root).ReadFile(".github/workflows/a.yaml")
			require.NoError(t, readErr)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("reference の root が 1 件も無ければ弾き、apply でも何も書かない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{"a.yaml": staleWorkflow("one"), "b.yaml": bWorkflow()})
			testenv.RemoveUnder(t, root, "usecases")
			require.ErrorIs(t, applyOrCheck(root, false), errNoReferenceRoots)
			got, readErr := openRoot(t, root).ReadFile(".github/workflows/a.yaml")
			require.NoError(t, readErr)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})

		t.Run("SSOT が解釈できなければ workflow を書き換えない", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{"a.yaml": staleWorkflow("one")})
			require.NoError(t, os.WriteFile(filepath.Join(root, ssotFile), []byte("hosts = [\"x.test\"]\n"), filePerm))
			require.ErrorIs(t, applyOrCheck(root, false), errSSOTSyntax)
			got, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "a.yaml")) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, staleWorkflow("one"), string(got))
		})
	})
}

func Test_writeChanges(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("差分が無ければ dry-run は成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, writeChanges(t.TempDir(), map[string]string{}, true))
		})

		t.Run("内容を書き出す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "a.yaml")
			require.NoError(t, writeChanges(root, map[string]string{path: "body"}, false))
			got, err := os.ReadFile(path) //nolint:gosec // path from t.TempDir
			require.NoError(t, err)
			assert.Equal(t, "body", string(got))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("dry-run は差分をドリフトとして報告する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			err := writeChanges(root, map[string]string{
				filepath.Join(root, "b.yaml"): "body",
				filepath.Join(root, "a.yaml"): "body",
			}, true)
			require.ErrorIs(t, err, errEgressDrift)
			assert.Contains(t, err.Error(), ": a.yaml, b.yaml")
			assert.NotContains(t, err.Error(), root)
		})

		t.Run("書き込み失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			err := writeChanges(root, map[string]string{filepath.Join(root, "missing", "a.yaml"): "body"}, false)
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		// 「exit 1 なのに一部だけ書き換わっている」状態を残さない。呼び出し側には
		// エラーしか見えないので、部分適用が起きると区別できなくなる。
		t.Run("途中で書けなければ、先のファイルも書き換えない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			first := filepath.Join(root, "a.yaml")
			require.NoError(t, os.WriteFile(first, []byte("old"), 0o600))

			err := writeChanges(root, map[string]string{
				first:                                    "new",
				filepath.Join(root, "missing", "b.yaml"): "new",
			}, false)

			require.ErrorIs(t, err, os.ErrNotExist)

			got, readErr := os.ReadFile(first) //nolint:gosec // path from t.TempDir
			require.NoError(t, readErr)
			assert.Equal(t, "old", string(got), "先に処理したファイルが書き換わっている")
		})
	})
}

func Test_relTo(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root からの相対パスを返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, filepath.Join(".github", "workflows", "a.yaml"),
				relTo("/repo", filepath.Join("/repo", ".github", "workflows", "a.yaml")))
		})

		t.Run("相対化できなければ元のパスを返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "a.yaml", relTo("/repo", "a.yaml"))
		})
	})
}

// replaceNth は、s の中の old の n 番目（1 起点）だけを repl にします。
func replaceNth(s, old, repl string, n int) string {
	idx := -1
	for range n {
		next := strings.Index(s[idx+1:], old)
		if next < 0 {
			return s
		}
		idx += next + 1
	}
	return s[:idx] + repl + s[idx+len(old):]
}
