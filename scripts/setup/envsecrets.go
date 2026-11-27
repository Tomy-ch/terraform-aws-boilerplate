package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	accountIDName    = "AWS_ACCOUNT_ID"
	environmentsFile = ".github/settings/environments.json"
	// managementEnv は、値を資格情報のアカウントの ID にする env。
	managementEnv = "admin"
)

var (
	errGHFailed     = xerrors.New("gh の呼び出しが失敗しました")
	errGHResponse   = xerrors.New("gh の応答を読めません")
	errEnvironment  = xerrors.New("environment が .github/settings/environments.json にありません")
	errNoAccountEnv = xerrors.New("environment の名前が <env>-<role> の形ではないので、置くアカウント ID が決まりません")
	errNoMember     = xerrors.New("メンバーアカウントが組織にありません")
	errEntryEnv     = xerrors.New("admin の environment は bootstrap-env-secrets で、メンバーの environment は member-env-secrets で置きます")

	accountID  = regexp.MustCompile(`^[0-9]{12}$`)
	repoName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	errGHStart = xerrors.New("gh を起動できません")
	// ghRepoEnv は、gh の書き込みの宛先のリポジトリを変える環境変数。
	ghRepoEnv = []string{"GH_REPO", "GH_HOST"}
)

type ghFunc func(stdin []byte, args ...string) awsResult

// hostGH は、bin（名前なら PATH から探す）を子プロセスとして起動する ghFunc を返す。
func hostGH(bin string) ghFunc {
	return func(stdin []byte, args ...string) awsResult {
		path, err := exec.LookPath(bin)
		if err != nil {
			return awsResult{err: err}
		}

		var stdout, stderr bytes.Buffer

		cmd := &exec.Cmd{Path: path, Args: append([]string{bin}, args...), Stdin: bytes.NewReader(stdin), Stdout: &stdout, Stderr: &stderr}
		err = cmd.Run()

		return awsResult{stdout: stdout.Bytes(), stderr: stderr.String(), err: err}
	}
}

// ghOps は、gh で起動してよい操作（ADR-0702 決定26）。
var ghOps = [][]string{{"secret", "set"}, {"secret", "list"}}

func runGH(gh ghFunc, stdin []byte, args ...string) ([]byte, error) {
	if len(args) < 2 || !slices.ContainsFunc(ghOps, func(o []string) bool { return slices.Equal(o, args[:2]) }) {
		return nil, xerrors.Wrap(errNotAllowed, "gh "+strings.Join(args, " "))
	}

	r := gh(stdin, args...)

	var exitErr *exec.ExitError

	switch {
	case r.err == nil:
		return r.stdout, nil
	case xerrors.As(r.err, &exitErr):
		return nil, xerrors.Wrap(errGHFailed, "gh "+args[0]+" "+args[1]+": "+firstLine(r.stderr))
	default:
		return nil, xerrors.Join(errGHStart, r.err)
	}
}

// secretUpdated は、environment の secret AWS_ACCOUNT_ID の更新日時。無ければ nil。
func secretUpdated(gh ghFunc, repo, env string) (*time.Time, error) {
	out, err := runGH(gh, nil, "secret", "list", "--repo", repo, "--env", env, "--json", "name,updatedAt")
	if err != nil {
		return nil, err
	}

	var secrets []struct {
		Name      string    `json:"name"`
		UpdatedAt time.Time `json:"updatedAt"`
	}

	if b := bytes.TrimSpace(out); len(b) == 0 || b[0] != '[' {
		return nil, xerrors.Wrap(errGHResponse, "gh secret list: JSON の配列ではありません")
	}

	if err := json.Unmarshal(out, &secrets); err != nil {
		return nil, xerrors.Join(errGHResponse, err)
	}

	for _, s := range secrets {
		if s.Name == "" {
			return nil, xerrors.Wrap(errGHResponse, "gh secret list: name の無い secret があります")
		}

		if s.Name == accountIDName {
			if s.UpdatedAt.IsZero() {
				return nil, xerrors.Wrap(errGHResponse, "gh secret list: "+accountIDName+" の updatedAt がありません")
			}

			t := s.UpdatedAt

			return &t, nil
		}
	}

	return nil, nil
}

func declaredEnvironments(readFile func(string) ([]byte, error)) ([]string, error) {
	b, err := readFile(environmentsFile)
	if err != nil {
		return nil, xerrors.Join(errEnvironment, err)
	}

	var envs []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(b, &envs); err != nil {
		return nil, xerrors.Join(errEnvironment, err)
	}

	var names []string

	for _, e := range envs {
		if e.Name == "" {
			return nil, xerrors.Wrap(errEnvironment, "name の無い environment があります")
		}

		names = append(names, e.Name)
	}

	if len(names) == 0 {
		return nil, xerrors.Wrap(errEnvironment, "environment が0件です")
	}

	return names, nil
}

func targetEnvironments(o options) ([]string, error) {
	declared, err := declaredEnvironments(o.readFile)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, e := range strings.Split(o.environments, ",") {
		e = strings.TrimSpace(e)

		switch {
		case e == "":
			return nil, xerrors.Wrap(errUsage, "-environments に空の名前があります")
		case !slices.Contains(declared, e):
			return nil, xerrors.Wrap(errEnvironment, e)
		case slices.Contains(out, e):
			return nil, xerrors.Wrap(errUsage, "-environments の "+e+" が重複しています")
		}

		out = append(out, e)
	}

	return out, nil
}

