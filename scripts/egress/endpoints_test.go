package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endpointSSOT は class と job だけを持つ宣言に tail（root と type）を続ける。a.yaml:one の許可リストは b1.test / kms.test / s3.test。
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

func endpointRepo(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "r/main.tf", body)
	return repo
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func parseBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	body, ok := f.Body.(*hclsyntax.Body)
	require.True(t, ok)
	return body.Blocks[0].Body
}

func terraformRuns(s *ssot) map[string]rootRuns {
	ran := map[string]rootRuns{}
	for name, r := range s.roots {
		for _, key := range r.jobs {
			if ran[key] == nil {
				ran[key] = rootRuns{}
			}
			ran[key][name] = true
		}
	}
	return ran
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
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("module を辿った root の provider の region と型が呼ぶ宛先の region が一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:kms\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:kms\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\"]\n[type.aws_none]\nhosts = []\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nmodule \"m\" {\n  source = \"./m\"\n}\ndata \"aws_none\" \"n\" {}\n")
			writeFile(t, repo, "r/m/main.tf", "resource \"aws_kms_key\" \"k\" {}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("ブロックの region 引数の region の宛先を、その型の宛先として認める", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"chatbot.us-west-2.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[type.aws_chatbot_]\nhosts = [\"chatbot.us-west-2.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n"+
				"resource \"aws_chatbot_slack_channel_configuration\" \"c\" {\n  region = \"us-west-2\"\n}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("同じ type に当たる型を置く region を合わせて、type の宛先と突き合わせる", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-west-2.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-west-2.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n"+
				"resource \"aws_kms_alias\" \"a\" {\n  region = \"us-west-2\"\n}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("root が型を置かない region の宛先は、その root のジョブに求めない", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[job.\"c.yaml:y\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[root.\"r/w\"]\njobs = [\"c.yaml:y\"]\n"+
				"[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			writeFile(t, repo, "r/w/main.tf", "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n"+
				"resource \"aws_kms_key\" \"u\" {\n  region = \"us-east-1\"\n}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("global の宛先は、provider の region が別でも呼ぶ宛先として数える", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"organizations.us-east-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_organizations_]\nhosts = [\"organizations.us-east-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_organizations_account\" \"a\" {}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})

		t.Run("lock の解除だけで root に触れるジョブには、型が呼ぶ宛先を求めない", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\", \"b.yaml:two\"]\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			ran := map[string]rootRuns{"a.yaml:one": {"r": true}, "b.yaml:two": {"r": false}}
			assert.NoError(t, s.checkEndpoints(repo, ran))
		})

		t.Run("base の宛先もジョブの許可リストとして数える", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\", \"b.yaml:two\"]\n[type.aws_sts_]\nhosts = [\"b1.test\"]\n")
			repo := endpointRepo(t, "data \"aws_sts_x\" \"c\" {}\n")
			assert.NoError(t, s.checkEndpoints(repo, terraformRuns(s)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root が 1 件も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "")
			require.ErrorIs(t, s.checkEndpoints(t.TempDir(), terraformRuns(s)), errNoRoots)
		})

		t.Run("ジョブを持たない root を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = []\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			require.ErrorIs(t, s.checkEndpoints(repo, terraformRuns(s)), errRootNoJobs)
		})

		t.Run("名指すジョブのどれもが lock の解除だけで触れる root を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, map[string]rootRuns{"a.yaml:one": {"r": false}})
			require.ErrorIs(t, err, errRootNoTerraform)
			assert.Contains(t, err.Error(), "r")
		})

		t.Run("lock の解除の入口とともに plan / apply の入口も呼ぶジョブに、型が呼ぶ宛先が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\", \"b.yaml:two\"]\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, map[string]rootRuns{"a.yaml:one": {"r": true}, "b.yaml:two": {"r": true}})
			require.ErrorIs(t, err, errEndpointMissing)
			assert.Contains(t, err.Error(), "kms.test → b.yaml:two")
		})

		t.Run("型が 1 件も無い root を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne)
			repo := endpointRepo(t, "locals {\n  a = 1\n}\n")
			require.ErrorIs(t, s.checkEndpoints(repo, terraformRuns(s)), errNoTypes)
		})

		t.Run("root のディレクトリが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne)
			require.ErrorIs(t, s.checkEndpoints(t.TempDir(), terraformRuns(s)), errNoTerraform)
		})

		t.Run("どの type にも当たらない型を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\nresource \"aws_dynamodb_table\" \"t\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errTypeUnmapped)
			assert.Contains(t, err.Error(), "aws_dynamodb_table")
		})

		t.Run("型が呼ぶ宛先がジョブの許可リストに無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\", \"kms-fips.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errEndpointMissing)
			assert.Contains(t, err.Error(), "kms-fips.test")
		})

		t.Run("どの root の型にも当たらない type を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_kms_]\nhosts = [\"kms.test\"]\n[type.aws_s3_]\nhosts = [\"s3.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errTypeOrphan)
			assert.Contains(t, err.Error(), "aws_s3_")
		})

		t.Run("type の宛先を許すジョブが root に名指されていなければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"b.yaml:two\"]\n[type.aws_sts_]\nhosts = [\"b1.test\"]\n")
			repo := endpointRepo(t, "data \"aws_sts_x\" \"c\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errJobNoRoot)
			assert.Contains(t, err.Error(), "a.yaml:one")
		})

		t.Run("型が呼ぶ宛先の region が root の provider の region に無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:kms\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:kms\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"us-east-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "root r")
		})

		t.Run("ブロックの region 引数の region に宛先が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"chatbot.ap-northeast-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_chatbot_]\nhosts = [\"chatbot.ap-northeast-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n"+
				"resource \"aws_chatbot_slack_channel_configuration\" \"c\" {\n  region = \"us-west-2\"\n}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_chatbot_: us-west-2 の宛先がありません")
		})

		t.Run("同じ type に当たる別の型を置く region に宛先が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n"+
				"resource \"aws_kms_alias\" \"a\" {\n  region = \"us-west-2\"\n}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_kms_: us-west-2 の宛先がありません")
		})

		t.Run("root が型を置く region の宛先がジョブの許可リストに無ければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n"+
				"resource \"aws_kms_key\" \"u\" {\n  region = \"us-east-1\"\n}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errEndpointMissing)
			assert.Contains(t, err.Error(), "kms.us-east-1.amazonaws.com → c.yaml:x")
		})

		t.Run("どの root も型を置かない region の宛先を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errHostUnplaced)
			assert.Contains(t, err.Error(), "aws_kms_: kms.us-east-1.amazonaws.com")
		})

		t.Run("2 つの root の和集合にも無い region の宛先を弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\", \"kms.us-west-2.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[root.\"r/w\"]\njobs = [\"c.yaml:x\"]\n"+
				"[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\", \"kms.us-east-1.amazonaws.com\", \"kms.us-west-2.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			writeFile(t, repo, "r/w/main.tf", "provider \"aws\" {\n  region = \"us-west-2\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errHostUnplaced)
			assert.Contains(t, err.Error(), "aws_kms_: kms.us-east-1.amazonaws.com")
		})

		t.Run("どの root も使わない type は、宛先ではなく type の残存として弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:x\"]\nextra = [\"kms.ap-northeast-1.amazonaws.com\", \"sns.us-east-1.amazonaws.com\"]\n"+
				"[root.r]\njobs = [\"c.yaml:x\"]\n[type.aws_kms_]\nhosts = [\"kms.ap-northeast-1.amazonaws.com\"]\n[type.aws_sns_]\nhosts = [\"sns.us-east-1.amazonaws.com\"]\n")
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\nresource \"aws_kms_key\" \"k\" {}\n")
			err := s.checkEndpoints(repo, terraformRuns(s))
			require.ErrorIs(t, err, errTypeOrphan)
			assert.NotErrorIs(t, err, errHostUnplaced)
		})

		t.Run("ブロックの region 引数がリテラルでなければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, rootOne+"[type.aws_chatbot_]\nhosts = [\"kms.test\"]\n")
			repo := endpointRepo(t, "resource \"aws_chatbot_slack_channel_configuration\" \"c\" {\n  region = var.region\n}\n")
			require.ErrorIs(t, s.checkEndpoints(repo, terraformRuns(s)), errBlockRegion)
		})
	})
}

