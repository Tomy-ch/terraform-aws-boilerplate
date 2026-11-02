package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// CI の plan の経路（.makefiles/terraform/operation/ci.mk）が使うサブコマンド。公開されるログに出るので、
// エラーの文に入力の値（アカウント ID、token、Bucket の key）を入れない。

// roleFormat は、usecases/deployment-identity の role 名 `<project>-<env>-ci-<役割>` の写し。
const roleFormat = "arn:aws:iam::%s:role/%s-%s-ci-%s"

const maxTokenSize = 16 << 10

var (
	roleNames      = []string{"plan", "apply", "apply-identity"}
	subPattern     = regexp.MustCompile(`^repo:[^\s:]+:environment:[A-Za-z0-9_.-]+$`)
	counterPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	branchPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
	rootPattern    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	errRole         = xerrors.New("❌ role の ARN を組めません（project・env・役割・アカウント ID のいずれかが不正です）")
	errOIDCToken    = xerrors.New("❌ OIDC token から sub を読めません")
	errPlanJSON     = xerrors.New("❌ plan の JSON を解釈できません")
	errPlanEmpty    = xerrors.New("❌ plan の JSON に resource の変更が1件もありません。検査の対象が0件です")
	errPlanErrored  = xerrors.New("❌ plan はエラーで終わっています")
	errPlanExitCode = xerrors.New("❌ plan の終了コードは 0 か 2 です")
	errPlanMismatch = xerrors.New("❌ plan の終了コードと JSON の変更の有無が食い違います")
	errPlanKey      = xerrors.New("❌ plan の出力の key を組めません")
)

func runRoleARN(args []string) (string, error) {
	fs := flag.NewFlagSet("role-arn", flag.ContinueOnError)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	role := fs.String("role", "", "")
	account := fs.String("account", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	return roleARN(*project, *env, *role, *account)
}

func roleARN(project, env, role, account string) (string, error) {
	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) ||
		!slices.Contains(roleNames, role) || !accountPattern.MatchString(account) {
		return "", errRole
	}

	return fmt.Sprintf(roleFormat, account, project, env, role), nil
}

