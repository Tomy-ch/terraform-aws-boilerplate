package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
)

func ptr[T any](v T) *T { return &v }

const testDeclPath = "conftest/hcl/infra_code/decl.json"

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

const acceptedADR = "# ADR-0001: x\n\n- Status: Accepted\n\n## ネイティブ統合で解けない理由\n\n## 入出力\n\n## 失敗時の扱い\n\n## 業務要件の変更で修正が必要にならない理由\n"

const declOK = `{"infra_code_decl": {` +
	`"types": {"resource.aws_s3_bucket": {"executes_code": false}, "resource.aws_lambda_function": {"executes_code": true, "path_args": ["filename"]}},` +
	`"providers": {"registry.terraform.io/hashicorp/aws": {"command_blocks": []}},` +
	`"languages": {"python": {"contexts": ["python-test"]}},` +
	`"allowlist": [` +
	`{"id": "usecases/x/internal/fn#resource.aws_lambda_function.f", "owner": "repository", "source": "usecases/x/internal/fn/src", "language": "python", "adr": "x/fn-runtime", "reasons": ["type"]},` +
	`{"id": "usecases/x#resource.aws_lambda_function.g", "owner": "external", "repository": "github.com/org/app", "reasons": ["type", "provisioner"]}` +
	`]}}`

const rulesetOK = `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "infra-code-gate"}, {"context": "python-test"}]}}]}`

var gateWorkflowFixtures = map[string]string{
	"terraform-gates.yaml":  "jobs:\n  g:\n    steps:\n      - run: make infra-code-gate RUNNER_MODE=host\n",
	"terraform-unlock.yaml": "jobs:\n  u:\n    steps:\n      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n",
	"terraform-apply.yaml":  "jobs:\n  a:\n    steps:\n      - run: |\n          make ci-apply-admin-identity RUNNER_MODE=host\n",
	"lint.yaml":             "jobs:\n  l:\n    steps:\n      # make infra-code-gate は走らせない\n      - name: make ci-apply-admin-identity の説明\n        run: make md-lint\n",
}

// goSourceFixtures は、検出の道具と、それが import するパッケージの最小の形です。lib/b はテストだけが、lib/c は
// lib/a だけが import します。
var goSourceFixtures = map[string]string{
	goModPath:                              "module example.com/m/scripts\n\ngo 1.22\n",
	"scripts/infra-code-gate/main.go":      "package main\n\nimport _ \"example.com/m/scripts/lib/a\"\n",
	"scripts/infra-code-gate/main_test.go": "package main\n\nimport (\n\t\"testing\"\n\n\t_ \"example.com/m/scripts/lib/b\"\n)\n",
	"scripts/hcl-input/main.go":            "package main\n",
	"scripts/hcl-tally/main.go":            "package main\n\nimport \"fmt\"\n",
	"scripts/tf-job-lint/main.go":          "package main\n",
	"scripts/lib/a/a.go":                   "package a\n\nimport _ \"example.com/m/scripts/lib/c\"\n",
	"scripts/lib/b/b.go":                   "package b\n",
	"scripts/lib/c/c.go":                   "package c\n",
}

var goTargetFixtures = []string{
	"scripts/hcl-input/", "scripts/hcl-tally/", "scripts/infra-code-gate/",
	"scripts/lib/a/", "scripts/lib/b/", "scripts/lib/c/", "scripts/tf-job-lint/",
}

var gateWorkflowTargets = []string{".github/workflows/terraform-apply.yaml", ".github/workflows/terraform-gates.yaml", ".github/workflows/terraform-unlock.yaml"}

func codeownersFor(extra ...string) string {
	var b strings.Builder
	for _, t := range append(append(append(append([]string{}, codeownerTargets...), goTargetFixtures...), gateWorkflowTargets...), extra...) {
		b.WriteString(t + " @owner\n")
	}
	return b.String()
}

func buildRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, testDeclPath), declOK)
	writeFile(t, filepath.Join(repo, defaultRulesetPath), rulesetOK)
	writeFile(t, filepath.Join(repo, defaultCodeownersPath), codeownersFor("usecases/x/internal/fn/src"))
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/src/handler.py"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/docs/adr/0001-fn-runtime.md"), acceptedADR)
	for name, body := range gateWorkflowFixtures {
		writeFile(t, filepath.Join(repo, ".github/workflows", name), body)
	}
	for name, body := range goSourceFixtures {
		writeFile(t, filepath.Join(repo, name), body)
	}
	for _, target := range codeownerTargets {
		if strings.HasSuffix(target, "/") {
			require.NoError(t, os.MkdirAll(filepath.Join(repo, target), 0o750))
			continue
		}
		if _, err := os.Stat(filepath.Join(repo, target)); err != nil {
			writeFile(t, filepath.Join(repo, target), "x")
		}
	}
	return repo
}