func Test_ssot_requireRoots(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("type の宛先を許すジョブがすべて root に名指されていれば成功する", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\"]\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			assert.NoError(t, s.requireRoots())
		})

		t.Run("type の宛先を許さないジョブと audit のジョブは名指されなくてよい", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\"]\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			assert.NotContains(t, s.roots["r"].jobs, "b.yaml:two")
			assert.NotContains(t, s.roots["r"].jobs, "b.yaml:audited")
			assert.NoError(t, s.requireRoots())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("type の宛先を許すジョブが root に名指されていなければ弾く", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"b.yaml:two\"]\n[type.aws_kms_]\nhosts = [\"kms.test\"]\n")
			err := s.requireRoots()
			require.ErrorIs(t, err, errJobNoRoot)
			assert.Contains(t, err.Error(), "a.yaml:one")
		})

		t.Run("wildcard で type の宛先を覆うジョブも名指しの対象になる", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[job.\"c.yaml:wide\"]\nextra = [\"*.ex.test\"]\n[root.r]\njobs = [\"a.yaml:one\"]\n[type.aws_kms_]\nhosts = [\"kms.ex.test\"]\n")
			err := s.requireRoots()
			require.ErrorIs(t, err, errJobNoRoot)
			assert.Contains(t, err.Error(), "c.yaml:wide")
		})

		t.Run("base の宛先を type が要るなら全ジョブが名指しの対象になる", func(t *testing.T) {
			t.Parallel()
			s := endpointSSOT(t, "[root.r]\njobs = [\"a.yaml:one\"]\n[type.aws_sts_]\nhosts = [\"b1.test\"]\n")
			require.ErrorIs(t, s.requireRoots(), errJobNoRoot)
		})
	})
}

