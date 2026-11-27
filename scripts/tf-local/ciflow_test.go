package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	planSecret  = "PLAN-SECRET"
	applySecret = "APPLY-SECRET"
	// planLineOne は、偽の show の既定（create 1件）に合う terraform の change_summary の行。
	planLineOne = `{"@level":"info","type":"change_summary","changes":{"add":1,"change":0,"import":0,"remove":0,"operation":"plan"}}` + "\n"
	// planLog は、plan の stream の、値を含む info の行。
	planLog   = `{"@level":"info","@message":"` + planSecret + `","type":"log"}` + "\n"
	onePolicy = "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n"
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
		stepCIPlanNoLock + ".out":         planLog + planLineOne,
		stepCIPlanNoLock + ".rc":          "2",
		stepCIPlanLock + ".out":           planLog + planLineOne,
		stepCIPlanLock + ".rc":            "2",
		stepCIShow + ".out":               planWith(change("a.b", `"create"`), ""),
		stepCIConftest + ".out":           `[{"namespace":"plan.p","successes":1}]`,
		stepCIApply + ".out":              `{"@level":"info","@message":"` + applySecret + `","type":"log"}` + "\n",
		stepCIPutObject + ".out":          `{"VersionId":"v1","ETag":"e"}`,
		stepCIInit + ".out":               "init\n",
		stepCIPermissions + ".out":        "# 結果: 209 件のうち NG 0 件\n",
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
bootstrap-check-plan-permissions) shift; echo "$*" > "$k/permissions.args"; cp plan.json "$k/permissions.plan"; pwd > "$k/permissions.pwd"; env > "$k/permissions.env" ;;
ci-terraform-apply) if [ -e "$k/apply.sleep" ]; then trap 'echo interrupted; exit 1' INT; while :; do sleep 0.05; done; fi ;;
s3api-get-object) cat input.json >> "$k/get.log"; echo >> "$k/get.log"; if [ -e "$k/lock.body" ]; then cp "$k/lock.body" lock.json; fi ;;
s3api-delete-object) cat input.json >> "$k/delete.log"; echo >> "$k/delete.log" ;;
esac
n=$(( $(cat "$k/$step.n" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$k/$step.n"
if [ -e "$k/$step.$n.out" ]; then cat "$k/$step.$n.out"; elif [ -e "$k/$step.out" ]; then cat "$k/$step.out"; fi
if [ -e "$k/$step.stderr" ]; then cat "$k/$step.stderr" >&2; fi
if [ "$step" = ci-terraform-plan-hold ] && [ -e "$k/hold.sleep" ]; then while :; do sleep 0.05; done; fi
if [ -e "$k/$step.signal" ]; then kill -KILL $$; fi
rc=0; if [ -e "$k/$step.rc" ]; then rc=$(cat "$k/$step.rc"); fi
exit "$rc"
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	tool := func(ctx context.Context, d string, env []string, step string, ops ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, "/bin/sh", append([]string{script, step}, ops...)...)
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
	f.d = deps{readFile: os.ReadFile, environ: environ, git: stubGit("", ""), now: time.Now, tool: tool, stderr: &stderr, root: t.TempDir()}
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

func (f *flow) all(mode, root string) error {
	return f.allSteps(mode, root, mode == ciModePlan && root == "identity")
}

// allSteps は all の段を起動する。permissions なら、ci-plan-admin-identity と同じく Policy の後に ci-permissions を起動する。
func (f *flow) allSteps(mode, root string, permissions bool) error {
	f.state = filepath.Join(filepath.Dir(f.state), "admin-"+root+".json")
	if err := f.step("ci-prepare", "-mode", mode, "-project", "tabp", "-env", "admin", "-root", root,
		"-profile", "tabp-admin-ci-"+mode, "-base", f.base, "-header", f.header, "-state", f.state); err != nil {
		return err
	}
	if err := f.step("ci-init", "-state", f.state); err != nil {
		return err
	}
	_ = f.step("ci-plan", "-state", f.state)
	_ = f.step("ci-policy", "-state", f.state, "-work", "tmp/ci-policy", "-policy", f.policy, "-decl", fileWith(f.t, "{}"), "-layer", fileWith(f.t, "{}"), "-namespace", "plan.p")
	if permissions {
		_ = f.step("ci-permissions", "-state", f.state, "-work", "tmp/ci-permissions")
	}
	if mode == ciModeApply {
		_ = f.step("ci-apply", "-state", f.state, "-timeout", "5s")
	}
	_ = f.step("ci-upload", "-state", f.state)

	return f.step("ci-finish", "-state", f.state)
}

func (f *flow) calls() string { return readKnob(f.t, f.k, "calls.log") }

func (f *flow) public() {
	f.t.Helper()
	summary := readKnob(f.t, filepath.Dir(f.summary), "summary")
	for _, line := range strings.Split(f.stdout.String()+"\n"+html.UnescapeString(summary), "\n") {
		if strings.HasPrefix(line, publicdiag.Prefix) {
			assert.True(f.t, publicdiag.Valid(line), line)
		}
	}
	for _, v := range []string{planSecret, applySecret, account} {
		assert.NotContains(f.t, f.stdout.String(), v)
		assert.NotContains(f.t, summary, v)
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
		code    int
	}{
		"plan が落ちれば Policy も apply もせず、全文を置いて plan で落ち、判定として終了コード 1": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "1"}, failure: "plan", upload: true, code: 1},
		"plan が lock を取れなければ lock で落ち、判定として終了コード 1": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "1", stepCIPlanLock + ".out": "Error: " + lockError + "\n"}, failure: "lock", upload: true, code: 1},
		"plan の JSON の変更と終了コードが食い違えば plan で落ち、判定に届かず終了コード 2": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "0"}, failure: "plan", upload: true, code: 2},
		"Policy を通らなければ apply せず、全文を置いて policy で落ち、判定として終了コード 1": {
			knobs: map[string]string{stepCIConftest + ".out": `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"bad"}]}]`}, failure: "policy", upload: true, code: 1},
		"Policy の規則が1つも評価されなければ policy で落ち、判定に届かず終了コード 2": {
			knobs: map[string]string{stepCIConftest + ".out": `[]`}, failure: "policy", upload: true, code: 2},
		"apply が落ちれば全文を置いて apply で落ち、判定として終了コード 1": {
			knobs: map[string]string{stepCIApply + ".rc": "1"}, failure: "apply", apply: true, upload: true, code: 1},
		"apply が lock を取れなければ lock で落ち、判定として終了コード 1": {
			knobs: map[string]string{stepCIApply + ".rc": "1", stepCIApply + ".out": lockError + "\n"}, failure: "lock", apply: true, upload: true, code: 1},
		"apply が終わった後に全文を置けなければ upload で落ち、判定に届かず終了コード 2": {
			knobs: map[string]string{stepCIPutObject + ".rc": "1"}, failure: "upload", apply: true, upload: true, code: 2},
		"版 ID の無い応答は置けなかったとして落ち、終了コード 2": {
			knobs: map[string]string{stepCIPutObject + ".out": `{}`}, failure: "upload", apply: true, upload: true, code: 2},
		"plan が判定で落ちた後に全文を置けなくても、plan の判定は上書きされず終了コード 1": {
			knobs: map[string]string{stepCIPlanLock + ".rc": "1", stepCIPutObject + ".rc": "1"}, failure: "plan", upload: true, code: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs)
			err := f.all(ciModeApply, "identity")
			require.Error(t, err)
			assert.Equal(t, c.code, exitCode(err), err.Error())
			if c.code == 2 {
				require.ErrorIs(t, err, errCINotEstablished)
			}
			assert.Equal(t, "failure="+c.failure+"\n", readKnob(t, filepath.Dir(f.output), "output"))
			assert.Equal(t, c.apply, strings.Contains(f.calls(), stepCIApply), f.calls())
			assert.Equal(t, c.upload, readKnob(t, f.k, "upload.log") != "")
			assert.NotContains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "apply が終わりました")
			f.public()
			f.leftovers()
		})
	}

	for name, step := range map[string]string{
		"plan を起動できなければ、plan で落ち、判定に届かず終了コード 2":   stepCIPlanLock,
		"apply を起動できなければ、apply で落ち、判定に届かず終了コード 2": stepCIApply,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, nil)
			inner := f.d.tool
			f.d.tool = func(ctx context.Context, dir string, env []string, s string, ops ...string) *exec.Cmd {
				if s == step {
					return tooltest.Missing(ctx, dir, env, s, ops...)
				}
				return inner(ctx, dir, env, s, ops...)
			}
			err := f.all(ciModeApply, "identity")
			require.ErrorIs(t, err, errCINotEstablished)
			assert.NotErrorIs(t, err, errCIFailed)
			assert.Equal(t, 2, exitCode(err))
		})
	}

	t.Run("管理アカウントの identity root の plan の入口は、Policy の後に権限を評価し、全文を置いて終わる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModePlan, "identity"))
		calls := f.calls()
		assert.Contains(t, calls, stepCIPermissions+" permissions-")
		assert.Less(t, strings.Index(calls, stepCIConftest), strings.Index(calls, stepCIPermissions))
		assert.Less(t, strings.Index(calls, stepCIPermissions), strings.Index(calls, stepCIPutObject))
		assert.Contains(t, f.stdout.String(), "# 結果: 209 件のうち NG 0 件\n")
		assert.Contains(t, f.stdout.String(), "✅ CI の role の権限が表と一致しました")
		f.public()
		f.leftovers()
	})

	t.Run("権限の段が要る入口で ci-permissions が走らなければ、finish が permissions の段で落とす", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		err := f.allSteps(ciModePlan, "identity", false)
		require.ErrorIs(t, err, errCINotEstablished)
		assert.Contains(t, err.Error(), "失敗の段: "+stagePermissions)
		assert.Equal(t, 2, exitCode(err))
		assert.NotEmpty(t, readKnob(t, f.k, "upload.log"), "upload は権限の段の成否に関わらず走る")
	})

	for name, c := range map[string]struct {
		knobs map[string]string
		want  error
		code  int
	}{
		"権限が表と一致しなければ、全文を置いて permissions で落ち、判定として終了コード 1": {map[string]string{stepCIPermissions + ".rc": "1"}, errCIFailed, 1},
		"権限を評価できなければ、全文を置いて permissions で落ち、判定に届かず終了コード 2": {map[string]string{stepCIPermissions + ".rc": "2"}, errCINotEstablished, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs)
			err := f.all(ciModePlan, "identity")
			require.ErrorIs(t, err, c.want)
			assert.Contains(t, err.Error(), "失敗の段: "+stagePermissions)
			assert.Equal(t, c.code, exitCode(err))
			assert.NotEmpty(t, readKnob(t, f.k, "upload.log"))
			f.leftovers()
		})
	}

	for name, c := range map[string]struct {
		knobs map[string]string
		stage string
	}{
		"Policy の違反": {map[string]string{stepCIConftest + ".out": `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"bad"}]}]`}, stagePolicy},
		"plan の失敗":   {map[string]string{stepCIPlanNoLock + ".rc": "1"}, stagePlan},
	} {
		t.Run("権限の段が要る入口で"+name+"なら、bootstrap-check を起動せず、失敗の段は "+c.stage+" のまま終了コード 1", func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs)
			err := f.all(ciModePlan, "identity")
			require.ErrorIs(t, err, errCIFailed)
			assert.Contains(t, err.Error(), "失敗の段: "+c.stage)
			assert.Equal(t, 1, exitCode(err))
			assert.NotContains(t, f.calls(), stepCIPermissions)
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

	t.Run("段が state に終わりを書かずに落ちていれば、判定に届いていないので errCINotEstablished で終了コード 2（失敗の種類は従来どおり書く）", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModeApply, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		err := f.step("ci-finish", "-state", f.state)
		require.ErrorIs(t, err, errCINotEstablished)
		assert.NotErrorIs(t, err, errCIUnfinish, "判定の sentinel を包むと終了コードが 1 になる")
		assert.Equal(t, 2, exitCode(err))
		assert.Equal(t, "failure=plan\n", readKnob(t, filepath.Dir(f.output), "output"))
		f.leftovers()
	})

	preparedState := func(t *testing.T, established bool, drop bool) *flow {
		t.Helper()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		st, err := loadCIState(f.state)
		require.NoError(t, err)
		st.Stage, st.Established = stagePlan, established
		require.NoError(t, st.save(f.state))
		if drop {
			raw, err := os.ReadFile(f.state) //nolint:gosec // t.TempDir() 由来
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			delete(m, "established")
			raw, err = json.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(f.state, raw, 0o600))
		}
		return f
	}

	t.Run("判定として落ちた段（established）は従来どおり errCIUnfinish で終了コード 1", func(t *testing.T) {
		t.Parallel()
		f := preparedState(t, true, false)
		err := f.step("ci-finish", "-state", f.state)
		require.ErrorIs(t, err, errCIUnfinish)
		assert.Equal(t, 1, exitCode(err))
	})

	t.Run("upload まで終わった失敗でも、判定に届いていなければ errCIFailed を包まず errCINotEstablished で終了コード 2", func(t *testing.T) {
		t.Parallel()
		f := preparedState(t, false, false)
		st, err := loadCIState(f.state)
		require.NoError(t, err)
		st.Done = append(st.Done, stagePlan, stagePolicy, stageUpload)
		require.NoError(t, st.save(f.state))
		err = f.step("ci-finish", "-state", f.state)
		require.ErrorIs(t, err, errCINotEstablished)
		assert.NotErrorIs(t, err, errCIFailed)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("upload まで終わった判定の失敗は errCIFailed で終了コード 1", func(t *testing.T) {
		t.Parallel()
		f := preparedState(t, true, false)
		st, err := loadCIState(f.state)
		require.NoError(t, err)
		st.Done = append(st.Done, stagePlan, stagePolicy, stageUpload)
		require.NoError(t, st.save(f.state))
		err = f.step("ci-finish", "-state", f.state)
		require.ErrorIs(t, err, errCIFailed)
		assert.Equal(t, 1, exitCode(err))
	})

	for name, c := range map[string]struct {
		established bool
		want        error
		code        int
	}{
		"判定に届かない失敗は、失敗の種類を書けなくても終了コード 2 のまま":       {false, errCINotEstablished, 2},
		"判定の失敗は、失敗の種類を書けなくても errCIFailed で終了コード 1": {true, errCIFailed, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, nil)
			require.NoError(t, f.step("ci-prepare", "-mode", ciModeApply, "-project", "tabp", "-env", "admin", "-root", "identity",
				"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
			require.NoError(t, f.step("ci-init", "-state", f.state))
			st, err := loadCIState(f.state)
			require.NoError(t, err)
			st.Stage, st.Established = stagePlan, c.established
			st.Done = append(st.Done, stagePlan, stagePolicy, stageApply, stageUpload)
			require.NoError(t, st.save(f.state))
			require.NoError(t, os.MkdirAll(f.output, 0o700), "GITHUB_OUTPUT をディレクトリにして書き込みを落とす")
			err = f.step("ci-finish", "-state", f.state)
			require.ErrorIs(t, err, c.want)
			require.ErrorIs(t, err, errCIOutput)
			assert.Equal(t, c.code, exitCode(err))
		})
	}

	t.Run("established を持たない古い state の失敗は、成り立たない側（2）に倒す", func(t *testing.T) {
		t.Parallel()
		f := preparedState(t, true, true)
		err := f.step("ci-finish", "-state", f.state)
		require.ErrorIs(t, err, errCINotEstablished)
		assert.Equal(t, 2, exitCode(err))
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

	for name, c := range map[string]struct {
		extra []string
		want  bool
	}{
		"管理アカウントの identity root の plan の入口": {[]string{"-mode", ciModePlan}, true},
		"identity root の apply の入口":         {[]string{"-mode", ciModeApply}, false},
		"identity root の lock を残す入口":        {[]string{"-mode", ciModeHold}, false},
		"workload root の plan の入口":          {[]string{"-mode", ciModePlan, "-root", "workload"}, false},
	} {
		t.Run(name+"の権限の段が要るかを、入口の値から state に残す", func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, nil)
			require.NoError(t, prepare(f, c.extra...))
			s, err := loadCIState(f.state)
			require.NoError(t, err)
			assert.Equal(t, c.want, s.Permissions)
			assert.Equal(t, "tabp", s.Project)
		})
	}

	t.Run("-permissions のフラグは受けない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.ErrorIs(t, prepare(f, "-mode", ciModePlan, "-permissions"), errUsage)
		assert.Empty(t, f.calls())
	})

	stale := func(t *testing.T, f *flow) string {
		t.Helper()
		bin := filepath.Join(f.d.root, filepath.FromSlash(bootstrapCheckBin))
		require.NoError(t, os.MkdirAll(filepath.Dir(bin), 0o700))
		require.NoError(t, os.WriteFile(bin, []byte("old"), 0o600))
		return bin
	}

	t.Run("権限の段が要る入口は、前の build の bootstrap-check を消す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		bin := stale(t, f)
		require.NoError(t, prepare(f, "-mode", ciModePlan))
		assert.NoFileExists(t, bin)
	})

	t.Run("権限の段が要らない入口は、bootstrap-check を消さない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		bin := stale(t, f)
		require.NoError(t, prepare(f, "-mode", ciModeApply))
		assert.FileExists(t, bin)
	})

	t.Run("前の bootstrap-check を消せなければ、aws を起動せずに prepare が失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		bin := filepath.Join(f.d.root, filepath.FromSlash(bootstrapCheckBin))
		require.NoError(t, os.MkdirAll(filepath.Join(bin, "x"), 0o700), "中身のあるディレクトリにして消せなくする")
		require.ErrorIs(t, prepare(f, "-mode", ciModePlan), errCIState)
		assert.NoFileExists(t, f.state)
		assert.Empty(t, f.calls())
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

	prepared := func(t *testing.T) ciState {
		t.Helper()
		return ciState{Mode: ciModePlan, Dir: t.TempDir(), Out: fileWith(t, ""), Done: []string{stagePrepare}, InitArgs: []string{"-backend-config=bucket=b"}}
	}

	t.Run("init が 0 以外なら errCIInit で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{stepCIInit + ".rc": "1"})
		err := ciRunInit(prepared(t), deps{tool: tool})
		require.ErrorIs(t, err, errCIInit)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("init を起動できなければ ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		err := ciRunInit(prepared(t), deps{tool: tooltest.Missing})
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.NotErrorIs(t, err, errCIInit)
		assert.Equal(t, 2, exitCode(err))
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

	t.Run("変わる resource の一覧をログと job summary に出し、値と識別子を出さない", func(t *testing.T) {
		t.Parallel()
		show := `{"format_version":"1.2","resource_changes":[` +
			`{"address":"a.b[\"` + account + `\"]","change":{"actions":["create"],"after":{"v":"` + planSecret + `"}}},` +
			`{"address":"a.c[\"x<y\"]","change":{"actions":["update"],"before":{"v":"` + planSecret + `"}}}]}`
		f := newFlow(t, map[string]string{
			stepCIShow + ".out":       show,
			stepCIPlanNoLock + ".out": planLog + summaryLine(1, 1),
		})
		require.NoError(t, f.all(ciModePlan, "identity"))
		assert.Contains(t, f.stdout.String(), "変わる resource（2 件）:\n  create a.b[<伏せた key>]\n  update a.c[<伏せた key>]\n")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"),
			"Plan: 0 to import, 1 to add, 1 to change, 0 to destroy.\n\n<pre>\ncreate a.b[&lt;伏せた key&gt;]\nupdate a.c[&lt;伏せた key&gt;]\n</pre>\n\n- key:")
		assert.NotContains(t, readKnob(t, filepath.Dir(f.summary), "summary"), planSecret)
		assert.NotContains(t, readKnob(t, filepath.Dir(f.summary), "summary"), account)
		f.public()
	})

	t.Run("apply の入口も、plan し直した一覧をログに出す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.all(ciModeApply, "identity"))
		assert.Contains(t, f.stdout.String(), "変わる resource（1 件）:\n  create a.b\n")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "<pre>\ncreate a.b\n</pre>")
		assert.Contains(t, readKnob(t, filepath.Dir(f.output), "output"), "plan=Plan: 0 to import, 1 to add, 0 to change, 0 to destroy.\n")
	})

	t.Run("一覧の件数が change_summary と食い違えば plan の段で落ち、判定に届かないので終了コード 2", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIPlanNoLock + ".out": planLog + summaryLine(2, 0)})
		err := f.all(ciModePlan, "identity")
		require.ErrorIs(t, f.errs["ci-plan"], errPlanList)
		require.ErrorIs(t, err, errCINotEstablished)
		assert.Equal(t, 2, exitCode(err))
		assert.NotContains(t, f.stdout.String(), "変わる resource")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "失敗の段: plan", "失敗の経路でも job summary を書く")
		assert.NotEmpty(t, readKnob(t, f.k, "body.txt"), "落ちても全文は置く")
		f.public()
		f.leftovers()
	})

	t.Run("plan の stream に change_summary が無ければ plan の段で落ちる", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIPlanNoLock + ".out": planLog})
		require.Error(t, f.all(ciModePlan, "identity"))
		require.ErrorIs(t, f.errs["ci-plan"], errChangeSummary)
	})

	t.Run("plan の失敗の診断を、許可の文法に当たる欄だけでログと job summary に出す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIPlanNoLock + ".rc": "1", stepCIPlanNoLock + ".out": planLog + `{"@level":"error","@message":"Error: creating IAM Role","type":"diagnostic","diagnostic":{"severity":"error",` +
			`"summary":"creating IAM Role (x): operation error IAM: CreateRole, https response error StatusCode: 403, RequestID: r, ` +
			`api error AccessDenied: User: arn:aws:sts::` + account + `:assumed-role/tabp-admin-ci-apply-identity/gha is not authorized ` +
			`to perform: iam:CreateRole on resource: x because no identity-based policy allows the iam:CreateRole action",` +
			`"detail":"","address":"aws_iam_role.this[\"` + account + `\"]"}}` + "\n"})
		require.Error(t, f.all(ciModePlan, "identity"))
		want := "診断: kind=iam-deny; address=aws_iam_role.this[<伏せた key>]; service=IAM; operation=CreateRole; status=403; " +
			"code=AccessDenied; action=iam:CreateRole; reason=implicit:identity-based"
		assert.Contains(t, f.stdout.String(), want+"\n")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "失敗の段: plan")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), html.EscapeString(want))
		assert.Contains(t, readKnob(t, f.k, "body.txt"), "is not authorized", "全文は stream をそのまま持つ")
		f.public()
	})

	t.Run("壊れた stream でも plan の失敗の種類は変わらない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIPlanNoLock + ".rc": "1", stepCIPlanNoLock + ".out": "{\"@level\":\"error\" broken\n"})
		require.Error(t, f.all(ciModePlan, "identity"))
		require.ErrorIs(t, f.errs["ci-plan"], errPlanFailed)
		assert.Contains(t, f.stdout.String(), "診断: count=0; exit=1\n")
		assert.Contains(t, f.stdout.String(), "診断: masked=1\n")
		f.public()
	})

	t.Run("人の読む差分を全文に足す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIShowText + ".out": "Terraform will perform the following actions\n"})
		require.NoError(t, f.all(ciModePlan, "identity"))
		assert.Contains(t, f.calls(), stepCIShowText+" identity profile=tabp-admin-ci-plan region= init= plan= show=")
		assert.Contains(t, readKnob(t, f.k, "body.txt"), "Terraform will perform the following actions")
		assert.NotContains(t, f.stdout.String(), "Terraform will perform")
	})
}