func Test_runDecl(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		repo := buildRepo(t)
		var out bytes.Buffer
		require.NoError(t, runDecl([]string{"-decl", testDeclPath}, repo, &out))
		assert.Contains(t, out.String(), "許可リスト 2 項目")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			mutate func(t *testing.T, repo string)
			noDecl bool
			want   error
			msg    string
		}{
			"宣言が無ければエラー": {
				mutate: func(t *testing.T, repo string) { require.NoError(t, os.Remove(filepath.Join(repo, testDeclPath))) },
				want:   os.ErrNotExist,
			},
			"保護設定に required が無ければ0件のエラー": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultRulesetPath), `{"rules": []}`)
				},
				want: errNoInput,
			},
			"宣言の指定が無ければ使い方のエラー": {
				mutate: func(*testing.T, string) {}, noDecl: true, want: errUsage,
			},
			"保護設定が無ければエラー": {
				mutate: func(t *testing.T, repo string) {
					require.NoError(t, os.Remove(filepath.Join(repo, defaultRulesetPath)))
				},
				want: requiredchecks.ErrRead,
			},
			"保護設定に context の無い required があればエラー": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultRulesetPath), `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"integration_id": 15368}]}}]}`)
				},
				want: requiredchecks.ErrRead,
			},
			"code owner の宣言が無ければエラー": {
				mutate: func(t *testing.T, repo string) {
					require.NoError(t, os.Remove(filepath.Join(repo, defaultCodeownersPath)))
				},
				want: os.ErrNotExist,
			},
			"code owner の宣言に解釈できない行があればエラー": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultCodeownersPath), codeownersFor()+"broken-line owner-without-at\n")
				},
				want: errCodeowners,
			},
			"code owner の対象が欠ければ違反": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultCodeownersPath), codeownersFor())
				},
				want: errViolations,
				msg:  "usecases/x/internal/fn/src に当たる行がありません",
			},
			"code owner の対象の workflow が欠ければ違反": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultCodeownersPath), strings.Replace(codeownersFor("usecases/x/internal/fn/src"), ".github/workflows/terraform-unlock.yaml @owner\n", "", 1))
				},
				want: errViolations,
				msg:  ".github/workflows/terraform-unlock.yaml に当たる行がありません",
			},
			"code owner の対象の path が実在しなければ違反": {
				mutate: func(t *testing.T, repo string) {
					require.NoError(t, os.Remove(filepath.Join(repo, ".makefiles/terraform/hcl-input.mk")))
				},
				want: errViolations,
				msg:  "対象 .makefiles/terraform/hcl-input.mk が実在しません",
			},
			"検出の道具が推移的に import するパッケージの code owner が欠ければ違反": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultCodeownersPath), strings.Replace(codeownersFor("usecases/x/internal/fn/src"), "scripts/lib/c/ @owner\n", "", 1))
				},
				want: errViolations,
				msg:  "scripts/lib/c/ に当たる行がありません",
			},
			"検出の道具のディレクトリが無ければエラー": {
				mutate: func(t *testing.T, repo string) {
					require.NoError(t, os.RemoveAll(filepath.Join(repo, "scripts/hcl-tally")))
				},
				want: errGoSource,
			},
			"ADR が Accepted でなければ違反": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, "usecases/x/docs/adr/0001-fn-runtime.md"), strings.Replace(acceptedADR, "Accepted", "Proposed", 1))
				},
				want: errViolations,
				msg:  "Status が Accepted ではありません",
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := buildRepo(t)
				tt.mutate(t, repo)
				var out bytes.Buffer
				args := []string{"-decl", testDeclPath}
				if tt.noDecl {
					args = nil
				}
				err := runDecl(args, repo, &out)
				require.ErrorIs(t, err, tt.want)
				if tt.msg != "" {
					assert.Contains(t, out.String(), tt.msg)
				}
			})
		}
	})
}