func Test_ssot_matchType(t *testing.T) {
	t.Parallel()

	s := endpointSSOT(t, "[type.aws_iam_]\nhosts = [\"iam.test\"]\n[type.aws_iam_policy_document]\nhosts = []\n[type.aws_region]\nhosts = []\n")

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

		t.Run("wildcard で宛先を覆うジョブは宛先を持つとみなす", func(t *testing.T) {
			t.Parallel()
			w := endpointSSOT(t, "[job.\"c.yaml:wide\"]\nextra = [\"*.ex.test\"]\n")
			assert.NoError(t, w.requireHosts([]string{"c.yaml:wide"}, []string{"kms.ex.test"}))
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
			writeFile(t, repo, "m/a/internal/b/main.tf", "ephemeral \"aws_x\" \"x\" {}\nresource \"aws_z\" \"z2\" {\n  region = \"us-west-2\"\n}\n")
			got, dirs, err := collectTypes(repo, "env/r")
			require.NoError(t, err)
			assert.Equal(t, typeRegions{"aws_x": {"": true}, "aws_y": {"": true}, "aws_z": {"": true, "us-west-2": true}}, got)
			assert.Equal(t, []string{"env/r", "m/a", "m/a/internal/b"}, dirs)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("型が 1 件も無ければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "variable \"a\" {}\n")
			_, _, err := collectTypes(repo, "r")
			require.ErrorIs(t, err, errNoTypes)
		})

		t.Run("辿った先の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"a\" {\n  source = \"./missing\"\n}\n")
			_, _, err := collectTypes(repo, "r")
			require.ErrorIs(t, err, errNoTerraform)
		})

		t.Run("リポジトリを開けなければ弾く", func(t *testing.T) {
			t.Parallel()
			_, _, err := collectTypes(filepath.Join(t.TempDir(), "missing"), "r")
			require.ErrorIs(t, err, os.ErrNotExist)
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
			found, visited := typeRegions{}, map[string]bool{}
			require.NoError(t, walkModule(openRoot(t, repo), "r", found, visited))
			assert.Equal(t, typeRegions{"aws_x": {"": true}}, found)
			assert.Equal(t, map[string]bool{"r": true, "r/m": true}, visited)
		})

		t.Run("サブディレクトリは module として呼ばれない限り読まない", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			writeFile(t, repo, "r/tests/main.tf", "resource \"aws_unrelated\" \"u\" {}\n")
			found := typeRegions{}
			require.NoError(t, walkModule(openRoot(t, repo), "r", found, map[string]bool{}))
			assert.Equal(t, typeRegions{"aws_x": {"": true}}, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("*.tf の無いディレクトリを弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, "r/README.md", "x")
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errNoTerraform)
		})

		t.Run("*.tf.json を持つディレクトリを弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			writeFile(t, repo, "r/extra.tf.json", `{"resource": {"aws_hidden": {"h": {}}}}`)
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errTerraformSyntax)
		})

		t.Run("リポジトリの外を指す module を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"a\" {\n  source = \"../../outside\"\n}\n")
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errModuleSource)
		})

		t.Run("ファイルの解釈の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" {\n")
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errTerraformSyntax)
		})

		t.Run("symlink の *.tf を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			outside := t.TempDir()
			writeFile(t, outside, "o.tf", "resource \"aws_hidden\" \"h\" {}\n")
			require.NoError(t, os.Symlink(filepath.Join(outside, "o.tf"), filepath.Join(repo, "r", "link.tf")))
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errSymlink)
		})

		t.Run("symlink の module ディレクトリを弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"m\" {\n  source = \"./m\"\n}\n")
			outside := t.TempDir()
			writeFile(t, outside, "main.tf", "resource \"aws_hidden\" \"h\" {}\n")
			require.NoError(t, os.Symlink(outside, filepath.Join(repo, "r", "m")))
			require.ErrorIs(t, walkModule(openRoot(t, repo), "r", typeRegions{}, map[string]bool{}), errSymlink)
		})
	})
}

