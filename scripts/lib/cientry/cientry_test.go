package cientry

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var repoRoot = filepath.Join("..", "..", "..")

const nonEntryBlock = ".PHONY: ci-aws-credentials ## x\n" +
	"ci-aws-credentials: SHELL := /bin/bash\n" +
	"ci-aws-credentials:\n" +
	"\t@$(if $(CI_ENV),,$(error x))$(call TF_LOCAL,ci-credentials -env $(CI_ENV) -role $(CI_ROLE))\n"

// fixture は登録簿どおりの ci.mk の本文を組む。entries は {種別, env, root}。登録簿に無い種別の role は plan にする。
func fixture(entries ...[3]string) string {
	var b strings.Builder
	var defined []string
	for _, e := range entries {
		if !slices.Contains(defined, e[0]) {
			defined = append(defined, e[0])
			b.WriteString("define CI_" + strings.ToUpper(e[0]) + "\n$(call CI_PREPARE,$(1),$(2)); x\nendef\n")
		}
	}
	b.WriteString(nonEntryBlock)
	for _, e := range entries {
		target := "ci-" + e[0] + "-" + e[1] + "-" + e[2]
		role := "plan"
		if k, ok := Lookup(e[0]); ok {
			role = k.Role(Root(e[2]))
		}
		b.WriteString(".PHONY: " + target + " ## [CI] x\n" +
			target + ": SHELL := /bin/bash\n" +
			target + ": TF_LOCAL_PROFILE := $(call CI_PROFILE," + e[1] + "," + role + ")\n" +
			target + ":\n" +
			"\t@$(call CI_" + strings.ToUpper(e[0]) + "," + e[1] + "," + e[2] + ")\n")
	}
	return b.String()
}

func allEntries(env string) [][3]string {
	var out [][3]string
	for _, k := range Kinds() {
		out = append(out, [3]string{k.Name, env, "identity"}, [3]string{k.Name, env, "workload"})
	}
	return out
}

func replaced(t *testing.T, s, old, repl string) string {
	t.Helper()
	require.Contains(t, s, old)
	return strings.Replace(s, old, repl, 1)
}

func TestKinds(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿の順に、種別ごとの属性を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []Kind{
				{Name: Plan, Terraform: true, Lock: LockSkip, Events: []string{EventPullRequest}, IdentityRole: "plan", WorkloadRole: "plan"},
				{Name: Apply, Terraform: true, Lock: LockRelease, Events: []string{EventPush, EventWorkflowDispatch}, IdentityRole: "apply-identity", WorkloadRole: "apply"},
				{Name: Unlock, Terraform: false, Lock: LockRemove, Events: []string{EventWorkflowDispatch}, IdentityRole: "unlock-identity", WorkloadRole: "unlock"},
				{Name: Hold, Terraform: true, Lock: LockKeep, Events: []string{EventWorkflowDispatch}, IdentityRole: "apply-identity", WorkloadRole: "apply"},
			}, Kinds())
		})

		t.Run("返り値を書き換えても次の呼び出しは変わらない", func(t *testing.T) {
			t.Parallel()
			got := Kinds()
			got[0].Name = "x"
			got[1].Events[0] = "x"
			again := Kinds()
			assert.Equal(t, Plan, again[0].Name)
			assert.Equal(t, EventPush, again[1].Events[0])
		})
	})
}

func TestRoot_Dir(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("identity は env の下の identity、workload は env の直下", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "env/admin/identity", RootIdentity.Dir("admin"))
			assert.Equal(t, "env/admin", RootWorkload.Dir("admin"))
		})
	})
}