func Test_readDeclaration(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "d.json")
		writeFile(t, p, declOK)
		d, err := readDeclaration(p)
		require.NoError(t, err)
		assert.Len(t, d.Types, 2)
		assert.Len(t, d.Allowlist, 2)
		assert.Equal(t, []string{"type", "provisioner"}, d.Allowlist[1].Reasons)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			dir     bool
			want    error
		}{
			"未知のキーはエラー":                {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}}, "extra": 1}}`, want: errDeclShape},
			"項目の未知のキーもエラー":             {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false, "x": 1}}}}`, want: errDeclShape},
			"許可リストの項目の未知のキーもエラー":       {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}}, "allowlist": [{"id": "a", "reason": ["type"]}]}}`, want: errDeclShape},
			"重複したキーはエラー":               {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}, "resource.a": {"executes_code": true}}}}`, want: errDuplicateKey},
			"infra_code_decl が無ければエラー": {content: `{}`, want: errDeclShape},
			"型の分類が0行なら0件のエラー":          {content: `{"infra_code_decl": {"types": {}}}`, want: errNoInput},
			"JSON として壊れていればエラー":        {content: `{"infra_code_decl": `, want: errDeclShape},
			"値の後ろの続きはエラー":              {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}}}} {}`, want: errDeclShape},
			"ディレクトリを渡せばエラー":            {content: declOK, dir: true, want: errDeclShape},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p := filepath.Join(t.TempDir(), "d.json")
				writeFile(t, p, tt.content)
				if tt.dir {
					p = filepath.Dir(p)
				}
				_, err := readDeclaration(p)
				require.ErrorIs(t, err, tt.want)
			})
		}
	})
}

func Test_checkDuplicateKeys(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
		want    error
	}{
		"重複が無ければ通る":        {content: `{"a": {"b": 1}, "c": [{"b": 1}, {"b": 2}]}`},
		"入れ子の重複も見つける":      {content: `{"a": {"b": 1, "b": 2}}`, want: errDuplicateKey},
		"配列の中のオブジェクトの重複も":  {content: `[{"b": 1, "b": 2}]`, want: errDuplicateKey},
		"大文字小文字だけが違うキーも重複": {content: `{"types": 1, "Types": 2}`, want: errDuplicateKey},
		"壊れた JSON はエラー":    {content: `{"a": `, want: errDeclShape},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkDuplicateKeys([]byte(tt.content))
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func Test_checkDeclaration(t *testing.T) {
	t.Parallel()

	contexts := []string{"infra-code-gate", "python-test"}
	base := func() *declaration {
		return &declaration{
			Types:     map[string]typeClass{"resource.aws_lambda_function": {ExecutesCode: ptr(true), PathArgs: []string{"filename"}}},
			Languages: map[string]language{"python": {Contexts: []string{"python-test"}}},
			Allowlist: []item{{ID: "usecases/x/internal/fn#resource.aws_lambda_function.f", Owner: "repository", Source: "usecases/x/internal/fn/src", Language: "python", ADR: "x/fn-runtime"}},
		}
	}

	tests := map[string]struct {
		mutate   func(d *declaration)
		contexts []string
		want     []string
	}{
		"整った宣言は違反を返さない": {mutate: func(*declaration) {}},
		"検出の context が required に無い": {
			mutate: func(*declaration) {}, contexts: []string{"python-test"},
			want: []string{"required status check に infra-code-gate がありません"},
		},
		"型の分類の行の形が違う": {
			mutate: func(d *declaration) { d.Types["aws_s3_bucket"] = typeClass{ExecutesCode: ptr(false)} },
			want:   []string{`型の分類の行 "aws_s3_bucket"`},
		},
		"コードを実行させない型が引数を名指す": {
			mutate: func(d *declaration) {
				d.Types["resource.aws_s3_bucket"] = typeClass{ExecutesCode: ptr(false), PathArgs: []string{"x"}}
			},
			want: []string{"resource.aws_s3_bucket はコードを実行させないのに"},
		},
		"executes_code の無い行": {
			mutate: func(d *declaration) { d.Types["resource.aws_instance"] = typeClass{} },
			want:   []string{"型の分類の行 resource.aws_instance に executes_code がありません"},
		},
		"言語が context を持たない": {
			mutate: func(d *declaration) { d.Languages["go"] = language{} },
			want:   []string{"言語 go に lint と test の required context がありません"},
		},
		"言語の context が required に無い": {
			mutate: func(d *declaration) { d.Languages["go"] = language{Contexts: []string{"go-x"}} },
			want:   []string{"言語 go の context go-x が required status check にありません"},
		},
		"識別の形が違う": {
			mutate: func(d *declaration) { d.Allowlist[0].ID = "aws_lambda_function.f" },
			want:   []string{"識別が `<module のディレクトリ>#<種別>.<型>.<名前>` の形ではありません"},
		},
		"同じ項目が2回": {
			mutate: func(d *declaration) { d.Allowlist = append(d.Allowlist, d.Allowlist[0]) },
			want:   []string{"が2回現れます"},
		},
		"本リポジトリ所有が repository を持つ": {
			mutate: func(d *declaration) { d.Allowlist[0].Repository = "github.com/o/r" },
			want:   []string{"本リポジトリ所有なのに repository を持っています"},
		},
		"本リポジトリ所有が source を欠く": {
			mutate: func(d *declaration) { d.Allowlist[0].Source = "" },
			want:   []string{"source・language・adr を要します"},
		},
		"本リポジトリ所有が language を欠く": {
			mutate: func(d *declaration) { d.Allowlist[0].Language = "" },
			want:   []string{"source・language・adr を要します"},
		},
		"provider と removed の識別は通る": {
			mutate: func(d *declaration) {
				d.Allowlist = append(d.Allowlist,
					item{ID: "env/dev#provider.kubernetes", Owner: "external", Repository: "github.com/o/r"},
					item{ID: "env/dev#provider.aws.use1", Owner: "external", Repository: "github.com/o/r"},
					item{ID: "m/tests/a.tftest.hcl#provider.aws", Owner: "external", Repository: "github.com/o/r"},
					item{ID: "m#removed.module.a.aws_s3_bucket.b", Owner: "external", Repository: "github.com/o/r"})
			},
		},
		"コードを実行させない型が本文の引数を名指す": {
			mutate: func(d *declaration) {
				d.Types["resource.aws_s3_bucket"] = typeClass{ExecutesCode: ptr(false), BodyArgs: []string{"x"}}
			},
			want: []string{"resource.aws_s3_bucket はコードを実行させないのに"},
		},
		"本リポジトリ所有が adr を欠く": {
			mutate: func(d *declaration) { d.Allowlist[0].ADR = "" },
			want:   []string{"source・language・adr を要します"},
		},
		"言語の対応に無い言語の項目": {
			mutate: func(d *declaration) { d.Allowlist[0].Language = "ruby" },
			want:   []string{"言語 ruby が言語の対応にありません"},
		},
		"番号で参照した ADR": {
			mutate: func(d *declaration) { d.Allowlist[0].ADR = "ADR-0001" },
			want:   []string{"ADR ADR-0001"},
		},
		"外部所有が source を持つ": {
			mutate: func(d *declaration) {
				d.Allowlist[0] = item{ID: d.Allowlist[0].ID, Owner: "external", Repository: "github.com/o/r", Source: "x"}
			},
			want: []string{"外部所有なのに source・language・adr のいずれかを持っています"},
		},
		"外部所有がリポジトリを欠く": {
			mutate: func(d *declaration) { d.Allowlist[0] = item{ID: d.Allowlist[0].ID, Owner: "external"} },
			want:   []string{"ソースを持つリポジトリ"},
		},
		"owner が未知": {
			mutate: func(d *declaration) { d.Allowlist[0].Owner = "partner" },
			want:   []string{`owner "partner"`},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := buildRepo(t)
			d := base()
			tt.mutate(d)
			c := contexts
			if tt.contexts != nil {
				c = tt.contexts
			}
			got, err := checkDeclaration(repo, "d.json", d, c)
			require.NoError(t, err)
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_checkSource(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/src/handler.py"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/main.tf"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/inline.tf"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/escape/src/a.py"), "x")
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(repo, "usecases/x/internal/escape/src/out")))
	writeFile(t, filepath.Join(repo, "usecases/x/internal/inner/src/a.py"), "x")
	require.NoError(t, os.Symlink(filepath.Join(repo, "usecases/x/internal/inner/src/a.py"), filepath.Join(repo, "usecases/x/internal/inner/src/b.py")))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(repo, "usecases/x/internal/linked")))
	require.NoError(t, os.Symlink(repo, filepath.Join(repo, "usecases/x/internal/self")))
	require.NoError(t, os.Symlink(filepath.Join(repo, "usecases/x/internal/fn"), filepath.Join(repo, "usecases/x/internal/alias")))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases/x/internal/empty/src"), 0o750))
	writeFile(t, filepath.Join(repo, "usecases/x/internal/keep/src/.gitkeep"), "")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/json/src/a.tf.json"), "{}")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/broken/src/a.py"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/docs/src/README.md"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/empty.tf"), "")
	require.NoError(t, os.Symlink(filepath.Join(repo, "no-such"), filepath.Join(repo, "usecases/x/internal/broken/src/dangling")))

	tests := map[string]struct {
		src  string
		want string
	}{
		"コードだけを持つディレクトリは通る":                     {src: "usecases/x/internal/fn/src"},
		"インラインのコードを持つ宣言ファイルは通る":                 {src: "usecases/x/internal/fn/inline.tf"},
		"所在の内側を指す symlink は通る":                  {src: "usecases/x/internal/inner/src"},
		"絶対パスは落ちる":                              {src: "/etc", want: "正規化した相対パスではありません"},
		"正規化されていないパスは落ちる":                       {src: "usecases/x/../x/internal/fn/src", want: "正規化した相対パスではありません"},
		"リポジトリの外は落ちる":                           {src: "../x", want: "正規化した相対パスではありません"},
		"root そのものは落ちる":                         {src: ".", want: "正規化した相対パスではありません"},
		"ユースケースの全体は落ちる":                         {src: "usecases/x", want: "全体を指しています"},
		"module の全体は落ちる":                        {src: "usecases/x/internal/fn", want: "Terraform の構成ファイル"},
		"実在しない所在は落ちる":                           {src: "usecases/x/internal/none", want: "実在しません"},
		"所在そのものが外を指す symlink なら落ちる":             {src: "usecases/x/internal/linked", want: "リポジトリの外か root そのもの"},
		"所在が root を指す symlink なら落ちる":            {src: "usecases/x/internal/self", want: "リポジトリの外か root そのもの"},
		"部分木に外を指す symlink があれば落ちる":              {src: "usecases/x/internal/escape/src", want: "所在の外を指す symlink"},
		"所在そのものが module を指す symlink なら中を歩いて落ちる": {src: "usecases/x/internal/alias", want: "Terraform の構成ファイル usecases/x/internal/alias/main.tf"},
		"コードのファイルを1つも含まないディレクトリは落ちる":            {src: "usecases/x/internal/empty/src", want: "コードのファイルを1つも含みません"},
		"隠しファイルだけのディレクトリも落ちる":                   {src: "usecases/x/internal/keep/src", want: "コードのファイルを1つも含みません"},
		"文書だけのディレクトリも落ちる":                       {src: "usecases/x/internal/docs/src", want: "コードのファイルを1つも含みません"},
		"空のファイルは落ちる":                            {src: "usecases/x/internal/fn/empty.tf", want: "空のファイルです"},
		"JSON の構成を含めば module の全体とみなして落ちる":       {src: "usecases/x/internal/json/src", want: "Terraform の構成ファイル usecases/x/internal/json/src/a.tf.json"},
		"解決できない symlink は外を指すものとして落ちる":          {src: "usecases/x/internal/broken/src", want: "所在の外を指す symlink"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := checkSource(repo, tt.src)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}
			require.NotEmpty(t, got)
			assert.Contains(t, strings.Join(got, "\n"), tt.want)
		})
	}
}

