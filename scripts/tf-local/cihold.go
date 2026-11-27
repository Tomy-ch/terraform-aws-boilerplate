package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// CI の lock の解除と、lock を残す入口の段（ci.mk の CI_UNLOCK と CI_HOLD）。1回の起動は1つの目的を持ち、その
// 目的の道具だけを固定の引数で起動する（ADR-0702 決定3・24）。
//
//	ci-unlock は aws だけを起動し、apply の job の timeout を超えて残った lock だけを、読んだ版の ETag を条件に消す。
//	terraform は起動しない。lock が無ければ何も消さずに成功する。
//	lock を残す入口は ci-prepare（-mode hold）・ci-init の後に、ci-hold-absent（aws）が lock の無いことを確かめ、
//	ci-hold-plan（terraform）が lock を取る plan を起動して lock を取った後の行が現れたら SIGKILL で止め、
//	ci-hold-verify（aws）が残った lock の形を確かめる。SIGINT と SIGTERM では terraform が lock を外してから止まる。
//	lock は terraform 自身に取らせる —— lock の解除が相手にする lock と同じ形にするためである（ADR-0604 決定8）。

const (
	stepS3List      = "s3api-list-objects-v2"
	stepS3GetObject = "s3api-get-object"
	stepS3Delete    = "s3api-delete-object"
	stepCIPlanHold  = "ci-terraform-plan-hold"

	ciModeHold = "hold"

	stageAbsent = "absent"
	stageHold   = "hold"

	// holdInterval と holdLimit は、lock を残す入口が、plan の全文に lock を取った後の行が現れるのを待つ周期と上限。
	holdInterval = 200 * time.Millisecond
	holdLimit    = 10 * time.Minute
)

var errHoldStarted = xerrors.New("❌ plan を起動した後に失敗しました。lock が残っていれば、lock の解除の閾値を過ぎてから Terraform Unlock で外してください")

// s3Call は、work に input.json を書いて aws の段を起動し、stdout を返します。
func s3Call(d deps, work string, env []string, step string, input map[string]string) ([]byte, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, xerrors.Wrap(err, "input.json")
	}
	if err := writeInRoot(filepath.Join(work, "input.json"), raw); err != nil {
		return nil, xerrors.Wrap(err, "input.json")
	}

	return ciRunTool(d.tool, work, env, step, d.stderr)
}

// lockTarget は、lock file のある Bucket と key と、aws の環境です。
type lockTarget struct {
	root, bucket, key, account string
	env                        []string
}

// etag は、lock file の ETag を返します。lock が無ければ空です。
func (l lockTarget) etag(d deps, work string) (string, error) {
	out, err := s3Call(d, work, l.env, stepS3List, map[string]string{"Bucket": l.bucket, "Prefix": l.key, "ExpectedBucketOwner": l.account})
	if err != nil {
		return "", err
	}

	return lockETag(l.key, out)
}

// read は、読んだ版の ETag を条件に lock file を読み、属性と中身を返します。
func (l lockTarget) read(d deps, work, etag string) ([]byte, []byte, error) {
	meta, err := s3Call(d, work, l.env, stepS3GetObject, map[string]string{"Bucket": l.bucket, "Key": l.key, "IfMatch": etag, "ExpectedBucketOwner": l.account})
	if err != nil {
		return nil, nil, err
	}
	body, err := readInRoot(filepath.Join(work, "lock.json"))
	if err != nil {
		return nil, nil, xerrors.Wrap(errLockInfo, err.Error())
	}

	return meta, body, nil
}

// ciLockTarget は、資格情報の呼び出し元から Bucket を、root の backend.tf から lock file の key を求めます。
func ciLockTarget(d deps, work, project, env, root, profile, base string) (lockTarget, error) {
	dir, err := localRoot(base, env, root)
	if err != nil {
		return lockTarget{}, err
	}
	region, err := rootRegion(dir, d.readFile)
	if err != nil {
		return lockTarget{}, err
	}
	key, err := lockKey(dir, d.readFile)
	if err != nil {
		return lockTarget{}, err
	}
	l := lockTarget{root: env + "-" + root, key: key, env: localEnv(d.environ, profile, "AWS_REGION="+region)}
	identity, err := ciRunTool(d.tool, work, l.env, stepCallerIdentity, d.stderr)
	if err != nil {
		return lockTarget{}, err
	}
	if l.account, err = accountOf(strings.TrimSpace(string(identity))); err != nil {
		return lockTarget{}, err
	}
	if l.bucket, err = bucketName(project, env, "tfstate", l.account, region); err != nil {
		return lockTarget{}, err
	}

	return l, nil
}

