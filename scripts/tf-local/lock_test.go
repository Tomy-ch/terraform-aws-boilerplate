package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const (
	lockKeyOK = "identity/terraform.tfstate.tflock"
	lockETagA = `"0123456789abcdef0123456789abcdef"`
	lockID    = "c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b"
)

var lockNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func lockList(prefix string, truncated bool, keyCount int, contents ...string) string {
	return `{"Name":"b","Prefix":` + strconv.Quote(prefix) + `,"KeyCount":` + strconv.Itoa(keyCount) +
		`,"MaxKeys":1000,"IsTruncated":` + strconv.FormatBool(truncated) + contentsJSON(contents) + `}`
}

func contentsJSON(contents []string) string {
	if len(contents) == 0 {
		return ""
	}

	return `,"Contents":[` + strings.Join(contents, ",") + `]`
}

func lockObject(key, etag string) string {
	return `{"Key":` + strconv.Quote(key) + `,"ETag":` + strconv.Quote(etag) + `,"Size":200}`
}

func lockMeta(etag string, modified time.Time) string {
	return `{"ETag":` + strconv.Quote(etag) + `,"LastModified":"` + modified.Format("2006-01-02T15:04:05-07:00") + `","VersionId":"ver-1","ServerSideEncryption":"aws:kms"}`
}

func lockBody(id, who string) string {
	return `{"ID":` + strconv.Quote(id) + `,"Operation":"OperationTypeApply","Info":"secret-info","Who":` + strconv.Quote(who) +
		`,"Version":"1.16.6","Created":"2026-11-12T10:00:00.123456789Z","Path":"tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"}`
}

func Test_lockKey(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		got, err := lockKey(rootWith(t, backendOK, providersOK), os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, lockKeyOK, got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, backend := range map[string]string{
			"key が無い":           "terraform {\n  backend \"s3\" {\n    region = \"ap-northeast-1\"\n  }\n}\n",
			"key が2つある":         "key = \"identity/terraform.tfstate\"\nkey = \"workload/terraform.tfstate\"\n",
			"key が空":            "key = \"\"\n",
			"key が tfstate でない": "key = \"identity/state.json\"\n",
			"key が階層を遡る":        "key = \"../terraform.tfstate\"\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := lockKey(rootWith(t, backend, providersOK), os.ReadFile)
				require.ErrorIs(t, err, errLockKey)
			})
		}

		t.Run("backend.tf が無ければ errBackendMissing", func(t *testing.T) {
			t.Parallel()

			_, err := lockKey(t.TempDir(), os.ReadFile)
			require.ErrorIs(t, err, errBackendMissing)
		})
	})

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		got, err := lockKey(filepath.Join("..", "..", "env", "admin", "identity"), os.ReadFile)
		require.NoError(t, err)

		raw, err := os.ReadFile(filepath.Join("..", "..", "conftest", "plan", "decl.json"))
		require.NoError(t, err)

		var decl struct {
			PolicyPlan struct {
				DeploymentIdentity struct {
					UnlockRole struct {
						LockKey string `json:"lock_key"`
					} `json:"unlock_role"`
				} `json:"deployment_identity"`
			} `json:"policy_plan"`
		}
		require.NoError(t, json.Unmarshal(raw, &decl))
		assert.Equal(t, got, decl.PolicyPlan.DeploymentIdentity.UnlockRole.LockKey,
			"env/admin/identity の lock file の key が、Policy Test の宣言 unlock_role.lock_key と一致する")
	})
}

