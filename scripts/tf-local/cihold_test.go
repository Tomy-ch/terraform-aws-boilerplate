package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const heldOutput = "a.b: Refreshing state... [id=x]\n"

func noLock() string { return lockList(lockKeyOK, false, 0) }

func withLock() string { return lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, lockETagA)) }

// lockKnobs は、lock が1つ残っている状態の偽の道具の設定を返す。modified は lock file の LastModified。
func lockKnobs(modified time.Time, op string) map[string]string {
	return map[string]string{
		stepS3List + ".out":      withLock(),
		stepS3GetObject + ".out": lockMeta(lockETagA, modified),
		"lock.body":              lockBodyOp(lockID, "runner@fv-az1-2", op),
		stepS3Delete + ".out":    `{"VersionId":"marker-1"}`,
	}
}

func unlockArgs(f *flow) []string {
	return []string{"-project", "tabp", "-env", "admin", "-root", "identity", "-profile", "tabp-admin-ci-unlock-identity", "-base", f.base}
}

func Test_ciUnlockStep(t *testing.T) {
	t.Parallel()

	stale := lockNow.Add(-(applyJobTimeout + staleLockMargin + time.Minute))

	t.Run("古い lock を、読んだ版の ETag を条件に1回だけ消し、terraform を起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, lockKnobs(stale, applyOperation))
		f.d.now = func() time.Time { return lockNow }
		require.NoError(t, f.step("ci-unlock", unlockArgs(f)...))
		calls := f.calls()
		assert.NotContains(t, calls, "terraform")
		assert.Contains(t, calls, stepS3List+" ")
		assert.Equal(t, 1, strings.Count(calls, stepS3Delete+" "))
		assert.NotContains(t, calls, "profile=outer")
		want := `{"Bucket":"tabp-admin-tfstate-` + account + `-ap-northeast-1-an","Key":"` + lockKeyOK + `","IfMatch":` + `"\"0123456789abcdef0123456789abcdef\""` + `,"ExpectedBucketOwner":"` + account + `"}`
		assert.JSONEq(t, want, strings.TrimSpace(readKnob(t, f.k, "get.log")))
		assert.JSONEq(t, want, strings.TrimSpace(readKnob(t, f.k, "delete.log")))
		assert.Contains(t, f.stdout.String(), "ID="+lockID)
		assert.Contains(t, f.stdout.String(), "✅ lock を外しました（delete marker の版 ID: marker-1）")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "lock を外しました（delete marker の版 ID: `marker-1`）")
		assert.NotContains(t, f.stdout.String(), "secret-info")
	})

	t.Run("lock が無ければ何も消さずに成功し、job summary に書く", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepS3List + ".out": noLock()})
		require.NoError(t, f.step("ci-unlock", unlockArgs(f)...))
		assert.NotContains(t, f.calls(), stepS3GetObject)
		assert.NotContains(t, f.calls(), stepS3Delete)
		assert.Contains(t, f.stdout.String(), "✅ lock はありません。何も消しません")
		assert.Equal(t, "### admin-identity\n\nlock はありません\n", readKnob(t, filepath.Dir(f.summary), "summary"))
	})

	for name, c := range map[string]struct {
		knobs map[string]string
		want  error
	}{
		"新しい lock は消さない": {knobs: lockKnobs(lockNow.Add(-time.Minute), applyOperation), want: errLockFresh},
		"人の手元の lock は消さない": {knobs: func() map[string]string {
			k := lockKnobs(stale, applyOperation)
			k["lock.body"] = lockBody(lockID, "me@laptop")
			return k
		}(), want: errLockForeign},
		"一覧を読めなければ消さない": {knobs: map[string]string{stepS3List + ".out": "x"}, want: errLockList},
		"lock file を読めなければ消さない": {knobs: func() map[string]string {
			k := lockKnobs(stale, applyOperation)
			k[stepS3GetObject+".rc"] = "1"
			return k
		}(), want: errLocalAWS},
		"呼び出し元を取れなければ一覧を読まない": {knobs: map[string]string{stepCallerIdentity + ".rc": "1"}, want: errLocalAWS},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, c.knobs)
			f.d.now = func() time.Time { return lockNow }
			require.ErrorIs(t, f.step("ci-unlock", unlockArgs(f)...), c.want)
			assert.NotContains(t, f.calls(), stepS3Delete)
		})
	}

	t.Run("delete-object の応答を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		k := lockKnobs(stale, applyOperation)
		k[stepS3Delete+".out"] = "x"
		f := newFlow(t, k)
		f.d.now = func() time.Time { return lockNow }
		require.ErrorIs(t, f.step("ci-unlock", unlockArgs(f)...), errLockList)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.ErrorIs(t, f.step("ci-unlock", "-env", "admin"), errUsage)
	})
}

