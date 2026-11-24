package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	planSecret  = "PLAN-SECRET"
	applySecret = "APPLY-SECRET"
	onePolicy   = "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n"
)

// fakeFlow は、CI の段の偽の道具を作る。起動ごとに「段 作業ディレクトリの名前 profile と TF_CLI_ARGS_*」を
// calls.log へ書く。段ごとに <段>.out・<段>.stderr・<段>.rc を置けばその振る舞いになる。plan は TF_CLI_ARGS_plan の
// 置き場に plan のファイルを書き、put-object は input.json と body.txt を控える。apply.sleep を置けば、apply は
// SIGINT を受けるまで待つ。偽の道具は /bin/sh に読ませて起動する（書いたばかりのファイルの exec は ETXTBSY で揺れる）。
func fakeFlow(t *testing.T, knobs map[string]string) (toolFunc, string) {
	t.Helper()
	k := t.TempDir()
	defaults := map[string]string{
		stepCallerIdentity + ".out":       callerIdentity,
		stepListAliases + ".out":          withAlias,
		stepBucketEncrypt + ".out":        encKMS(stateKeyARN),
		stepCIPlanNoLock + ".out":         planSecret + "\n",
		stepCIPlanNoLock + ".rc":          "2",
		stepCIPlanLock + ".out":           planSecret + "\n",
		stepCIPlanLock + ".rc":            "2",
		stepCIShow + ".out":               planWith(change("a.b", `"create"`), ""),
		stepCIConftest + ".out":           `[{"namespace":"plan.p","successes":1}]`,
		stepCIApply + ".out":              applySecret + "\n",
		stepCIPutObject + ".out":          `{"VersionId":"v1","ETag":"e"}`,
		stepCIInit + ".out":               "init\n",
		"sts-caller-identity.placeholder": "",
	}
	for name, body := range knobs {
		defaults[name] = body
	}
	for name, body := range defaults {
		if body == "\x00" {
			continue
		}
		require.NoError(t, os.WriteFile(filepath.Join(k, name), []byte(body), 0o600))
	}
	script := filepath.Join(k, "tool")
	body := `#!/bin/sh
step="$1"; k='` + k + `'
echo "$step $(basename "$PWD") profile=${AWS_PROFILE:-} region=${AWS_REGION:-} init=${TF_CLI_ARGS_init:-} plan=${TF_CLI_ARGS_plan:-} show=${TF_CLI_ARGS_show:-} apply=${TF_CLI_ARGS_apply:-}" >> "$k/calls.log"
case "$step" in
s3api-put-object) cat input.json >> "$k/upload.log"; echo >> "$k/upload.log"; cp body.txt "$k/body.txt" ;;
s3api-get-bucket-encryption) cat input.json >> "$k/encryption.log" ;;
ci-terraform-plan*) out="${TF_CLI_ARGS_plan#-out=}"; echo tfplan > "$out" ;;
ci-conftest) ls -R > "$k/conftest.ls" ;;
ci-terraform-apply) if [ -e "$k/apply.sleep" ]; then trap 'echo interrupted; exit 1' INT; while :; do sleep 0.05; done; fi ;;
esac
if [ -e "$k/$step.out" ]; then cat "$k/$step.out"; fi
if [ -e "$k/$step.stderr" ]; then cat "$k/$step.stderr" >&2; fi
rc=0; if [ -e "$k/$step.rc" ]; then rc=$(cat "$k/$step.rc"); fi
exit "$rc"
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	tool := func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), "/bin/sh", script, step)
		c.Dir, c.Env = d, env
		return c
	}
	return tool, k
}

func readKnob(t *testing.T, k, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(k, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(raw)
}

// flow は、make の入口と同じ順で段を起動する。prepare と init が落ちればそこで止まり、それより後の段は
// 落ちても続ける（make の `-`）。
type flow struct {
	t       *testing.T
	d       deps
	k       string
	base    string
	state   string
	header  string
	policy  string
	output  string
	summary string
	stdout  bytes.Buffer
	errs    map[string]error
}

func newFlow(t *testing.T, knobs map[string]string, extra ...string) *flow {
	t.Helper()
	tool, k := fakeFlow(t, knobs)
	base := localBase(t)
	require.NoError(t, os.WriteFile(filepath.Join(base, "env", "admin", "backend.tf"), []byte(backendOK), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "env", "admin", "providers.tf"), []byte(providersOK), 0o600))
	work := t.TempDir()
	f := &flow{t: t, k: k, base: base, state: filepath.Join(work, "ci-plan", "admin-identity.json"),
		header: filepath.Join(work, "header"), policy: policyDirWith(t, map[string]string{"p.rego": onePolicy}),
		output: filepath.Join(work, "output"), summary: filepath.Join(work, "summary"), errs: map[string]error{}}
	require.NoError(t, os.WriteFile(f.header, []byte("commit: "+commitSHA+"\n"), 0o600))
	environ := []string{"PATH=" + os.Getenv("PATH"), "GITHUB_RUN_ID=8", "GITHUB_RUN_ATTEMPT=1", "CI_PR=12",
		"GITHUB_OUTPUT=" + f.output, "GITHUB_STEP_SUMMARY=" + f.summary, "AWS_ACCESS_KEY_ID=outer", "TF_CLI_ARGS_plan=-target=x"}
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		environ = slicesDeletePrefix(environ, name+"=")
		environ = append(environ, kv)
	}
	var stderr bytes.Buffer
	f.d = deps{readFile: os.ReadFile, environ: environ, git: stubGit("", ""), now: time.Now, tool: tool, stderr: &stderr}
	return f
}

func slicesDeletePrefix(env []string, prefix string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

func (f *flow) step(name string, args ...string) error {
	err := run(append([]string{name}, args...), &f.stdout, f.d)
	f.errs[name] = err
	return err
}

// all は、mode の入口の段を root（identity か workload）について順に起動し、finish の結果を返す。
func (f *flow) all(mode, root string) error {
	f.state = filepath.Join(filepath.Dir(f.state), "admin-"+root+".json")
	if err := f.step("ci-prepare", "-mode", mode, "-project", "tabp", "-env", "admin", "-root", root,
		"-profile", "tabp-admin-ci-"+mode, "-base", f.base, "-header", f.header, "-state", f.state); err != nil {
		return err
	}
	if err := f.step("ci-init", "-state", f.state); err != nil {
		return err
	}
	_ = f.step("ci-plan", "-state", f.state)
	_ = f.step("ci-policy", "-state", f.state, "-work", f.t.TempDir(), "-policy", f.policy, "-decl", fileWith(f.t, "{}"), "-layer", fileWith(f.t, "{}"), "-namespace", "plan.p")
	if mode == ciModeApply {
		_ = f.step("ci-apply", "-state", f.state, "-timeout", "5s")
	}
	_ = f.step("ci-upload", "-state", f.state)

	return f.step("ci-finish", "-state", f.state)
}

func (f *flow) calls() string { return readKnob(f.t, f.k, "calls.log") }

// public は、公開されるログに出ないはずの値が出ていないことを見る。
func (f *flow) public() {
	f.t.Helper()
	for _, v := range []string{planSecret, applySecret, account} {
		assert.NotContains(f.t, f.stdout.String(), v)
		for _, err := range f.errs {
			if err != nil {
				assert.NotContains(f.t, err.Error(), v)
			}
		}
	}
}

func (f *flow) leftovers() {
	f.t.Helper()
	out, jsonPath, tfplan := ciStatePaths(f.state)
	for _, p := range []string{out, jsonPath, tfplan} {
		assert.NoFileExists(f.t, p, "アカウント ID を含むファイルが残っています")
	}
}

func Test_ciFinishStep(t *testing.T) {
	t.Parallel()

	t.Run("plan の入口は lock を取らずに plan し、Policy を掛け、apply せず、全文を plan の出力の Bucket に置く", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModePlan, "identity"))
		calls := f.calls()
		assert.Contains(t, calls, stepCIPlanNoLock+" identity profile=tabp-admin-ci-plan region= init= plan=-out=")
		assert.NotContains(t, calls, stepCIPlanLock+" ")
		assert.NotContains(t, calls, stepCIApply)
		assert.Contains(t, calls, stepCIInit+" identity profile=tabp-admin-ci-plan region= init=-backend-config=bucket=tabp-admin-tfstate-"+account+"-ap-northeast-1-an -backend-config=kms_key_id="+stateKeyARN+" plan= ")
		assert.JSONEq(t, `{"Bucket":"tabp-admin-tfplan-`+account+`-ap-northeast-1-an","Key":"pr/12/8-1-admin-identity.txt","ChecksumSHA256":"`+mustSHA(t, readKnob(t, f.k, "body.txt"))+`","ExpectedBucketOwner":"`+account+`"}`, strings.TrimSpace(readKnob(t, f.k, "upload.log")))
		body := readKnob(t, f.k, "body.txt")
		assert.True(t, strings.HasPrefix(body, "commit: "+commitSHA+"\n"), body)
		assert.Contains(t, body, "init\n"+planSecret+"\n")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "### admin-identity\n\nPlan: 0 to import, 1 to add, 0 to change, 0 to destroy.\n")
		assert.Empty(t, readKnob(t, filepath.Dir(f.output), "output"))
		assert.Contains(t, f.stdout.String(), "plan の終了コード: 2\n")
		assert.Contains(t, f.stdout.String(), "✅ plan.p: 規則 1 件を評価し、違反はありません")
		f.public()
		f.leftovers()
	})

	t.Run("apply の入口は lock を取って plan し直し、Policy を通った plan を apply し、outputs に要約を書く", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModeApply, "identity"))
		calls := f.calls()
		assert.Contains(t, calls, stepCIPlanLock+" identity profile=tabp-admin-ci-apply")
		assert.NotContains(t, calls, stepCIPlanNoLock)
		assert.Contains(t, calls, stepCIApply+" identity profile=tabp-admin-ci-apply region= init= plan= show= apply=-input=false -no-color /")
		assert.Less(t, strings.Index(calls, stepCIConftest), strings.Index(calls, stepCIApply))
		assert.Contains(t, readKnob(t, f.k, "upload.log"), `"Bucket":"tabp-admin-tfplan-`)
		assert.Contains(t, readKnob(t, f.k, "upload.log"), `"Key":"apply/8-1-admin-identity.txt"`)
		assert.Contains(t, readKnob(t, f.k, "body.txt"), applySecret)
		assert.Equal(t, "plan=Plan: 0 to import, 1 to add, 0 to change, 0 to destroy.\n", readKnob(t, filepath.Dir(f.output), "output"))
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "apply が終わりました")
		f.public()
		f.leftovers()
	})

	t.Run("workload root の apply の全文は apply の出力の Bucket に置く", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModeApply, "workload"))
		assert.Contains(t, f.calls(), stepCIApply+" admin profile=")
		assert.Contains(t, readKnob(t, f.k, "upload.log"), `"Bucket":"tabp-admin-tfapply-`)
		assert.Contains(t, readKnob(t, f.k, "upload.log"), `"Key":"apply/8-1-admin-workload.txt"`)
	})

	for name, c := range map[string]struct {
		knobs   map[string]string
		failure string
		apply   bool
		upload  bool
	}{
		"plan が落ちれば Policy も apply もせず、全文を置いて plan で落ちる": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "1"}, failure: "plan", upload: true},
		"plan が lock を取れなければ lock で落ちる": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "1", stepCIPlanLock + ".out": "Error: " + lockError + "\n"}, failure: "lock", upload: true},
		"plan の JSON の変更と終了コードが食い違えば plan で落ちる": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "0"}, failure: "plan", upload: true},
		"Policy を通らなければ apply せず、全文を置いて policy で落ちる": {
			knobs: map[string]string{stepCIConftest + ".out": `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"bad"}]}]`}, failure: "policy", upload: true},
		"apply が落ちれば全文を置いて apply で落ちる": {
			knobs: map[string]string{stepCIApply + ".rc": "1"}, failure: "apply", apply: true, upload: true},
		"apply が lock を取れなければ lock で落ちる": {
			knobs: map[string]string{stepCIApply + ".rc": "1", stepCIApply + ".out": lockError + "\n"}, failure: "lock", apply: true, upload: true},
		"apply が終わった後に全文を置けなければ upload で落ちる": {
			knobs: map[string]string{stepCIPutObject + ".rc": "1"}, failure: "upload", apply: true, upload: true},
		"版 ID の無い応答は置けなかったとして落ちる": {
			knobs: map[string]string{stepCIPutObject + ".out": `{}`}, failure: "upload", apply: true, upload: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs)
			err := f.all(ciModeApply, "identity")
			require.Error(t, err)
			assert.Equal(t, "failure="+c.failure+"\n", readKnob(t, filepath.Dir(f.output), "output"))
			assert.Equal(t, c.apply, strings.Contains(f.calls(), stepCIApply), f.calls())
			assert.Equal(t, c.upload, readKnob(t, f.k, "upload.log") != "")
			assert.NotContains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "apply が終わりました")
			f.public()
			f.leftovers()
		})
	}

	t.Run("plan の入口の失敗は outputs に書かずに落ちる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIPlanNoLock + ".rc": "1"})
		require.ErrorIs(t, f.all(ciModePlan, "identity"), errCIFailed)
		assert.Empty(t, readKnob(t, filepath.Dir(f.output), "output"))
		assert.NotEmpty(t, readKnob(t, f.k, "upload.log"))
	})

	t.Run("apply が終わった後に job summary へ書けなければ applied で落ちる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, os.MkdirAll(f.summary, 0o700))
		require.ErrorIs(t, f.all(ciModeApply, "identity"), errCIOutput)
		assert.Contains(t, readKnob(t, filepath.Dir(f.output), "output"), "failure=applied\n")
	})

	t.Run("段が state に終わりを書かずに落ちていれば、その段で落ちる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModeApply, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		require.ErrorIs(t, f.step("ci-finish", "-state", f.state), errCIUnfinish)
		assert.Equal(t, "failure=plan\n", readKnob(t, filepath.Dir(f.output), "output"))
		f.leftovers()
	})

	t.Run("state が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := ciFinishStep([]string{"-state", filepath.Join(t.TempDir(), "none.json")}, testDeps(nil))
		require.ErrorIs(t, err, errCIState)
	})
}