func Test_checkADR(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "docs/adr/0001-ok.md"), acceptedADR)
	writeFile(t, filepath.Join(repo, "docs/adr/0002-proposed.md"), strings.Replace(acceptedADR, "Accepted", "Proposed", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0003-no-heading.md"), strings.Replace(acceptedADR, "## 入出力\n", "", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0004-fenced.md"), strings.Replace(acceptedADR, "## 入出力\n", "```\n## 入出力\n```\n", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0005-deep.md"), strings.Replace(acceptedADR, "## 入出力", "#### 入出力", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0006-no-status.md"), strings.Replace(acceptedADR, "- Status: Accepted\n", "", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0007-commented.md"), strings.Replace(acceptedADR, "## 入出力\n", "<!--\n## 入出力\n-->\n", 1))
	writeFile(t, filepath.Join(repo, "docs/adr/0008-unclosed.md"), acceptedADR+"<!-- 閉じない\n")
	writeFile(t, filepath.Join(repo, "docs/adr/0009-unclosed-proposed.md"), strings.Replace(acceptedADR, "Accepted", "Proposed", 1)+"<!--\n")

	tests := map[string]struct {
		id   string
		want []string
	}{
		"Accepted で4見出しを持てば通る":         {id: "repository-wide/ok"},
		"見出しのレベルは問わない":                 {id: "repository-wide/deep"},
		"Status 行の無い ADR は落ちる":         {id: "repository-wide/no-status", want: []string{"Status が Accepted ではありません"}},
		"HTML コメントの中の見出しは数えない":         {id: "repository-wide/commented", want: []string{"見出し「入出力」がありません"}},
		"Accepted でなければ落ちる":            {id: "repository-wide/proposed", want: []string{"Status が Accepted ではありません"}},
		"見出しが欠ければ落ちる":                  {id: "repository-wide/no-heading", want: []string{"見出し「入出力」がありません"}},
		"フェンスの中の見出しは数えない":              {id: "repository-wide/fenced", want: []string{"見出し「入出力」がありません"}},
		"見出しをすべて持っても、閉じないコメントは落ちる":     {id: "repository-wide/unclosed", want: []string{"HTML コメントが閉じていません"}},
		"閉じないコメントは Status の違反と並べて報告する": {id: "repository-wide/unclosed-proposed", want: []string{"Status が Accepted ではありません", "HTML コメントが閉じていません"}},
		"実在しない ADR は落ちる":               {id: "repository-wide/none", want: []string{"identity に当たる ADR がありません"}},
		"identity の形でない参照は落ちる":         {id: "0001", want: []string{"`<scope>/<slug>` の形ではありません"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := checkADR(repo, tt.id)
			require.NoError(t, err)
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Contains(t, got[i], w)
			}
		})
	}
}

