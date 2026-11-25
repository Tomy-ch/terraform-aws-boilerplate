package main

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	stageStage1       = "stage1"
	stageBeforeApply  = "before-apply"
	stageStage4       = "stage4"
	stagePermissions  = "permissions"
	stageStage5       = "stage5"
	stageOutputPolicy = "output-policy"

	sourceLive      = "live"
	sourceReference = "reference"
)

var (
	accountID = regexp.MustCompile(`^[0-9]{12}$`)
	// awsErrorCode は、aws が失敗したときの stderr の `An error occurred (<code>)`。
	awsErrorCode = regexp.MustCompile(`An error occurred \(([A-Za-z0-9.]+)\)`)

	errNoAccount = xerrors.New("アカウント ID が12桁ではありません。make aws-login をやり直してください")
	// errNoChecks は、確認が1件も無いこと（ADR-0702 決定13）。
	errNoChecks = xerrors.New("確認が1件もありません")
	// errBadStep は、確認の表に見出しも判定も持たない行があること。
	errBadStep = xerrors.New("確認の表に、見出しも判定も持たない行があります")
)

type verifyOptions struct {
	stage        string
	project      string
	region       string
	executor     string
	policySource string
}

// parseVerifyArgs は verify の引数を読む。-executor は stage4、-policy-source は output-policy にだけ渡せる。
// -policy-source の既定は live。
func parseVerifyArgs(args []string) (verifyOptions, error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var o verifyOptions

	fs.StringVar(&o.stage, "stage", "", "")
	fs.StringVar(&o.project, "project", "", "")
	fs.StringVar(&o.region, "region", "", "")
	fs.StringVar(&o.executor, "executor", "", "")
	fs.StringVar(&o.policySource, "policy-source", "", "")

	if err := fs.Parse(args); err != nil {
		return verifyOptions{}, xerrors.Wrap(errUsage, err.Error())
	}

	stages := []string{stageStage1, stageBeforeApply, stageStage4, stagePermissions, stageStage5, stageOutputPolicy}

	switch {
	case fs.NArg() != 0 || o.project == "" || o.region == "" || !slices.Contains(stages, o.stage):
		return verifyOptions{}, errUsage
	case o.executor != "" && o.stage != stageStage4:
		return verifyOptions{}, errUsage
	case o.policySource != "" && o.stage != stageOutputPolicy:
		return verifyOptions{}, errUsage
	}

	if o.stage == stageOutputPolicy {
		if o.policySource == "" {
			o.policySource = sourceLive
		}

		if o.policySource != sourceLive && o.policySource != sourceReference {
			return verifyOptions{}, errUsage
		}
	}

	return o, nil
}

// ciRole は、CI の入口の登録簿で種別 kind が root の種類 r に対して取る role。登録簿は固定なので、無ければ panic する。
func ciRole(kind string, r cientry.Root) string {
	k, ok := cientry.Lookup(kind)
	if role := k.Role(r); ok && role != "" {
		return role
	}

	panic(fmt.Sprintf("CI の入口の登録簿に種別 %s の %s の role がありません", kind, r))
}

// names は、確認が名指す resource の名前。CI の role の名前は登録簿から組む。
type names struct {
	account  string
	region   string
	project  string
	prefix   string
	executor string

	bucket      string
	planBucket  string
	applyBucket string
	keyAlias    string

	plan           string
	apply          string
	applyIdentity  string
	unlockIdentity string
	unlock         string
}

func newNames(o verifyOptions, account string) names {
	prefix := o.project + "-admin"
	bucket := func(kind string) string {
		return fmt.Sprintf("%s-%s-%s-%s-an", prefix, kind, account, o.region)
	}

	return names{
		account: account, region: o.region, project: o.project, prefix: prefix, executor: o.executor,
		bucket: bucket("tfstate"), planBucket: bucket("tfplan"), applyBucket: bucket("tfapply"),
		keyAlias:       "alias/" + prefix + "-tfstate",
		plan:           ciRole(cientry.Plan, cientry.RootIdentity),
		apply:          ciRole(cientry.Apply, cientry.RootWorkload),
		applyIdentity:  ciRole(cientry.Apply, cientry.RootIdentity),
		unlockIdentity: ciRole(cientry.Unlock, cientry.RootIdentity),
		unlock:         ciRole(cientry.Unlock, cientry.RootWorkload),
	}
}

// ciRoles は CI の role の短い名前を、確認の出力の順で返す。
func (n names) ciRoles() []string {
	return []string{n.plan, n.apply, n.applyIdentity, n.unlockIdentity, n.unlock}
}

func (n names) roleName(role string) string { return n.prefix + "-ci-" + role }

func (n names) roleARN(role string) string {
	return "arn:aws:iam::" + n.account + ":role/" + n.roleName(role)
}

func (n names) policyARN(name string) string {
	return "arn:aws:iam::" + n.account + ":policy/" + n.roleName(name)
}

// s3 は、bucket の所有者を確かめる s3api の呼び出しの引数。
func (n names) s3(sub, bucket string, extra ...string) []string {
	return append([]string{"s3api", sub, "--bucket", bucket, "--expected-bucket-owner", n.account}, extra...)
}

// step は出力の1行。check の eval が nil なら note をそのまま出す（見出しと注記）。skipped は、前提の policy が
// 付いていないなどで確かめなかった区分を表す行であること。
type step struct {
	note    string
	check   check
	skipped bool
}