func mustSHA(t *testing.T, body string) string {
	t.Helper()
	p := fileWith(t, body)
	sha, err := fileSHA256(p, os.ReadFile)
	require.NoError(t, err)
	return sha
}

func Test_ciPrepareStep(t *testing.T) {
	t.Parallel()

	prepare := func(f *flow, extra ...string) error {
		return f.step("ci-prepare", append([]string{"-mode", ciModeApply, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state}, extra...)...)
	}

	t.Run("全文の1行目を書いて端末に出し、state を所有者だけが読める権限で書く", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, prepare(f))
		assert.True(t, strings.HasPrefix(f.stdout.String(), "commit: "+commitSHA+"\n"))
		info, err := os.Stat(f.state)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		s, err := loadCIState(f.state)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(s.TFPlan), "terraform は root で起動するので絶対パスで渡す")
		assert.JSONEq(t, `{"Bucket":"tabp-admin-tfstate-`+account+`-ap-northeast-1-an","ExpectedBucketOwner":"`+account+`"}`, readKnob(t, f.k, "encryption.log"))
		assert.NotContains(t, f.calls(), "profile=outer")
	})

	for name, c := range map[string]struct {
		knobs map[string]string
		extra []string
		calls bool
	}{
		"呼び出し元を取れなければ":      {knobs: map[string]string{stepCallerIdentity + ".rc": "1"}, calls: true},
		"state の鍵を決められなければ": {knobs: map[string]string{stepListAliases + ".out": withoutAlias}, calls: true},
		"run の番号が無ければ":      {extra: []string{"GITHUB_RUN_ID="}},
	} {
		t.Run(name+"、state を書かずに failure に prepare を書く", func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs, c.extra...)
			require.Error(t, prepare(f))
			assert.NoFileExists(t, f.state)
			assert.Equal(t, "failure=prepare\n", readKnob(t, filepath.Dir(f.output), "output"))
			assert.Equal(t, c.calls, f.calls() != "")
			f.leftovers()
		})
	}

	t.Run("1行目が空なら aws を起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, os.WriteFile(f.header, nil, 0o600))
		require.ErrorIs(t, prepare(f), errCIHeader)
		assert.Empty(t, f.calls())
	})

	t.Run("plan の入口は失敗しても outputs に書かない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCallerIdentity + ".rc": "1"})
		require.Error(t, prepare(f, "-mode", ciModePlan))
		assert.Empty(t, readKnob(t, filepath.Dir(f.output), "output"))
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.ErrorIs(t, prepare(f, "-mode", "x"), errUsage)
		require.ErrorIs(t, f.step("ci-prepare", "-x"), errUsage)
	})
}

