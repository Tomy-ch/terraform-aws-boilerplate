package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// applyJobTimeout は、apply の job の timeout-minutes の写し。正本は .github/workflows/ の
	// `make ci-apply-*` を走らせる job。
	applyJobTimeout = 30 * time.Minute
	// staleLockMargin は、cancel の後にランナーが apply を止めるまでの猶予と、S3 とランナーの時計の差の分。
	staleLockMargin = 5 * time.Minute

	// lockFormatTerraform は、lockInfo の形を確かめた Terraform の版。
	lockFormatTerraform = "1.17.1"

	lockSuffix   = ".tflock"
	maxLogLength = 200

	// ciLockWho は、GitHub-hosted runner の利用者。Terraform は Who を `<利用者>@<ホスト>` で書く。
	ciLockWho = "runner@"
	// apply の job と lock を残す job が取る lock の Operation。どちらの job も plan で lock を取る（apply の job は
	// その後に apply する）。値は Terraform の backendrun.OperationType の String() で、lockFormatTerraform の版の
	// operationtype_string.go から写す。
	planOperation  = "OperationTypePlan"
	applyOperation = "OperationTypeApply"
)

var (
	keyLine    = regexp.MustCompile(`^\s*key\s*=\s*"([^"]*)"`)
	keyPattern = regexp.MustCompile(`^[a-z0-9-]+/terraform\.tfstate$`)
	// etagPattern は、シェルの引数として安全な形に限る。中身の規則（MD5 か否か）は暗号化の方式で変わるので見ない。
	etagPattern = regexp.MustCompile(`^"[0-9A-Za-z-]{1,64}"$`)

	errLockKey     = xerrors.New("❌ backend.tf の key が1つではないか、形が不正です")
	errLockList    = xerrors.New("❌ lock file の一覧を読めません")
	errLockMeta    = xerrors.New("❌ lock file の属性を読めません")
	errLockChanged = xerrors.New("❌ 一覧を読んだ後に lock file が置き換わりました")
	errLockInfo    = xerrors.New("❌ lock file の中身を Terraform の lock として読めません")
	errLockFresh   = xerrors.New("❌ lock が新しいので消しません（apply が走っているか、止まって間もない）")
	errLockForeign = xerrors.New("❌ CI のランナーが apply の job か lock を残す job で取った lock ではないので消しません（人の手元の lock は、取った人が terraform force-unlock で外す）")
	errLockExists  = xerrors.New("❌ lock が既にあります。どの lock を残したのか区別できないので、lock を取りません")
	errLockMissing = xerrors.New("❌ lock が残りませんでした（terraform が lock を外してから止まったか、lock を取る前に終わった）。起動し直してください")
	errLockWait    = xerrors.New("❌ plan が lock を取った後の行を、待つ時間の内に出しませんでした")
	errLockWant    = xerrors.New("❌ -want は absent か present です")
)

// heldLine は、plan が lock を取った後にだけ全文へ書く行。Terraform は backend の lock を取ってから
// refresh と data source の読み取りを始め、resource ごとに `<address>: Refreshing state...` か
// `<address>: Reading...` を書く（lockFormatTerraform の版）。
var heldLine = regexp.MustCompile(`(?m)^\S.*: (Refreshing state|Reading)\.\.\.`)

// listObjects は、`aws s3api list-objects-v2 --no-paginate` の1ページの応答。Contents は0件なら欠ける。
// 他は欠ければ別の応答を読んでいるので、ポインタで欠落を区別する。
type listObjects struct {
	Prefix      *string `json:"Prefix"`
	IsTruncated *bool   `json:"IsTruncated"`
	KeyCount    *int    `json:"KeyCount"`
	Contents    []struct {
		Key  string `json:"Key"`
		ETag string `json:"ETag"`
	} `json:"Contents"`
}

type objectMeta struct {
	ETag         string `json:"ETag"`
	LastModified string `json:"LastModified"`
	VersionID    string `json:"VersionId"`
}

// lockInfo は、Terraform（lockFormatTerraform の版）の internal/states/statemgr/locker.go の LockInfo の写し。
// json の tag は無く、キーはフィールド名のまま。Path（`<Bucket>/<key>`。Bucket 名はアカウント ID を含む）と Info は読まない。
type lockInfo struct {
	ID        string
	Operation string
	Who       string
	Version   string
	Created   string
}