func noteStep(s string) step { return step{note: s} }

// skipStep は、確かめなかった区分の注記。
func skipStep(s string) step { return step{note: s, skipped: true} }

func checkStep(c check) step { return step{check: c} }

// fixedCheck は、先に決まった判定をそのまま返す確認。
func fixedCheck(desc string, v verdict) check {
	return check{desc: desc, eval: func(*query) (verdict, error) { return v, nil }}
}

// runChecks は steps を順に出し、確認の件数と NG の件数を結果の行に出す。確かめなかった区分があれば、その数も
// 結果の行に添える（終了コードは変えない）。NG があれば errMismatch。aws を起動できなくなれば、結果の行を出さずに
// そのエラーを返す。
func runChecks(q *query, out io.Writer, steps []step) error {
	total, fails, skipped := 0, 0, 0

	for _, s := range steps {
		if s.skipped {
			skipped++
		}

		if s.check.eval == nil {
			if s.note == "" {
				return errBadStep
			}

			fmt.Fprintln(out, s.note)

			continue
		}

		v, err := s.check.eval(q)
		if err != nil {
			return err
		}

		total++

		switch {
		case v.ok:
			fmt.Fprintf(out, "ok  %s\n", s.check.desc)
		case v.reason != "":
			fails++

			fmt.Fprintf(out, "NG  %s（%s）\n", s.check.desc, v.reason)
		default:
			fails++

			fmt.Fprintf(out, "NG  %s\n", s.check.desc)
		}
	}

	if total == 0 {
		return errNoChecks
	}

	if skipped > 0 {
		fmt.Fprintf(out, "# 結果: %d 件のうち NG %d 件（確かめていない区分 %d）\n", total, fails, skipped)
	} else {
		fmt.Fprintf(out, "# 結果: %d 件のうち NG %d 件\n", total, fails)
	}

	if fails > 0 {
		return errMismatch
	}

	return nil
}

// expectError は、aws の呼び出しが code のエラーで失敗すれば ok とする確認。
func expectError(desc, code string, args ...string) check {
	return check{desc: desc, eval: func(q *query) (verdict, error) {
		r, err := q.call("json", args...)
		if err != nil {
			return verdict{}, err
		}

		if !r.failed() {
			return verdict{reason: "エラーが返らなかった"}, nil
		}

		got := ""
		if m := awsErrorCode.FindStringSubmatch(r.stderr); m != nil {
			got = m[1]
		}

		switch got {
		case code:
			return verdict{ok: true}, nil
		case "":
			return verdict{reason: code + " ではないエラー"}, nil
		default:
			return verdict{reason: code + " ではないエラー: " + got}, nil
		}
	}}
}

// document は、応答の Policy（JSON の文字列）を object として読む。
func (q *query) document(args ...string) (map[string]any, string, error) {
	v, reason, err := q.object(args...)
	if err != nil || reason != "" {
		return nil, reason, err
	}

	s, _ := v["Policy"].(string)

	doc, ok := decodeObject([]byte(s))
	if !ok {
		return nil, "応答を読めない", nil
	}

	return doc, "", nil
}

// expectDocument は、応答の Policy の文書が pred を満たせば ok とする確認。
func expectDocument(desc string, pred func(map[string]any) bool, args ...string) check {
	return check{desc: desc, eval: func(q *query) (verdict, error) {
		v, reason, err := q.document(args...)
		if err != nil || reason != "" {
			return verdict{reason: reason}, err
		}

		return verdict{ok: pred(v)}, nil
	}}
}

// verifyEnv は、段の確認の表を組むときに使うもの。
type verifyEnv struct {
	q        *query
	n        names
	readFile func(string) ([]byte, error)
	source   string
}

// verifySteps は、段の確認の表。表を組むために読む応答（条件つきの確認の有無、文書）はここで読む。
func verifySteps(stage string, e verifyEnv) ([]step, error) {
	switch stage {
	case stageStage1:
		return stage1Steps(e.n), nil
	case stageBeforeApply:
		return beforeApplySteps(e.n), nil
	case stageStage4:
		return stage4Steps(e.n, e.readFile), nil
	case stagePermissions:
		return permissionsSteps(e.q, e.n)
	case stageStage5:
		return stage5Steps(e.n), nil
	case stageOutputPolicy:
		return outputPolicySteps(e)
	}

	return nil, errUsage
}

// checkVerify は、bootstrap の段の確認を実環境を読んで行う。何も作らず、何も変えない。
func checkVerify(o verifyOptions, out io.Writer, d deps) error {
	profile, err := awsProfile(d.getenv)
	if err != nil {
		return err
	}

	q := newQuery(d.aws, o.region)

	r, err := q.call("text", "sts", "get-caller-identity", "--query", "Account")
	if err != nil {
		return err
	}

	if r.failed() {
		return xerrors.Wrapf(errNoAccount, "sts get-caller-identity が失敗: %s", firstLine(r.stderr))
	}

	account := strings.TrimSpace(string(r.stdout))
	if !accountID.MatchString(account) {
		return xerrors.Wrapf(errNoAccount, "sts get-caller-identity の Account: %q", account)
	}

	steps, err := verifySteps(o.stage, verifyEnv{q: q, n: newNames(o, account), readFile: d.readFile, source: o.policySource})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "# %s（profile: %s、region: %s）\n", o.stage, profile, o.region)

	return runChecks(q, out, steps)
}