func Test_rejectSymlink(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("通常のファイルとディレクトリを受ける", func(t *testing.T) {
			t.Parallel()
			root := openRoot(t, endpointRepo(t, "x"))
			assert.NoError(t, rejectSymlink(root, "r"))
			assert.NoError(t, rejectSymlink(root, "r/main.tf"))
		})

		t.Run("存在しないパスは後続の検査へ委ねる", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, rejectSymlink(openRoot(t, t.TempDir()), "missing"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("symlink を弾く", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dir, "link")))
			require.ErrorIs(t, rejectSymlink(openRoot(t, dir), "link"), errSymlink)
		})

		t.Run("repo の中を指す途中の要素の symlink を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			require.NoError(t, os.Symlink("r", filepath.Join(repo, "env")))
			err := rejectSymlink(openRoot(t, repo), "env/main.tf")
			require.ErrorIs(t, err, errSymlink)
			assert.Contains(t, err.Error(), "env")
		})
	})
}

func Test_covers(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ allowed, host string }{
			"字面の一致": {"kms.test", "kms.test"},
			"wildcard が 1 段のサブドメインに当たる":  {"*.test", "kms.test"},
			"wildcard が深いサブドメインに当たる":     {"*.amazonaws.com", "kms.ap-northeast-1.amazonaws.com"},
			"wildcard が wildcard の宛先を覆う": {"*.amazonaws.com", "*.s3.ap-northeast-1.amazonaws.com"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.True(t, covers([]string{"other.test", tc.allowed}, tc.host))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ allowed, host string }{
			"wildcard は apex に当たらない": {"*.amazonaws.com", "amazonaws.com"},
			"別のドメイン":                 {"*.example.com", "kms.test"},
			"許可リストが空":                {"", "kms.test"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.False(t, covers([]string{tc.allowed}, tc.host))
			})
		}
	})
}

