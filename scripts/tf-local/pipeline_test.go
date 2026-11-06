package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ciCommit = "0123456789abcdef0123456789abcdef01234567"
	ciNoop   = `{"format_version":"1.2","resource_changes":[{"address":"a.b","change":{"actions":["no-op"]}}]}`
	ciCreate = `{"format_version":"1.2","resource_changes":[{"address":"a.b","change":{"actions":["create"]}}]}`
	ciPass   = `[{"namespace":"plan.stateful_protection","successes":2,"failures":[]}]`
	ciDeny   = `[{"namespace":"plan.stateful_protection","successes":0,"failures":[{"msg":"[PLAN-STATE-01] stub"}]}]`
)

// ciFixture は、偽の terraform・aws・conftest と root・Policy の構成を t.TempDir() に置きます。偽物は引数と
// 環境を calls に記録し、終了コードと出力を同じディレクトリのファイルから取ります。
type ciFixture struct {
	dir, bin, base, plans, summary string
}

func newCIFixture(t *testing.T) ciFixture {
	t.Helper()

	dir := t.TempDir()
	f := ciFixture{dir: dir, bin: filepath.Join(dir, "bin"), base: filepath.Join(dir, "base"), plans: filepath.Join(dir, "plans"), summary: filepath.Join(dir, "summary.md")}
	root := filepath.Join(f.base, "env", "admin", "identity")
	policy := filepath.Join(dir, "policy")

	for _, d := range []string{f.bin, root, policy} {
		require.NoError(t, os.MkdirAll(d, 0o750))
	}

	files := map[string]string{
		filepath.Join(root, "backend.tf"):                 "terraform {\n  backend \"s3\" {\n    region = \"ap-northeast-1\"\n  }\n}\n",
		filepath.Join(root, "providers.tf"):               "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n",
		filepath.Join(policy, "stateful_protection.rego"): "package plan.stateful_protection\n\ndeny contains msg if {\n\tfalse\n}\n\ndeny contains msg if {\n\tfalse\n}\n",
		filepath.Join(policy, "decl.json"):                "{}\n",
		filepath.Join(f.bin, "identity"):                  "123456789012\tarn:aws:sts::123456789012:assumed-role/tabp-admin-ci-plan/gha\n",
		filepath.Join(f.bin, "plan.json"):                 ciCreate,
		filepath.Join(f.bin, "policy.json"):               ciPass,
		filepath.Join(f.bin, "plan-rc"):                   "2",
		filepath.Join(f.bin, "calls"):                     "",
		filepath.Join(f.bin, "terraform"): `#!/bin/sh
d=$(dirname "$0"); echo "terraform $* profile=$AWS_PROFILE key=${AWS_ACCESS_KEY_ID:-} cfg=${TF_CLI_CONFIG_FILE:-}" >> "$d/calls"
rc() { if [ -e "$d/$1-rc" ]; then cat "$d/$1-rc"; else echo 0; fi; }
for a in "$@"; do case "$a" in -out=*) printf plan > "${a#-out=}";; esac; done
case "$2" in
init) echo "init の全文"; exit "$(rc init)";;
output) [ "$(rc output)" = 0 ] || exit 1; printf tabp-admin-tfplan-123456789012-ap-northeast-1-an;;
plan) echo "plan の全文"; exit "$(rc plan)";;
show) [ "$(rc show)" = 0 ] || exit 1; cat "$d/plan.json";;
apply) echo "apply の全文"; exit "$(rc apply)";;
esac
`,
		filepath.Join(f.bin, "aws"): `#!/bin/sh
d=$(dirname "$0"); echo "aws $* profile=$AWS_PROFILE" >> "$d/calls"
case "$1" in
sts) [ -e "$d/sts-rc" ] && exit 1; cat "$d/identity";;
s3api) [ -e "$d/put-rc" ] && exit 1; echo version-1;;
esac
`,
		filepath.Join(f.bin, "conftest"): `#!/bin/sh
d=$(dirname "$0"); echo "conftest $*" >> "$d/calls"
cat "$d/policy.json"; [ -s "$d/policy.json" ] || exit 1; grep -q msg "$d/policy.json" && exit 1; exit 0
`,
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o700))
	}

	return f
}