func Test_runLockETag(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "list.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		got, err := runLockETag([]string{"-key", lockKeyOK, write(t, lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, lockETagA)))}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, lockETagA, got)
	})

	t.Run("-want は lock の有無を確かめる", func(t *testing.T) {
		t.Parallel()

		held := write(t, lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, lockETagA)))
		empty := write(t, lockList(lockKeyOK, false, 0))

		got, err := runLockETag([]string{"-key", lockKeyOK, "-want", "present", held}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, lockETagA, got)

		got, err = runLockETag([]string{"-key", lockKeyOK, "-want", "absent", empty}, os.ReadFile)
		require.NoError(t, err)
		assert.Empty(t, got)

		_, err = runLockETag([]string{"-key", lockKeyOK, "-want", "absent", held}, os.ReadFile)
		require.ErrorIs(t, err, errLockExists)

		_, err = runLockETag([]string{"-key", lockKeyOK, "-want", "present", empty}, os.ReadFile)
		require.ErrorIs(t, err, errLockMissing)
	})

	t.Run("-want の一覧が読めなければ有無を判定せず errLockList", func(t *testing.T) {
		t.Parallel()

		_, err := runLockETag([]string{"-key", lockKeyOK, "-want", "absent", write(t, lockList(lockKeyOK, true, 0))}, os.ReadFile)
		require.ErrorIs(t, err, errLockList)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイルを読めなければ errLockList", func(t *testing.T) {
			t.Parallel()

			_, err := runLockETag([]string{"-key", lockKeyOK, filepath.Join(t.TempDir(), "none")}, os.ReadFile)
			require.ErrorIs(t, err, errLockList)
		})

		for name, args := range map[string][]string{
			"位置引数が無い":   {"-key", lockKeyOK},
			"位置引数が2つ":   {"-key", lockKeyOK, "a", "b"},
			"未知のフラグ":    {"-nope", "a"},
			"未知の -want": {"-key", lockKeyOK, "-want", "maybe", "a"},
		} {
			t.Run(name+"なら errUsage", func(t *testing.T) {
				t.Parallel()

				_, err := runLockETag(args, os.ReadFile)
				require.ErrorIs(t, err, errUsage)
			})
		}
	})
}

func Test_lockETag(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct{ list, want string }{
			"lock が在ればその ETag を返す":         {lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, lockETagA)), lockETagA},
			"lock が無ければ空を返す":               {lockList(lockKeyOK, false, 0), ""},
			"接頭辞だけが一致する object は lock でない": {lockList(lockKeyOK, false, 1, lockObject(lockKeyOK+".x", lockETagA)), ""},
			"マルチパートの ETag も受ける":            {lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, `"`+strings.Repeat("a", 32)+`-2"`)), `"` + strings.Repeat("a", 32) + `-2"`},
			"64 文字の ETag は受ける":             {lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, `"`+strings.Repeat("a", 64)+`"`)), `"` + strings.Repeat("a", 64) + `"`},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := lockETag(lockKeyOK, []byte(c.list))
				require.NoError(t, err)
				assert.Equal(t, c.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, list := range map[string]string{
			"空のファイルを lock 無しと読まない":             "",
			"ページングした出力（Prefix が無い）を読まない":       `{"Contents":[]}`,
			"別の Prefix の一覧を読まない":               lockList("identity/", false, 0),
			"途中で切れた一覧を読まない":                    lockList(lockKeyOK, true, 0),
			"IsTruncated が無い一覧を読まない":           `{"Prefix":"` + lockKeyOK + `","KeyCount":0}`,
			"KeyCount が無い一覧を読まない":              `{"Prefix":"` + lockKeyOK + `","IsTruncated":false}`,
			"KeyCount と Contents が食い違う一覧を読まない": lockList(lockKeyOK, false, 1),
			"同じ key が2件あれば読まない":                lockList(lockKeyOK, false, 2, lockObject(lockKeyOK, lockETagA), lockObject(lockKeyOK, lockETagA)),
			"ETag が空なら読まない":                    lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, "")),
			"ETag が引用符だけなら読まない":                lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, `""`)),
			"65 文字の ETag は読まない":                lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, `"`+strings.Repeat("a", 65)+`"`)),
			"ETag がシェルの引数として安全でなければ読まない":       lockList(lockKeyOK, false, 1, lockObject(lockKeyOK, `"a b"; rm`)),
			"JSON でない": "nope",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := lockETag(lockKeyOK, []byte(list))
				require.ErrorIs(t, err, errLockList)
			})
		}

		t.Run("key が空なら、空の Prefix の一覧を読まない", func(t *testing.T) {
			t.Parallel()

			_, err := lockETag("", []byte(lockList("", false, 0)))
			require.ErrorIs(t, err, errLockList)
		})
	})
}