func Test_addType(t *testing.T) {
	t.Parallel()

	block := func(t *testing.T, src string) *hclsyntax.Block {
		t.Helper()
		f, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		body, ok := f.Body.(*hclsyntax.Body)
		require.True(t, ok)
		return body.Blocks[0]
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ラベルの先頭を、region 引数を持たない型として足す", func(t *testing.T) {
			t.Parallel()
			found := typeRegions{}
			require.NoError(t, addType(block(t, "data \"aws_a\" \"x\" {}\n"), found))
			assert.Equal(t, typeRegions{"aws_a": {"": true}}, found)
		})

		t.Run("action の config の region 引数を、その型を置く region として足す", func(t *testing.T) {
			t.Parallel()
			found := typeRegions{}
			require.NoError(t, addType(block(t, "action \"aws_a\" \"x\" {\n  config {\n    region = \"us-east-1\"\n  }\n}\n"), found))
			assert.Equal(t, typeRegions{"aws_a": {"us-east-1": true}}, found)
		})

		t.Run("region 引数のリテラルを、その型を置く region として足す", func(t *testing.T) {
			t.Parallel()
			found := typeRegions{"aws_a": {"": true}}
			require.NoError(t, addType(block(t, "resource \"aws_a\" \"x\" {\n  region = \"us-west-2\"\n}\n"), found))
			assert.Equal(t, typeRegions{"aws_a": {"": true, "us-west-2": true}}, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ラベルが 2 つでなければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, addType(block(t, "data \"aws_a\" {}\n"), typeRegions{}), errTerraformSyntax)
		})

		t.Run("region 引数が空文字列なら、region 引数を持たないブロックと取り違えずに弾く", func(t *testing.T) {
			t.Parallel()
			err := addType(block(t, "resource \"aws_a\" \"x\" {\n  region = \"\"\n}\n"), typeRegions{})
			require.ErrorIs(t, err, errBlockRegion)
		})

		t.Run("action の region を読めない形なら、どのブロックかを添えて弾く", func(t *testing.T) {
			t.Parallel()
			err := addType(block(t, "action \"aws_a\" \"x\" {\n  region = \"us-east-1\"\n}\n"), typeRegions{})
			require.ErrorIs(t, err, errBlockRegion)
			assert.Contains(t, err.Error(), "aws_a.x")
		})

		t.Run("action の config の region 引数がリテラルでなければ弾く", func(t *testing.T) {
			t.Parallel()
			err := addType(block(t, "action \"aws_a\" \"x\" {\n  config {\n    region = var.region\n  }\n}\n"), typeRegions{})
			require.ErrorIs(t, err, errBlockRegion)
		})

		t.Run("region 引数が region の形のリテラルでなければ弾く", func(t *testing.T) {
			t.Parallel()
			err := addType(block(t, "resource \"aws_a\" \"x\" {\n  region = var.region\n}\n"), typeRegions{})
			require.ErrorIs(t, err, errBlockRegion)
			assert.Contains(t, err.Error(), "aws_a.x")
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
			found := typeRegions{}
			sources, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", found)
			require.NoError(t, err)
			assert.Equal(t, typeRegions{"aws_a": {"": true}, "aws_b": {"": true}, "aws_c": {"": true}}, found)
			assert.Equal(t, []string{"./m"}, sources)
		})

		t.Run("check の中の data と action の型を足す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "check \"c\" {\n  data \"aws_d\" \"d\" {}\n  assert {\n    condition     = true\n    error_message = \"x\"\n  }\n}\n"+
				"action \"aws_e\" \"e\" {}\n")
			found := typeRegions{}
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", found)
			require.NoError(t, err)
			assert.Equal(t, typeRegions{"aws_d": {"": true}, "aws_e": {"": true}}, found)
		})

		t.Run("コメントの中のブロックの字面は数えない", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "# resource \"aws_z\" \"z\" {}\nresource \"aws_a\" \"a\" {}\n")
			found := typeRegions{}
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", found)
			require.NoError(t, err)
			assert.Equal(t, typeRegions{"aws_a": {"": true}}, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読めないファイルを弾く", func(t *testing.T) {
			t.Parallel()
			_, err := scanTerraformFile(openRoot(t, t.TempDir()), "missing.tf", typeRegions{})
			require.Error(t, err)
		})

		t.Run("解釈できない構文を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_a\" {\n")
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", typeRegions{})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		t.Run("ラベルが 2 つでない resource を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_a\" {}\n")
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", typeRegions{})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		t.Run("module の source の失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "module \"m\" {\n  source = \"hashicorp/consul/aws\"\n}\n")
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", typeRegions{})
			require.ErrorIs(t, err, errModuleSource)
		})

		t.Run("check の中の data のラベルの不正を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "check \"c\" {\n  data \"aws_d\" {}\n}\n")
			_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", typeRegions{})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		for name, body := range map[string]string{
			// 走査が読まないブロックの中に型が在ると、解析の件数が字面より少なくなる。
			"知らないブロックの中の resource": "terraform {\n  resource \"aws_z\" \"z\" {}\n}\nresource \"aws_a\" \"a\" {}\n",
			"heredoc の中のブロックの字面":   "locals {\n  t = <<EOT\nresource \"aws_z\" \"z\" {}\nEOT\n}\nresource \"aws_a\" \"a\" {}\n",
		} {
			t.Run(name+"で件数が食い違えば弾く", func(t *testing.T) {
				t.Parallel()
				repo := endpointRepo(t, body)
				_, err := scanTerraformFile(openRoot(t, repo), "r/main.tf", typeRegions{})
				require.ErrorIs(t, err, errBlockCount)
			})
		}
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

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("鍵を整列して返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"a", "b"}, sortedKeys(map[string]bool{"b": true, "a": true}))
		})
	})
}