func (f ciFixture) set(t *testing.T, name, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(f.bin, name), []byte(content), 0o600))
}

func (f ciFixture) args(apply bool) []string {
	args := []string{
		"-project", "tabp", "-env", "admin", "-root", "identity", "-profile", "tabp-admin-ci-plan",
		"-base", f.base, "-dir", f.plans, "-terraform", filepath.Join(f.bin, "terraform"), "-aws", filepath.Join(f.bin, "aws"),
		"-conftest", filepath.Join(f.bin, "conftest"), "-policy", filepath.Join(f.dir, "policy"), "-decl", filepath.Join(f.dir, "policy", "decl.json"),
	}
	if apply {
		args[7] = "tabp-admin-ci-apply-identity"

		return args
	}

	return append(args, "-pr", "7", "-run", "100", "-attempt", "2")
}

func (f ciFixture) deps() deps {
	return deps{
		readFile: os.ReadFile,
		environ:  []string{"PATH=" + os.Getenv("PATH"), "AWS_ACCESS_KEY_ID=leak", "TF_CLI_CONFIG_FILE=/x", "GITHUB_STEP_SUMMARY=" + f.summary},
		git: func(args ...string) (string, error) {
			if args[0] == "rev-parse" {
				return ciCommit + "\n", nil
			}

			return "", nil
		},
		stderr: &bytes.Buffer{},
	}
}

func (f ciFixture) run(t *testing.T, name string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	err := run(append([]string{name}, f.args(name == "ci-apply")...), &out, f.deps())

	return out.String(), err
}

func (f ciFixture) calls(t *testing.T, prefix string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(f.bin, "calls"))
	require.NoError(t, err)

	var out []string

	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}

	return out
}