func Test_runLockStale(t *testing.T) {
	t.Parallel()

	files := func(t *testing.T, meta, body string) (string, string) {
		t.Helper()

		dir := t.TempDir()
		m, b := filepath.Join(dir, "meta.json"), filepath.Join(dir, "lock.json")
		require.NoError(t, os.WriteFile(m, []byte(meta), 0o600))
		require.NoError(t, os.WriteFile(b, []byte(body), 0o600))

		return m, b
	}
	d := deps{readFile: os.ReadFile, now: func() time.Time { return lockNow }}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		m, b := files(t, lockMeta(lockETagA, lockNow.Add(-time.Hour)), lockBody(lockID, "runner@fv-az1-2"))
		got, err := runLockStale([]string{"-etag", lockETagA, "-meta", m, b}, d)
		require.NoError(t, err)
		assert.Contains(t, got, "ID="+lockID)
		assert.Contains(t, got, "経過=60 分", "時刻は deps.now から読む")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		m, b := files(t, lockMeta(lockETagA, lockNow.Add(-time.Hour)), lockBody(lockID, "runner@fv-az1-2"))
		none := filepath.Join(t.TempDir(), "none")

		for name, c := range map[string]struct {
			args []string
			want error
		}{
			"位置引数が無ければ errUsage":            {[]string{"-etag", lockETagA, "-meta", m}, errUsage},
			"未知のフラグなら errUsage":             {[]string{"-nope", b}, errUsage},
			"属性のファイルを読めなければ errLockMeta":    {[]string{"-etag", lockETagA, "-meta", none, b}, errLockMeta},
			"lock file を読めなければ errLockInfo": {[]string{"-etag", lockETagA, "-meta", m, none}, errLockInfo},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := runLockStale(c.args, d)
				require.ErrorIs(t, err, c.want)
			})
		}
	})
}

