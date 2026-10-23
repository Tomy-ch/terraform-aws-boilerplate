package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
`

const workflowHead = "name: T\n\njobs:\n"

var (
	errWD = xerrors.New("getwd failed")
	// oneHosts は testSSOT が a.yaml:one へ展開する許可リスト。
	oneHosts = []string{"b1.test", "b2.test", "m1.test", "m2.test", "i1.test", "x1.test"}
	testIPs  = []string{"127.0.0.100"}
)

// testWorkflow は bullfrog の step の uses: 行に tail を続け、その後ろへ checkout の step を置いた 1 ジョブの workflow。
func testWorkflow(jobID string, tail ...string) string {
	var b strings.Builder
	b.WriteString(workflowHead + "  " + jobID + ":\n    steps:\n")
	b.WriteString("      - name: Harden the runner\n        uses: bullfrogsec/bullfrog@x\n")
	for _, l := range tail {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n      - name: Checkout\n")
	return b.String()
}

func staleWorkflow(jobID string) string {
	return testWorkflow(jobID, "        with:", "          egress-policy: block",
		"          allowed-endpoints: >", "            stale:443")
}

func syncedWorkflow(jobID, policy string, hosts ...string) string {
	return testWorkflow(jobID, renderWith(policy, testIPs, hosts)...)
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
	for name, body := range workflows {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "workflows", name), []byte(body), filePerm))
	}
	return root
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
		})

		t.Run("egress_policy 未指定は block とみなす", func(t *testing.T) {
			t.Parallel()
			s := mustParse(t, testSSOT)
			assert.Equal(t, policyBlock, s.jobs["a.yaml:one"].policy)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

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
			require.NoError(t, assign(c, nil, "includes", []string{"mise"}))
			require.NoError(t, assign(c, nil, "hosts", []string{"a.test"}))
			require.NoError(t, assign(c, nil, "ips", []string{"127.0.0.100"}))
			assert.Equal(t, []string{"mise"}, c.includes)
			assert.Equal(t, []string{"a.test"}, c.hosts)
			assert.Equal(t, []string{"127.0.0.100"}, c.ips)
		})

		t.Run("ジョブのキーを載せる", func(t *testing.T) {
			t.Parallel()
			j := &jobSpec{}
			require.NoError(t, assign(nil, j, "classes", []string{"image"}))
			require.NoError(t, assign(nil, j, "extra", []string{"a.test"}))
			require.NoError(t, assign(nil, j, "egress_policy", []string{policyAudit}))
			assert.Equal(t, []string{"image"}, j.classes)
			assert.Equal(t, []string{"a.test"}, j.extra)
			assert.Equal(t, policyAudit, j.policy)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未知のキーを弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(&class{}, nil, "endpoints", []string{"a.test"}), errSSOTSyntax)
		})

		t.Run("クラスのキーをジョブへ書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(nil, &jobSpec{}, "includes", []string{"mise"}), errSSOTSyntax)
		})

		t.Run("ips をジョブへ書けない", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(nil, &jobSpec{}, "ips", []string{"127.0.0.100"}), errSSOTSyntax)
		})

		t.Run("リスト内の重複を弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, assign(&class{}, nil, "hosts", []string{"a.test", "a.test"}), errSSOTDuplicate)
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

		t.Run("クラスとジョブを登録する", func(t *testing.T) {
			t.Parallel()
			s := &ssot{classes: map[string]*class{}, jobs: map[string]*jobSpec{}}
			require.NoError(t, s.openSection("class", "base"))
			require.NoError(t, s.openSection("job", "a.yaml:one"))
			assert.NotNil(t, s.classes["base"])
			assert.Equal(t, []string{"a.yaml:one"}, s.jobOrder)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("クラスの重複を弾く", func(t *testing.T) {
			t.Parallel()
			s := &ssot{classes: map[string]*class{}, jobs: map[string]*jobSpec{}}
			require.NoError(t, s.openSection("class", "base"))
			require.ErrorIs(t, s.openSection("class", "base"), errSSOTDuplicate)
		})

		t.Run("ジョブの重複を弾く", func(t *testing.T) {
			t.Parallel()
			s := &ssot{classes: map[string]*class{}, jobs: map[string]*jobSpec{}}
			require.NoError(t, s.openSection("job", "a.yaml:one"))
			require.ErrorIs(t, s.openSection("job", "a.yaml:one"), errSSOTDuplicate)
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
		// allowed-ips はドメインの判定を経ずに通すので、loopback の外や広すぎる CIDR は遮断を無効にする。
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

		t.Run("ジョブごとに with: の範囲を返す", func(t *testing.T) {
			t.Parallel()
			targets, err := scanJobs("a.yaml", staleWorkflow("one"))
			require.NoError(t, err)
			// 0 起点で uses: は 6 行目、with: の4行は 7-10 行目。
			assert.Equal(t, []target{{jobKey: "a.yaml:one", start: 7, end: 11}}, targets)
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
			assert.Equal(t, []target{{jobKey: "a.yaml:one", start: 7, end: 9}}, targets)
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

		t.Run("1 ジョブに 2 つの bullfrog を弾く", func(t *testing.T) {
			t.Parallel()
			body := staleWorkflow("one") + "      - name: Again\n        uses: bullfrogsec/bullfrog@x\n"
			_, err := scanJobs("a.yaml", body)
			require.ErrorIs(t, err, errStepDuplicate)
		})

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

func Test_checkStepShape(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("with: の範囲を返す", func(t *testing.T) {
			t.Parallel()
			got, err := checkStepShape(stepOf(nameLine, usesLine, "        with:", "          egress-policy: block",
				"          allowed-domains: |", "            a.test"))
			require.NoError(t, err)
			assert.Equal(t, target{start: 2, end: 6}, got)
		})

		t.Run("with: が無ければ uses: の直後への挿入にする", func(t *testing.T) {
			t.Parallel()
			got, err := checkStepShape(stepOf(nameLine, usesLine))
			require.NoError(t, err)
			assert.Equal(t, target{start: 2, end: 2}, got)
		})

		t.Run("中身の無い with: は with: の行だけを範囲にする", func(t *testing.T) {
			t.Parallel()
			got, err := checkStepShape(stepOf(nameLine, usesLine, "        with:"))
			require.NoError(t, err)
			assert.Equal(t, target{start: 2, end: 3}, got)
		})

		t.Run("pin 済みの uses: を受け付ける", func(t *testing.T) {
			t.Parallel()
			_, err := checkStepShape(stepOf(nameLine, "        uses: bullfrogsec/bullfrog@7dee337d4575320b6d8cbe9a56d48d2fb765963a # v0.11.1"))
			assert.NoError(t, err)
		})

		t.Run("後ろの空行とコメントは範囲に含めない", func(t *testing.T) {
			t.Parallel()
			got, err := checkStepShape(stepOf(nameLine, usesLine, "        with:", "          egress-policy: block",
				"", "      # 次の step の説明"))
			require.NoError(t, err)
			assert.Equal(t, target{start: 2, end: 4}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()
		tests := map[string]workflow.Step{
			"if: を弾く":                 stepOf(nameLine, "        if: always()", usesLine),
			"continue-on-error: を弾く":  stepOf(nameLine, usesLine, "        continue-on-error: true"),
			"with: の後ろの env: を弾く":     stepOf(nameLine, usesLine, "        with:", "          egress-policy: block", "        env:"),
			"name: が先頭に無い step を弾く":   stepOf("      - uses: bullfrogsec/bullfrog@x", "        name: Harden the runner"),
			"1 行形の uses: を弾く":         stepOf("      - uses: bullfrogsec/bullfrog@x"),
			"name: の後に uses: が無ければ弾く": stepOf(nameLine),
			"@ の無い uses: を弾く":         stepOf(nameLine, "        uses: bullfrogsec/bullfrog"),
			"引用符付きの uses: を弾く":        stepOf(nameLine, `        uses: "bullfrogsec/bullfrog@x"`),
			"with: の中の空行を弾く":          stepOf(nameLine, usesLine, "        with:", "          egress-policy: block", "", "          enable-sudo: true"),
		}
		for name, st := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := checkStepShape(st)
				require.ErrorIs(t, err, errStepMalformed)
			})
		}
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

		// 差し替えと挿入で行数が変わっても、2 回目は何も変えない。
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
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("SSOT が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, applyOrCheck(t.TempDir(), true), os.ErrNotExist)
		})

		t.Run("check はドリフトを非ゼロ終了で報告する", func(t *testing.T) {
			t.Parallel()
			root := newTestRepo(t, map[string]string{
				"a.yaml": staleWorkflow("one"),
				"b.yaml": bWorkflow(),
			})
			require.ErrorIs(t, applyOrCheck(root, true), errEgressDrift)
		})

		// apply が orphan で落ちるときも、ずれた workflow を書き換えない。
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