func Test_ciLockTarget(t *testing.T) {
	t.Parallel()

	t.Run("root の backend.tf の key から lock file の key を、呼び出し元から Bucket を求める", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		l, err := ciLockTarget(f.d, t.TempDir(), "tabp", "admin", "workload", "p", f.base)
		require.NoError(t, err)
		assert.Equal(t, "admin-workload", l.root)
		assert.Equal(t, lockKeyOK, l.key)
		assert.Equal(t, account, l.account)
		assert.Equal(t, "tabp-admin-tfstate-"+account+"-ap-northeast-1-an", l.bucket)
	})

	t.Run("形の違う root は aws を起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		_, err := ciLockTarget(f.d, t.TempDir(), "tabp", "admin", "x", "p", f.base)
		require.ErrorIs(t, err, errUsage)
		assert.Empty(t, f.calls())
	})
}

func Test_s3Call(t *testing.T) {
	t.Parallel()

	t.Run("input.json を所有者だけが読める権限で書いて起動する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		work := t.TempDir()
		_, err := s3Call(f.d, work, nil, stepS3Delete, map[string]string{"Key": "k"})
		require.NoError(t, err)
		info, err := os.Stat(filepath.Join(work, "input.json"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		assert.JSONEq(t, `{"Key":"k"}`, strings.TrimSpace(readKnob(t, f.k, "delete.log")))
	})
}

func Test_lockTarget_etag(t *testing.T) {
	t.Parallel()

	t.Run("lock が無ければ空、あれば ETag を返す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepS3List + ".1.out": noLock(), stepS3List + ".2.out": withLock()})
		l := lockTarget{key: lockKeyOK}
		got, err := l.etag(f.d, t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, got)
		got, err = l.etag(f.d, t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, lockETagA, got)
	})
}

func Test_lockTarget_read(t *testing.T) {
	t.Parallel()

	t.Run("get-object が lock file を書かなければ失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepS3GetObject + ".out": "{}"})
		_, _, err := lockTarget{key: lockKeyOK}.read(f.d, t.TempDir(), lockETagA)
		require.ErrorIs(t, err, errLockInfo)
	})
}

// hold は、lock を残す入口の段を make と同じ順に起動する。どの段が落ちてもそこで止まる。
func (f *flow) hold(extra ...string) error {
	if err := f.step("ci-prepare", "-mode", ciModeHold, "-project", "tabp", "-env", "admin", "-root", "identity",
		"-profile", "tabp-admin-ci-apply-identity", "-base", f.base, "-header", f.header, "-state", f.state); err != nil {
		return err
	}
	for _, step := range []string{"ci-init", "ci-hold-absent"} {
		if err := f.step(step, "-state", f.state); err != nil {
			return err
		}
	}
	if err := f.step("ci-hold-plan", append([]string{"-state", f.state, "-interval", "20ms", "-limit", "5s"}, extra...)...); err != nil {
		return err
	}

	return f.step("ci-hold-verify", "-state", f.state)
}

func holdKnobs(extra map[string]string) map[string]string {
	k := lockKnobs(lockNow.Add(-time.Minute), planOperation)
	k[stepS3List+".1.out"] = noLock()
	k[stepCIPlanHold+".out"] = heldOutput
	k["hold.sleep"] = ""
	for name, v := range extra {
		k[name] = v
	}
	return k
}