func Test_lockStale(t *testing.T) {
	t.Parallel()

	threshold := applyJobTimeout + staleLockMargin
	stale := func(etag string, modified time.Time, body string) (string, error) {
		return lockStale(lockETagA, []byte(lockMeta(etag, modified)), []byte(body), lockNow)
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		got, err := stale(lockETagA, lockNow.Add(-threshold-time.Minute), lockBody(lockID, "runner@fv-az1-2"))
		require.NoError(t, err)
		assert.Contains(t, got, "ID="+lockID)
		assert.Contains(t, got, "Operation=OperationTypeApply")
		assert.Contains(t, got, "Who=runner@fv-az1-2")
		assert.Contains(t, got, "経過=36 分")
		assert.Contains(t, got, "版 ID=ver-1")
		assert.Contains(t, got, "Version=1.16.6")
		assert.Contains(t, got, "Created=2026-11-12T10:00:00.123456789Z", "書き手の時計の値はそのまま出す")
		assert.Contains(t, got, "LastModified="+lockNow.Add(-threshold-time.Minute).Format(time.RFC3339), "LastModified は UTC で出す")
		assert.NotContains(t, got, "123456789012", "Path の Bucket 名のアカウント ID を出さない")
		assert.NotContains(t, got, "secret-info", "Info を出さない")
	})

	t.Run("境界", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			age  time.Duration
			want error
		}{
			"閾値ちょうどは消さない":            {threshold, errLockFresh},
			"閾値を1秒超えたら消す":            {threshold + time.Second, nil},
			"新しい lock は消さない":         {time.Minute, errLockFresh},
			"未来の LastModified は消さない": {-time.Minute, errLockFresh},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := stale(lockETagA, lockNow.Add(-c.age), lockBody(lockID, "runner@fv-az1-2"))
				if c.want == nil {
					require.NoError(t, err)
					assert.Contains(t, got, "ID="+lockID)

					return
				}

				require.ErrorIs(t, err, c.want)
				assert.Empty(t, got)
				assert.Contains(t, err.Error(), "ID="+lockID, "消さない lock も ID を出す")
			})
		}
	})

	t.Run("CI のランナーが apply の job で取った plan と apply の lock は消す", func(t *testing.T) {
		t.Parallel()

		for _, op := range []string{"OperationTypePlan", "OperationTypeApply"} {
			t.Run(op, func(t *testing.T) {
				t.Parallel()

				body := strings.Replace(lockBody(lockID, "runner@fv-az1-2"), "OperationTypeApply", op, 1)
				got, err := stale(lockETagA, lockNow.Add(-time.Hour), body)
				require.NoError(t, err)
				assert.Contains(t, got, "Operation="+op)
			})
		}
	})

	t.Run("CI のランナーが apply の job で取った lock でなければ、古くても消さない", func(t *testing.T) {
		t.Parallel()

		old := lockNow.Add(-time.Hour)
		withOp := func(op string) string {
			return strings.Replace(lockBody(lockID, "runner@fv-az1-2"), "OperationTypeApply", op, 1)
		}

		for name, c := range map[string]struct {
			body, never string
		}{
			"人の手元の apply の lock":       {lockBody(lockID, "alice@laptop.local"), "alice"},
			"runner を含むだけの利用者の lock":   {lockBody(lockID, "xrunner@host"), "xrunner"},
			"Who の無い lock":             {lockBody(lockID, ""), ""},
			"CI のランナーの refresh の lock": {withOp("OperationTypeRefresh"), ""},
			"CI のランナーの invalid の lock": {withOp("OperationTypeInvalid"), ""},
			"小文字の Operation の lock":    {withOp("operationtypeapply"), ""},
			"Operation の無い lock":       {`{"ID":` + strconv.Quote(lockID) + `,"Who":"runner@fv-az1-2"}`, ""},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := stale(lockETagA, old, c.body)
				require.ErrorIs(t, err, errLockForeign)
				assert.Empty(t, got)
				assert.Contains(t, err.Error(), "ID="+lockID, "消さない lock も ID を出す")

				if c.never != "" {
					assert.NotContains(t, err.Error(), c.never, "人の手元の利用者とホストを出さない")
				}
			})
		}
	})

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		t.Run("閾値の timeout は apply の job の timeout-minutes と一致する", func(t *testing.T) {
			t.Parallel()

			got, n := entryTimeout(t, filepath.Join("..", "..", ".github", "workflows"), "make ci-apply-")
			require.Positive(t, n, "make ci-apply- を走らせる job が見つかりません。検査の対象が0件です")
			assert.Equal(t, applyJobTimeout, got)
		})

		t.Run("lock を残す job の timeout-minutes は閾値の apply の job の timeout を超えない", func(t *testing.T) {
			t.Parallel()

			got, n := entryTimeout(t, filepath.Join("..", "..", ".github", "workflows"), "make ci-hold-")
			require.Positive(t, n, "make ci-hold- を走らせる job が見つかりません。検査の対象が0件です")
			assert.LessOrEqual(t, got, applyJobTimeout, "lock を残す job が閾値より長く生きると、生きている lock を lock の解除が消す")
		})

		t.Run("lock の形を確かめた版は mise.toml の Terraform の版と一致する", func(t *testing.T) {
			t.Parallel()

			entries, err := misetoml.ParseFile(filepath.Join("..", "..", "mise.toml"))
			require.NoError(t, err)

			got, ok := misetoml.Lookup(entries, "aqua:hashicorp/terraform")
			require.True(t, ok, "mise.toml に Terraform の宣言がありません")
			assert.Equal(t, lockFormatTerraform, got, "Terraform を上げたら、lockInfo の形を新しい版の locker.go で確かめ直す")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		old := lockNow.Add(-time.Hour)

		for name, c := range map[string]struct {
			meta, body string
			want       error
		}{
			"一覧の後に置き換わった lock は消さない":  {lockMeta(`"ffffffffffffffffffffffffffffffff"`, old), lockBody(lockID, "runner@x"), errLockChanged},
			"ETag の無い属性は読めないと扱う":      {lockMeta("", old), lockBody(lockID, "runner@x"), errLockMeta},
			"版 ID の無い属性の lock は消さない":  {`{"ETag":` + strconv.Quote(lockETagA) + `,"LastModified":"2026-11-12T10:00:00+00:00"}`, lockBody(lockID, "runner@x"), errLockMeta},
			"属性が JSON でない":            {"nope", lockBody(lockID, "runner@x"), errLockMeta},
			"LastModified が無い":        {`{"ETag":` + strconv.Quote(lockETagA) + `}`, lockBody(lockID, "runner@x"), errLockMeta},
			"LastModified の形が違う":      {`{"ETag":` + strconv.Quote(lockETagA) + `,"LastModified":"yesterday"}`, lockBody(lockID, "runner@x"), errLockMeta},
			"空の lock file":            {lockMeta(lockETagA, old), "", errLockInfo},
			"JSON でない lock file":      {lockMeta(lockETagA, old), "nope", errLockInfo},
			"ID の無い lock file":        {lockMeta(lockETagA, old), `{"Who":"runner@x"}`, errLockInfo},
			"ID が UUID でない lock file": {lockMeta(lockETagA, old), lockBody("not-a-uuid", "runner@x"), errLockInfo},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := lockStale(lockETagA, []byte(c.meta), []byte(c.body), lockNow)
				require.ErrorIs(t, err, c.want)
				assert.Empty(t, got)
			})
		}
	})
}