func Test_changeList(t *testing.T) {
	t.Parallel()

	t.Run("一覧が空なら、変わる resource が無いと書く", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "変わる resource はありません\n", changeList(nil))
	})

	t.Run("件数と、1件ずつ字下げした行を書く", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "変わる resource（2 件）:\n  create a.b\n  delete a.c\n", changeList([]string{"create a.b", "delete a.c"}))
	})
}

func Test_ciPermissionsStep(t *testing.T) {
	t.Parallel()

	prepared := func(t *testing.T, mutate func(*ciState)) *flow {
		t.Helper()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "tabp-admin-ci-plan", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		require.NoError(t, f.step("ci-plan", "-state", f.state))
		s, err := loadCIState(f.state)
		require.NoError(t, err)
		s.Done = append(s.Done, stagePolicy)
		if mutate != nil {
			mutate(&s)
		}
		require.NoError(t, s.save(f.state))
		return f
	}

	for name, mutate := range map[string]func(*ciState){
		"Policy の段が終わっていない": func(s *ciState) { s.Done = slices.DeleteFunc(s.Done, func(d string) bool { return d == stagePolicy }) },
		"前の段が失敗している":        func(s *ciState) { s.Stage = stagePolicy },
	} {
		t.Run(name+"なら、bootstrap-check を起動せず、段の失敗も残さない", func(t *testing.T) {
			t.Parallel()
			f := prepared(t, mutate)
			got, err := ciPermissionsStep([]string{"-state", f.state, "-work", "tmp/ci-permissions"}, f.d, &f.stdout)
			require.NoError(t, err)
			assert.Equal(t, "前の段が終わっていないので権限を評価しません", got)
			assert.NotContains(t, f.calls(), stepCIPermissions)
			s, err := loadCIState(f.state)
			require.NoError(t, err)
			assert.NotContains(t, s.Done, stagePermissions)
		})
	}

	for name, c := range map[string]struct {
		knobs       map[string]string
		want        error
		established bool
	}{
		"終了コード 1 は判定":     {map[string]string{stepCIPermissions + ".rc": "1"}, errCIPermissions, true},
		"終了コード 2 は評価できない": {map[string]string{stepCIPermissions + ".rc": "2"}, errCIPermissionsRun, false},
	} {
		t.Run(name+"として permissions の段の失敗を残す", func(t *testing.T) {
			t.Parallel()
			f := prepared(t, nil)
			tool, _ := fakeFlow(t, c.knobs)
			f.d.tool = tool
			_, err := ciPermissionsStep([]string{"-state", f.state, "-work", "tmp/ci-permissions"}, f.d, &f.stdout)
			require.ErrorIs(t, err, c.want)
			s, lerr := loadCIState(f.state)
			require.NoError(t, lerr)
			assert.Equal(t, stagePermissions, s.Stage)
			assert.Equal(t, c.established, s.Established)
			assert.NotContains(t, s.Done, stagePermissions)
		})
	}

	t.Run("bootstrap-check が無ければ、評価できなかったとして permissions の段の失敗を残す", func(t *testing.T) {
		t.Parallel()
		f := prepared(t, nil)
		f.d.tool = tooltest.Missing
		_, err := ciPermissionsStep([]string{"-state", f.state, "-work", "tmp/ci-permissions"}, f.d, &f.stdout)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
		s, lerr := loadCIState(f.state)
		require.NoError(t, lerr)
		assert.Equal(t, stagePermissions, s.Stage)
		assert.False(t, s.Established)
	})

	t.Run("成功すれば permissions の段を終わりに残す", func(t *testing.T) {
		t.Parallel()
		f := prepared(t, nil)
		got, err := ciPermissionsStep([]string{"-state", f.state, "-work", "tmp/ci-permissions"}, f.d, &f.stdout)
		require.NoError(t, err)
		assert.Equal(t, "✅ CI の role の権限が表と一致しました", got)
		s, err := loadCIState(f.state)
		require.NoError(t, err)
		assert.Contains(t, s.Done, stagePermissions)
		assert.Empty(t, s.Stage)
	})

	t.Run("権限の段を要らない入口の state なら errUsage", func(t *testing.T) {
		t.Parallel()
		f := prepared(t, func(s *ciState) { s.Permissions = false })
		_, err := ciPermissionsStep([]string{"-state", f.state, "-work", "tmp/ci-permissions"}, f.d, &f.stdout)
		require.ErrorIs(t, err, errUsage)
		assert.NotContains(t, f.calls(), stepCIPermissions)
	})

	for name, args := range map[string][]string{
		"-work が無い": {"-state", "s"},
		"余分な引数がある":  {"-state", "s", "-work", "w", "x"},
	} {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()
			_, err := ciPermissionsStep(args, testDeps(nil), &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		})
	}

	t.Run("state を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := ciPermissionsStep([]string{"-state", filepath.Join(t.TempDir(), "none.json"), "-work", "w"}, testDeps(nil), &bytes.Buffer{})
		require.ErrorIs(t, err, errCIState)
	})
}