func Test_goTargets(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T) string {
		t.Helper()
		repo := t.TempDir()
		for name, body := range goSourceFixtures {
			writeFile(t, filepath.Join(repo, name), body)
		}
		return repo
	}

	t.Run("道具と、テストを含めて推移的に import する同じモジュールのパッケージを返す", func(t *testing.T) {
		t.Parallel()
		got, err := goTargets(build(t), detectionTools)
		require.NoError(t, err)
		assert.Equal(t, goTargetFixtures, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(t *testing.T, repo string){
			"モジュールの宣言が無い": func(t *testing.T, repo string) {
				require.NoError(t, os.Remove(filepath.Join(repo, goModPath)))
			},
			"import 先のディレクトリが無い": func(t *testing.T, repo string) {
				require.NoError(t, os.RemoveAll(filepath.Join(repo, "scripts/lib/c")))
			},
			"道具のディレクトリに Go のソースが無い": func(t *testing.T, repo string) {
				require.NoError(t, os.Remove(filepath.Join(repo, "scripts/tf-job-lint/main.go")))
			},
			"解釈できないソース": func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "scripts/lib/b/b.go"), "package b\nimport (\n")
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := build(t)
				mutate(t, repo)
				got, err := goTargets(repo, detectionTools)
				require.ErrorIs(t, err, errGoSource)
				assert.Nil(t, got)
			})
		}
	})
}