func Test_describeLock(t *testing.T) {
	t.Parallel()

	meta := objectMeta{VersionID: "ver-1"}
	modified := lockNow.Add(-time.Hour)

	for name, c := range map[string]struct {
		who, want, never string
	}{
		"CI のランナーの利用者はそのまま出す":  {"runner@fv-az1-2", "Who=runner@fv-az1-2", ""},
		"人の手元の利用者とホストは伏せる":     {"alice@laptop.local", "Who=CI のランナー以外", "alice"},
		"runner を含むだけの利用者も伏せる": {"xrunner@host", "Who=CI のランナー以外", "xrunner"},
		"Who が無ければ伏せる":         {"", "Who=CI のランナー以外", ""},
		"行を分ける文字を含めない":         {"runner@a\n::error::x", "Who=runner@a::error::x", "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := describeLock(lockInfo{ID: lockID, Operation: "OperationTypeApply", Who: c.who}, meta, modified, time.Hour)
			assert.Contains(t, got, c.want)
			assert.True(t, strings.HasPrefix(got, "lock: "), "行頭を workflow command にしない")
			if c.never != "" {
				assert.NotContains(t, got, c.never)
			}
		})
	}
}

func Test_logSafe(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ in, want string }{
		"そのままの値":        {"OperationTypeApply", "OperationTypeApply"},
		"制御文字と空白を除く":    {"a\r\n\tb c\x1b", "abc"},
		"長すぎる値は文字単位で切る": {strings.Repeat("あ", maxLogLength+1), strings.Repeat("あ", maxLogLength)},
		"上限ちょうどの値は切らない": {strings.Repeat("あ", maxLogLength), strings.Repeat("あ", maxLogLength)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, logSafe(c.in))
		})
	}
}

// entryTimeout は、dir の workflow のうち entry（`make ci-apply-` など）を走らせる job の timeout-minutes の最大値と、
// その job の数を返す。
func entryTimeout(t *testing.T, dir, entry string) (time.Duration, int) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	timeoutLine := regexp.MustCompile(`^ {4}timeout-minutes:\s*([0-9]+)\s*$`)

	var (
		maxMinutes int
		n          int
	)

	for _, path := range workflow.SelectFiles(names, dir) {
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		for _, job := range workflow.SplitJobs(string(content)).Jobs {
			minutes, applies := jobEntryTimeout(job, timeoutLine, entry)
			if !applies {
				continue
			}

			require.Positive(t, minutes, "%s の job %s に timeout-minutes がありません", path, job.ID)

			n++
			maxMinutes = max(maxMinutes, minutes)
		}
	}

	return time.Duration(maxMinutes) * time.Minute, n
}

var skippedLine = regexp.MustCompile(`^\s*#|^\s+(?:- )?name:`)

func jobEntryTimeout(job workflow.Job, timeoutLine *regexp.Regexp, entry string) (int, bool) {
	minutes, applies := 0, false

	for _, l := range job.Lines {
		if strings.Contains(l.Text, entry) && !skippedLine.MatchString(l.Text) {
			applies = true
		}

		if m := timeoutLine.FindStringSubmatch(l.Text); m != nil {
			minutes, _ = strconv.Atoi(m[1])
		}
	}

	return minutes, applies
}

func Test_entryTimeout(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, files map[string]string) string {
		t.Helper()

		dir := t.TempDir()
		for name, body := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
		}

		return dir
	}
	job := func(id, minutes, run string) string {
		return "  " + id + ":\n    runs-on: ubuntu-24.04\n    timeout-minutes: " + minutes + "\n    steps:\n      - run: " + run + "\n"
	}

	t.Run("apply の job だけを読み、unlock の job の timeout を拾わない", func(t *testing.T) {
		t.Parallel()

		dir := write(t, map[string]string{
			"apply.yaml":  "jobs:\n" + job("apply", "30", "make ci-apply-admin-identity RUNNER_MODE=host"),
			"unlock.yaml": "jobs:\n" + job("unlock", "15", "make ci-unlock-admin-identity RUNNER_MODE=host"),
		})
		got, n := entryTimeout(t, dir, "make ci-apply-")
		assert.Equal(t, 30*time.Minute, got)
		assert.Equal(t, 1, n)
	})

	t.Run("apply の job が無ければ0件と返す", func(t *testing.T) {
		t.Parallel()

		_, n := entryTimeout(t, write(t, map[string]string{"unlock.yaml": "jobs:\n" + job("unlock", "15", "make ci-unlock-admin-identity")}), "make ci-apply-")
		assert.Zero(t, n)
	})

	t.Run("複数の apply の job は最大値を返す", func(t *testing.T) {
		t.Parallel()

		got, n := entryTimeout(t, write(t, map[string]string{
			"a.yaml": "jobs:\n" + job("a", "30", "make ci-apply-admin-identity") + job("b", "45", "make ci-apply-admin"),
		}), "make ci-apply-")
		assert.Equal(t, 45*time.Minute, got)
		assert.Equal(t, 2, n)
	})

	t.Run("lock を残す入口は lock を残す job だけを数える", func(t *testing.T) {
		t.Parallel()

		got, n := entryTimeout(t, write(t, map[string]string{
			"apply.yaml": "jobs:\n" + job("apply", "30", "make ci-apply-admin-identity"),
			"hold.yaml":  "jobs:\n" + job("hold", "20", "make ci-hold-admin-identity"),
		}), "make ci-hold-")
		assert.Equal(t, 20*time.Minute, got)
		assert.Equal(t, 1, n)
	})
}