func TestLookup(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{Plan, Apply, Unlock, Hold} {
			t.Run(name+" を引ける", func(t *testing.T) {
				t.Parallel()
				k, ok := Lookup(name)
				require.True(t, ok)
				assert.Equal(t, name, k.Name)
			})
		}

		t.Run("返り値を書き換えても次の呼び出しは変わらない", func(t *testing.T) {
			t.Parallel()
			k, ok := Lookup(Apply)
			require.True(t, ok)
			k.Events[0] = "x"
			again, _ := Lookup(Apply)
			assert.Equal(t, EventPush, again.Events[0])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, in := range map[string]string{"知らない種別": "revert", "空": "", "大文字": "PLAN"} {
			t.Run(name+"は引けない", func(t *testing.T) {
				t.Parallel()
				k, ok := Lookup(in)
				assert.False(t, ok)
				assert.Equal(t, Kind{}, k)
			})
		}
	})
}

func TestRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別の順に identity、workload の role を重複なく返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"plan", "apply-identity", "apply", "unlock-identity", "unlock"}, Roles())
		})
	})
}

func TestDeniedRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("pull_request で起動する plan の role を除き、ほかの role を登録簿の順で返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"apply-identity", "apply", "unlock-identity", "unlock"}, DeniedRoles())
		})
	})
}

func Test_registry_deniedRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock の扱いではなく起動のイベントで分け、pull_request の種別と共有する role も除く", func(t *testing.T) {
			t.Parallel()
			r := registry{
				{Name: "a", Lock: LockRelease, Events: []string{EventPullRequest}, IdentityRole: "a-identity", WorkloadRole: "s"},
				{Name: "b", Lock: LockSkip, Events: []string{EventPush}, IdentityRole: "b-identity", WorkloadRole: "s"},
				{Name: "c", Lock: LockKeep, Events: []string{EventWorkflowDispatch, EventPullRequest}, IdentityRole: "c-identity", WorkloadRole: "c"},
			}
			assert.Equal(t, []string{"b-identity"}, r.deniedRoles())
		})

		t.Run("pull_request の種別が無ければすべての role を返す", func(t *testing.T) {
			t.Parallel()
			r := registry{{Name: "b", Lock: LockRelease, Events: []string{EventPush}, IdentityRole: "b-identity", WorkloadRole: "b"}}
			assert.Equal(t, []string{"b-identity", "b"}, r.deniedRoles())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべての種別が pull_request で起動すれば空を返す", func(t *testing.T) {
			t.Parallel()
			r := registry{{Name: "a", Lock: LockSkip, Events: []string{EventPullRequest}, IdentityRole: "a", WorkloadRole: "a"}}
			assert.Empty(t, r.deniedRoles())
		})
	})
}

func Test_registry_roles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock を取らない種別も含め、種別の順に identity、workload の role を重複なく返す", func(t *testing.T) {
			t.Parallel()
			r := registry{
				{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"},
				{Name: "b", Lock: LockRelease, IdentityRole: "b-identity", WorkloadRole: "b"},
				{Name: "c", Lock: LockKeep, IdentityRole: "b-identity", WorkloadRole: "c"},
			}
			assert.Equal(t, []string{"a", "b-identity", "b", "c"}, r.roles())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別が無ければ空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, registry{}.roles())
		})
	})
}

func TestKind_Role(t *testing.T) {
	t.Parallel()

	k := Kind{IdentityRole: "a-identity", WorkloadRole: "a"}
	for name, c := range map[string]struct {
		root Root
		want string
	}{
		"identity root は IdentityRole": {RootIdentity, "a-identity"},
		"workload root は WorkloadRole": {RootWorkload, "a"},
		"知らない root は空":                 {Root("other"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, k.Role(c.root))
		})
	}
}