func Test_modulePath(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		content string
		want    string
		wantErr bool
	}{
		"module 行のパスを返す":     {"// x\nmodule example.com/m/scripts\n\ngo 1.22\n", "example.com/m/scripts", false},
		"module 行が無ければエラー":   {"go 1.22\n", "", true},
		"パスの無い module 行はエラー": {"module \n", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, filepath.Join(repo, goModPath), c.content)
			got, err := modulePath(os.DirFS(repo))
			if c.wantErr {
				require.ErrorIs(t, err, errGoSource)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("宣言が無ければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := modulePath(os.DirFS(t.TempDir()))
		require.ErrorIs(t, err, errGoSource)
	})
}

func Test_dirImports(t *testing.T) {
	t.Parallel()

	t.Run("直下のソースとテストの import を返し、Go でないファイルと下のディレクトリは読まない", func(t *testing.T) {
		t.Parallel()
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, "d/a.go"), "package d\n\nimport \"fmt\"\n")
		writeFile(t, filepath.Join(repo, "d/a_test.go"), "package d\n\nimport \"testing\"\n")
		writeFile(t, filepath.Join(repo, "d/README.md"), "import \"x\"")
		writeFile(t, filepath.Join(repo, "d/sub/s.go"), "package sub\n\nimport \"os\"\n")
		got, err := dirImports(os.DirFS(repo), "d")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fmt", "testing"}, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, files := range map[string]map[string]string{
			"ディレクトリが無い":    {"other/a.go": "package other\n"},
			"Go のソースが無い":   {"d/README.md": "x"},
			"解釈できないソースがある": {"d/a.go": "package d\nimport (\n"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := t.TempDir()
				for p, body := range files {
					writeFile(t, filepath.Join(repo, p), body)
				}
				got, err := dirImports(os.DirFS(repo), "d")
				require.ErrorIs(t, err, errGoSource)
				assert.Nil(t, got)
			})
		}
	})
}

func Test_ownedTargets(t *testing.T) {
	t.Parallel()

	d := &declaration{Allowlist: []item{
		{Owner: "repository", Source: "usecases/x/src"},
		{Owner: "external", Repository: "github.com/o/r"},
	}}
	got := ownedTargets(d, gateWorkflowTargets)
	assert.Equal(t, append(append(append([]string{}, codeownerTargets...), gateWorkflowTargets...), "usecases/x/src"), got)
	assert.Len(t, codeownerTargets, 5, "固定の対象を書き換えていない")
	assert.NotContains(t, codeownerTargets, "scripts/lib/adrdoc/", "検出の道具が import するパッケージは直書きせず導く")
	assert.NotContains(t, codeownerTargets, ".github/workflows/terraform-apply.yaml", "workflow は直書きせず導く")
}

func Test_gateWorkflows(t *testing.T) {
	t.Parallel()

	t.Run("検出の job か CI の入口を走らせる workflow だけを返し、run: | の本文の make も拾い、コメントと name: は数えない", func(t *testing.T) {
		t.Parallel()
		got, err := gateWorkflows(buildRepo(t))
		require.NoError(t, err)
		assert.Equal(t, gateWorkflowTargets, got)
	})

	t.Run("対象の workflow が1つも無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, ".github/workflows/lint.yaml"), gateWorkflowFixtures["lint.yaml"])
		_, err := gateWorkflows(repo)
		require.ErrorIs(t, err, errNoGateWorkflow)
	})

	t.Run("workflow のディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := gateWorkflows(t.TempDir())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run(".yml も読み、.yaml・.yml でないファイルとディレクトリは読まない", func(t *testing.T) {
		t.Parallel()
		repo := t.TempDir()
		run := "jobs:\n  a:\n    steps:\n      - run: make ci-apply-admin-identity\n"
		writeFile(t, filepath.Join(repo, ".github/workflows/apply.yml"), run)
		writeFile(t, filepath.Join(repo, ".github/workflows/README.md"), run)
		writeFile(t, filepath.Join(repo, ".github/workflows/dir.yaml/x.yaml"), run)
		got, err := gateWorkflows(repo)
		require.NoError(t, err)
		assert.Equal(t, []string{".github/workflows/apply.yml"}, got)
	})

	t.Run("読めない workflow があれば失敗する", func(t *testing.T) {
		t.Parallel()
		repo := buildRepo(t)
		require.NoError(t, os.Symlink(filepath.Join(repo, "missing"), filepath.Join(repo, ".github/workflows/broken.yaml")))
		_, err := gateWorkflows(repo)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_readCodeowners(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "CODEOWNERS")
		writeFile(t, p, "# コメント\n\n/a/ @o\nb @o/team x@example.com\nc\n")
		got, err := readCodeowners(p)
		require.NoError(t, err)
		assert.Equal(t, []ownerRule{
			{line: 3, pattern: "/a/", owners: []string{"@o"}},
			{line: 4, pattern: "b", owners: []string{"@o/team", "x@example.com"}},
			{line: 5, pattern: "c", owners: []string{}},
		}, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			want    error
		}{
			"所有者の形が違う行はエラー":     {content: "a owner\n", want: errCodeowners},
			"行末のコメントもエラー":       {content: "a @o # note\n", want: errCodeowners},
			"否定のパターンはエラー":       {content: "!a @o\n", want: errCodeowners},
			"文字クラスのパターンはエラー":    {content: "a[bc] @o\n", want: errCodeowners},
			"バックスラッシュのパターンはエラー": {content: "a\\b @o\n", want: errCodeowners},
			"閉じ括弧だけのパターンもエラー":   {content: "a] @o\n", want: errCodeowners},
			"規則が0行なら0件のエラー":     {content: "# only comments\n", want: errNoInput},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p := filepath.Join(t.TempDir(), "CODEOWNERS")
				writeFile(t, p, tt.content)
				_, err := readCodeowners(p)
				require.ErrorIs(t, err, tt.want)
			})
		}
	})
}