func Test_ciPrepareState(t *testing.T) {
	t.Parallel()

	t.Run("形の違う root は失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		_, err := ciPrepareState(f.d, &bytes.Buffer{}, ciModePlan, "tabp", "admin", "x", "p", f.base, f.header, f.state)
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_ciInitStep(t *testing.T) {
	t.Parallel()

	t.Run("init が落ちれば、全文を消して failure に prepare を書き、全文はランナーの外に出さない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIInit + ".rc": "1", stepCIInit + ".out": planSecret})
		require.ErrorIs(t, f.all(ciModeApply, "identity"), errCIInit)
		assert.NotContains(t, f.calls(), stepCIPlanLock)
		assert.Empty(t, readKnob(t, f.k, "upload.log"))
		assert.Equal(t, "failure=prepare\n", readKnob(t, filepath.Dir(f.output), "output"))
		f.public()
		f.leftovers()
	})

	t.Run("state を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := ciInitStep([]string{"-state", fileWith(t, "{}")}, testDeps(nil))
		require.ErrorIs(t, err, errCIState)
	})
}

func Test_ciRunInit(t *testing.T) {
	t.Parallel()

	t.Run("prepare が終わっていなければ terraform を起動しない", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		require.ErrorIs(t, ciRunInit(ciState{Mode: ciModePlan, Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: tool}), errCIState)
		assert.Empty(t, readKnob(t, k, "calls.log"))
	})
}