func Test_ciHoldVerifyStep(t *testing.T) {
	t.Parallel()

	t.Run("lock を取る plan を起動し、lock を取った後に止め、残った lock の形と消せる時刻を出して、全文を消す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(nil))
		f.d.now = func() time.Time { return lockNow }
		require.NoError(t, f.hold())
		calls := f.calls()
		assert.Contains(t, calls, stepCIPlanHold+" identity profile=tabp-admin-ci-apply-identity")
		assert.NotContains(t, calls, stepCIApply)
		assert.NotContains(t, calls, stepCIPutObject)
		assert.NotContains(t, calls, stepS3Delete)
		assert.Less(t, strings.Index(calls, stepS3List), strings.Index(calls, stepCIPlanHold), "plan の前に lock の無いことを確かめる")
		assert.Contains(t, f.stdout.String(), "Terraform Unlock が消せるのは")
		assert.Contains(t, f.stdout.String(), "✅ lock を残しました。外すのは Terraform Unlock です")
		assert.Contains(t, readKnob(t, filepath.Dir(f.summary), "summary"), "lock を残しました")
		f.public()
		f.leftovers()
		assert.NoFileExists(t, holdPath(f.state))
	})

	t.Run("lock が残っていなければ失敗し、Terraform Unlock の案内を出す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(map[string]string{stepS3List + ".2.out": noLock()}))
		f.d.now = func() time.Time { return lockNow }
		err := f.hold()
		require.ErrorIs(t, err, errLockMissing)
		require.ErrorIs(t, err, errHoldStarted)
		f.leftovers()
	})

	t.Run("lock の解除が消せない形の lock が残れば失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(map[string]string{"lock.body": lockBody(lockID, "me@laptop")}))
		f.d.now = func() time.Time { return lockNow }
		require.ErrorIs(t, f.hold(), errLockForeign)
	})

	t.Run("plan が終わっていなければ失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(nil))
		require.NoError(t, f.step("ci-prepare", "-mode", ciModeHold, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		require.ErrorIs(t, f.step("ci-hold-verify", "-state", f.state), errCIState)
		f.leftovers()
	})
}

func Test_ciHoldVerify(t *testing.T) {
	t.Parallel()

	t.Run("一覧を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, map[string]string{stepS3List + ".out": "x"})
		s := ciState{Dir: filepath.Join(f.base, "env", "admin", "identity"), InitArgs: []string{"-backend-config=bucket=b"}}
		_, err := ciHoldVerify(f.d, s)
		require.ErrorIs(t, err, errLockList)
	})
}

func Test_ciHoldAbsentStep(t *testing.T) {
	t.Parallel()

	t.Run("lock が既にあれば plan を起動せずに失敗し、全文を消す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(map[string]string{stepS3List + ".1.out": withLock()}))
		require.ErrorIs(t, f.hold(), errLockExists)
		assert.NotContains(t, f.calls(), stepCIPlanHold)
		f.leftovers()
	})

	t.Run("plan や apply の入口の state では起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
			"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
		require.NoError(t, f.step("ci-init", "-state", f.state))
		require.ErrorIs(t, f.step("ci-hold-absent", "-state", f.state), errCIState)
		assert.NotContains(t, f.calls(), stepS3List)
	})
}

func Test_ciHoldAbsent(t *testing.T) {
	t.Parallel()

	t.Run("lock file の key を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, nil)
		err := ciHoldAbsent(f.d, ciState{Dir: t.TempDir(), InitArgs: []string{"-backend-config=bucket=b"}})
		require.Error(t, err)
		assert.Empty(t, f.calls())
	})
}