func TestSplitEnvRoot(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for in, want := range map[string]struct {
			env  string
			root Root
		}{
			"admin-identity":       {"admin", RootIdentity},
			"log-archive-workload": {"log-archive", RootWorkload},
			"x-identity-workload":  {"x-identity", RootWorkload},
		} {
			t.Run(in+" を末尾の root で切る", func(t *testing.T) {
				t.Parallel()
				env, root, ok := SplitEnvRoot(in)
				require.True(t, ok)
				assert.Equal(t, want.env, env)
				assert.Equal(t, want.root, root)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, in := range []string{"-identity", "workload", "admin-other", ""} {
			t.Run(in+" は切れない", func(t *testing.T) {
				t.Parallel()
				env, root, ok := SplitEnvRoot(in)
				assert.False(t, ok)
				assert.Empty(t, env)
				assert.Empty(t, root)
			})
		}
	})
}

func TestMustPattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別の名前を prefix と suffix で挟む", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, `ci-(?:plan|apply)-`, MustPattern(`ci-`, `-`, Kind{Name: Plan}, Kind{Name: Apply}).String())
		})

		t.Run("区切りまで含めるので、種別を接頭辞に持つ別の名前に当たらない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, MustPattern(`ci-`, `-`, Kind{Name: Plan}).MatchString("ci-planner-x"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別が無ければ panic する", func(t *testing.T) {
			t.Parallel()
			assert.Panics(t, func() { MustPattern(`ci-`, `-`) })
		})

		for name, k := range map[string]Kind{
			"零値の種別": {},
			"名前が正規表現の字を含む種別": {Name: "a.b"},
			"名前が2語の種別":       {Name: "force-unlock"},
			"名前が大文字の種別":      {Name: "Plan"},
		} {
			t.Run(name+"が1つでも混じれば panic する", func(t *testing.T) {
				t.Parallel()
				assert.Panics(t, func() { MustPattern(`ci-`, `-`, Kind{Name: Plan}, k) })
			})
		}
	})
}

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿のすべての種別の入口を、書かれた順で返す", func(t *testing.T) {
			t.Parallel()
			data := fixture(allEntries("t")...)
			got, err := Parse(data)
			require.NoError(t, err)
			require.Len(t, got, 8)
			lines := strings.Split(data, "\n")
			for i, want := range allEntries("t") {
				k, _ := Lookup(want[0])
				root := Root(want[2])
				target := "ci-" + want[0] + "-t-" + want[2]
				assert.Equal(t, Entry{Target: target, Kind: k, Env: "t", Root: root, Role: k.Role(root), Line: slices.Index(lines, target+":") + 1}, got[i])
			}
		})

		t.Run("ハイフンを含む env を読む", func(t *testing.T) {
			t.Parallel()
			got, err := Parse(fixture([3]string{Plan, "log-archive", "workload"}))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "log-archive", got[0].Env)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		base := fixture([3]string{Plan, "t", "identity"})
		both := fixture([3]string{Plan, "t", "identity"}, [3]string{Plan, "t", "workload"})
		const call = "\t@$(call CI_PLAN,t,identity)\n"
		const profile = "ci-plan-t-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,t,plan)\n"
		const phony = ".PHONY: ci-plan-t-identity ## [CI] x\n"
		for name, c := range map[string]struct {
			data   func(t *testing.T) string
			err    error
			reason string
		}{
			"入口が1件も無い":             {func(*testing.T) string { return nonEntryBlock }, ErrParse, "入口が 1 件もありません"},
			"ルール行の次の行が呼び出しでない":     {func(t *testing.T) string { return replaced(t, base, call, "\t@echo x\n") }, ErrParse, "の次の行が"},
			"ルール行がファイルの最終行":        {func(*testing.T) string { return "ci-plan-t-identity:" }, ErrParse, "の次の行が"},
			"呼び出しの種別がターゲットと違う":     {func(t *testing.T) string { return replaced(t, base, call, "\t@$(call CI_APPLY,t,identity)\n") }, ErrParse, "ci-apply-t-identity の入口です"},
			"呼び出しの env がターゲットと違う":  {func(t *testing.T) string { return replaced(t, base, call, "\t@$(call CI_PLAN,u,identity)\n") }, ErrParse, "ci-plan-u-identity の入口です"},
			"呼び出しの root がターゲットと違う": {func(t *testing.T) string { return replaced(t, base, call, "\t@$(call CI_PLAN,t,workload)\n") }, ErrParse, "ci-plan-t-workload の入口です"},
			"root が identity / workload でない": {func(*testing.T) string {
				return "ci-plan-t-other:\n\t@$(call CI_PLAN,t,other)\n"
			}, ErrParse, "の次の行が"},
			"種別が登録簿に無い": {func(*testing.T) string { return fixture([3]string{"import", "t", "identity"}) }, ErrKindUnknown, "ci-import-t-identity（種別 import）"},
			"種別の名前が1語でない": {func(*testing.T) string {
				return "ci-force-unlock-t-identity:\n\t@$(call CI_FORCE_UNLOCK,t,identity)\n"
			}, ErrParse, "種別は1語（[a-z]+）に限ります"},
			"入口が重複している":            {func(*testing.T) string { return base + "ci-plan-t-identity:\n" + call }, ErrParse, "ci-plan-t-identity が重複しています"},
			"未分類の ci- のルールがある":     {func(*testing.T) string { return base + "ci-other: x\n" }, ErrParse, "ci-other は入口でも"},
			"未分類の ci- の変数の行がある":    {func(*testing.T) string { return base + "ci-other: SHELL := /bin/bash\n" }, ErrParse, "ci-other は入口でも"},
			"コロンの無い ci- の行がある":     {func(*testing.T) string { return base + "ci-other\n" }, ErrParse, "ci- で始まる行を読めません"},
			"TF_LOCAL_PROFILE が無い": {func(t *testing.T) string { return replaced(t, base, profile, "") }, ErrParse, "TF_LOCAL_PROFILE の行が 0 行です"},
			"TF_LOCAL_PROFILE が2行": {func(t *testing.T) string { return replaced(t, base, profile, profile+profile) }, ErrParse, "TF_LOCAL_PROFILE の行が 2 行です"},
			"TF_LOCAL_PROFILE の形が違う": {func(t *testing.T) string {
				return replaced(t, base, profile, "ci-plan-t-identity: TF_LOCAL_PROFILE = t-plan\n")
			}, ErrParse, "TF_LOCAL_PROFILE は"},
			"入口でないターゲットが TF_LOCAL_PROFILE を持つ": {func(*testing.T) string {
				return base + "ci-aws-credentials: TF_LOCAL_PROFILE := $(call CI_PROFILE,t,plan)\n"
			}, ErrParse, "ci-aws-credentials の TF_LOCAL_PROFILE は"},
			"TF_LOCAL_PROFILE の env が入口と違う": {func(t *testing.T) string {
				return replaced(t, base, profile, "ci-plan-t-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,u,plan)\n")
			}, ErrParse, "env u が入口の env t と違います"},
			"TF_LOCAL_PROFILE の role が登録簿と違う": {func(t *testing.T) string {
				return replaced(t, base, profile, "ci-plan-t-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,t,apply)\n")
			}, ErrRole, "role apply（登録簿は plan）"},
			".PHONY が無い":            {func(t *testing.T) string { return replaced(t, base, phony, "") }, ErrParse, ".PHONY の行が 0 行です"},
			"未分類の .PHONY の ci- がある": {func(*testing.T) string { return base + ".PHONY: ci-x ## x\n" }, ErrParse, ".PHONY の ci-x は"},
			"種別の define が無い": {func(t *testing.T) string {
				return replaced(t, base, "define CI_PLAN\n", "define CI_OTHER\n")
			}, ErrParse, "define CI_PLAN が 0 個です"},
			"ルール行を経ない呼び出しの行がある": {func(*testing.T) string { return base + "x:\n" + call }, ErrParse, "呼び出しの行 2 件 / 読めた入口 1 件"},
			"本文が空": {func(*testing.T) string { return "" }, ErrParse, "入口が 1 件もありません"},
			"ルール行のコロンの後に空白がある": {func(t *testing.T) string {
				return replaced(t, both, "ci-plan-t-identity:\n", "ci-plan-t-identity: \n")
			}, ErrParse, "ci-plan-t-identity は入口でも"},
			"1行のルール行に入口が2つある": {func(t *testing.T) string {
				return replaced(t, both, "ci-plan-t-identity:\n", "ci-plan-t-identity ci-plan-t-workload:\n")
			}, ErrParse, "ci-plan-t-identity は入口でも"},
			"行末が CRLF": {func(*testing.T) string { return strings.ReplaceAll(base, "\n", "\r\n") }, ErrParse, "入口が 1 件もありません"},
			"入口の1つのルール行の行末が CRLF": {func(t *testing.T) string {
				return replaced(t, both, "ci-plan-t-identity:\n", "ci-plan-t-identity:\r\n")
			}, ErrParse, "ci-plan-t-identity は入口でも"},
			"呼び出しの行に @ が無い": {func(t *testing.T) string {
				return replaced(t, base, call, "\t$(call CI_PLAN,t,identity)\n")
			}, ErrParse, "の次の行が"},
			"呼び出しの引数のカンマの後に空白がある": {func(t *testing.T) string {
				return replaced(t, base, call, "\t@$(call CI_PLAN, t,identity)\n")
			}, ErrParse, "の次の行が"},
			"種別の define が2つある": {func(t *testing.T) string {
				return replaced(t, base, "define CI_PLAN\n", "define CI_PLAN\nendef\ndefine CI_PLAN\n")
			}, ErrParse, "define CI_PLAN が 2 個です"},
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := Parse(c.data(t))
				require.ErrorIs(t, err, c.err)
				assert.Contains(t, err.Error(), c.reason)
			})
		}
	})
}