func Test_ciPlanStep(t *testing.T) {
	t.Parallel()

	t.Run("init が終わっていなければ plan しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-plan", "-state", f.state))
		assert.NotContains(t, f.calls(), "ci-terraform-plan")
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		_, err := ciPlanStep([]string{"x"}, testDeps(nil), &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_ciRunPlan(t *testing.T) {
	t.Parallel()

	t.Run("呼び出し元の TF_CLI_ARGS_plan を渡さず、plan のファイルの置き場だけを渡す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModePlan, "identity"))
		assert.NotContains(t, f.calls(), "-target=x")
	})
}

func Test_ciPolicyStep(t *testing.T) {
	t.Parallel()

	t.Run("名前空間が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		_, err := ciPolicyStep([]string{"-state", "s", "-work", "w", "-policy", "p", "-decl", "d", "-layer", "l"}, testDeps(nil), &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_ciRunPolicy(t *testing.T) {
	t.Parallel()

	t.Run("Policy の木・宣言・plan を作業ディレクトリへ写して conftest を起動する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModePlan, "identity"))
		ls := readKnob(t, f.k, "conftest.ls")
		for _, name := range []string{"rego", "decl.json", "layer.json", "plan.json", "p.rego"} {
			assert.Contains(t, ls, name)
		}
	})

	for name, c := range map[string]struct {
		files map[string]string
		ns    string
		out   string
		want  error
	}{
		"名前空間の rego が無ければ失敗する":       {files: map[string]string{"p.rego": onePolicy}, ns: "plan.q", out: "[]", want: errPolicyRules},
		"rego に規則が無ければ失敗する":          {files: map[string]string{"p.rego": "package plan.p\n"}, ns: "plan.p", out: "[]", want: errPolicyRules},
		"conftest の結果を読めなければ失敗する":    {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: "x", want: errPolicyJSON},
		"評価の件数が規則の数と違えば Policy で落ちる": {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: `[{"namespace":"plan.p","successes":0}]`, want: errCIPolicy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, _ := fakeFlow(t, map[string]string{stepCIConftest + ".out": c.out})
			s := ciState{Out: filepath.Join(t.TempDir(), "o.txt"), JSON: fileWith(t, "{}")}
			err := ciRunPolicy(s, deps{tool: tool, stderr: &bytes.Buffer{}}, &bytes.Buffer{}, t.TempDir(), policyDirWith(t, c.files), fileWith(t, "{}"), fileWith(t, "{}"), []string{c.ns})
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_stringList_String(t *testing.T) {
	t.Parallel()

	t.Run("空白で連ねる", func(t *testing.T) {
		t.Parallel()
		l := stringList{"a", "b"}
		assert.Equal(t, "a b", l.String())
	})
}

func Test_stringList_Set(t *testing.T) {
	t.Parallel()

	t.Run("繰り返し渡した値を順に積む", func(t *testing.T) {
		t.Parallel()
		var l stringList
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		fs.Var(&l, "n", "")
		require.NoError(t, fs.Parse([]string{"-n", "a", "-n", "b"}))
		assert.Equal(t, stringList{"a", "b"}, l)
	})
}

func Test_ciApplyStep(t *testing.T) {
	t.Parallel()

	t.Run("limit を過ぎれば SIGINT で打ち切り、timeout で落ちる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{"apply.sleep": ""})
		require.NoError(t, f.step("ci-prepare", "-mode", ciModeApply, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		require.NoError(t, f.step("ci-plan", "-state", f.state))
		require.NoError(t, f.step("ci-policy", "-state", f.state, "-work", t.TempDir(), "-policy", f.policy, "-decl", fileWith(t, "{}"), "-layer", fileWith(t, "{}"), "-namespace", "plan.p"))
		require.ErrorIs(t, f.step("ci-apply", "-state", f.state, "-timeout", "200ms"), errApplyTimeout)
		require.NoError(t, f.step("ci-upload", "-state", f.state))
		require.Error(t, f.step("ci-finish", "-state", f.state))
		assert.Equal(t, "failure=timeout\n", readKnob(t, filepath.Dir(f.output), "output"))
		assert.Contains(t, readKnob(t, f.k, "body.txt"), "interrupted")
	})

	t.Run("plan の入口の state は apply しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.ErrorIs(t, f.step("ci-apply", "-state", f.state, "-timeout", "1s"), errUsage)
	})

	t.Run("既定の打ち切りは、apply の job の timeout より、後の段を走らせる分だけ短い", func(t *testing.T) {
		t.Parallel()
		assert.LessOrEqual(t, applyTimeout+5*time.Minute, applyJobTimeout)
	})

	t.Run("正でない timeout は errUsage", func(t *testing.T) {
		t.Parallel()
		_, err := ciApplyStep([]string{"-state", "s", "-timeout", "0s"}, testDeps(nil), &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_ciRunApply(t *testing.T) {
	t.Parallel()

	t.Run("起動できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		tool := func(string, []string, string) *exec.Cmd {
			return exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "none"))
		}
		_, err := ciRunApply(ciState{Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: tool}, time.Second)
		require.ErrorIs(t, err, errLocalTF)
	})
}

func Test_ciUploadStep(t *testing.T) {
	t.Parallel()

	t.Run("state を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := ciUploadStep([]string{"-state", filepath.Join(t.TempDir(), "x.json")}, testDeps(nil), &bytes.Buffer{})
		require.ErrorIs(t, err, errCIState)
	})
}

func Test_ciRunUpload(t *testing.T) {
	t.Parallel()

	t.Run("全文が無ければ aws を起動しない", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		s := ciState{Out: filepath.Join(t.TempDir(), "none.txt")}
		require.ErrorIs(t, ciRunUpload(&s, deps{tool: tool}, &bytes.Buffer{}), errCIUpload)
		assert.Empty(t, readKnob(t, k, "calls.log"))
	})
}

func Test_failureKind(t *testing.T) {
	t.Parallel()

	lock := []byte("Error: " + lockError + "\n")
	for name, c := range map[string]struct {
		step string
		code int
		out  []byte
		want string
	}{
		"plan が lock の取得で落ちれば lock":     {stagePlan, 1, lock, "lock"},
		"plan がほかの理由で落ちれば plan":         {stagePlan, 1, []byte("Error: x"), "plan"},
		"終了コード 1 でない plan は全文を見ずに plan": {stagePlan, 2, lock, "plan"},
		"plan の 124 は timeout にしない":     {stagePlan, applyTimedOut, nil, "plan"},
		"apply の 0 は applied":           {stageApply, 0, lock, "applied"},
		"apply の打ち切りは timeout":          {stageApply, applyTimedOut, lock, "timeout"},
		"apply の打ち切りの1つ手前は apply":       {stageApply, applyTimedOut - 1, nil, "apply"},
		"apply の打ち切りの1つ後は apply":        {stageApply, applyTimedOut + 1, nil, "apply"},
		"apply が lock の取得で落ちれば lock":    {stageApply, 1, lock, "lock"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, failureKind(c.step, c.code, c.out))
		})
	}
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	t.Run("終了コードを返し、起動できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		code, err := exitCode(nil)
		require.NoError(t, err)
		assert.Zero(t, code)
		code, err = exitCode(exec.CommandContext(context.Background(), "/bin/sh", "-c", "exit 3").Run())
		require.NoError(t, err)
		assert.Equal(t, 3, code)
		_, err = exitCode(errBoom)
		require.ErrorIs(t, err, errBoom)
	})
}