func Test_jobEntryTimeout(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`^ {4}timeout-minutes:\s*([0-9]+)\s*$`)
	lines := func(texts ...string) workflow.Job {
		job := workflow.Job{ID: "j"}
		for i, s := range texts {
			job.Lines = append(job.Lines, workflow.Line{Number: i + 1, Text: s})
		}

		return job
	}

	for name, c := range map[string]struct {
		job     workflow.Job
		minutes int
		applies bool
	}{
		"apply の job":             {lines("    timeout-minutes: 30", "      - run: make ci-apply-admin-identity"), 30, true},
		"timeout の無い apply の job": {lines("      - run: make ci-apply-admin-identity"), 0, true},
		"apply でない job":           {lines("    timeout-minutes: 15", "      - run: make ci-unlock-admin-identity"), 15, false},
		"コメントと name: の make ci-apply- は apply の job と数えない": {lines("    timeout-minutes: 15", "      # make ci-apply-x", "      - name: make ci-apply-x"), 15, false},
		"step の timeout は読まない":                             {lines("        timeout-minutes: 99", "      - run: make ci-apply-x"), 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			minutes, applies := jobEntryTimeout(c.job, re, "make ci-apply-")
			assert.Equal(t, c.minutes, minutes)
			assert.Equal(t, c.applies, applies)
		})
	}
}

func Test_readLockMeta(t *testing.T) {
	t.Parallel()

	modified := lockNow.Add(-time.Hour)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		meta, got, err := readLockMeta(lockETagA, []byte(lockMeta(lockETagA, modified)))
		require.NoError(t, err)
		assert.True(t, got.Equal(modified), got)
		assert.Equal(t, "ver-1", meta.VersionID)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			meta string
			want error
		}{
			"JSON でない":           {"nope", errLockMeta},
			"ETag が無い":           {`{"LastModified":"2026-11-12T10:00:00+00:00","VersionId":"v"}`, errLockMeta},
			"ETag が一覧と違う":        {lockMeta(`"ffffffffffffffffffffffffffffffff"`, modified), errLockChanged},
			"版 ID が無い":           {`{"ETag":` + strconv.Quote(lockETagA) + `,"LastModified":"2026-11-12T10:00:00+00:00"}`, errLockMeta},
			"LastModified の形が違う": {`{"ETag":` + strconv.Quote(lockETagA) + `,"LastModified":"yesterday","VersionId":"v"}`, errLockMeta},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, _, err := readLockMeta(lockETagA, []byte(c.meta))
				require.ErrorIs(t, err, c.want)
			})
		}
	})
}

func lockBodyOp(id, who, operation string) string {
	return strings.Replace(lockBody(id, who), `"OperationTypeApply"`, strconv.Quote(operation), 1)
}

func Test_wantLock(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		want, etag string
		err        error
	}{
		"指定が無ければ lock が在っても ETag を返す":         {"", lockETagA, nil},
		"指定が無ければ lock が無くても空を返す":              {"", "", nil},
		"absent で lock が無ければ空を返す":             {"absent", "", nil},
		"absent で lock が在れば errLockExists":    {"absent", lockETagA, errLockExists},
		"present で lock が在れば ETag を返す":        {"present", lockETagA, nil},
		"present で lock が無ければ errLockMissing": {"present", "", errLockMissing},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := wantLock(c.want, c.etag)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, c.etag, got)
		})
	}
}