func Test_ciHoldPlanStep(t *testing.T) {
	t.Parallel()

	t.Run("lock を取った後の行を出さずに plan が終われば失敗し、全文を消す", func(t *testing.T) {
		t.Parallel()
		k := holdKnobs(map[string]string{stepCIPlanHold + ".out": "Initializing...\n"})
		delete(k, "hold.sleep")
		f := newFlow(t, k)
		require.ErrorIs(t, f.hold(), errLockMissing)
		assert.NotContains(t, f.calls(), stepS3GetObject)
		f.leftovers()
		assert.NoFileExists(t, holdPath(f.state))
	})

	t.Run("待つ時間の内に行が出なければ止めて失敗し、Terraform Unlock の案内を出す", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(map[string]string{stepCIPlanHold + ".out": "Initializing...\n"}))
		err := f.hold("-limit", "100ms")
		require.ErrorIs(t, err, errLockWait)
		require.ErrorIs(t, err, errHoldStarted)
	})

	t.Run("正でない待つ時間は errUsage", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(nil))
		require.ErrorIs(t, f.hold("-interval", "0s"), errUsage)
		assert.NotContains(t, f.calls(), stepCIPlanHold)
	})
}

func Test_ciHoldPlan(t *testing.T) {
	t.Parallel()

	t.Run("起動できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		tool := func(context.Context, string, []string, string, ...string) *exec.Cmd {
			return exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "none"))
		}
		err := ciHoldPlan(deps{tool: tool}, ciState{Dir: t.TempDir()}, filepath.Join(t.TempDir(), "h"), time.Millisecond, time.Second)
		require.ErrorIs(t, err, errLocalTF)
	})
}

func Test_ciHoldTarget(t *testing.T) {
	t.Parallel()

	t.Run("state の init の値から Bucket を、root の backend.tf から key を読む", func(t *testing.T) {
		t.Parallel()
		root := rootWith(t, backendOK, providersOK)
		l, err := ciHoldTarget(testDeps(nil), ciState{Root: "admin-identity", Dir: root, Account: account, Region: "r", Profile: "p",
			InitArgs: []string{"-backend-config=bucket=b", "-backend-config=kms_key_id=k"}})
		require.NoError(t, err)
		assert.Equal(t, "b", l.bucket)
		assert.Equal(t, lockKeyOK, l.key)
		assert.Contains(t, l.env, "AWS_PROFILE=p")
		assert.Contains(t, l.env, "AWS_REGION=r")
	})
}

func Test_loadHoldState(t *testing.T) {
	t.Parallel()

	t.Run("init の値が無ければ全文を消して失敗する", func(t *testing.T) {
		t.Parallel()
		out := fileWith(t, "x")
		p := filepath.Join(t.TempDir(), "s.json")
		require.NoError(t, ciState{Mode: ciModeHold, Dir: "d", Out: out, Done: []string{stageInit}}.save(p))
		_, _, err := loadHoldState("x", []string{"-state", p}, testDeps(nil), stageInit, nil)
		require.ErrorIs(t, err, errCIState)
		assert.NoFileExists(t, out)
	})
}

func Test_holdPath(t *testing.T) {
	t.Parallel()

	t.Run("state の名前から plan の全文の置き場を決める", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/d/admin-identity.hold.txt", holdPath("/d/admin-identity.json"))
	})
}

func Test_removeInRoot(t *testing.T) {
	t.Parallel()

	t.Run("消し、無いファイルとディレクトリは何もしない", func(t *testing.T) {
		t.Parallel()
		p := fileWith(t, "x")
		removeInRoot(p)
		assert.NoFileExists(t, p)
		removeInRoot(p)
		removeInRoot(filepath.Join(t.TempDir(), "none", "f"))
	})
}

func Test_ciHoldPlan_output(t *testing.T) {
	t.Parallel()

	t.Run("plan の全文は公開されるログに出さない", func(t *testing.T) {
		t.Parallel()
		f := newFlow(t, holdKnobs(map[string]string{stepCIPlanHold + ".out": planSecret + "\n" + heldOutput}))
		f.d.now = func() time.Time { return lockNow }
		require.NoError(t, f.hold())
		assert.NotContains(t, f.stdout.String(), planSecret)
		var buf bytes.Buffer
		buf.WriteString(f.d.stderr.(*bytes.Buffer).String())
		assert.NotContains(t, buf.String(), planSecret)
	})
}