var errBoom = errors.New("boom")

func Test_ciOutputBucketKind(t *testing.T) {
	t.Parallel()

	t.Run("plan の全文と identity root の apply の全文は tfplan、workload root の apply の全文は tfapply", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "tfplan", ciOutputBucketKind(ciModePlan, "workload"))
		assert.Equal(t, "tfplan", ciOutputBucketKind(ciModeApply, "identity"))
		assert.Equal(t, "tfapply", ciOutputBucketKind(ciModeApply, "workload"))
	})
}

func Test_ciStatePaths(t *testing.T) {
	t.Parallel()

	t.Run("state の名前から全文・plan の JSON・plan のファイルの置き場を決める", func(t *testing.T) {
		t.Parallel()
		out, jsonPath, tfplan := ciStatePaths("/d/admin-identity.json")
		assert.Equal(t, []string{"/d/admin-identity.txt", "/d/admin-identity.plan.json", "/d/admin-identity.tfplan"}, []string{out, jsonPath, tfplan})
	})
}

func Test_ciRequired(t *testing.T) {
	t.Parallel()

	t.Run("apply の入口だけが apply の段を要る", func(t *testing.T) {
		t.Parallel()
		assert.NotContains(t, ciRequired(ciModePlan), stageApply)
		assert.Equal(t, []string{stagePrepare, stageInit, stagePlan, stagePolicy, stageApply, stageUpload}, ciRequired(ciModeApply))
	})
}