func Test_checkRegions(t *testing.T) {
	t.Parallel()

	const provider = "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n"
	// atProvider は region 引数を持たないブロックだけの type。
	atProvider := func(hosts ...string) typeUse {
		return typeUse{hosts: hosts, regions: map[string]bool{"": true}}
	}
	regionsErr := func(repo string, dirs []string, uses map[string]typeUse) error {
		_, err := checkRegions(repo, dirs, uses)
		return err
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宛先の region が provider の region にあれば成功する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_s3_": atProvider("iam.amazonaws.com", "*.s3.ap-northeast-1.amazonaws.com")}
			got, err := checkRegions(repo, []string{"r"}, uses)
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{"aws_s3_": {"iam.amazonaws.com", "*.s3.ap-northeast-1.amazonaws.com"}}, got)
		})

		t.Run("region を含む宛先が無ければ provider を読まず、宛先をそのまま返す", func(t *testing.T) {
			t.Parallel()
			got, err := checkRegions(t.TempDir(), []string{"missing"}, map[string]typeUse{"aws_iam_": atProvider("iam.amazonaws.com")})
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{"aws_iam_": {"iam.amazonaws.com"}}, got)
		})

		t.Run("ブロックの region 引数の region と宛先の region が一致すれば、provider を読まずに成功する", func(t *testing.T) {
			t.Parallel()
			uses := map[string]typeUse{"aws_chatbot_": {hosts: []string{"chatbot.us-west-2.amazonaws.com"}, regions: map[string]bool{"us-west-2": true}}}
			assert.NoError(t, regionsErr(t.TempDir(), []string{"missing"}, uses))
		})

		t.Run("provider の region と region 引数の region の両方に置く type は、両方の宛先を持てば成功する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_kms_": {
				hosts:   []string{"kms.ap-northeast-1.amazonaws.com", "kms.us-west-2.amazonaws.com"},
				regions: map[string]bool{"": true, "us-west-2": true},
			}}
			got, err := checkRegions(repo, []string{"r"}, uses)
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{"aws_kms_": {"kms.ap-northeast-1.amazonaws.com", "kms.us-west-2.amazonaws.com"}}, got)
		})

		t.Run("AWS が region を固定した global の宛先は provider の region と突き合わせない", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_organizations_": atProvider("organizations.us-east-1.amazonaws.com", "kms.ap-northeast-1.amazonaws.com")}
			assert.NoError(t, regionsErr(repo, []string{"r"}, uses))
		})

		t.Run("type を置かない region の宛先は、その root で呼ぶ宛先から外す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			got, err := checkRegions(repo, []string{"r"}, map[string]typeUse{"aws_kms_": atProvider("kms.ap-northeast-1.amazonaws.com", "kms.us-east-1.amazonaws.com")})
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{"aws_kms_": {"kms.ap-northeast-1.amazonaws.com"}}, got)
		})

		t.Run("type を置かない region の宛先を外しても、region を持たない宛先と global の宛先は返す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_organizations_": atProvider("iam.amazonaws.com", "organizations.us-east-1.amazonaws.com", "kms.ap-northeast-1.amazonaws.com", "kms.us-west-2.amazonaws.com")}
			got, err := checkRegions(repo, []string{"r"}, uses)
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{"aws_organizations_": {"iam.amazonaws.com", "organizations.us-east-1.amazonaws.com", "kms.ap-northeast-1.amazonaws.com"}}, got)
		})

		t.Run("global の宛先だけなら provider を読まない", func(t *testing.T) {
			t.Parallel()
			uses := map[string]typeUse{"aws_organizations_": atProvider("organizations.us-east-1.amazonaws.com")}
			assert.NoError(t, regionsErr(t.TempDir(), []string{"missing"}, uses))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宛先の region が provider の region に無ければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			err := regionsErr(repo, []string{"r"}, map[string]typeUse{"aws_kms_": atProvider("kms.us-east-1.amazonaws.com")})
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_kms_: ap-northeast-1 の宛先がありません（宛先の region: us-east-1）")
		})

		t.Run("region を含む宛先を持たない type の後の type も突き合わせる", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{
				"aws_iam_": atProvider("iam.amazonaws.com"),
				"aws_kms_": atProvider("kms.us-east-1.amazonaws.com"),
			}
			err := regionsErr(repo, []string{"r"}, uses)
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_kms_: ap-northeast-1 の宛先がありません")
		})

		t.Run("type を置く region のうち宛先の無いものがあれば弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_kms_": {hosts: []string{"kms.ap-northeast-1.amazonaws.com"}, regions: map[string]bool{"": true, "us-west-2": true}}}
			err := regionsErr(repo, []string{"r"}, uses)
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_kms_: us-west-2")
		})

		t.Run("region 引数の region に宛先が無ければ、provider の region の宛先があっても弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			uses := map[string]typeUse{"aws_chatbot_": {hosts: []string{"chatbot.ap-northeast-1.amazonaws.com"}, regions: map[string]bool{"us-west-2": true}}}
			err := regionsErr(repo, []string{"r"}, uses)
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_chatbot_: us-west-2 の宛先がありません（宛先の region: ap-northeast-1）")
		})

		t.Run("provider の region が 2 つ以上なら、宛先の region が片方に当たっても弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider+"provider \"aws\" {\n  alias  = \"us\"\n  region = \"us-east-1\"\n}\n")
			err := regionsErr(repo, []string{"r"}, map[string]typeUse{"aws_kms_": atProvider("kms.ap-northeast-1.amazonaws.com")})
			require.ErrorIs(t, err, errRegionNotOne)
			assert.Contains(t, err.Error(), "ap-northeast-1, us-east-1")
		})

		t.Run("provider を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "resource \"aws_x\" \"x\" {}\n")
			err := regionsErr(repo, []string{"r"}, map[string]typeUse{"aws_kms_": atProvider("kms.ap-northeast-1.amazonaws.com")})
			require.ErrorIs(t, err, errRegionUnknown)
		})

		t.Run("wildcard の付いた global の宛先は突き合わせから外さない", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			err := regionsErr(repo, []string{"r"}, map[string]typeUse{"aws_organizations_": atProvider("*.organizations.us-east-1.amazonaws.com")})
			require.ErrorIs(t, err, errRegionMismatch)
		})

		t.Run("global の宛先が混ざっていても、provider を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			uses := map[string]typeUse{"aws_organizations_": atProvider("organizations.us-east-1.amazonaws.com", "kms.ap-northeast-1.amazonaws.com")}
			require.ErrorIs(t, regionsErr(t.TempDir(), []string{"missing"}, uses), errRegionUnknown)
		})

		t.Run("global の宛先と同じサービスでも、別の region の宛先は弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, provider)
			err := regionsErr(repo, []string{"r"}, map[string]typeUse{"aws_organizations_": atProvider("organizations.us-west-2.amazonaws.com")})
			require.ErrorIs(t, err, errRegionMismatch)
			assert.Contains(t, err.Error(), "aws_organizations_: ap-northeast-1 の宛先がありません（宛先の region: us-west-2）")
		})
	})
}

