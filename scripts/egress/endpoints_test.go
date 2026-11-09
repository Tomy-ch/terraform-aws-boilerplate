package main

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endpointSSOT は testSSOT の root と type だけを差し替えた宣言。a.yaml:one の許可リストは oneHosts。
func endpointSSOT(t *testing.T, tail string) *ssot {
	t.Helper()
	return mustParse(t, `[class.base]
hosts = ["b1.test"]

[class.aws]
hosts = ["kms.test", "s3.test"]

[job."a.yaml:one"]
classes = ["aws"]

[job."b.yaml:two"]
classes = []

[job."b.yaml:audited"]
egress_policy = "audit"
`+tail)
}

// endpointRepo は root "r" に body を置いたリポジトリ。
func endpointRepo(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "r/main.tf", body)
	return repo
}

func parseBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	body, ok := f.Body.(*hclsyntax.Body)
	require.True(t, ok)
	return body.Blocks[0].Body
}

func Test_ssot_checkEndpoints(t *testing.T) {
	t.Parallel()

	const rootOne = "[root.r]\njobs = [\"a.yaml:one\"]\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("型が呼ぶ宛先がジョブの許可リストにあれば成功する", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n[type.aws_none]\nhosts = []\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\ndata \"aws_none\" \"n\" {}\n")
			assert.NoError(t, s.checkEndpoints(repo))
		})

		t.Run("base の宛先もジョブの許可リストとして数える", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_sts_]\nhosts = [\"b1.test\"]\n")
			repo := endpointRepo(t, "data \"aws_sts_x\" \"c\" {}\n")
			assert.NoError(t, s.checkEndpoints(repo))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root が 1 件も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "")
			require.ErrorIs(t, s.checkEndpoints(t.TempDir()), errNoRoots)
		})

		t.Run("ジョブを持たない root を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = []\n[type.aws_kms_]\nhosts = []\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			require.ErrorIs(t, s.checkEndpoints(repo), errRootNoJobs)
		})

		t.Run("型が 1 件も無い root を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne)
			repo := endpointRepo(t, "locals {\n  a = 1\n}\n")
			require.ErrorIs(t, s.checkEndpoints(repo), errNoTypes)
		})

		t.Run("root のディレクトリが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne)
			require.ErrorIs(t, s.checkEndpoints(t.TempDir()), errNoTerraform)
		})

		t.Run("どの type にも当たらない型を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\nresource \"aws_dynamodb_table\" \"t\" {}\n")
			err := s.checkEndpoints(repo)
			require.ErrorIs(t, err, errTypeUnmapped)
			assert.Contains(t, err.Error(), "aws_dynamodb_table")
		})

		t.Run("型が呼ぶ宛先がジョブの許可リストに無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\", \"kms-fips.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo)
			require.ErrorIs(t, err, errEndpointMissing)
			assert.Contains(t, err.Error(), "kms-fips.test")
		})

		t.Run("どの root の型にも当たらない type を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n[type.aws_s3_]\nhosts = [\"s3.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo)
			require.ErrorIs(t, err, errTypeOrphan)
			assert.Contains(t, err.Error(), "aws_s3_")
		})
	})
}

func Test_ssot_matchType(t *testing.T) {
	t.Parallel()

	s := endpointSSOT(t, "[type.aws_iam_]\nhosts = []\n[type.aws_iam_policy_document]\nhosts = []\n[type.aws_region]\nhosts = []\n")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("接頭辞が当たる", func(t *testing.T) {
			t.Parallel()
			got, ok := s.matchType("aws_iam_role")
			require.True(t, ok)
			assert.Equal(t, "aws_iam_", got)
		})

		t.Run("より長い宣言が勝つ", func(t *testing.T) {
			t.Parallel()
			got, ok := s.matchType("aws_iam_policy_document")
			require.True(t, ok)
			assert.Equal(t, "aws_iam_policy_document", got)
		})

		t.Run("型そのものの宣言が当たる", func(t *testing.T) {
			t.Parallel()
			got, ok := s.matchType("aws_region")
			require.True(t, ok)
			assert.Equal(t, "aws_region", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("_ で終わらない宣言は接頭辞として当たらない", func(t *testing.T) {
			t.Parallel()
			_, ok := s.matchType("aws_regions")
			assert.False(t, ok)
		})

		t.Run("どれにも当たらない型は無しを返す", func(t *testing.T) {
			t.Parallel()
			_, ok := s.matchType("aws_kms_key")
			assert.False(t, ok)
		})
	})
}

func Test_ssot_requireHosts(t *testing.T) {
	t.Parallel()

	s := endpointSSOT(t, "")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべてのジョブが宛先を持てば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, s.requireHosts([]string{"a.yaml:one"}, []string{"kms.test", "b1.test"}))
		})

		t.Run("audit のジョブは見ない", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, s.requireHosts([]string{"b.yaml:audited"}, []string{"kms.test"}))
		})

		t.Run("宛先が空なら何も要らない", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, s.requireHosts([]string{"b.yaml:two"}, nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("1 つのジョブでも宛先を欠けば弾く", func(t *testing.T) {
			t.Parallel()
			err := s.requireHosts([]string{"a.yaml:one", "b.yaml:two"}, []string{"kms.test"})
			require.ErrorIs(t, err, errEndpointMissing)
			assert.Contains(t, err.Error(), "b.yaml:two")
		})
	})
}