func Test_checkCodeowners(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rules []ownerRule
		want  []lintreport.Finding
	}{
		"対象そのものの行が最後なら通る": {
			rules: []ownerRule{{line: 1, pattern: "*", owners: []string{"@a"}}, {line: 2, pattern: "/conftest/hcl/", owners: []string{"@o"}}},
		},
		"当たる行が無い": {
			rules: []ownerRule{{line: 1, pattern: "docs/", owners: []string{"@o"}}},
			want:  []lintreport.Finding{{File: "CODEOWNERS", Line: 1, Message: "conftest/hcl/ に当たる行がありません（ADR-0103 決定14）"}},
		},
		"後ろの広い行が上書きする": {
			rules: []ownerRule{{line: 1, pattern: "conftest/hcl/", owners: []string{"@o"}}, {line: 2, pattern: "conftest/", owners: []string{"@other"}}},
			want:  []lintreport.Finding{{File: "CODEOWNERS", Line: 2, Message: "conftest/hcl/ に最後に当たる行が conftest/ です。対象そのものの行を最後に置いてください"}},
		},
		"所有者の無い行": {
			rules: []ownerRule{{line: 3, pattern: "conftest/hcl/"}},
			want:  []lintreport.Finding{{File: "CODEOWNERS", Line: 3, Message: "conftest/hcl/ の行に所有者がありません"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, checkCodeowners("CODEOWNERS", tt.rules, []string{"conftest/hcl/"}))
		})
	}
}

func Test_checkTargets(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "conftest/hcl"), 0o750))
		writeFile(t, filepath.Join(repo, ".github/CODEOWNERS"), "x")
		got, err := checkTargets(repo, "CODEOWNERS", []string{"conftest/hcl/", ".github/CODEOWNERS"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			prepare func(t *testing.T, repo string)
			target  string
			want    string
		}{
			"ディレクトリの対象が無い": {
				prepare: func(*testing.T, string) {},
				target:  "conftest/hcl/",
				want:    "対象 conftest/hcl/ が実在しません（ADR-0103 決定14）",
			},
			"ファイルの対象が無い": {
				prepare: func(*testing.T, string) {},
				target:  ".github/CODEOWNERS",
				want:    "対象 .github/CODEOWNERS が実在しません（ADR-0103 決定14）",
			},
			"ディレクトリの対象がファイル": {
				prepare: func(t *testing.T, repo string) { writeFile(t, filepath.Join(repo, "conftest/hcl"), "x") },
				target:  "conftest/hcl/",
				want:    "対象 conftest/hcl/ はディレクトリではありません（ADR-0103 決定14）",
			},
			"ファイルの対象がディレクトリ": {
				prepare: func(t *testing.T, repo string) {
					require.NoError(t, os.MkdirAll(filepath.Join(repo, ".github/CODEOWNERS"), 0o750))
				},
				target: ".github/CODEOWNERS",
				want:   "対象 .github/CODEOWNERS は通常のファイルではありません（ADR-0103 決定14）",
			},
			"ディレクトリの対象が symlink": {
				prepare: func(t *testing.T, repo string) {
					require.NoError(t, os.MkdirAll(filepath.Join(repo, "real"), 0o750))
					require.NoError(t, os.MkdirAll(filepath.Join(repo, "conftest"), 0o750))
					require.NoError(t, os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, "conftest/hcl")))
				},
				target: "conftest/hcl/",
				want:   "対象 conftest/hcl/ はディレクトリではありません（ADR-0103 決定14）",
			},
			"ファイルの対象が symlink": {
				prepare: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, "real"), "x")
					require.NoError(t, os.MkdirAll(filepath.Join(repo, ".github"), 0o750))
					require.NoError(t, os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, ".github/CODEOWNERS")))
				},
				target: ".github/CODEOWNERS",
				want:   "対象 .github/CODEOWNERS は通常のファイルではありません（ADR-0103 決定14）",
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := t.TempDir()
				tt.prepare(t, repo)
				got, err := checkTargets(repo, "CODEOWNERS", []string{tt.target})
				require.NoError(t, err)
				assert.Equal(t, []lintreport.Finding{{File: "CODEOWNERS", Line: 1, Message: tt.want}}, got)
			})
		}
	})

	t.Run("リポジトリの root を開けなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := checkTargets(filepath.Join(t.TempDir(), "missing"), "CODEOWNERS", []string{"conftest/hcl/"})
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("確かめられない対象は所見でなくエラー", func(t *testing.T) {
		t.Parallel()

		tests := map[string]func(t *testing.T, repo string){
			"親がファイル": func(t *testing.T, repo string) { writeFile(t, filepath.Join(repo, "conftest"), "x") },
			"親が root の外への symlink": func(t *testing.T, repo string) {
				outside := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(outside, "hcl"), 0o750))
				require.NoError(t, os.Symlink(outside, filepath.Join(repo, "conftest")))
			},
		}
		for name, prepare := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := t.TempDir()
				prepare(t, repo)
				got, err := checkTargets(repo, "CODEOWNERS", []string{"conftest/hcl/"})
				require.Error(t, err)
				require.NotErrorIs(t, err, os.ErrNotExist)
				assert.Contains(t, err.Error(), "conftest/hcl/ の確認")
				assert.Nil(t, got)
			})
		}
	})

	t.Run("固定の対象はファイルとディレクトリの両方を持つ", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, codeownerTargets)
		var dirs, files int
		for _, target := range codeownerTargets {
			if strings.HasSuffix(target, "/") {
				dirs++
			} else {
				files++
			}
		}
		assert.Positive(t, dirs)
		assert.Positive(t, files)
	})
}