func Test_hostRegion(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for host, want := range map[string]string{
			"kms.ap-northeast-1.amazonaws.com":         "ap-northeast-1",
			"*.s3-control.us-gov-west-1.amazonaws.com": "us-gov-west-1",
			"s3-us-west-2.amazonaws.com":               "us-west-2",
			"iam.amazonaws.com":                        "",
			"s3-control.amazonaws.com":                 "",
			"s3-external-1.amazonaws.com":              "",
		} {
			t.Run(host+" の region は「"+want+"」", func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, want, hostRegion(host))
			})
		}
	})
}

func Test_providerRegions(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("辿ったディレクトリの aws の provider の region を重複なく整列して返す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"us-east-1\"\n}\nprovider \"aws\" {\n  alias  = \"a\"\n  region = \"ap-northeast-1\"\n}\n")
			writeFile(t, repo, "m/main.tf", "provider \"aws\" {\n  region = \"us-east-1\"\n}\nprovider \"other\" {}\n")
			got, err := providerRegions(repo, []string{"r", "m"})
			require.NoError(t, err)
			assert.Equal(t, []string{"ap-northeast-1", "us-east-1"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("aws の provider が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "provider \"other\" {}\n")
			_, err := providerRegions(repo, []string{"r"})
			require.ErrorIs(t, err, errRegionUnknown)
		})

		t.Run("ファイルの失敗を伝播する", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "provider \"aws\" {\n")
			_, err := providerRegions(repo, []string{"r"})
			require.ErrorIs(t, err, errTerraformSyntax)
		})

		t.Run("リポジトリを開けなければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := providerRegions(filepath.Join(t.TempDir(), "missing"), []string{"r"})
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	})
}