// oidcSub は、GitHub Actions の OIDC token（JWT）の payload の sub を返します。署名は検証しない
// —— 値は公開ログへの表示用で、信頼の判断は AWS の STS が行う。token そのものはどこにも出さない。
func oidcSub(path string, readFile func(string) ([]byte, error)) (string, error) {
	raw, err := readFile(path)
	if err != nil {
		return "", xerrors.Wrap(errOIDCToken, "token のファイルを読めません")
	}

	token := strings.TrimSpace(string(raw))
	if token == "" || len(token) > maxTokenSize {
		return "", xerrors.Wrap(errOIDCToken, "token が空か、大きすぎます")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", xerrors.Wrap(errOIDCToken, "JWT の形ではありません")
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", xerrors.Wrap(errOIDCToken, "payload が base64url ではありません")
	}

	var claims struct {
		Sub string `json:"sub"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", xerrors.Wrap(errOIDCToken, "payload が JSON ではありません")
	}

	if !subPattern.MatchString(claims.Sub) {
		return "", xerrors.Wrap(errOIDCToken, "sub が environment 形式ではありません")
	}

	return claims.Sub, nil
}

type planJSON struct {
	FormatVersion   string           `json:"format_version"`
	Errored         bool             `json:"errored"`
	ResourceChanges []resourceChange `json:"resource_changes"`
	OutputChanges   map[string]struct {
		Actions []string `json:"actions"`
	} `json:"output_changes"`
}

type resourceChange struct {
	Address         string `json:"address"`
	PreviousAddress string `json:"previous_address"`
	Change          struct {
		Actions   []string        `json:"actions"`
		Importing json.RawMessage `json:"importing"`
	} `json:"change"`
}

type planCounts struct {
	importing, add, change, destroy, read, forget, move, outputs int
}

func (c planCounts) changed() bool {
	return c != planCounts{}
}

func (c planCounts) String() string {
	if !c.changed() {
		return "No changes."
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Plan: %d to import, %d to add, %d to change, %d to destroy.", c.importing, c.add, c.change, c.destroy)

	for _, extra := range []struct {
		n    int
		what string
	}{{c.read, "to read"}, {c.forget, "to forget"}, {c.move, "to move"}, {c.outputs, "output changes"}} {
		if extra.n > 0 {
			fmt.Fprintf(&b, " %d %s.", extra.n, extra.what)
		}
	}

	return b.String()
}

func runPlanSummary(args []string, readFile func(string) ([]byte, error)) (string, error) {
	fs := flag.NewFlagSet("plan-summary", flag.ContinueOnError)
	exitCode := fs.String("exitcode", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	data, err := readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errPlanJSON, "ファイルを読めません")
	}

	return planSummary(*exitCode, data)
}

// planSummary は、`terraform plan -detailed-exitcode` の終了コードと `terraform show -json` の変更を
// 独立に数え、食い違えば失敗します。どちらか一方だけでは、plan の失敗と「変更なし」を取り違えうるためです。
func planSummary(exitCode string, data []byte) (string, error) {
	if exitCode != "0" && exitCode != "2" {
		return "", errPlanExitCode
	}

	var p planJSON
	if err := json.Unmarshal(data, &p); err != nil || p.FormatVersion == "" {
		return "", errPlanJSON
	}

	if p.Errored {
		return "", errPlanErrored
	}

	if len(p.ResourceChanges) == 0 {
		return "", errPlanEmpty
	}

	c, err := countChanges(p)
	if err != nil {
		return "", err
	}

	if c.changed() != (exitCode == "2") {
		return "", xerrors.Wrap(errPlanMismatch, "終了コード "+exitCode+"、JSON "+c.String())
	}

	return c.String(), nil
}

func countChanges(p planJSON) (planCounts, error) {
	var c planCounts

	for _, rc := range p.ResourceChanges {
		if len(rc.Change.Importing) > 0 && string(rc.Change.Importing) != "null" {
			c.importing++
		}

		if rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address {
			c.move++
		}

		switch strings.Join(rc.Change.Actions, ",") {
		case "no-op":
		case "create":
			c.add++
		case "update":
			c.change++
		case "delete":
			c.destroy++
		case "delete,create", "create,delete":
			c.add++
			c.destroy++
		case "read":
			c.read++
		case "forget":
			c.forget++
		default:
			return planCounts{}, xerrors.Wrap(errPlanJSON, "知らない actions: "+strconv.Quote(strings.Join(rc.Change.Actions, ",")))
		}
	}

	for _, oc := range p.OutputChanges {
		if !slices.Equal(oc.Actions, []string{"no-op"}) {
			c.outputs++
		}
	}

	return c, nil
}

func runPlanKey(args []string) (string, error) {
	fs := flag.NewFlagSet("plan-key", flag.ContinueOnError)
	pr := fs.String("pr", "", "")
	branch := fs.String("branch", "", "")
	runID := fs.String("run", "", "")
	attempt := fs.String("attempt", "", "")
	root := fs.String("root", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	return planKey(*pr, *branch, *runID, *attempt, *root)
}

// planKey は、plan の出力を置く object の key を返します。Pull Request の plan は `pr/`、保護ブランチの
// plan は `branch/` の下に置く —— plan の role が書けるのはこの2つの接頭辞だけである
// （usecases/deployment-identity/internal/github-oidc/policies.tf）。
func planKey(pr, branch, runID, attempt, root string) (string, error) {
	if !counterPattern.MatchString(runID) || !counterPattern.MatchString(attempt) || !rootPattern.MatchString(root) {
		return "", xerrors.Wrap(errPlanKey, "run・attempt・root のいずれかが不正です")
	}

	name := fmt.Sprintf("%s-%s-%s.txt", runID, attempt, root)

	switch {
	case pr != "" && branch == "":
		if !counterPattern.MatchString(pr) {
			return "", xerrors.Wrap(errPlanKey, "Pull Request の番号が不正です")
		}

		return "pr/" + pr + "/" + name, nil
	case branch != "" && pr == "":
		if !branchPattern.MatchString(branch) || slices.Contains(strings.Split(branch, "/"), "..") || slices.Contains(strings.Split(branch, "/"), ".") {
			return "", xerrors.Wrap(errPlanKey, "ブランチ名が不正です")
		}

		return "branch/" + branch + "/" + name, nil
	default:
		return "", xerrors.Wrap(errPlanKey, "-pr と -branch のどちらか1つを渡してください")
	}
}