// envOf は、environment 名 `<env>-<role>` の env。role は CI の入口の種別の登録簿（lib/cientry）から導く。
func envOf(name string) (string, bool) {
	for _, r := range cientry.Roles() {
		if base, ok := strings.CutSuffix(name, "-"+r); ok && base != "" {
			return base, true
		}
	}

	return "", false
}

// accountIDs は、environment ごとに置くアカウント ID。member が偽なら admin の environment だけを受け、資格情報の
// アカウントの ID にする。真ならメンバーの environment だけを受け、組織の `<project>-<env>` のアカウントの ID にする
// （資格情報が組織の管理アカウントのものであることは、入口の前提として確かめてある）。
func accountIDs(o options, c *client, envs []string, member bool) (map[string]string, error) {
	if len(envs) == 0 {
		return nil, xerrors.Wrap(errUsage, "置く environment がありません")
	}

	bases := map[string]string{}

	for _, e := range envs {
		base, ok := envOf(e)

		switch {
		case !ok:
			return nil, xerrors.Wrap(errNoAccountEnv, e)
		case (base == managementEnv) == member:
			return nil, xerrors.Wrap(errEntryEnv, e)
		}

		bases[e] = base
	}

	ids := map[string]string{}

	if !member {
		var caller struct {
			Account string `json:"Account"`
		}

		if err := c.read(&caller, "sts", "get-caller-identity"); err != nil {
			return nil, err
		}

		if !accountID.MatchString(caller.Account) {
			return nil, xerrors.Wrap(errAWSResponse, "資格情報のアカウント ID を読めません")
		}

		for _, e := range envs {
			ids[e] = caller.Account
		}

		return ids, nil
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return nil, err
	}

	for _, e := range envs {
		name := accountName(o.project, bases[e])

		a, found, err := accountNamed(accounts, name)

		switch {
		case err != nil:
			return nil, err
		case !found:
			return nil, xerrors.Wrap(errNoMember, name)
		case isClosed(a):
			return nil, xerrors.Wrap(errPrecondition, "アカウント "+name+" は閉鎖済みか閉鎖の途中です")
		case !accountID.MatchString(a.ID):
			return nil, xerrors.Wrap(errAWSResponse, "アカウント "+name+" の ID を読めません")
		}

		ids[e] = a.ID
	}

	return ids, nil
}

// setEnvSecrets は、environment に secret AWS_ACCOUNT_ID（その environment の env のアカウントの ID。member による違いは
// accountIDs）を置く。値は gh の標準入力で渡し、出力にも引数にも出さない。値は読み返せないので、在る secret は
// -replace のときだけ置き直し、置いたことは更新日時が新しくなることで確かめる。宛先のリポジトリは -repo で固定する。
func setEnvSecrets(o options, c *client, t *terminal, gh ghFunc, member bool) error {
	if !repoName.MatchString(o.repo) {
		return xerrors.Wrap(errUsage, "-repo は <owner>/<リポジトリ> の形で渡します")
	}

	for _, k := range ghRepoEnv {
		if o.getenv(k) != "" {
			return xerrors.Wrap(errPrecondition, k+" が環境に在ります。gh の宛先が変わるので書き込みません")
		}
	}

	envs, err := targetEnvironments(o)
	if err != nil {
		return err
	}

	ids, err := accountIDs(o, c, envs, member)
	if err != nil {
		return err
	}

	before := map[string]*time.Time{}

	var targets []string

	for _, e := range envs {
		u, err := secretUpdated(gh, o.repo, e)
		if err != nil {
			return err
		}

		before[e] = u

		switch {
		case u == nil:
			fmt.Fprintf(t.out, "  %s: %s 無い\n", e, accountIDName)

			targets = append(targets, e)
		case o.replace:
			fmt.Fprintf(t.out, "  %s: %s 在る（更新 %s）。置き直す\n", e, accountIDName, u.UTC().Format(time.DateTime))

			targets = append(targets, e)
		default:
			fmt.Fprintf(t.out, "  %s: %s 在る（更新 %s。値は読めないので確かめていない）。書かない（置き直すなら REPLACE=1）\n", e, accountIDName, u.UTC().Format(time.DateTime))
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(t.out, "✅ すべての environment に secret が在ります（値は読めないので確かめていません）。書き込みません")

		return nil
	}

	var order []string

	byID := map[string][]string{}

	for _, e := range targets {
		if _, ok := byID[ids[e]]; !ok {
			order = append(order, ids[e])
		}

		byID[ids[e]] = append(byID[ids[e]], e)
	}

	for _, id := range order {
		fmt.Fprintf(t.out, "置くもの: リポジトリ %s の %s に %s（アカウント ID の末尾 %s）\n",
			o.repo, strings.Join(byID[id], "・"), accountIDName, id[len(id)-4:])
	}

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, e := range targets {
		if _, err := runGH(gh, []byte(ids[e]), "secret", "set", accountIDName, "--repo", o.repo, "--env", e); err != nil {
			return err
		}

		if err := reread(o, func() (bool, error) {
			u, err := secretUpdated(gh, o.repo, e)

			return u != nil && (before[e] == nil || u.After(*before[e])), err
		}); err != nil {
			return xerrors.Wrap(err, "置いた後に "+e+" の "+accountIDName+" の更新を読めません。gh secret list --repo "+o.repo+" --env "+e+" で確かめてください")
		}

		fmt.Fprintf(t.out, "✅ %s に %s を置きました\n", e, accountIDName)
	}

	return nil
}
