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
)

func ptr[T any](v T) *T { return &v }

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
	`{"id": "usecases/x/internal/fn#resource.aws_lambda_function.f", "owner": "repository", "source": "usecases/x/internal/fn/src", "language": "python", "adr": "x/fn-runtime"},` +
	`{"id": "usecases/x#resource.aws_lambda_function.g", "owner": "external", "repository": "github.com/org/app"}` +
	`]}}`

const rulesetOK = `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "infra-code-gate"}, {"context": "python-test"}]}}]}`

func codeownersFor(extra ...string) string {
	var b strings.Builder
	for _, t := range append(append([]string{}, codeownerTargets...), extra...) {
		b.WriteString(t + " @owner\n")
	}
	return b.String()
}

func buildRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, defaultDeclPath), declOK)
	writeFile(t, filepath.Join(repo, defaultRulesetPath), rulesetOK)
	writeFile(t, filepath.Join(repo, defaultCodeownersPath), codeownersFor("usecases/x/internal/fn/src"))
	writeFile(t, filepath.Join(repo, "usecases/x/internal/fn/src/handler.py"), "x")
	writeFile(t, filepath.Join(repo, "usecases/x/docs/adr/0001-fn-runtime.md"), acceptedADR)
	return repo
}

func Test_runDecl(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		repo := buildRepo(t)
		var out bytes.Buffer
		require.NoError(t, runDecl(nil, repo, &out))
		assert.Contains(t, out.String(), "許可リスト 2 項目")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			mutate func(t *testing.T, repo string)
			want   error
			msg    string
		}{
			"宣言が無ければエラー": {
				mutate: func(t *testing.T, repo string) { require.NoError(t, os.Remove(filepath.Join(repo, defaultDeclPath))) },
				want:   os.ErrNotExist,
			},
			"保護設定に required が無ければ0件のエラー": {
				mutate: func(t *testing.T, repo string) {
					writeFile(t, filepath.Join(repo, defaultRulesetPath), `{"rules": []}`)
				},
				want: errNoInput,
			},
			"保護設定が無ければエラー": {
				mutate: func(t *testing.T, repo string) {
					require.NoError(t, os.Remove(filepath.Join(repo, defaultRulesetPath)))
				},
				want: os.ErrNotExist,
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
				err := runDecl(nil, repo, &out)
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
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			extra   string
			want    error
		}{
			"未知のキーはエラー":                {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}}, "extra": 1}}`, want: errDeclShape},
			"項目の未知のキーもエラー":             {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false, "x": 1}}}}`, want: errDeclShape},
			"重複したキーはエラー":               {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}, "resource.a": {"executes_code": true}}}}`, want: errDuplicateKey},
			"infra_code_decl が無ければエラー": {content: `{}`, want: errDeclShape},
			"型の分類が0行なら0件のエラー":          {content: `{"infra_code_decl": {"types": {}}}`, want: errNoInput},
			"JSON として壊れていればエラー":        {content: `{"infra_code_decl": `, want: errDeclShape},
			"値の後ろの続きはエラー":              {content: `{"infra_code_decl": {"types": {"resource.a": {"executes_code": false}}}} {}`, want: errDeclShape},
			"宣言の置き場所に他のファイルがあればエラー":    {content: declOK, extra: "extra.json", want: errDeclShape},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p := filepath.Join(t.TempDir(), "d.json")
				writeFile(t, p, tt.content)
				if tt.extra != "" {
					writeFile(t, filepath.Join(filepath.Dir(p), tt.extra), "{}")
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

func Test_readRequiredContexts(t *testing.T) {
	t.Parallel()

	t.Run("required_status_checks の context だけを集める", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.json")
		writeFile(t, p, `{"rules": [{"type": "pull_request"}, {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "a"}, {"context": "b"}]}}]}`)
		got, err := readRequiredContexts(p)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("0件はエラー", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.json")
		writeFile(t, p, `{"rules": [{"type": "pull_request"}]}`)
		_, err := readRequiredContexts(p)
		require.ErrorIs(t, err, errNoInput)
	})

	t.Run("壊れた JSON はエラー", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.json")
		writeFile(t, p, `{`)
		_, err := readRequiredContexts(p)
		require.Error(t, err)
	})
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

	tests := map[string]struct {
		id   string
		want []string
	}{
		"Accepted で4見出しを持てば通る": {id: "repository-wide/ok"},
		"見出しのレベルは問わない":         {id: "repository-wide/deep"},
		"Status 行の無い ADR は落ちる": {id: "repository-wide/no-status", want: []string{"Status が Accepted ではありません"}},
		"HTML コメントの中の見出しは数えない": {id: "repository-wide/commented", want: []string{"見出し「入出力」がありません"}},
		"Accepted でなければ落ちる":    {id: "repository-wide/proposed", want: []string{"Status が Accepted ではありません"}},
		"見出しが欠ければ落ちる":          {id: "repository-wide/no-heading", want: []string{"見出し「入出力」がありません"}},
		"フェンスの中の見出しは数えない":      {id: "repository-wide/fenced", want: []string{"見出し「入出力」がありません"}},
		"実在しない ADR は落ちる":       {id: "repository-wide/none", want: []string{"identity に当たる ADR がありません"}},
		"identity の形でない参照は落ちる": {id: "0001", want: []string{"`<scope>/<slug>` の形ではありません"}},
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

func Test_ownedTargets(t *testing.T) {
	t.Parallel()

	d := &declaration{Allowlist: []item{
		{Owner: "repository", Source: "usecases/x/src"},
		{Owner: "external", Repository: "github.com/o/r"},
	}}
	got := ownedTargets(d)
	assert.Equal(t, append(append([]string{}, codeownerTargets...), "usecases/x/src"), got)
	assert.Len(t, codeownerTargets, 10, "固定の対象を書き換えていない")
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
		"** は0段以上に当たる":           {pattern: "conftest/**/infra-code.json", target: "conftest/data/infra-code.json", want: true},
		"拡張子の glob":              {pattern: "*.json", target: "conftest/data/infra-code.json", want: true},
		"ディレクトリだけの行はファイルに当たらない":  {pattern: "infra-code.mk/", target: ".makefiles/terraform/infra-code.mk", want: false},
		"固定した行は別の深さに当たらない":       {pattern: "/hcl/", target: "conftest/hcl/", want: false},
		"別のパスには当たらない":            {pattern: "conftest/data/", target: "conftest/hcl/", want: false},
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