func Test_ciRunPermissions(t *testing.T) {
	t.Parallel()

	state := func(t *testing.T) ciState {
		t.Helper()
		return ciState{Project: "tabp", Region: "ap-northeast-1", Profile: "tabp-admin-ci-plan", JSON: fileWith(t, `{"resource_changes":[]}`)}
	}

	t.Run("plan の JSON を tmp/ の下の作業ディレクトリへ写し、project と region を operand で渡し、出力をそのまま出す", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, map[string]string{stepCIPermissions + ".out": "# S3 と IAM: ok 28 件、NG 0 件\n"})
		root := t.TempDir()
		var out bytes.Buffer
		require.NoError(t, ciRunPermissions(state(t), deps{tool: tool, stderr: &bytes.Buffer{}, root: root, environ: []string{"PATH=" + os.Getenv("PATH"), "AWS_ACCESS_KEY_ID=outer"}}, &out, "tmp/ci-permissions"))
		assert.Equal(t, "tabp ap-northeast-1\n", readKnob(t, k, "permissions.args"))
		assert.JSONEq(t, `{"resource_changes":[]}`, readKnob(t, k, "permissions.plan"))
		pwd := strings.TrimSpace(readKnob(t, k, "permissions.pwd"))
		rootReal, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(pwd, filepath.Join(rootReal, "tmp", "ci-permissions", "permissions-")), pwd)
		assert.NoDirExists(t, pwd, "作業ディレクトリは消す")
		assert.Contains(t, readKnob(t, k, "calls.log"), stepCIPermissions+" "+filepath.Base(pwd)+" profile=tabp-admin-ci-plan ")
		assert.Equal(t, "# S3 と IAM: ok 28 件、NG 0 件\n", out.String())
	})

	t.Run("静的な資格情報を外し、plan の role の profile だけを渡す", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		environ := []string{"PATH=" + os.Getenv("PATH"), "AWS_ACCESS_KEY_ID=outer", "AWS_SECRET_ACCESS_KEY=s", "AWS_SESSION_TOKEN=t", "AWS_CONFIG_FILE=/x", "AWS_PROFILE=outer"}
		require.NoError(t, ciRunPermissions(state(t), deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir(), environ: environ}, &bytes.Buffer{}, "tmp/ci-permissions"))
		env := strings.Split(readKnob(t, k, "permissions.env"), "\n")
		for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_CONFIG_FILE"} {
			assert.False(t, slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") }), name)
		}
		assert.Contains(t, env, "AWS_PROFILE=tabp-admin-ci-plan")
		assert.NotContains(t, env, "AWS_PROFILE=outer")
	})

	t.Run("子の stdout と stderr の12桁の数字を伏せてから出す", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{stepCIPermissions + ".out": "out 123456789012\n", stepCIPermissions + ".stderr": "err 123456789012\n", stepCIPermissions + ".rc": "1"})
		var out, stderr bytes.Buffer
		err := ciRunPermissions(state(t), deps{tool: tool, stderr: &stderr, root: t.TempDir()}, &out, "tmp/ci-permissions")
		require.ErrorIs(t, err, errCIPermissions)
		assert.Equal(t, "out <account>\n", out.String())
		assert.Equal(t, "err <account>\n", stderr.String())
	})

	t.Run("子がシグナルで終われば、評価できなかったとして errCIPermissionsRun で終了コード 2", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{stepCIPermissions + ".signal": ""})
		err := ciRunPermissions(state(t), deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-permissions")
		require.ErrorIs(t, err, errCIPermissionsRun)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("bootstrap-check を組み立てられなければ、評価できなかったとして終了コード 2", func(t *testing.T) {
		t.Parallel()
		err := ciRunPermissions(state(t), deps{tool: command, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-permissions")
		require.ErrorIs(t, err, errCIPermissionsRun)
		assert.Equal(t, 2, exitCode(err))
	})

	for name, c := range map[string]struct {
		mutate func(*ciState)
		want   error
	}{
		"project の形が違う": {func(s *ciState) { s.Project = "-x" }, errToken},
		"region の形が違う":  {func(s *ciState) { s.Region = "--region=x" }, errRegionMalformed},
	} {
		t.Run(name+"なら、bootstrap-check を起動しない", func(t *testing.T) {
			t.Parallel()
			tool, k := fakeFlow(t, nil)
			s := state(t)
			c.mutate(&s)
			err := ciRunPermissions(s, deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-permissions")
			require.ErrorIs(t, err, c.want)
			assert.Equal(t, 2, exitCode(err))
			assert.Empty(t, readKnob(t, k, "calls.log"))
		})
	}

	t.Run("作業ディレクトリが tmp/ の外なら errUsage", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		err := ciRunPermissions(state(t), deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "../x")
		require.ErrorIs(t, err, errUsage)
		assert.Empty(t, readKnob(t, k, "calls.log"))
	})

	t.Run("plan の JSON が無ければ、起動せずに評価できない", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		s := state(t)
		s.JSON = filepath.Join(t.TempDir(), "none.json")
		err := ciRunPermissions(s, deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-permissions")
		require.ErrorIs(t, err, errCIPermissionsRun)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, readKnob(t, k, "calls.log"))
	})

	for name, c := range map[string]struct {
		rc   string
		want error
		code int
	}{
		"終了コード 1 は errCIPermissions で、終了コード 1":     {"1", errCIPermissions, 1},
		"終了コード 2 は errCIPermissionsRun で、終了コード 2":  {"2", errCIPermissionsRun, 2},
		"知らない終了コードは errCIPermissionsRun で、終了コード 2": {"3", errCIPermissionsRun, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, _ := fakeFlow(t, map[string]string{stepCIPermissions + ".rc": c.rc})
			err := ciRunPermissions(state(t), deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-permissions")
			require.ErrorIs(t, err, c.want)
			assert.Equal(t, c.code, exitCode(err))
		})
	}
}

func Test_permissionsRequired(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		mode, env, root string
		want            bool
	}{
		"管理アカウントの identity root の plan": {ciModePlan, "admin", "identity", true},
		"apply の入口":      {ciModeApply, "admin", "identity", false},
		"lock を残す入口":     {ciModeHold, "admin", "identity", false},
		"workload root":  {ciModePlan, "admin", "workload", false},
		"管理アカウントでない env": {ciModePlan, "dev", "identity", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, permissionsRequired(c.mode, c.env, c.root))
		})
	}
}