func Test_ciLock(t *testing.T) {
	t.Parallel()

	modified := lockNow.Add(-time.Minute)

	t.Run("CI のランナーの Plan と Apply の lock は説明と LastModified を返す", func(t *testing.T) {
		t.Parallel()

		for _, op := range []string{planOperation, applyOperation} {
			line, got, err := ciLock(lockETagA, []byte(lockMeta(lockETagA, modified)), []byte(lockBodyOp(lockID, "runner@fv-az1-2", op)), lockNow)
			require.NoError(t, err)
			assert.Contains(t, line, "Operation="+op)
			assert.True(t, got.Equal(modified))
		}
	})

	t.Run("古さでは失敗しない", func(t *testing.T) {
		t.Parallel()

		_, _, err := ciLock(lockETagA, []byte(lockMeta(lockETagA, lockNow)), []byte(lockBody(lockID, "runner@fv-az1-2")), lockNow)
		require.NoError(t, err)
	})

	for name, c := range map[string]struct {
		meta, body string
		want       error
	}{
		"人の手元の lock は errLockForeign":     {lockMeta(lockETagA, modified), lockBody(lockID, "alice@laptop"), errLockForeign},
		"Plan と Apply 以外は errLockForeign": {lockMeta(lockETagA, modified), lockBodyOp(lockID, "runner@fv-az1-2", "OperationTypeRefresh"), errLockForeign},
		"ETag が違えば errLockChanged":        {lockMeta(`"ffffffffffffffffffffffffffffffff"`, modified), lockBody(lockID, "runner@fv-az1-2"), errLockChanged},
		"ID の形が違えば errLockInfo":           {lockMeta(lockETagA, modified), lockBody("not-a-uuid", "runner@fv-az1-2"), errLockInfo},
		"JSON でなければ errLockInfo":          {lockMeta(lockETagA, modified), "nope", errLockInfo},
		"空なら errLockInfo":                 {lockMeta(lockETagA, modified), "", errLockInfo},
		"属性が JSON でなければ errLockMeta":      {"nope", lockBody(lockID, "runner@fv-az1-2"), errLockMeta},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ciLock(lockETagA, []byte(c.meta), []byte(c.body), lockNow)
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_lockHeld(t *testing.T) {
	t.Parallel()

	t.Run("消せる時刻は LastModified に apply の job の timeout と余裕を足した時刻", func(t *testing.T) {
		t.Parallel()

		modified := time.Date(2026, 11, 12, 11, 59, 0, 0, time.UTC)
		got, err := lockHeld(lockETagA, []byte(lockMeta(lockETagA, modified)), []byte(lockBodyOp(lockID, "runner@fv-az1-2", planOperation)), lockNow)
		require.NoError(t, err)
		assert.Contains(t, got, "ID="+lockID)
		assert.Contains(t, got, "Terraform Unlock が消せるのは 2026-11-12T12:34:00Z より後")
	})

	t.Run("消せる時刻は lockStale が消し始める境界と一致する", func(t *testing.T) {
		t.Parallel()

		modified := lockNow.Add(-(applyJobTimeout + staleLockMargin))
		meta, body := []byte(lockMeta(lockETagA, modified)), []byte(lockBody(lockID, "runner@fv-az1-2"))

		_, err := lockStale(lockETagA, meta, body, lockNow)
		require.ErrorIs(t, err, errLockFresh, "境界の時刻ちょうどでは消さない")

		_, err = lockStale(lockETagA, meta, body, lockNow.Add(time.Second))
		require.NoError(t, err, "境界を過ぎれば消す")

		got, err := lockHeld(lockETagA, meta, body, lockNow)
		require.NoError(t, err)
		assert.Contains(t, got, lockNow.UTC().Format(time.RFC3339)+" より後")
	})

	t.Run("lock の解除が消さない形なら errLockForeign", func(t *testing.T) {
		t.Parallel()

		_, err := lockHeld(lockETagA, []byte(lockMeta(lockETagA, lockNow)), []byte(lockBody(lockID, "alice@laptop")), lockNow)
		require.ErrorIs(t, err, errLockForeign)
	})
}

func Test_runLockHeld(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m, b := filepath.Join(dir, "meta.json"), filepath.Join(dir, "lock.json")
	require.NoError(t, os.WriteFile(m, []byte(lockMeta(lockETagA, lockNow)), 0o600))
	require.NoError(t, os.WriteFile(b, []byte(lockBodyOp(lockID, "runner@fv-az1-2", planOperation)), 0o600))
	none := filepath.Join(dir, "none")
	d := deps{readFile: os.ReadFile, now: func() time.Time { return lockNow }}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		got, err := runLockHeld([]string{"-etag", lockETagA, "-meta", m, b}, d)
		require.NoError(t, err)
		assert.Contains(t, got, "Operation="+planOperation)
	})

	for name, c := range map[string]struct {
		args []string
		want error
	}{
		"位置引数が無ければ errUsage":            {[]string{"-etag", lockETagA, "-meta", m}, errUsage},
		"未知のフラグなら errUsage":             {[]string{"-nope", b}, errUsage},
		"属性のファイルを読めなければ errLockMeta":    {[]string{"-etag", lockETagA, "-meta", none, b}, errLockMeta},
		"lock file を読めなければ errLockInfo": {[]string{"-etag", lockETagA, "-meta", m, none}, errLockInfo},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runLockHeld(c.args, d)
			require.ErrorIs(t, err, c.want)
		})
	}
}