func lockKey(root string, readFile func(string) ([]byte, error)) (string, error) {
	path := filepath.Join(root, "backend.tf")

	content, err := readFile(path)
	if err != nil {
		return "", xerrors.Wrap(errBackendMissing, path)
	}

	var keys []string

	for line := range strings.Lines(string(content)) {
		if m := keyLine.FindStringSubmatch(line); m != nil {
			keys = append(keys, m[1])
		}
	}

	if len(keys) != 1 || !keyPattern.MatchString(keys[0]) {
		return "", xerrors.Wrap(errLockKey, fmt.Sprintf("%s: %q", path, keys))
	}

	return keys[0] + lockSuffix, nil
}

func runLockETag(args []string, readFile func(string) ([]byte, error)) (string, error) {
	fs := flag.NewFlagSet("lock-etag", flag.ContinueOnError)
	key := fs.String("key", "", "")
	want := fs.String("want", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	if *want != "" && *want != "absent" && *want != "present" {
		return "", xerrors.Wrap(errUsage, errLockWant.Error())
	}

	data, err := readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errLockList, err.Error())
	}

	etag, err := lockETag(*key, data)
	if err != nil {
		return "", err
	}

	return wantLock(*want, etag)
}

// wantLock は、-want の指定に合わない lock の有無を失敗にします。指定が空なら、有無を問わず ETag（無ければ空）を返す。
func wantLock(want, etag string) (string, error) {
	switch {
	case want == "absent" && etag != "":
		return "", errLockExists
	case want == "present" && etag == "":
		return "", errLockMissing
	}

	return etag, nil
}

// lockETag は、key に完全一致する object の ETag を返します。lock が無ければ空文字を返す。
// 応答が key の1ページ全体であることを Prefix・IsTruncated・KeyCount で確かめ、確かめられなければ
// 「lock 無し」と読まずに失敗します。
func lockETag(key string, data []byte) (string, error) {
	var list listObjects
	if err := json.Unmarshal(data, &list); err != nil {
		return "", xerrors.Wrap(errLockList, "JSON ではありません")
	}

	if list.Prefix == nil || *list.Prefix != key || key == "" {
		return "", xerrors.Wrap(errLockList, "Prefix が lock file の key ではありません")
	}

	if list.IsTruncated == nil || *list.IsTruncated || list.KeyCount == nil || *list.KeyCount != len(list.Contents) {
		return "", xerrors.Wrap(errLockList, "1ページに収まった一覧ではありません")
	}

	var etags []string

	for _, c := range list.Contents {
		if c.Key == key {
			etags = append(etags, c.ETag)
		}
	}

	switch {
	case len(etags) == 0:
		return "", nil
	case len(etags) > 1:
		return "", xerrors.Wrap(errLockList, "同じ key が2件あります")
	case !etagPattern.MatchString(etags[0]):
		return "", xerrors.Wrap(errLockList, "ETag の形が不正です")
	}

	return etags[0], nil
}

func runLockStale(args []string, d deps) (string, error) {
	fs := flag.NewFlagSet("lock-stale", flag.ContinueOnError)
	etag := fs.String("etag", "", "")
	metaPath := fs.String("meta", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	meta, err := d.readFile(*metaPath)
	if err != nil {
		return "", xerrors.Wrap(errLockMeta, err.Error())
	}

	body, err := d.readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errLockInfo, err.Error())
	}

	return lockStale(*etag, meta, body, d.now())
}

func runLockHeld(args []string, d deps) (string, error) {
	fs := flag.NewFlagSet("lock-held", flag.ContinueOnError)
	etag := fs.String("etag", "", "")
	metaPath := fs.String("meta", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	meta, err := d.readFile(*metaPath)
	if err != nil {
		return "", xerrors.Wrap(errLockMeta, err.Error())
	}

	body, err := d.readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errLockInfo, err.Error())
	}

	return lockHeld(*etag, meta, body, d.now())
}

// lockHeld は、lock を残す job が残した lock の説明と、lock の解除がそれを消せるようになる時刻を返します。
// lock の解除が消す形（lockStale）でなければ、待っても消せないので失敗する。古さでは失敗しない。
func lockHeld(etag string, metaJSON, body []byte, now time.Time) (string, error) {
	line, modified, err := ciLock(etag, metaJSON, body, now)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s\nTerraform Unlock が消せるのは %s より後", line,
		modified.Add(applyJobTimeout+staleLockMargin).UTC().Format(time.RFC3339)), nil
}

func runLockWait(args []string, d deps) (string, error) {
	fs := flag.NewFlagSet("lock-wait", flag.ContinueOnError)
	pid := fs.Int("pid", 0, "")
	interval := fs.Duration("interval", time.Second, "")
	limit := fs.Duration("limit", 10*time.Minute, "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 || *pid <= 0 || *interval <= 0 || *limit <= 0 {
		return "", errUsage
	}

	return lockWait(fs.Arg(0), *pid, *interval, *limit, d)
}