func (f ciFixture) leftover(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(f.plans)
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func Test_runCI(t *testing.T) {
	t.Parallel()

	t.Run("plan は lock を取らずに plan し、全文を Bucket に置いて要約を出す", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		out, err := f.run(t, "ci-plan")
		require.NoError(t, err, out)

		assert.Contains(t, out, "commit: "+ciCommit)
		assert.Contains(t, out, "plan の終了コード: 2")
		assert.Contains(t, out, "1 to add")
		assert.Contains(t, out, "plan の全文の版 ID: version-1")
		assert.Contains(t, out, "規則 2 件を評価し、違反はありません")
		assert.Len(t, f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" plan -lock=false -input=false -no-color -detailed-exitcode -out="), 1)
		assert.Empty(t, f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" apply"))

		put := f.calls(t, "aws s3api put-object")
		require.Len(t, put, 1)
		assert.Contains(t, put[0], "--bucket tabp-admin-tfplan-123456789012-ap-northeast-1-an --key pr/7/100-2-admin-identity.txt")
		assert.Contains(t, put[0], "--expected-bucket-owner 123456789012")
		assert.Contains(t, out, "key=pr/7/100-2-admin-identity.txt sha256=")

		summary, err := os.ReadFile(f.summary)
		require.NoError(t, err)
		assert.Contains(t, string(summary), "### admin-identity\n\nPlan: 0 to import, 1 to add")
		assert.Contains(t, string(summary), "- 版 ID: `version-1`")
		assert.Equal(t, []string{"admin-identity.json"}, f.leftover(t), "全文と plan のファイルを残さない")
	})

	t.Run("apply は lock を取って plan し直し、plan した plan を apply する", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		out, err := f.run(t, "ci-apply")
		require.NoError(t, err, out)

		plan := f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" plan ")
		require.Len(t, plan, 1)
		assert.NotContains(t, plan[0], "-lock=false")
		assert.Equal(t, []string{"terraform -chdir=" + filepath.Join(f.base, "env", "admin", "identity") + " apply -input=false -no-color " +
			filepath.Join(f.plans, "admin-identity.tfplan") + " profile=tabp-admin-ci-apply-identity key= cfg=/dev/null"},
			f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" apply"))
		assert.Empty(t, f.calls(t, "aws s3api"), "apply の全文はランナーの外に出さない")
		assert.Contains(t, out, "apply の終了コード: 0")

		summary, err := os.ReadFile(f.summary)
		require.NoError(t, err)
		assert.Equal(t, "### admin-identity\n\nPlan: 0 to import, 1 to add, 0 to change, 0 to destroy.\n\napply の終了コード: 0\n", string(summary))
		assert.Equal(t, []string{"admin-identity.json"}, f.leftover(t))
	})

	t.Run("変更の無い plan も apply する", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "plan-rc", "0")
		f.set(t, "plan.json", ciNoop)
		out, err := f.run(t, "ci-apply")
		require.NoError(t, err, out)
		assert.Contains(t, out, "No changes.")
		assert.Len(t, f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" apply"), 1)
	})

	t.Run("道具は CI の profile だけで、外す変数と CLI の設定を渡さずに起動する", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		_, err := f.run(t, "ci-apply")
		require.NoError(t, err)

		for _, c := range f.calls(t, "terraform ") {
			assert.True(t, strings.HasSuffix(c, " profile=tabp-admin-ci-apply-identity key= cfg=/dev/null"), c)
		}

		for _, c := range f.calls(t, "aws ") {
			assert.True(t, strings.HasSuffix(c, " profile=tabp-admin-ci-apply-identity"), c)
		}
	})

	failures := map[string]struct {
		name    string
		knobs   map[string]string
		want    error
		applied int
		put     int
	}{
		"apply: init が落ちれば plan もしない":         {"ci-apply", map[string]string{"init-rc": "1"}, errInit, 0, 0},
		"apply: plan が落ちれば apply しない":         {"ci-apply", map[string]string{"plan-rc": "1"}, errPlanFailed, 0, 0},
		"apply: JSON にできなければ apply しない":       {"ci-apply", map[string]string{"show-rc": "1"}, errShow, 0, 0},
		"apply: 終了コード 0 で変更があれば apply しない":    {"ci-apply", map[string]string{"plan-rc": "0"}, errPlanMismatch, 0, 0},
		"apply: 終了コード 2 で変更が無ければ apply しない":   {"ci-apply", map[string]string{"plan.json": ciNoop}, errPlanMismatch, 0, 0},
		"apply: Policy の違反があれば apply しない":     {"ci-apply", map[string]string{"policy.json": ciDeny}, errPolicy, 0, 0},
		"apply: Policy が評価されなければ apply しない":   {"ci-apply", map[string]string{"policy.json": "[]"}, errPolicy, 0, 0},
		"apply: conftest が何も出さなければ apply しない": {"ci-apply", map[string]string{"policy.json": ""}, errPolicy, 0, 0},
		"apply: apply が落ちれば失敗する":              {"ci-apply", map[string]string{"apply-rc": "1"}, errApply, 1, 0},
		"apply: アカウントを得られなければ init しない":       {"ci-apply", map[string]string{"sts-rc": "1"}, errIdentity, 0, 0},
		"apply: root の資格情報では init しない":        {"ci-apply", map[string]string{"identity": "123456789012\tarn:aws:iam::123456789012:root\n"}, errRootCredentials, 0, 0},
		"plan: Bucket の名前を読めなければ plan しない":    {"ci-plan", map[string]string{"output-rc": "1"}, errPlanBucket, 0, 0},
		"plan: plan が落ちても全文を置いてから失敗する":        {"ci-plan", map[string]string{"plan-rc": "1"}, errPlanFailed, 0, 1},
		"plan: JSON にできなければ全文を置いてから失敗する":      {"ci-plan", map[string]string{"show-rc": "1"}, errPlanFailed, 0, 1},
		"plan: 全文を置けなければ失敗する":                 {"ci-plan", map[string]string{"put-rc": "1"}, errPutObject, 0, 1},
		"plan: Policy の違反は全文を置いてから失敗する":       {"ci-plan", map[string]string{"policy.json": ciDeny}, errPolicy, 0, 1},
	}
	for name, c := range failures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newCIFixture(t)
			for k, v := range c.knobs {
				f.set(t, k, v)
			}

			out, err := f.run(t, c.name)
			require.ErrorIs(t, err, c.want, out)
			assert.Len(t, f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" apply"), c.applied)
			assert.Len(t, f.calls(t, "aws s3api put-object"), c.put)
			assert.NoFileExists(t, f.summary, "失敗した run は要約を書かない")
		})
	}

	t.Run("Policy の違反の文をログに出す", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "policy.json", ciDeny)
		out, err := f.run(t, "ci-apply")
		require.ErrorIs(t, err, errPolicy)
		assert.Contains(t, out, "[PLAN-STATE-01] stub")
	})

	t.Run("init の失敗では plan を呼ばない", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "init-rc", "1")
		_, err := f.run(t, "ci-plan")
		require.ErrorIs(t, err, errInit)
		assert.Empty(t, f.calls(t, "terraform -chdir="+filepath.Join(f.base, "env", "admin", "identity")+" plan"))
	})

	t.Run("plan の全文の SHA-256 は置いたファイルの中身のもの", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		// put-object の --body を残すため、偽の aws に写させる。
		require.NoError(t, os.WriteFile(filepath.Join(f.bin, "aws"), []byte(`#!/bin/sh
d=$(dirname "$0")
case "$1" in
sts) cat "$d/identity";;
s3api) while [ $# -gt 0 ]; do [ "$1" = --body ] && cp "$2" "$d/body"; shift; done; echo version-1;;
esac
`), 0o700))
		out, err := f.run(t, "ci-plan")
		require.NoError(t, err, out)

		body, err := os.ReadFile(filepath.Join(f.bin, "body"))
		require.NoError(t, err)
		sum := sha256.Sum256(body)
		assert.Contains(t, out, "sha256="+base64.StdEncoding.EncodeToString(sum[:]))
		assert.True(t, strings.HasPrefix(string(body), "commit: "+ciCommit+"\n"), string(body))
		assert.Contains(t, string(body), "plan の全文")
	})

	t.Run("道具を起動できなければ失敗する", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		args := f.args(true)
		args[13] = filepath.Join(f.dir, "missing-terraform")
		err := run(append([]string{"ci-apply"}, args...), &bytes.Buffer{}, f.deps())
		require.ErrorIs(t, err, errInit)
	})
}