func TestParseRepo(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		repo := t.TempDir()
		path := filepath.Join(repo, filepath.FromSlash(Makefile))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		return repo
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリの ci.mk を読む", func(t *testing.T) {
			t.Parallel()
			got, err := ParseRepo(write(t, fixture([3]string{Apply, "t", "workload"})))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "ci-apply-t-workload", got[0].Target)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := ParseRepo(filepath.Join(t.TempDir(), "missing"))
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("ci.mk が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := ParseRepo(t.TempDir())
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("ci.mk が空なら弾く", func(t *testing.T) {
			t.Parallel()
			_, err := ParseRepo(write(t, ""))
			require.ErrorIs(t, err, ErrParse)
			assert.Contains(t, err.Error(), "入口が 1 件もありません")
		})

		t.Run("登録簿に無い種別のエラーをそのまま返す", func(t *testing.T) {
			t.Parallel()
			_, err := ParseRepo(write(t, fixture([3]string{"import", "t", "identity"})))
			require.ErrorIs(t, err, ErrKindUnknown)
			assert.Contains(t, err.Error(), Makefile)
		})
	})
}

func TestCheckDeclared(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿のすべての種別の入口があれば成功する", func(t *testing.T) {
			t.Parallel()
			entries, err := Parse(fixture(allEntries("t")...))
			require.NoError(t, err)
			assert.NoError(t, CheckDeclared(entries))
		})

		// 写しと正本のずれを検出する検査は、正本の実物を読んでよい（ADR-0702 決定17）。
		t.Run("写しと正本のずれ: 実物の ci.mk が登録簿のすべての種別の入口を持つ", func(t *testing.T) {
			t.Parallel()
			entries, err := ParseRepo(repoRoot)
			require.NoError(t, err)
			assert.NoError(t, CheckDeclared(entries))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("入口の無い種別があれば、その種別を名指して弾く", func(t *testing.T) {
			t.Parallel()
			entries, err := Parse(fixture(slices.DeleteFunc(allEntries("t"), func(e [3]string) bool { return e[0] == Hold })...))
			require.NoError(t, err)
			err = CheckDeclared(entries)
			require.ErrorIs(t, err, ErrKindAbsent)
			assert.Contains(t, err.Error(), Hold)
			assert.NotContains(t, err.Error(), Plan)
		})

		t.Run("入口が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, CheckDeclared(nil), ErrKindAbsent)
		})
	})
}