func Test_maskDigits(t *testing.T) {
	t.Parallel()

	t.Run("12桁の数字を伏せ、ほかは残す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "a <account> b 12345678901 <account>9", maskDigits("a 123456789012 b 12345678901 1234567890129"))
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
		for _, name := range []string{"rego", "decl.json", "layer.json", "p.rego", "admin-identity.json"} {
			assert.Contains(t, ls, name)
		}
		assert.NotContains(t, ls, "plan.json")
	})

	t.Run("root の名前がファイル名の形でなければ、conftest を起動せずに失敗する", func(t *testing.T) {
		t.Parallel()
		tool, k := fakeFlow(t, nil)
		s := ciState{Root: "../x", Out: fileWith(t, ""), JSON: fileWith(t, "{}")}
		err := ciRunPolicy(s, deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-policy", policyDirWith(t, map[string]string{"p.rego": onePolicy}), fileWith(t, "{}"), fileWith(t, "{}"), []string{"plan.p"})
		require.ErrorIs(t, err, errPolicyJSON)
		assert.NoFileExists(t, filepath.Join(k, "conftest.ls"))
	})

	for name, c := range map[string]struct {
		files map[string]string
		ns    string
		out   string
		want  error
		code  int
	}{
		"名前空間の rego が無ければ失敗する":                         {files: map[string]string{"p.rego": onePolicy}, ns: "plan.q", out: "[]", want: errPolicyRules},
		"rego に規則が無ければ失敗する":                            {files: map[string]string{"p.rego": "package plan.p\n"}, ns: "plan.p", out: "[]", want: errPolicyRules},
		"conftest の結果を読めなければ失敗する":                      {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: "x", want: errPolicyJSON},
		"評価の件数が規則の数と違えば評価できなかったとして ErrCount で、終了コード 2": {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: `[{"namespace":"plan.p","successes":2}]`, want: policyresult.ErrCount, code: 2},
		"評価の件数が規則の数より少なければ ErrCount で、終了コード 2":         {files: map[string]string{"p.rego": onePolicy + "\ndeny contains msg if {\n\tfalse\n\tmsg := \"y\"\n}\n"}, ns: "plan.p", out: `[{"namespace":"plan.p","successes":1}]`, want: policyresult.ErrCount, code: 2},
		"名前空間の規則が1つも評価されなければ ErrEmpty で、終了コード 2":       {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: `[]`, want: policyresult.ErrEmpty, code: 2},
		"違反だけなら errCIPolicy で、終了コード 1":                 {files: map[string]string{"p.rego": onePolicy}, ns: "plan.p", out: `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"bad"}]}]`, want: errCIPolicy, code: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, _ := fakeFlow(t, map[string]string{stepCIConftest + ".out": c.out})
			s := ciState{Root: "admin-identity", Out: fileWith(t, ""), JSON: fileWith(t, "{}")}
			err := ciRunPolicy(s, deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-policy", policyDirWith(t, c.files), fileWith(t, "{}"), fileWith(t, "{}"), []string{c.ns})
			require.ErrorIs(t, err, c.want)
			if c.code != 0 {
				assert.Equal(t, c.code, exitCode(err))
			}
		})
	}

	t.Run("違反の名前空間と評価できなかった名前空間が混ざれば、評価できなかった側を優先して終了コード 2", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{stepCIConftest + ".out": `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"bad"}]}]`})
		s := ciState{Root: "admin-identity", Out: fileWith(t, ""), JSON: fileWith(t, "{}")}
		files := map[string]string{"p.rego": onePolicy, "q.rego": strings.Replace(onePolicy, "plan.p", "plan.q", 1)}
		for _, order := range [][]string{{"plan.p", "plan.q"}, {"plan.q", "plan.p"}} {
			err := ciRunPolicy(s, deps{tool: tool, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-policy", policyDirWith(t, files), fileWith(t, "{}"), fileWith(t, "{}"), order)
			require.ErrorIs(t, err, policyresult.ErrEmpty, order)
			assert.NotErrorIs(t, err, errCIPolicy, order)
			assert.Equal(t, 2, exitCode(err), order)
			assert.Contains(t, err.Error(), "plan.p", order)
			assert.Contains(t, err.Error(), "plan.q", order)
		}
	})

	t.Run("conftest を起動できなければ ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		s := ciState{Root: "admin-identity", Out: fileWith(t, ""), JSON: fileWith(t, "{}")}
		err := ciRunPolicy(s, deps{tool: tooltest.Missing, stderr: &bytes.Buffer{}, root: t.TempDir()}, &bytes.Buffer{}, "tmp/ci-policy", policyDirWith(t, map[string]string{"p.rego": onePolicy}), fileWith(t, "{}"), fileWith(t, "{}"), []string{"plan.p"})
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("違反の文と conftest の stderr はログへ出さず全文へ置き、規則の ID と件数だけを出す", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{
			stepCIConftest + ".out":    `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"[PLAN-P-01] ` + account + ` が書ける"}]}]`,
			stepCIConftest + ".stderr": "warn " + account + "\n",
		})
		s := ciState{Root: "admin-identity", Out: fileWith(t, ""), JSON: fileWith(t, "{}")}
		var stdout, stderr bytes.Buffer
		err := ciRunPolicy(s, deps{tool: tool, stderr: &stderr, root: t.TempDir()}, &stdout, "tmp/ci-policy", policyDirWith(t, map[string]string{"p.rego": onePolicy}), fileWith(t, "{}"), fileWith(t, "{}"), []string{"plan.p"})
		require.ErrorIs(t, err, errCIPolicy)
		assert.Contains(t, stdout.String(), "診断: kind=policy; rule=[PLAN-P-01]; namespace=plan.p; count=1\n")
		assert.Contains(t, stdout.String(), "conftest の stderr（1 行）は全文にあります")
		assert.NotContains(t, stdout.String()+stderr.String()+err.Error(), account)
		raw, rerr := os.ReadFile(s.Out)
		require.NoError(t, rerr)
		assert.Contains(t, string(raw), "warn "+account)
		assert.Contains(t, string(raw), "plan.p: [PLAN-P-01] "+account+" が書ける")
	})
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
		require.NoError(t, f.step("ci-policy", "-state", f.state, "-work", "tmp/ci-policy", "-policy", f.policy, "-decl", fileWith(t, "{}"), "-layer", fileWith(t, "{}"), "-namespace", "plan.p"))
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

	t.Run("apply の失敗の診断を、apply が書いた区間からだけ出す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepCIApply + ".rc": "1", stepCIApply + ".out": `{"@level":"error","@message":"Error: creating IAM Role","type":"diagnostic","diagnostic":{"severity":"error",` +
			`"summary":"creating IAM Role (x): operation error IAM: CreateRole, https response error StatusCode: 403, RequestID: r, ` +
			`api error AccessDenied: User: arn:aws:sts::` + account + `:assumed-role/tabp-admin-ci-apply-identity/gha is not authorized ` +
			`to perform: iam:CreateRole on resource: x because no identity-based policy allows the iam:CreateRole action",` +
			`"detail":"","address":"aws_iam_role.this[\"` + account + `\"]"}}` + "\n"})
		require.Error(t, f.all(ciModeApply, "identity"))
		assert.Equal(t, 1, strings.Count(f.stdout.String(), "診断: kind=iam-deny"), f.stdout.String())
		assert.Contains(t, readKnob(t, filepath.Dir(f.output), "output"), "failure=apply\n")
		f.public()
	})
}