func Test_parseCI(t *testing.T) {
	t.Parallel()

	f := ciFixture{base: "b", plans: "p", bin: "/bin", dir: "/d"}

	t.Run("plan と apply の設定を読む", func(t *testing.T) {
		t.Parallel()

		c, err := parseCI("ci-plan", f.args(false), false)
		require.NoError(t, err)
		assert.Equal(t, "7", c.pr)
		assert.Equal(t, "identity", c.root)

		c, err = parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)
		assert.Equal(t, "tabp-admin-ci-apply-identity", c.profile)
	})

	cases := map[string]struct {
		args  []string
		apply bool
	}{
		"plan に -pr が無ければ失敗する":                 {f.args(true), false},
		"apply に -pr があれば失敗する":                 {f.args(false), true},
		"位置引数があれば失敗する":                         {append(f.args(true), "x"), true},
		"root が identity か workload でなければ失敗する": {replaceArg(f.args(true), "-root", "other"), true},
		"env が token の形でなければ失敗する":              {replaceArg(f.args(true), "-env", "Admin"), true},
		"必須の値が空なら失敗する":                         {replaceArg(f.args(true), "-conftest", ""), true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := parseCI("ci", c.args, c.apply)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func replaceArg(args []string, flag, value string) []string {
	out := append([]string{}, args...)
	for i := range out {
		if out[i] == flag {
			out[i+1] = value
		}
	}

	return out
}

func Test_ciPrepare(t *testing.T) {
	t.Parallel()

	t.Run("前の run の作業ファイルを消し、所有者だけが読める全文を作る", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		require.NoError(t, os.MkdirAll(f.plans, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(f.plans, "admin-identity.json"), []byte("old"), 0o600))
		cfg, err := parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)

		var out bytes.Buffer
		r := &ciRun{cfg: cfg, d: f.deps(), out: &out, name: "admin-identity", rootDir: filepath.Join(f.base, "env", "admin", "identity")}
		require.NoError(t, ciPrepare(r))
		assert.NoFileExists(t, r.json)

		info, err := os.Stat(r.log)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		assert.Equal(t, "123456789012", r.account)
		assert.Equal(t, "ap-northeast-1", r.region)
		assert.Len(t, f.calls(t, "terraform -chdir="+r.rootDir+" init -input=false -no-color -lockfile=readonly -backend-config=bucket=tabp-admin-tfstate-123456789012-ap-northeast-1-an"), 1)
	})

	t.Run("git が commit を答えなければ init しない", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		cfg, err := parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)

		d := f.deps()
		d.git = func(...string) (string, error) { return "", errGit }
		r := &ciRun{cfg: cfg, d: d, out: &bytes.Buffer{}, name: "admin-identity", rootDir: filepath.Join(f.base, "env", "admin", "identity")}
		require.ErrorIs(t, ciPrepare(r), errGit)
		assert.Empty(t, f.calls(t, "terraform"))
	})
}