func Test_codeownersMatch(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pattern string
		target  string
		want    bool
	}{
		"同じパスに当たる":               {pattern: ".github/CODEOWNERS", target: ".github/CODEOWNERS", want: true},
		"先頭の / は root に固定する":     {pattern: "/conftest/hcl/", target: "conftest/hcl/", want: true},
		"祖先のディレクトリの行が当たる":        {pattern: "conftest/", target: "conftest/hcl/", want: true},
		"固定しない名前はどの深さにも当たる":      {pattern: "hcl", target: "conftest/hcl/", want: true},
		"* はすべてに当たる":             {pattern: "*", target: "a/b.json", want: true},
		"** は0段以上に当たる":           {pattern: "conftest/**/decl.json", target: "conftest/hcl/infra_code/decl.json", want: true},
		"拡張子の glob":              {pattern: "*.json", target: "conftest/hcl/infra_code/decl.json", want: true},
		"ディレクトリだけの行はファイルに当たらない":  {pattern: "infra-code.mk/", target: ".makefiles/terraform/infra-code.mk", want: false},
		"固定した行は別の深さに当たらない":       {pattern: "/hcl/", target: "conftest/hcl/", want: false},
		"別のパスには当たらない":            {pattern: "conftest/plan/", target: "conftest/hcl/", want: false},
		"子のディレクトリの行は親に当たらない":     {pattern: "conftest/hcl/", target: "conftest/", want: false},
		"dir/* は直下に当たる":          {pattern: "conftest/*", target: "conftest/README.md", want: true},
		"dir/* は孫には当たらない":        {pattern: "/scripts/*", target: "scripts/lib/adrdoc/", want: false},
		"ディレクトリだけの行はファイルの祖先に当たる": {pattern: ".makefiles/", target: ".makefiles/terraform/infra-code.mk", want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, codeownersMatch(tt.pattern, tt.target))
		})
	}
}

func Test_globMatch(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pattern []string
		segs    []string
		want    bool
	}{
		"段ごとに当てる":        {pattern: []string{"a", "*.go"}, segs: []string{"a", "b.go"}, want: true},
		"段の数が違えば当たらない":   {pattern: []string{"a"}, segs: []string{"a", "b"}, want: false},
		"** は0段に当たる":     {pattern: []string{"a", "**", "c"}, segs: []string{"a", "c"}, want: true},
		"** は複数段に当たる":    {pattern: []string{"**", "c"}, segs: []string{"a", "b", "c"}, want: true},
		"壊れたパターンは当たらない":  {pattern: []string{"["}, segs: []string{"["}, want: false},
		"空のパターンは空にだけ当たる": {pattern: nil, segs: nil, want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, globMatch(tt.pattern, tt.segs))
		})
	}
}

func Test_within(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		root, p string
		want    bool
	}{
		"子は内側":          {root: "/a", p: "/a/b", want: true},
		"同じパスは内側":       {root: "/a", p: "/a", want: true},
		"親は外側":          {root: "/a/b", p: "/a", want: false},
		"接頭辞が同じ別名は外側":   {root: "/a", p: "/ab", want: false},
		"..で始まる名前の子は内側": {root: "/a", p: "/a/..b", want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, within(tt.root, tt.p))
		})
	}
}

func Test_hasSuffix(t *testing.T) {
	t.Parallel()

	assert.True(t, hasSuffix("a.tf.json", configFiles))
	assert.True(t, hasSuffix("a.tf", configFiles))
	assert.False(t, hasSuffix("a.tfvars", configFiles))
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]int{"c": 1, "a": 2, "b": 3}))
	assert.Empty(t, sortedKeys(map[string]int{}))
}