func Test_newRegistry(t *testing.T) {
	t.Parallel()

	valid := func() Kind {
		return Kind{Name: "a", Lock: LockSkip, Events: []string{EventPullRequest}, IdentityRole: "a-identity", WorkloadRole: "a"}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別を渡した順で持ち、Events を複製する", func(t *testing.T) {
			t.Parallel()
			b := valid()
			b.Name, b.IdentityRole, b.WorkloadRole = "b", "b-identity", "b"
			in := []Kind{valid(), b}
			r, err := newRegistry(in...)
			require.NoError(t, err)
			in[0].Events[0] = "x"
			assert.Equal(t, registry{valid(), b}, r)
		})

		t.Run("Terraform と Lock の組み合わせは突き合わせない", func(t *testing.T) {
			t.Parallel()
			k := valid()
			k.Terraform, k.Lock = false, LockSkip
			_, err := newRegistry(k)
			assert.NoError(t, err)
		})

		t.Run("種別をまたいで同じ role を持てる", func(t *testing.T) {
			t.Parallel()
			b := valid()
			b.Name = "b"
			_, err := newRegistry(valid(), b)
			assert.NoError(t, err)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(k *Kind){
			"名前が空":                    func(k *Kind) { k.Name = "" },
			"名前が大文字":                  func(k *Kind) { k.Name = "Plan" },
			"名前が2語":                   func(k *Kind) { k.Name = "force-unlock" },
			"lock の扱いが無い":             func(k *Kind) { k.Lock = 0 },
			"lock の扱いが範囲の外":           func(k *Kind) { k.Lock = LockRemove + 1 },
			"起動のイベントが無い":              func(k *Kind) { k.Events = nil },
			"起動のイベントが重複する":            func(k *Kind) { k.Events = []string{EventPush, EventPush} },
			"起動のイベントを知らない":            func(k *Kind) { k.Events = []string{"schedule"} },
			"identity root の role が空": func(k *Kind) { k.IdentityRole = "" },
			"workload root の role が空": func(k *Kind) { k.WorkloadRole = "" },
			"role の形が違う":              func(k *Kind) { k.WorkloadRole = "A_b" },
			"role の末尾が別の role と同じ":    func(k *Kind) { k.IdentityRole = "x-a" },
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				k := valid()
				mutate(&k)
				_, err := newRegistry(k)
				require.ErrorIs(t, err, errRegistry)
			})
		}

		t.Run("種別が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := newRegistry()
			require.ErrorIs(t, err, errRegistry)
		})

		t.Run("種別をまたいで role の末尾が別の role と同じなら、渡す順を問わず弾く", func(t *testing.T) {
			t.Parallel()
			b := valid()
			b.Name, b.IdentityRole, b.WorkloadRole = "b", "b-identity", "x-a"
			for _, kinds := range [][]Kind{{valid(), b}, {b, valid()}} {
				_, err := newRegistry(kinds...)
				require.ErrorIs(t, err, errRegistry)
				assert.Contains(t, err.Error(), "の末尾が role")
			}
		})

		t.Run("名前が重複すれば弾く", func(t *testing.T) {
			t.Parallel()
			_, err := newRegistry(valid(), valid())
			require.ErrorIs(t, err, errRegistry)
			assert.Contains(t, err.Error(), "重複")
		})
	})
}