func Test_ciPlan(t *testing.T) {
	t.Parallel()

	t.Run("plan の失敗でも全文の key と版 ID を出す", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "plan-rc", "1")
		out, err := f.run(t, "ci-plan")
		require.ErrorIs(t, err, errPlanFailed)
		assert.Contains(t, out, "plan の全文の版 ID: version-1")
		assert.Empty(t, f.calls(t, "conftest"), "失敗した plan に Policy を掛けない")
	})
}

func Test_ciApply(t *testing.T) {
	t.Parallel()

	t.Run("Policy を通る前に apply を呼ばない", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		_, err := f.run(t, "ci-apply")
		require.NoError(t, err)

		raw, err := os.ReadFile(filepath.Join(f.bin, "calls"))
		require.NoError(t, err)
		assert.Less(t, strings.Index(string(raw), "conftest test"), strings.Index(string(raw), " apply -input=false"))
	})
}

func Test_ciTerraformPlan(t *testing.T) {
	t.Parallel()

	t.Run("終了コードを出して返す", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "plan-rc", "2")
		cfg, err := parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)

		var out bytes.Buffer
		r := &ciRun{cfg: cfg, d: f.deps(), out: &out, name: "admin-identity", rootDir: "r", log: filepath.Join(f.dir, "log"), plan: filepath.Join(f.dir, "p")}
		rc, err := ciTerraformPlan(r, "-lock=false")
		require.NoError(t, err)
		assert.Equal(t, 2, rc)
		assert.Equal(t, "plan の終了コード: 2\n", out.String())
		assert.Len(t, f.calls(t, "terraform -chdir=r plan -lock=false -input=false -no-color -detailed-exitcode -out="+r.plan), 1)
	})
}

func Test_ciShow(t *testing.T) {
	t.Parallel()

	t.Run("show の出力を JSON のファイルに書き、失敗なら errShow", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		cfg, err := parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)

		r := &ciRun{cfg: cfg, d: f.deps(), out: &bytes.Buffer{}, rootDir: "r", log: filepath.Join(f.dir, "log"), json: filepath.Join(f.dir, "j"), plan: "p"}
		require.NoError(t, ciShow(r))

		got, err := os.ReadFile(r.json)
		require.NoError(t, err)
		assert.Equal(t, ciCreate, string(got))

		f.set(t, "show-rc", "1")
		require.ErrorIs(t, ciShow(r), errShow)
	})
}

func Test_ciPolicy(t *testing.T) {
	t.Parallel()

	t.Run("宣言の規則の数で conftest の結果を判定する", func(t *testing.T) {
		t.Parallel()

		f := newCIFixture(t)
		f.set(t, "policy.json", `[{"namespace":"plan.stateful_protection","successes":1,"failures":[]}]`)
		cfg, err := parseCI("ci-apply", f.args(true), true)
		require.NoError(t, err)

		json := filepath.Join(f.dir, "j")
		require.NoError(t, os.WriteFile(json, []byte(ciCreate), 0o600))

		var out bytes.Buffer
		r := &ciRun{cfg: cfg, d: f.deps(), out: &out, json: json}
		_, err = ciPolicy(r, 2)
		require.ErrorIs(t, err, errPolicy)
		assert.Contains(t, out.String(), "評価 1 件、宣言 2 件")
	})
}