func Test_ciState_fail(t *testing.T) {
	t.Parallel()

	t.Run("先に残した種類を上書きしない", func(t *testing.T) {
		t.Parallel()
		var s ciState
		s.fail("plan")
		s.fail("upload")
		assert.Equal(t, "plan", s.Stage)
	})
}

func Test_ciState_done(t *testing.T) {
	t.Parallel()

	t.Run("同じ段を重ねない", func(t *testing.T) {
		t.Parallel()
		var s ciState
		s.done("a")
		s.done("a")
		assert.Equal(t, []string{"a"}, s.Done)
	})
}

func Test_ciState_remove(t *testing.T) {
	t.Parallel()

	t.Run("全文・plan の JSON・plan のファイルを消し、空の置き場は無視する", func(t *testing.T) {
		t.Parallel()
		s := ciState{Out: fileWith(t, "a"), TFPlan: fileWith(t, "b")}
		s.remove()
		assert.NoFileExists(t, s.Out)
		assert.NoFileExists(t, s.TFPlan)
	})
}

func Test_ciState_save(t *testing.T) {
	t.Parallel()

	t.Run("所有者だけが読める権限で書き、読み戻せる", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "s.json")
		s := ciState{Mode: ciModePlan, Dir: "d", Out: "o", Stage: "plan"}
		require.NoError(t, s.save(p))
		got, err := loadCIState(p)
		require.NoError(t, err)
		assert.Equal(t, s, got)
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}