func Test_registry_checkDeclared(t *testing.T) {
	t.Parallel()

	r := registry{{Name: "a"}, {Name: "b"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべての種別に入口があれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, r.checkDeclared([]Entry{{Kind: Kind{Name: "b"}}, {Kind: Kind{Name: "a"}}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("欠けた種別を名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkDeclared([]Entry{{Kind: Kind{Name: "a"}}})
			require.ErrorIs(t, err, ErrKindAbsent)
			assert.Contains(t, err.Error(), "b")
		})

		t.Run("入口が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, r.checkDeclared(nil), ErrKindAbsent)
		})

		t.Run("登録簿が空なら、入口の有無を問わず弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, registry{}.checkDeclared(nil), errRegistry)
			require.ErrorIs(t, registry{}.checkDeclared([]Entry{{Kind: Kind{Name: "a"}}}), errRegistry)
		})
	})
}

func Test_parseRules(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("入口でないと名指したターゲットを除き、ルール行と呼び出しの行から入口を読む", func(t *testing.T) {
			t.Parallel()
			got, err := parseRules(strings.Split("ci-aws-credentials:\n\t@x\nci-unlock-t-workload:\n\t@$(call CI_UNLOCK,t,workload)\n", "\n"))
			require.NoError(t, err)
			unlock, _ := Lookup(Unlock)
			assert.Equal(t, []Entry{{Target: "ci-unlock-t-workload", Kind: unlock, Env: "t", Root: RootWorkload, Line: 3}}, got)
		})

		t.Run("ルール行が無ければ空を返す", func(t *testing.T) {
			t.Parallel()
			got, err := parseRules([]string{"all:"})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("呼び出しの無いルール行を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseRules([]string{"ci-x:", "\t@echo"})
			require.ErrorIs(t, err, ErrParse)
		})

		t.Run("登録簿に無い種別を弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseRules([]string{"ci-import-t-identity:", "\t@$(call CI_IMPORT,t,identity)"})
			require.ErrorIs(t, err, ErrKindUnknown)
		})
	})
}