func Test_collectTypes(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("module を辿って型を重複なく整列して返す", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, "env/r/main.tf", "module \"a\" {\n  source = \"../../m/a\"\n}\nresource \"aws_z\" \"z\" {}\n")
			writeFile(t, repo, "m/a/main.tf", "module \"b\" {\n  source = \"./internal/b\"\n}\nresource \"aws_y\" \"y\" {}\n")
			writeFile(t, repo, "m/a/internal/b/main.tf", "ephemeral \"aws_x\" \"x\" {}\nresource \"aws_z\" \"z2\" {}\n")
			got, err := collectTypes(repo, "env/r")
			require.NoError(t, err)
			assert.Equal(t, []string{"aws_x", "aws_y", "aws_z"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("型が 1 件も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "variable \"a\" {}\n")
			_, err := collectTypes(repo, "r")
			require.ErrorIs(t, err, errNoTypes)
		})

		t.Run("辿った先の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"a\" {\n  source = \"./missing\"\n}\n")
			_, err := collectTypes(repo, "r")
			require.ErrorIs(t, err, errNoTerraform)
		})
	})
}

func Test_walkModule(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("同じ module を 2 経路で呼んでも 1 度だけ読む", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, "r/main.tf", "module \"a\" {\n  source = \"./m\"\n}\nmodule \"b\" {\n  source = \"./m\"\n}\n")
			writeFile(t, repo, "r/m/main.tf", "module \"back\" {\n  source = \"../\"\n}\nresource \"aws_x\" \"x\" {}\n")
			found, visited := map[string]bool{}, map[string]bool{}
			require.NoError(t, walkModule(repo, "r", found, visited))
			assert.Equal(t, map[string]bool{"aws_x": true}, found)
			assert.Equal(t, map[string]bool{"r": true, "r/m": true}, visited)
		})

		t.Run("サブディレクトリは module として呼ばれない限り読まない", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			writeFile(t, repo, "r/tests/main.tf", "resource \"aws_unrelated\" \"u\" {}\n")
			found := map[string]bool{}
			require.NoError(t, walkModule(repo, "r", found, map[string]bool{}))
			assert.Equal(t, map[string]bool{"aws_x": true}, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("*.tf の無いディレクトリを弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, "r/README.md", "x")
			require.ErrorIs(t, walkModule(repo, "r", map[string]bool{}, map[string]bool{}), errNoTerraform)
		})

		t.Run("*.tf.json を持つディレクトリを弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			writeFile(t, repo, "r/extra.tf.json", `{"resource": {"aws_hidden": {"h": {}}}}`)
			require.ErrorIs(t, walkModule(repo, "r", map[string]bool{}, map[string]bool{}), errTerraformSyntax)
		})

		t.Run("リポジトリの外を指す module を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"a\" {\n  source = \"../../outside\"\n}\n")
			require.ErrorIs(t, walkModule(repo, "r", map[string]bool{}, map[string]bool{}), errModuleSource)
		})

		t.Run("ファイルの解釈の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" {\n")
			require.ErrorIs(t, walkModule(repo, "r", map[string]bool{}, map[string]bool{}), errTerraformSyntax)
		})
	})
}

func Test_scanTerraformFile(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("resource / data / ephemeral の型と module の source を返す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_a\" \"a\" {}\ndata \"aws_b\" \"b\" {}\nephemeral \"aws_c\" \"c\" {}\n"+
				"import {\n  to = aws_a.a\n  id = \"x\"\n}\nmodule \"m\" {\n  source = \"./m\"\n}\n")
			found := map[string]bool{}
			sources, err := scanTerraformFile(repo+"/r/main.tf", found)
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"aws_a": true, "aws_b": true, "aws_c": true}, found)
			assert.Equal(t, []string{"./m"}, sources)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読めないファイルを弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanTerraformFile(t.TempDir()+"/missing.tf", map[string]bool{})
			require.Error(t, err)
		})

		t.Run("解釈できない構文を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_a\" {\n")
			_, err := scanTerraformFile(repo+"/r/main.tf", map[string]bool{})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		t.Run("ラベルが 2 つでない resource を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_a\" {}\n")
			_, err := scanTerraformFile(repo+"/r/main.tf", map[string]bool{})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		t.Run("module の source の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"m\" {\n  source = \"hashicorp/consul/aws\"\n}\n")
			_, err := scanTerraformFile(repo+"/r/main.tf", map[string]bool{})
			require.ErrorIs(t, err, errModuleSource)
		})
	})
}

func Test_moduleSource(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, src := range []string{"./internal/a", "../../modules/kms"} {
			t.Run(src+" を返す", func(t *testing.T) {
				t.Parallel()
				got, err := moduleSource(parseBody(t, "module \"m\" {\n  source = \""+src+"\"\n}\n"))
				require.NoError(t, err)
				assert.Equal(t, src, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, body := range map[string]string{
			"source が無い":   "module \"m\" {}\n",
			"変数を使う":        "module \"m\" {\n  source = var.src\n}\n",
			"文字列でない":       "module \"m\" {\n  source = 1\n}\n",
			"null":         "module \"m\" {\n  source = null\n}\n",
			"registry を指す": "module \"m\" {\n  source = \"hashicorp/consul/aws\"\n}\n",
			"VCS を指す":      "module \"m\" {\n  source = \"git::https://example.com/m.git\"\n}\n",
			"絶対パス":         "module \"m\" {\n  source = \"/abs/m\"\n}\n",
			"接頭辞の無い相対パス":   "module \"m\" {\n  source = \"modules/kms\"\n}\n",
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := moduleSource(parseBody(t, body))
				require.ErrorIs(t, err, errModuleSource)
			})
		}
	})
}