func Test_loadCIState(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"JSON でない":  "x",
		"知らない入口":    `{"mode":"x","dir":"d","out":"o"}`,
		"root が無い":  `{"mode":"plan","out":"o"}`,
		"全文の置き場が無い": `{"mode":"plan","dir":"d"}`,
	} {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()
			_, err := loadCIState(fileWith(t, body))
			require.ErrorIs(t, err, errCIState)
		})
	}
}

func Test_ciStepArgs(t *testing.T) {
	t.Parallel()

	t.Run("-state が無いか余分な引数があれば errUsage", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{nil, {"-state", "s", "x"}, {"-x"}} {
			_, err := ciStepArgs("x", args, testDeps(nil), nil)
			require.ErrorIs(t, err, errUsage)
		}
	})
}

func Test_appendOut(t *testing.T) {
	t.Parallel()

	t.Run("全文が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := appendOut(ciState{Out: filepath.Join(t.TempDir(), "none")})
		require.Error(t, err)
	})
}

func Test_writeFailure(t *testing.T) {
	t.Parallel()

	t.Run("GITHUB_OUTPUT が無ければ何も書かない", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, writeFailure(nil, "plan"))
	})

	t.Run("GITHUB_OUTPUT の failure に追記する", func(t *testing.T) {
		t.Parallel()
		p := fileWith(t, "a=1\n")
		require.NoError(t, writeFailure([]string{"GITHUB_OUTPUT=" + p}, "plan"))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "a=1\nfailure=plan\n", string(raw))
	})
}