func Test_checkTargets(t *testing.T) {
	t.Parallel()

	entries := func() []Entry {
		plan, _ := Lookup(Plan)
		return []Entry{{Target: "ci-plan-t-identity", Kind: plan, Env: "t", Root: RootIdentity, Line: 3}}
	}
	const phony = ".PHONY: ci-plan-t-identity ci-aws-credentials ## x"
	const profile = "ci-plan-t-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,t,plan)"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("1行の .PHONY に並んだターゲットを数え、TF_LOCAL_PROFILE の role を入口に埋める", func(t *testing.T) {
			t.Parallel()
			got := entries()
			require.NoError(t, checkTargets([]string{phony, profile, "ci-plan-t-identity:", "ci-aws-credentials: SHELL := /bin/bash"}, got))
			assert.Equal(t, "plan", got[0].Role)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run(".PHONY に無い ci- のルール行は、ルール行として弾く", func(t *testing.T) {
			t.Parallel()
			err := checkTargets([]string{phony, profile, "ci-plan-t-identity:", "ci-zzz-t-identity:"}, entries())
			require.ErrorIs(t, err, ErrParse)
			assert.Contains(t, err.Error(), "4 行目: ci-zzz-t-identity は入口でも")
			assert.NotContains(t, err.Error(), ".PHONY の")
		})

		t.Run(".PHONY が2行なら弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, checkTargets([]string{phony, phony, profile}, entries()), ErrParse)
		})

		t.Run("role が登録簿と違えば弾く", func(t *testing.T) {
			t.Parallel()
			err := checkTargets([]string{phony, "ci-plan-t-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,t,unlock)"}, entries())
			require.ErrorIs(t, err, ErrRole)
		})
	})
}

func Test_checkDefines(t *testing.T) {
	t.Parallel()

	plan, _ := Lookup(Plan)
	entries := []Entry{{Target: "ci-plan-t-identity", Kind: plan}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別の define が1つなら成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, checkDefines([]string{"define CI_PREPARE", "define CI_PLAN", "endef"}, entries))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, lines := range map[string][]string{
			"define が無い":   {"define CI_PREPARE"},
			"define が2つ在る": {"define CI_PLAN", "define CI_PLAN"},
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, checkDefines(lines, entries), ErrParse)
			})
		}
	})
}