func Test_countRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		rego string
		want int
	}{
		"行頭の deny contains を数える": {"deny contains msg if {\n}\ndeny contains msg if {\n}\n", 2},
		"字下げしたものは数えない":           {"  deny contains msg if {\n", 0},
		"規則が無ければ 0":              {"package x\n", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, countRules([]byte(c.rego)))
		})
	}
}

func Test_ciTool(t *testing.T) {
	t.Parallel()

	f := newCIFixture(t)
	cfg, err := parseCI("ci-apply", f.args(true), true)
	require.NoError(t, err)

	r := &ciRun{cfg: cfg, d: f.deps()}

	t.Run("終了コードを返す", func(t *testing.T) {
		t.Parallel()

		rc, err := ciTool(r, "/bin/sh", []string{"-c", "exit 3"}, &bytes.Buffer{}, &bytes.Buffer{})
		require.NoError(t, err)
		assert.Equal(t, 3, rc)
	})

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()

		_, err := ciTool(r, filepath.Join(t.TempDir(), "missing"), nil, &bytes.Buffer{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errTool)
	})
}

func Test_childEnv(t *testing.T) {
	t.Parallel()

	t.Run("外す変数を除き、profile と固定の値を足す", func(t *testing.T) {
		t.Parallel()

		got := childEnv([]string{"HOME=/h", "AWS_PROFILE=other", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=t", "TF_LOG=trace"}, "p")
		assert.Equal(t, []string{"HOME=/h", "AWS_PROFILE=p", "TF_CLI_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1"}, got)
	})
}

func Test_withLog(t *testing.T) {
	t.Parallel()

	t.Run("全文のファイルへ足し、関数の失敗を返す", func(t *testing.T) {
		t.Parallel()

		r := &ciRun{log: filepath.Join(t.TempDir(), "log")}
		require.NoError(t, withLog(r, func(w io.Writer) error { _, err := w.Write([]byte("a")); return err }))
		require.NoError(t, withLog(r, func(w io.Writer) error { _, err := w.Write([]byte("b")); return err }))
		require.ErrorIs(t, withLog(r, func(io.Writer) error { return errShow }), errShow)

		got, err := os.ReadFile(r.log)
		require.NoError(t, err)
		assert.Equal(t, "ab", string(got))
	})

	t.Run("開けなければ errTool", func(t *testing.T) {
		t.Parallel()

		r := &ciRun{log: filepath.Join(t.TempDir(), "missing", "log")}
		require.ErrorIs(t, withLog(r, func(io.Writer) error { return nil }), errTool)
	})
}

func Test_ciRemove(t *testing.T) {
	t.Parallel()

	t.Run("在るものを消し、無いものは無視する", func(t *testing.T) {
		t.Parallel()

		p := filepath.Join(t.TempDir(), "x")
		require.NoError(t, os.WriteFile(p, nil, 0o600))
		require.NoError(t, ciRemove(p, p+"-missing"))
		assert.NoFileExists(t, p)
	})
}

func Test_ciStepSummary(t *testing.T) {
	t.Parallel()

	t.Run("GITHUB_STEP_SUMMARY が在れば足し、無ければ何もしない", func(t *testing.T) {
		t.Parallel()

		p := filepath.Join(t.TempDir(), "s")
		require.NoError(t, ciStepSummary(&ciRun{d: deps{environ: []string{"GITHUB_STEP_SUMMARY=" + p}}}, "a"))
		require.NoError(t, ciStepSummary(&ciRun{d: deps{environ: []string{"GITHUB_STEP_SUMMARY=" + p}}}, "b"))
		require.NoError(t, ciStepSummary(&ciRun{d: deps{environ: nil}}, "c"))

		got, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "ab", string(got))
	})
}

func Test_unset(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"AWS_PROFILE": true, "TF_VAR_x": true, "ACTIONS_ID_TOKEN_REQUEST_URL": true,
		"HOME": false, "TF_PLUGIN_CACHE_DIR": false, "ACTIONS_ID_TOKEN_REQUEST_URLX": false,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, unset(name))
		})
	}
}
