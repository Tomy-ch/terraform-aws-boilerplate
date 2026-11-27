package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const callerIdentity = "123456789012\tarn:aws:sts::123456789012:assumed-role/x/y\n"

// fakeLocalTool は、段ごとの出力を outs から返す偽の道具を作る。起動ごとに「段 作業ディレクトリ」と、そのときの環境の
// AWS_PROFILE・AWS_REGION・TF_CLI_ARGS_init と、AWS_ で始まる余計な変数の有無を calls.log へ書く。fail に入れた段は
// 1 で終わる。偽の道具は /bin/sh に読ませて起動する（書いたばかりのファイルの exec は ETXTBSY で揺れる）。
func fakeLocalTool(t *testing.T, outs map[string]string, fail ...string) (toolFunc, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	for step, out := range outs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, step+".out"), []byte(out), 0o600))
	}
	for _, step := range fail {
		require.NoError(t, os.WriteFile(filepath.Join(dir, step+".fail"), nil, 0o600))
	}
	script := filepath.Join(dir, "tool")
	body := `#!/bin/sh
step="$1"
extra=$(env | grep '^AWS_' | grep -vE '^AWS_(PROFILE|REGION|EC2_METADATA_DISABLED)=' | cut -d= -f1 | tr '\n' ' ')
echo "$step $PWD profile=${AWS_PROFILE:-} region=${AWS_REGION:-} init=${TF_CLI_ARGS_init:-} cfg=${TF_CLI_CONFIG_FILE:-} extra=$extra" >> '` + log + `'
if [ "$step" = s3api-get-bucket-encryption ]; then cat input.json >> '` + log + `'; echo >> '` + log + `'; fi
if [ -e '` + dir + `'/"$step.out" ]; then cat '` + dir + `'/"$step.out"; fi
if [ -e '` + dir + `'/"$step.fail" ]; then echo "$step が落ちた" >&2; exit 1; fi
exit 0
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	tool := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, "/bin/sh", script, step)
		c.Dir, c.Env = d, env
		return c
	}
	calls := func() []string {
		raw, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}
	return tool, calls
}

func localBase(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	dir := filepath.Join(base, "env", "admin", "identity")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(backendOK), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "providers.tf"), []byte(providersOK), 0o600))
	return base
}

func localDeps(tool toolFunc, environ ...string) (deps, *bytes.Buffer) {
	var stderr bytes.Buffer
	return deps{readFile: os.ReadFile, environ: environ, git: stubGit("", ""), now: time.Now, tool: tool, stderr: &stderr}, &stderr
}

func prepareArgs(base, out string, extra ...string) []string {
	return append([]string{"-project", "tabp", "-profile", "tabp-bootstrap", "-env", "admin", "-root", "identity", "-base", base, "-out", out}, extra...)
}

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepCallerIdentity: {"aws", "sts", "get-caller-identity", "--query", "[Account,Arn]", "--output", "text"},
		stepListAliases:    {"aws", "kms", "list-aliases", "--output", "json"},
		stepBucketEncrypt:  {"aws", "s3api", "get-bucket-encryption", "--cli-input-json", "file://input.json", "--output", "json"},
		stepLocalInit:      {"terraform", "init", "-input=false", "-lockfile=readonly"},
		stepLocalPlan:      {"terraform", "plan", "-lock=false", "-input=false", "-no-color"},
		stepCIInit:         {"terraform", "init", "-input=false", "-no-color", "-json", "-lockfile=readonly"},
		stepCIPlanNoLock:   {"terraform", "plan", "-lock=false", "-input=false", "-no-color", "-json", "-detailed-exitcode"},
		stepCIPlanLock:     {"terraform", "plan", "-input=false", "-no-color", "-json", "-detailed-exitcode"},
		stepCIShow:         {"terraform", "show"},
		stepCIShowText:     {"terraform", "show"},
		stepCIApply:        {"terraform", "apply"},
	} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			c := command(context.Background(), "/x", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

func Test_commandAt(t *testing.T) {
	t.Parallel()

	t.Run("bootstrap-check の段は root の tmp/bin の bootstrap-check を起動し、dir と env を渡す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		c := commandAt(root)(context.Background(), "/x", []string{"A=1"}, stepCIPermissions, "tabp", "ap-northeast-1")
		require.NoError(t, c.Err)
		assert.Equal(t, filepath.Join(root, "tmp", "bin", "bootstrap-check"), c.Path)
		assert.Equal(t, "/x", c.Dir)
		assert.Equal(t, []string{"A=1"}, c.Env)
	})

	t.Run("ほかの段は command と同じ", func(t *testing.T) {
		t.Parallel()
		c := commandAt(t.TempDir())(context.Background(), "/x", []string{"A=1"}, stepListAliases)
		assert.Equal(t, []string{"aws", "kms", "list-aliases", "--output", "json"}, c.Args)
	})
}

func Test_permissionsCommand(t *testing.T) {
	t.Parallel()

	t.Run("root の tmp/bin の bootstrap-check を、project と region を option の値にした固定の引数で起動する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		c := permissionsCommand(context.Background(), root, []string{"tabp", "ap-northeast-1"})
		require.NoError(t, c.Err)
		assert.Equal(t, []string{filepath.Join(root, "tmp", "bin", "bootstrap-check"), "plan-permissions", "-plan", "plan.json", "-project=tabp", "-region=ap-northeast-1"}, c.Args)
	})

	t.Run("root が相対パスでも、絶対パスの bootstrap-check を起動する", func(t *testing.T) {
		t.Parallel()
		c := permissionsCommand(context.Background(), ".", []string{"tabp", "ap-northeast-1"})
		require.NoError(t, c.Err)
		assert.True(t, filepath.IsAbs(c.Path), c.Path)
	})

	for name, ops := range map[string][]string{"operand が無い": nil, "operand が1つ": {"tabp"}, "operand が3つ": {"tabp", "r", "x"}} {
		t.Run(name+"なら、起動せずに道具を起動できない（ErrTool、終了コード 2）", func(t *testing.T) {
			t.Parallel()
			c := permissionsCommand(context.Background(), t.TempDir(), ops)
			require.Error(t, c.Err)
			_, err := toolrun.Exec(c)
			require.ErrorIs(t, err, toolrun.ErrTool)
			assert.Equal(t, 2, exitCode(err))
		})
	}

	t.Run("command で bootstrap-check の段を組めば、起動せずに ErrTool", func(t *testing.T) {
		t.Parallel()
		_, err := toolrun.Exec(command(context.Background(), "/x", nil, stepCIPermissions, "tabp", "ap-northeast-1"))
		require.ErrorIs(t, err, toolrun.ErrTool)
	})
}

func Test_localEnv(t *testing.T) {
	t.Parallel()

	t.Run("資格情報と引数を変える変数を外し、bootstrap の profile と provider の差し替えの封じを足す", func(t *testing.T) {
		t.Parallel()
		got := localEnv([]string{"PATH=/bin", "AWS_ACCESS_KEY_ID=x", "TF_CLI_ARGS_plan=-target=a", "TF_VAR_x=1", "TF_DATA_DIR=/d", "HOME=/h"}, "p", "X=1")
		assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "AWS_PROFILE=p", "TF_CLI_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1", "X=1"}, got)
	})
}

func Test_localRoot(t *testing.T) {
	t.Parallel()

	t.Run("identity と workload の root の場所", func(t *testing.T) {
		t.Parallel()
		got, err := localRoot("b", "admin", "identity")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("b", "env", "admin", "identity"), got)
		got, err = localRoot("b", "admin", "workload")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("b", "env", "admin"), got)
	})

	t.Run("形の違う env と root は失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := localRoot("b", "Admin/..", "identity")
		require.ErrorIs(t, err, errToken)
		_, err = localRoot("b", "admin", "x")
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_runTool(t *testing.T) {
	t.Parallel()

	t.Run("stdout を返し、落ちれば errLocalAWS", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, map[string]string{"a": "out"}, "b")
		got, err := runTool(tool, t.TempDir(), nil, "a", &bytes.Buffer{})
		require.NoError(t, err)
		assert.Equal(t, "out", string(got))
		_, err = runTool(tool, t.TempDir(), nil, "b", &bytes.Buffer{})
		require.ErrorIs(t, err, errLocalAWS)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("起動できなければ errLocalAWS ではなく ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		_, err := runTool(tooltest.Missing, t.TempDir(), nil, "a", &bytes.Buffer{})
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.NotErrorIs(t, err, errLocalAWS)
		assert.Equal(t, 2, exitCode(err))
	})
}

func Test_localPrepare(t *testing.T) {
	t.Parallel()

	outs := func(aliases, encryption string) map[string]string {
		return map[string]string{stepCallerIdentity: callerIdentity, stepListAliases: aliases, stepBucketEncrypt: encryption}
	}

	t.Run("aws だけを起動し、Bucket 名と state の鍵を init の値として書く", func(t *testing.T) {
		t.Parallel()
		base := localBase(t)
		out := filepath.Join(t.TempDir(), "local", "admin-identity.json")
		tool, calls := fakeLocalTool(t, outs(withAlias, encKMS(stateKeyARN)))
		d, _ := localDeps(tool, "AWS_ACCESS_KEY_ID=x", "PATH=/bin")
		_, err := localPrepare(prepareArgs(base, out), d, d.stderr)
		require.NoError(t, err)

		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		var s localState
		require.NoError(t, json.Unmarshal(raw, &s))
		assert.Equal(t, filepath.Join(base, "env", "admin", "identity"), s.Dir)
		assert.Equal(t, []string{"-backend-config=bucket=tabp-admin-tfstate-123456789012-ap-northeast-1-an", "-backend-config=kms_key_id=" + stateKeyARN}, s.InitArgs)
		info, err := os.Stat(out)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "アカウント ID を含むので所有者だけが読める")

		got := calls()
		require.Len(t, got, 4)
		for _, c := range []string{got[0], got[1], got[2]} {
			assert.Contains(t, c, "profile=tabp-bootstrap region=ap-northeast-1")
			assert.Contains(t, c, "cfg=/dev/null")
			assert.True(t, strings.HasSuffix(c, " extra="), "呼び出し元の AWS の資格情報を渡さない: %s", c)
		}
		assert.True(t, strings.HasPrefix(got[0], stepCallerIdentity))
		assert.True(t, strings.HasPrefix(got[1], stepListAliases))
		assert.True(t, strings.HasPrefix(got[2], stepBucketEncrypt))
		assert.JSONEq(t, `{"Bucket":"tabp-admin-tfstate-123456789012-ap-northeast-1-an","ExpectedBucketOwner":"123456789012"}`, got[3])
	})

	t.Run("鍵が無く sse-s3 を選べば、鍵を渡さない値を書き、そう告げる", func(t *testing.T) {
		t.Parallel()
		base := localBase(t)
		out := filepath.Join(t.TempDir(), "s.json")
		tool, _ := fakeLocalTool(t, outs(withoutAlias, encAES256))
		d, _ := localDeps(tool)
		_, err := localPrepare(prepareArgs(base, out, "-missing", "sse-s3"), d, d.stderr)
		require.NoError(t, err)
		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		var s localState
		require.NoError(t, json.Unmarshal(raw, &s))
		assert.Equal(t, []string{"-backend-config=bucket=tabp-admin-tfstate-123456789012-ap-northeast-1-an"}, s.InitArgs)
	})

	for name, c := range map[string]struct {
		outs map[string]string
		fail []string
		want error
	}{
		"鍵が無ければ既定では失敗する":            {outs: outs(withoutAlias, encAES256), want: errStateKeyMissing},
		"呼び出し元を取れなければ失敗する":          {outs: outs(withAlias, encKMS(stateKeyARN)), fail: []string{stepCallerIdentity}, want: errLocalAWS},
		"alias の一覧を取れなければ失敗する":      {outs: outs(withAlias, encKMS(stateKeyARN)), fail: []string{stepListAliases}, want: errLocalAWS},
		"Bucket の既定の暗号化を取れなければ失敗する": {outs: outs(withAlias, encKMS(stateKeyARN)), fail: []string{stepBucketEncrypt}, want: errLocalAWS},
		"alias と既定の鍵が違えば失敗する":       {outs: outs(withAlias, encKMS(otherKeyARN)), want: errStateKeyMismatch},
	} {
		t.Run(name+"し、値を書かない", func(t *testing.T) {
			t.Parallel()
			base := localBase(t)
			out := filepath.Join(t.TempDir(), "s.json")
			tool, _ := fakeLocalTool(t, c.outs, c.fail...)
			d, _ := localDeps(tool)
			_, err := localPrepare(prepareArgs(base, out), d, d.stderr)
			require.ErrorIs(t, err, c.want)
			assert.NoFileExists(t, out)
		})
	}

	t.Run("root の region を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localPrepare(prepareArgs(t.TempDir(), filepath.Join(t.TempDir(), "s.json")), d, d.stderr)
		require.ErrorIs(t, err, errBackendMissing)
		assert.Empty(t, calls())
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localPrepare([]string{"-project", "tabp"}, d, d.stderr)
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_localInit(t *testing.T) {
	t.Parallel()

	writeState := func(t *testing.T, s localState) string {
		t.Helper()
		raw, err := json.Marshal(s)
		require.NoError(t, err)
		p := filepath.Join(t.TempDir(), "s.json")
		require.NoError(t, os.WriteFile(p, raw, 0o600))
		return p
	}

	t.Run("terraform だけを起動し、init の値を TF_CLI_ARGS_init で渡す", func(t *testing.T) {
		t.Parallel()
		root := localBase(t)
		dir := filepath.Join(root, "env", "admin", "identity")
		tool, calls := fakeLocalTool(t, nil)
		d, _ := localDeps(tool, "TF_CLI_ARGS_init=-reconfigure", "AWS_SECRET_ACCESS_KEY=x")
		p := writeState(t, localState{Dir: dir, InitArgs: []string{"-backend-config=bucket=b", "-backend-config=kms_key_id=k"}})
		_, err := localInit([]string{"-profile", "tabp-bootstrap", "-state", p}, d, &bytes.Buffer{}, d.stderr)
		require.NoError(t, err)
		got := calls()
		require.Len(t, got, 1)
		assert.Equal(t, stepLocalInit+" "+dir+" profile=tabp-bootstrap region= init=-backend-config=bucket=b -backend-config=kms_key_id=k cfg=/dev/null extra=", got[0], "呼び出し元の TF_CLI_ARGS_init と AWS の資格情報を渡さない")
	})

	t.Run("init が 0 以外なら errLocalInit で、終了コード 2（値を決められない側）", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, nil, stepLocalInit)
		d, _ := localDeps(tool)
		p := writeState(t, localState{Dir: t.TempDir(), InitArgs: []string{"-backend-config=bucket=b"}})
		_, err := localInit([]string{"-profile", "p", "-state", p}, d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errLocalInit)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("init を起動できなければ ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		d, _ := localDeps(tooltest.Missing)
		p := writeState(t, localState{Dir: t.TempDir(), InitArgs: []string{"-backend-config=bucket=b"}})
		_, err := localInit([]string{"-profile", "p", "-state", p}, d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})

	for name, body := range map[string]string{
		"JSON でない":   "x",
		"dir が無い":    `{"init_args":["-backend-config=bucket=b"]}`,
		"init の値が無い": `{"dir":"/d","init_args":[]}`,
	} {
		t.Run(name+"の state は terraform を起動せずに errLocalState", func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "s.json")
			require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
			tool, calls := fakeLocalTool(t, nil)
			d, _ := localDeps(tool)
			_, err := localInit([]string{"-profile", "p", "-state", p}, d, &bytes.Buffer{}, d.stderr)
			require.ErrorIs(t, err, errLocalState)
			assert.Empty(t, calls())
		})
	}

	t.Run("state が無ければ errLocalState", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localInit([]string{"-profile", "p", "-state", filepath.Join(t.TempDir(), "none")}, d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errLocalState)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localInit([]string{"-profile", "p"}, d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_localPlan(t *testing.T) {
	t.Parallel()

	planArgs := func(base, header, log string) []string {
		return []string{"-profile", "tabp-bootstrap", "-env", "admin", "-root", "identity", "-base", base, "-header", header, "-log", log}
	}

	t.Run("plan を起動できなければ ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		base := localBase(t)
		work := t.TempDir()
		header := filepath.Join(work, "h")
		require.NoError(t, os.WriteFile(header, []byte("commit: "+commitSHA+"\n"), 0o600))
		d, _ := localDeps(tooltest.Missing)
		_, err := localPlan(planArgs(base, header, filepath.Join(work, "p.log")), d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.NotErrorIs(t, err, errLocalPlan)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("lock を取らない plan を起動し、header と出力を log と端末へ書き、log を所有者だけの権限で作り直す", func(t *testing.T) {
		t.Parallel()
		base := localBase(t)
		work := t.TempDir()
		header := filepath.Join(work, "h")
		require.NoError(t, os.WriteFile(header, []byte("commit: "+commitSHA+"\n"), 0o600))
		log := filepath.Join(work, "plan", "admin-identity.log")
		require.NoError(t, os.MkdirAll(filepath.Dir(log), 0o700))
		require.NoError(t, os.WriteFile(log, []byte("前の log"), 0o644))
		tool, calls := fakeLocalTool(t, map[string]string{stepLocalPlan: "No changes.\n"})
		d, _ := localDeps(tool)
		var stdout bytes.Buffer
		_, err := localPlan(planArgs(base, header, log), d, &stdout, d.stderr)
		require.NoError(t, err)
		want := "commit: " + commitSHA + "\nNo changes.\n"
		raw, err := os.ReadFile(log)
		require.NoError(t, err)
		assert.Equal(t, want, string(raw))
		assert.Equal(t, want, stdout.String())
		info, err := os.Stat(log)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		require.Len(t, calls(), 1)
		assert.True(t, strings.HasPrefix(calls()[0], stepLocalPlan+" "+filepath.Join(base, "env", "admin", "identity")+" profile=tabp-bootstrap"))
	})

	t.Run("plan が 0 以外なら errLocalPlan で終了コード 1 になり、出力は log に残る", func(t *testing.T) {
		t.Parallel()
		base := localBase(t)
		work := t.TempDir()
		header := filepath.Join(work, "h")
		require.NoError(t, os.WriteFile(header, []byte("commit: x"), 0o600))
		log := filepath.Join(work, "p.log")
		tool, _ := fakeLocalTool(t, map[string]string{stepLocalPlan: "Error: x\n"}, stepLocalPlan)
		d, _ := localDeps(tool)
		_, err := localPlan(planArgs(base, header, log), d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errLocalPlan)
		assert.Equal(t, 1, exitCode(err))
		raw, err := os.ReadFile(log)
		require.NoError(t, err)
		assert.Contains(t, string(raw), "Error: x")
	})

	t.Run("header を読めなければ terraform を起動しない", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localPlan(planArgs(localBase(t), filepath.Join(t.TempDir(), "none"), filepath.Join(t.TempDir(), "l")), d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errLocalLog)
		assert.Empty(t, calls())
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeLocalTool(t, nil)
		d, _ := localDeps(tool)
		_, err := localPlan([]string{"-profile", "p"}, d, &bytes.Buffer{}, d.stderr)
		require.ErrorIs(t, err, errUsage)
	})
}