func Test_ciRunApply(t *testing.T) {
	t.Parallel()

	t.Run("起動できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		tool := func(ctx context.Context, _ string, _ []string, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "none"))
		}
		_, err := ciRunApply(ciState{Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: tool}, time.Second)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})

	shTool := func(script string) toolFunc {
		return func(ctx context.Context, dir string, env []string, _ string, _ ...string) *exec.Cmd {
			c := exec.CommandContext(ctx, "/bin/sh", "-c", script)
			c.Dir, c.Env = dir, env
			return c
		}
	}

	t.Run("打ち切りの割り込みを無視して 0 で終わった apply は timeout にせず 0 を返す", func(t *testing.T) {
		t.Parallel()
		code, err := ciRunApply(ciState{Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: shTool(`trap '' INT; sleep 0.3; exit 0`)}, 50*time.Millisecond)
		require.NoError(t, err)
		assert.Zero(t, code)
	})

	t.Run("割り込みで落ちた apply は applyTimedOut を返す", func(t *testing.T) {
		t.Parallel()
		code, err := ciRunApply(ciState{Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: shTool(`trap 'exit 1' INT; while :; do sleep 0.05; done`)}, 50*time.Millisecond)
		require.NoError(t, err)
		assert.Equal(t, applyTimedOut, code)
	})

	t.Run("期限の内に 0 以外で終わった apply は timeout にせず、その終了コードを返す", func(t *testing.T) {
		t.Parallel()
		code, err := ciRunApply(ciState{Dir: t.TempDir(), Out: fileWith(t, "")}, deps{tool: shTool(`exit 1`)}, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, 1, code)
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

	t.Run("aws の stderr は、許可の文法に当たる欄だけをログへ出す", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeFlow(t, map[string]string{
			stepCIPutObject + ".rc": "254",
			stepCIPutObject + ".stderr": "\naws: [ERROR]: An error occurred (AccessDenied) when calling the PutObject operation: User: arn:aws:sts::" +
				account + ":assumed-role/x is not authorized to perform: s3:PutObject\n",
		})
		var stderr bytes.Buffer
		s := ciState{Out: fileWith(t, "x"), OutBucket: "b", Key: "k", Account: account, Region: "ap-northeast-1"}
		require.Error(t, ciRunUpload(&s, deps{tool: tool, stderr: &stderr, root: t.TempDir()}, &bytes.Buffer{}))
		assert.Equal(t, "診断: kind=cli; operation=PutObject; code=AccessDenied; action=s3:PutObject\n", stderr.String())
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

func Test_waitCode(t *testing.T) {
	t.Parallel()

	t.Run("終了コードを返し、シグナルや待てない失敗は ErrTool", func(t *testing.T) {
		t.Parallel()
		code, err := waitCode(nil)
		require.NoError(t, err)
		assert.Zero(t, code)
		code, err = waitCode(exec.CommandContext(context.Background(), "/bin/sh", "-c", "exit 3").Run())
		require.NoError(t, err)
		assert.Equal(t, 3, code)
		_, err = waitCode(exec.CommandContext(context.Background(), "/bin/sh", "-c", "kill -9 $$").Run())
		require.ErrorIs(t, err, toolrun.ErrTool)
		_, err = waitCode(errBoom)
		require.ErrorIs(t, err, toolrun.ErrTool)
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

	for name, c := range map[string]struct {
		s    ciState
		want []string
	}{
		"plan の入口は apply の段を要らない":      {ciState{Mode: ciModePlan}, []string{stagePrepare, stageInit, stagePlan, stagePolicy, stageUpload}},
		"apply の入口は apply の段を要る":       {ciState{Mode: ciModeApply}, []string{stagePrepare, stageInit, stagePlan, stagePolicy, stageApply, stageUpload}},
		"権限の段が要る入口は、Policy の後に権限の段を要る": {ciState{Mode: ciModePlan, Permissions: true}, []string{stagePrepare, stageInit, stagePlan, stagePolicy, stagePermissions, stageUpload}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, ciRequired(c.s))
		})
	}
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

func Test_isVerdict(t *testing.T) {
	t.Parallel()

	t.Run("判定の失敗は真、成り立たない失敗と nil は偽", func(t *testing.T) {
		t.Parallel()
		assert.True(t, isVerdict(xerrors.Wrap(errPlanFailed, "x")))
		assert.True(t, isVerdict(policyresult.ErrViolation))
		assert.False(t, isVerdict(toolrun.ErrTool))
		assert.False(t, isVerdict(errCIInit))
		assert.False(t, isVerdict(nil))
	})
}

// summaryLine は、plan の stream の change_summary の1行。
func summaryLine(add, chg int) string {
	return `{"@level":"info","type":"change_summary","changes":{"add":` + strconv.Itoa(add) + `,"change":` + strconv.Itoa(chg) +
		`,"import":0,"remove":0,"operation":"plan"}}` + "\n"
}