func Test_appendGitHub(t *testing.T) {
	t.Parallel()

	t.Run("書けなければ errCIOutput", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, appendGitHub(t.TempDir(), "x"), errCIOutput)
	})
}

func Test_readInRoot(t *testing.T) {
	t.Parallel()

	t.Run("ファイルを読み、ディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		got, err := readInRoot(fileWith(t, "x"))
		require.NoError(t, err)
		assert.Equal(t, "x", string(got))
		_, err = readInRoot(filepath.Join(t.TempDir(), "none", "f"))
		require.Error(t, err)
	})
}

func Test_openInRoot(t *testing.T) {
	t.Parallel()

	t.Run("所有者だけが読める権限で作る", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "f")
		f, err := openInRoot(p, os.O_WRONLY|os.O_CREATE)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}

func Test_writeInRoot(t *testing.T) {
	t.Parallel()

	t.Run("中身を置き換える", func(t *testing.T) {
		t.Parallel()
		p := fileWith(t, "old-old")
		require.NoError(t, writeInRoot(p, []byte("new")))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "new", string(raw))
	})
}

func Test_copyFile(t *testing.T) {
	t.Parallel()

	t.Run("写し、元が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "d")
		require.NoError(t, copyFile(fileWith(t, "x"), dst))
		raw, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "x", string(raw))
		require.Error(t, copyFile(filepath.Join(t.TempDir(), "none"), dst))
	})
}

func Test_copyTree(t *testing.T) {
	t.Parallel()

	t.Run("下の階層ごと写す", func(t *testing.T) {
		t.Parallel()
		src := policyDirWith(t, map[string]string{"a.rego": "a", "lib/b.rego": "b"})
		dst := filepath.Join(t.TempDir(), "rego")
		require.NoError(t, copyTree(src, dst))
		assert.FileExists(t, filepath.Join(dst, "lib", "b.rego"))
		require.Error(t, copyTree(filepath.Join(t.TempDir(), "none"), dst))
	})
}