func Test_regionAttribute(t *testing.T) {
	t.Parallel()

	block := func(t *testing.T, src string) *hclsyntax.Block {
		t.Helper()
		f, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		body, ok := f.Body.(*hclsyntax.Body)
		require.True(t, ok)
		return body.Blocks[0]
	}

	literal := func(t *testing.T, attr *hclsyntax.Attribute) string {
		t.Helper()
		got, ok := regionLiteral(attr)
		require.True(t, ok)
		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("resource の直下の region 引数を返す", func(t *testing.T) {
			t.Parallel()
			attr, ok, err := regionAttribute(block(t, "resource \"aws_a\" \"x\" {\n  region = \"us-west-2\"\n}\n"))
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "us-west-2", literal(t, attr))
		})

		t.Run("action の config の中の region 引数を返す", func(t *testing.T) {
			t.Parallel()
			attr, ok, err := regionAttribute(block(t, "action \"aws_a\" \"x\" {\n  config {\n    region = \"us-east-1\"\n  }\n}\n"))
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "us-east-1", literal(t, attr))
		})

		for name, src := range map[string]string{
			"region 引数を持たない resource":        "resource \"aws_a\" \"x\" {}\n",
			"config の中の region を持つ resource": "resource \"aws_a\" \"x\" {\n  config {\n    region = \"us-east-1\"\n  }\n}\n",
			"config を持たない action":            "action \"aws_a\" \"x\" {}\n",
			"config に region 引数を持たない action": "action \"aws_a\" \"x\" {\n  config {}\n}\n",
		} {
			t.Run(name+"は region 引数を持たない", func(t *testing.T) {
				t.Parallel()
				_, ok, err := regionAttribute(block(t, src))
				require.NoError(t, err)
				assert.False(t, ok)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"config の外に region を持つ action":   "action \"aws_a\" \"x\" {\n  region = \"us-east-1\"\n}\n",
			"config の外と中に region を持つ action": "action \"aws_a\" \"x\" {\n  region = \"us-west-2\"\n  config {\n    region = \"us-east-1\"\n  }\n}\n",
			"config が 2 つの action":           "action \"aws_a\" \"x\" {\n  config {}\n  config {\n    region = \"us-east-1\"\n  }\n}\n",
			"dynamic の config を持つ action":    "action \"aws_a\" \"x\" {\n  dynamic \"config\" {\n    for_each = []\n    content {}\n  }\n}\n",
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				_, _, err := regionAttribute(block(t, src))
				require.ErrorIs(t, err, errBlockRegion)
			})
		}
	})
}

func Test_regionLiteral(t *testing.T) {
	t.Parallel()

	attr := func(t *testing.T, value string) *hclsyntax.Attribute {
		t.Helper()
		return parseBody(t, "x {\n  region = "+value+"\n}\n").Attributes["region"]
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("region の形のリテラルを返す", func(t *testing.T) {
			t.Parallel()
			got, ok := regionLiteral(attr(t, "\"us-west-2\""))
			assert.True(t, ok)
			assert.Equal(t, "us-west-2", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, value := range map[string]string{
			"変数":              "var.region",
			"region の形でない文字列": "\"tokyo\"",
			"文字列でない値":         "1",
			"null":            "null",
			"空文字列":            "\"\"",
			"補間を含む文字列":        "\"${var.r}\"",
		} {
			t.Run(name+"なら読めない", func(t *testing.T) {
				t.Parallel()
				_, ok := regionLiteral(attr(t, value))
				assert.False(t, ok)
			})
		}
	})
}

func Test_fileRegions(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("aws の provider の region を足す", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n")
			found := map[string]bool{}
			require.NoError(t, fileRegions(openRoot(t, repo), "r/main.tf", found))
			assert.Equal(t, map[string]bool{"ap-northeast-1": true}, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読めないファイルを弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, fileRegions(openRoot(t, t.TempDir()), "missing.tf", map[string]bool{}), os.ErrNotExist)
		})

		for name, body := range map[string]string{
			"region が無い":            "provider \"aws\" {}\n",
			"region が変数":            "provider \"aws\" {\n  region = var.region\n}\n",
			"region が region の形でない": "provider \"aws\" {\n  region = \"tokyo\"\n}\n",
			"region が文字列でない":        "provider \"aws\" {\n  region = 1\n}\n",
			"region が null":         "provider \"aws\" {\n  region = null\n}\n",
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				repo := endpointRepo(t, body)
				require.ErrorIs(t, fileRegions(openRoot(t, repo), "r/main.tf", map[string]bool{}), errRegionUnknown)
			})
		}

		t.Run("解釈できない構文を弾く", func(t *testing.T) {
			t.Parallel()
			repo := endpointRepo(t, "provider \"aws\" {\n")
			require.ErrorIs(t, fileRegions(openRoot(t, repo), "r/main.tf", map[string]bool{}), errTerraformSyntax)
		})
	})
}