// waitDeps は、alive の値を使い切った後は最後の値を返す。
func waitDeps(alive []bool, sleeps *int) deps {
	i := 0

	return deps{
		readFile: os.ReadFile,
		alive: func(int) bool {
			v := alive[min(i, len(alive)-1)]
			i++

			return v
		},
		sleep: func(time.Duration) { *sleeps++ },
	}
}

func Test_lockWait(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "out.txt")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}
	header := "plan: commit=0123 dirty=false\nInitializing the backend...\n"

	for name, line := range map[string]string{
		"resource の refresh":  `aws_iam_role.apply: Refreshing state... [id=apply]`,
		"data source の読み取り":   `data.aws_caller_identity.current: Reading...`,
		"空白を含む key の address": `module.ci.aws_iam_role.this["a b"]: Refreshing state... [id=x]`,
	} {
		t.Run(name+"の行が在れば held を返す", func(t *testing.T) {
			t.Parallel()

			sleeps := 0
			got, err := lockWait(write(t, header+line+"\n"), 1, time.Second, time.Minute, waitDeps([]bool{true}, &sleeps))
			require.NoError(t, err)
			assert.Equal(t, "held", got)
			assert.Zero(t, sleeps)
		})
	}

	t.Run("plan が終わった後に書かれた行も読む", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		got, err := lockWait(write(t, header+"aws_iam_role.apply: Refreshing state... [id=apply]\n"), 1, time.Second, time.Minute, waitDeps([]bool{false}, &sleeps))
		require.NoError(t, err)
		assert.Equal(t, "held", got)
	})

	t.Run("行が現れずに plan が終われば空を返す", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		got, err := lockWait(write(t, header+"Acquiring state lock. This may take a few moments...\n"), 1, time.Second, time.Minute, waitDeps([]bool{true, true, false}, &sleeps))
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.Equal(t, 2, sleeps)
	})

	t.Run("行に似た文の途中の一致は数えない", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		got, err := lockWait(write(t, header+"  note: Refreshing state... is printed later\n"), 1, time.Second, time.Minute, waitDeps([]bool{false}, &sleeps))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("limit を過ぎれば errLockWait", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		_, err := lockWait(write(t, header), 1, time.Second, 3*time.Second, waitDeps([]bool{true}, &sleeps))
		require.ErrorIs(t, err, errLockWait)
		assert.Equal(t, 3, sleeps, "limit ちょうどの回まで読む")
	})

	t.Run("全文を読めなければ errLockWait", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		_, err := lockWait(filepath.Join(t.TempDir(), "none"), 1, time.Second, time.Minute, waitDeps([]bool{true}, &sleeps))
		require.ErrorIs(t, err, errLockWait)
	})
}

func Test_runLockWait(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.txt")
	require.NoError(t, os.WriteFile(path, []byte("data.aws_caller_identity.current: Reading...\n"), 0o600))

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		sleeps := 0
		got, err := runLockWait([]string{"-pid", "1", "-interval", "500ms", "-limit", "1m", path}, waitDeps([]bool{true}, &sleeps))
		require.NoError(t, err)
		assert.Equal(t, "held", got)
	})

	for name, args := range map[string][]string{
		"位置引数が無い":       {"-pid", "1"},
		"pid が無い":       {path},
		"pid が0以下":      {"-pid", "0", path},
		"interval が0以下": {"-pid", "1", "-interval", "0s", path},
		"limit が0以下":    {"-pid", "1", "-limit", "-1s", path},
		"未知のフラグ":        {"-nope", path},
	} {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()

			sleeps := 0
			_, err := runLockWait(args, waitDeps([]bool{true}, &sleeps))
			require.ErrorIs(t, err, errUsage)
		})
	}
}