// lockWait は、裏で走る plan（pid）の全文を読み、lock を取った後の行（heldLine）が現れたら `held` を返します。
// 現れる前に plan が終われば空を返す（make はそれを条件に後続を省いて失敗する）。どちらも起きないまま limit を
// 過ぎたら失敗する。plan が終わったかは、全文を読む前に確かめる —— 終わってから書かれた行を取りこぼさない。
func lockWait(path string, pid int, interval, limit time.Duration, d deps) (string, error) {
	for waited := time.Duration(0); ; waited += interval {
		alive := d.alive(pid)

		data, err := d.readFile(path)
		if err != nil {
			return "", xerrors.Wrap(errLockWait, err.Error())
		}

		if heldLine.Match(data) {
			return "held", nil
		}

		if !alive {
			return "", nil
		}

		if waited >= limit {
			return "", errLockWait
		}

		d.sleep(interval)
	}
}

// lockStale は、消してよい lock の説明の行を返します。CI のランナーが apply の job か lock を残す job で取った lock でなければ
// errLockForeign で、apply の job の timeout と余裕を超えて古くなければ errLockFresh で、説明の行を添えて失敗する。古さは S3 が書いた LastModified で測り、
// lock の Created（書き手のランナーの時計）は説明に出すだけにする。
func lockStale(etag string, metaJSON, body []byte, now time.Time) (string, error) {
	line, modified, err := ciLock(etag, metaJSON, body, now)
	if err != nil {
		return "", err
	}

	if now.Sub(modified) <= applyJobTimeout+staleLockMargin {
		return "", xerrors.Wrap(errLockFresh, line)
	}

	return line, nil
}

// ciLock は、CI のランナーが取った Plan か Apply の lock の説明の行と LastModified を返します。それ以外の lock は
// errLockForeign で、説明の行を添えて失敗する。
func ciLock(etag string, metaJSON, body []byte, now time.Time) (string, time.Time, error) {
	meta, modified, err := readLockMeta(etag, metaJSON)
	if err != nil {
		return "", time.Time{}, err
	}

	var info lockInfo
	if err := json.Unmarshal(body, &info); err != nil || !keyIDPattern.MatchString(info.ID) {
		return "", time.Time{}, errLockInfo
	}

	line := describeLock(info, meta, modified, now.Sub(modified))

	if !strings.HasPrefix(info.Who, ciLockWho) || (info.Operation != planOperation && info.Operation != applyOperation) {
		return "", time.Time{}, xerrors.Wrap(errLockForeign, line)
	}

	return line, modified, nil
}

// readLockMeta は、get-object の応答から属性と LastModified を返します。版 ID が無ければ、消した lock が
// noncurrent の版として残らない（版管理の外の）Bucket なので消さない。
func readLockMeta(etag string, data []byte) (objectMeta, time.Time, error) {
	var meta objectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, time.Time{}, xerrors.Wrap(errLockMeta, "JSON ではありません")
	}

	switch {
	case meta.ETag == "":
		return meta, time.Time{}, xerrors.Wrap(errLockMeta, "ETag がありません")
	case meta.ETag != etag:
		return meta, time.Time{}, errLockChanged
	case meta.VersionID == "":
		return meta, time.Time{}, xerrors.Wrap(errLockMeta, "版 ID がありません（版管理の下にない lock は消しません）")
	}

	modified, err := time.Parse(time.RFC3339, meta.LastModified)
	if err != nil {
		return meta, time.Time{}, xerrors.Wrap(errLockMeta, "LastModified の形が不正です")
	}

	return meta, modified, nil
}

// describeLock は、公開されるログに出す説明の行を返します。人の手元の利用者名とホスト名を出さないため、
// Who は GitHub-hosted runner の利用者（runner@）のときだけそのまま出す。
func describeLock(info lockInfo, meta objectMeta, modified time.Time, elapsed time.Duration) string {
	who := "CI のランナー以外"
	if strings.HasPrefix(info.Who, ciLockWho) {
		who = logSafe(info.Who)
	}

	return fmt.Sprintf("lock: ID=%s Operation=%s Who=%s Version=%s Created=%s LastModified=%s 経過=%d 分 版 ID=%s",
		info.ID, logSafe(info.Operation), who, logSafe(info.Version), logSafe(info.Created),
		modified.UTC().Format(time.RFC3339), int(elapsed.Minutes()), logSafe(meta.VersionID))
}

// logSafe は、制御文字と空白を除いて長さを切ります。値は `lock: ` の後ろに置くので、行頭の
// workflow command（`::`）にはならない。
func logSafe(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return -1
		}

		return r
	}, s)

	if r := []rune(s); len(r) > maxLogLength {
		return string(r[:maxLogLength])
	}

	return s
}