// ciUnlockStep は ci-unlock です。
func ciUnlockStep(args []string, d deps, stdout io.Writer) (string, error) {
	fs := flag.NewFlagSet("ci-unlock", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	root := fs.String("root", "", "")
	profile := fs.String("profile", "", "")
	base := fs.String("base", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *project == "" || *profile == "" || *base == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	work, cleanup, err := workdir.Temp("", "tf-local-unlock-")
	if err != nil {
		return "", xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer cleanup()
	l, err := ciLockTarget(d, work, *project, *env, *root, *profile, *base)
	if err != nil {
		return "", err
	}
	etag, err := l.etag(d, work)
	if err != nil {
		return "", err
	}
	summary := envValue(d.environ, "GITHUB_STEP_SUMMARY")
	if etag == "" {
		if err := appendGitHub(summary, fmt.Sprintf("### %s\n\nlock はありません\n", l.root)); err != nil {
			return "", err
		}

		return "✅ lock はありません。何も消しません", nil
	}
	meta, body, err := l.read(d, work, etag)
	if err != nil {
		return "", err
	}
	line, err := lockStale(etag, meta, body, d.now())
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(stdout, line); err != nil {
		return "", err
	}
	out, err := s3Call(d, work, l.env, stepS3Delete, map[string]string{"Bucket": l.bucket, "Key": l.key, "IfMatch": etag, "ExpectedBucketOwner": l.account})
	if err != nil {
		return "", err
	}
	var resp struct {
		VersionID string `json:"VersionId"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", xerrors.Wrap(errLockList, "delete-object の応答を読めません")
	}
	done := fmt.Sprintf("lock を外しました（delete marker の版 ID: %s）", resp.VersionID)
	if err := appendGitHub(summary, fmt.Sprintf("### %s\n\n```text\n%s\n```\n\n%s\n", l.root, line, strings.Replace(done, resp.VersionID, "`"+resp.VersionID+"`", 1))); err != nil {
		return "", err
	}

	return "✅ " + done, nil
}

// removeInRoot は、path を、そのディレクトリの外へ出ない形で消します。無ければ何もしません。
func removeInRoot(path string) {
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return
	}
	defer func() { _ = r.Close() }()
	_ = r.Remove(filepath.Base(path))
}

// holdPath は、lock を残す入口の plan の全文の置き場です。
func holdPath(state string) string {
	return strings.TrimSuffix(state, ".json") + ".hold.txt"
}

// ciHoldTarget は、hold の段の state から lock file の置き場を組みます。
func ciHoldTarget(d deps, s ciState) (lockTarget, error) {
	key, err := lockKey(s.Dir, d.readFile)
	if err != nil {
		return lockTarget{}, err
	}
	bucket := strings.TrimPrefix(s.InitArgs[0], "-backend-config=bucket=")

	return lockTarget{root: s.Root, bucket: bucket, key: key, account: s.Account,
		env: localEnv(d.environ, s.Profile, "AWS_REGION="+s.Region)}, nil
}

// loadHoldState は、hold の段の state を読み、前の段が終わっていなければ失敗します。失敗したときは全文を消す。
func loadHoldState(name string, args []string, d deps, after string, more func(*flag.FlagSet)) (ciState, string, error) {
	statePath, err := ciStepArgs(name, args, d, more)
	if err != nil {
		return ciState{}, "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return ciState{}, "", err
	}
	if s.Mode != ciModeHold || !slices.Contains(s.Done, after) || len(s.InitArgs) == 0 {
		s.remove()

		return ciState{}, "", xerrors.Wrapf(errCIState, "%s が終わっていません", after)
	}

	return s, statePath, nil
}

// ciHoldAbsentStep は ci-hold-absent です。lock が既にあれば、どの lock を残したのか区別できないので失敗します。
func ciHoldAbsentStep(args []string, d deps) (string, error) {
	s, statePath, err := loadHoldState("ci-hold-absent", args, d, stageInit, nil)
	if err != nil {
		return "", err
	}
	if err := ciHoldAbsent(d, s); err != nil {
		s.remove()

		return "", err
	}
	s.done(stageAbsent)

	return "✅ lock はありません", s.save(statePath)
}

func ciHoldAbsent(d deps, s ciState) error {
	l, err := ciHoldTarget(d, s)
	if err != nil {
		return err
	}
	work, cleanup, err := workdir.Temp("", "tf-local-hold-")
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer cleanup()
	etag, err := l.etag(d, work)
	if err != nil {
		return err
	}
	_, err = wantLock("absent", etag)

	return err
}

// ciHoldPlanStep は ci-hold-plan です。lock を取る plan を起動し、lock を取った後の行が全文に現れたら SIGKILL で
// 止めます。現れる前に plan が終われば失敗します。全文はランナーの外に出しません。
func ciHoldPlanStep(args []string, d deps) (string, error) {
	interval, limit := holdInterval, holdLimit
	s, statePath, err := loadHoldState("ci-hold-plan", args, d, stageAbsent, func(fs *flag.FlagSet) {
		fs.DurationVar(&interval, "interval", holdInterval, "")
		fs.DurationVar(&limit, "limit", holdLimit, "")
	})
	if err != nil {
		return "", err
	}
	if interval <= 0 || limit <= 0 {
		s.remove()

		return "", xerrors.Wrap(errUsage, "-interval と -limit は正の時間です")
	}
	if err := ciHoldPlan(d, s, holdPath(statePath), interval, limit); err != nil {
		s.remove()
		removeInRoot(holdPath(statePath))

		return "", err
	}
	s.done(stageHold)

	return "✅ plan は lock を取った後に止まりました", s.save(statePath)
}

func ciHoldPlan(d deps, s ciState, hold string, interval, limit time.Duration) error {
	f, err := openInRoot(hold, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return xerrors.Wrap(err, "plan の全文")
	}
	defer func() { _ = f.Close() }()
	cmd := d.tool(context.Background(), s.Dir, localEnv(d.environ, s.Profile), stepCIPlanHold)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return xerrors.Wrapf(toolrun.ErrTool, "plan: %v", err)
	}
	var exited atomic.Bool
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		exited.Store(true)
		close(waited)
	}()
	watch := d
	watch.readFile = func(string) ([]byte, error) { return readInRoot(hold) }
	watch.alive = func(int) bool { return !exited.Load() }
	watch.sleep = time.Sleep
	held, err := lockWait(hold, cmd.Process.Pid, interval, limit, watch)
	_ = cmd.Process.Kill()
	<-waited
	if err != nil {
		return errors.Join(err, errHoldStarted)
	}
	if held == "" {
		return xerrors.Wrap(errLockMissing, "plan が lock を取った後の行を出さずに終わりました。全文はランナーの外に出しません")
	}

	return nil
}

// ciHoldVerifyStep は ci-hold-verify です。残った lock が、lock の解除が消す形であることを確かめ、消せるようになる
// 時刻を出します。終われば全文と plan の全文を消します。
func ciHoldVerifyStep(args []string, d deps, stdout io.Writer) (string, error) {
	s, statePath, err := loadHoldState("ci-hold-verify", args, d, stageHold, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		s.remove()
		removeInRoot(holdPath(statePath))
	}()
	line, err := ciHoldVerify(d, s)
	if err != nil {
		return "", errors.Join(err, errHoldStarted)
	}
	if _, err := fmt.Fprintln(stdout, line); err != nil {
		return "", err
	}
	const done = "lock を残しました。外すのは Terraform Unlock です"
	if err := appendGitHub(envValue(d.environ, "GITHUB_STEP_SUMMARY"), fmt.Sprintf("### %s\n\n```text\n%s\n```\n\n%s\n", s.Root, line, done)); err != nil {
		return "", err
	}

	return "✅ " + done, nil
}

func ciHoldVerify(d deps, s ciState) (string, error) {
	l, err := ciHoldTarget(d, s)
	if err != nil {
		return "", err
	}
	work, cleanup, err := workdir.Temp("", "tf-local-hold-")
	if err != nil {
		return "", xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer cleanup()
	etag, err := l.etag(d, work)
	if err != nil {
		return "", err
	}
	if _, err := wantLock("present", etag); err != nil {
		return "", err
	}
	meta, body, err := l.read(d, work, etag)
	if err != nil {
		return "", err
	}

	return lockHeld(etag, meta, body, d.now())
}
