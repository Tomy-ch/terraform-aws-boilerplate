package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// CI の資格情報の段。どちらも OIDC の token を GitHub Actions から取り、aws だけを起動する（ADR-0702 決定24）。
//
//	ci-credentials は、token で role を1回だけ assume し、使い捨てのランナーの ~/.aws に役割の profile を置く。
//	ci-assume-denied は、同じ token で pull_request で起動しない種別の role（cientry.DeniedRoles）を assume し、
//	すべてが STS の AccessDenied で拒否されることを確かめる。資格情報はどこにも置かない。
//
// token と assume の入力は、作業ディレクトリの input.json（--cli-input-json）で渡し、終わるときに消す。

const (
	stepAssumeRole = "assume-role"

	oidcAttempts = 3
	oidcTimeout  = 30 * time.Second
)

var (
	errCredResponse = xerrors.New("❌ assume の応答から資格情報を読めません")
	errCredProfile  = xerrors.New("❌ profile に資格情報を置けません")
	errCredRun      = xerrors.New("❌ run の番号（GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT）がありません")
)

// fetchFunc は、bearer を付けて url を GET し、応答の本文を返します。テストは差し替えます。
type fetchFunc func(url, bearer string) ([]byte, error)

// fetchOIDC は fetchFunc の実物です。接続の失敗と 5xx は oidcAttempts 回まで試します。
func fetchOIDC(url, bearer string) ([]byte, error) {
	client := &http.Client{Timeout: oidcTimeout}
	var last error
	for range oidcAttempts {
		body, retry, err := fetchOnce(client, url, bearer)
		if err == nil {
			return body, nil
		}
		last = err
		if !retry {
			break
		}
	}

	return nil, last
}

func fetchOnce(client *http.Client, url, bearer string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "bearer "+bearer)
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*maxTokenSize))
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= http.StatusInternalServerError, xerrors.Newf("HTTP %d", resp.StatusCode)
	}

	return body, false, nil
}

// ciOIDC は、GitHub Actions から STS 向けの OIDC token を取り、sub を stdout へ出して token を返します。
func ciOIDC(d deps, stdout io.Writer) (string, error) {
	u, err := oidcURL(d.environ)
	if err != nil {
		return "", err
	}
	raw, err := d.fetch(u, envValue(d.environ, "ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	if err != nil {
		// 要求先の URL と bearer は文に入れない。
		return "", xerrors.Wrap(errOIDCResponse, "token の要求が失敗しました")
	}
	token, err := oidcToken(raw)
	if err != nil {
		return "", err
	}
	sub, err := oidcSub(token)
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(stdout, "OIDC の sub: %s\n", sub); err != nil {
		return "", err
	}

	return token, nil
}

// envValue は environ の name の値を返します。無ければ空です。
func envValue(environ []string, name string) string {
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}

	return ""
}

// ciAssumeEnv は、assume に渡す環境です。資格情報を変える変数を外し（envUnset と同じ集合）、profile を使わずに
// token だけで assume させます。
func ciAssumeEnv(environ []string, region string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !unset(name) {
			env = append(env, kv)
		}
	}

	return append(env, "AWS_EC2_METADATA_DISABLED=true", "AWS_REGION="+region)
}

// ciTarget は、ci-credentials と ci-assume-denied が共有する値（token・region・アカウント ID・run の番号）です。
type ciTarget struct {
	project, env, region, account, run string
	token                              string
}

// ciPrepare は、引数の検査の後に token を取り、identity root の region と、environment の secret の
// アカウント ID を読みます。
func ciPrepare(d deps, stdout io.Writer, project, env, base string) (ciTarget, error) {
	run, attempt := envValue(d.environ, "GITHUB_RUN_ID"), envValue(d.environ, "GITHUB_RUN_ATTEMPT")
	if !counterPattern.MatchString(run) || !counterPattern.MatchString(attempt) {
		return ciTarget{}, errCredRun
	}
	dir, err := localRoot(base, env, "identity")
	if err != nil {
		return ciTarget{}, err
	}
	region, err := rootRegion(dir, d.readFile)
	if err != nil {
		return ciTarget{}, err
	}
	token, err := ciOIDC(d, stdout)
	if err != nil {
		return ciTarget{}, err
	}

	return ciTarget{project: project, env: env, region: region, account: envValue(d.environ, "CI_ACCOUNT_ID"),
		run: run + "-" + attempt, token: token}, nil
}

// assumeInput は、role を assume する input.json を work に書きます。token を含むので所有者だけが読めます。
func (c ciTarget) assumeInput(work, role, session string) error {
	arn, err := roleARN(c.project, c.env, role, c.account)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]string{"RoleArn": arn, "RoleSessionName": session, "WebIdentityToken": c.token})
	if err != nil {
		return xerrors.Wrap(err, "input.json")
	}

	return xerrors.Wrap(os.WriteFile(filepath.Join(work, "input.json"), raw, 0o600), "input.json")
}

// ciWorkUnder は、-work の値 parent を root の tmp/ の下に限って検証し、その下に pattern の一時ディレクトリを
// 作ります。返す関数がその子だけを消し、消し残せば w へ警告します（子は OIDC の token を置く）。tmp/ の外の値は
// errUsage です。
func ciWorkUnder(root, parent, pattern string, w io.Writer) (string, func(), error) {
	dir, err := workdir.Resolve(root, parent)
	if err != nil {
		return "", nil, xerrors.Wrapf(errUsage, "-work %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, xerrors.Wrapf(err, "%s の作成", parent)
	}
	work, cleanup, err := workdir.Temp(dir, pattern)
	if err != nil {
		return "", nil, xerrors.Wrap(err, "作業ディレクトリ")
	}
	if w == nil {
		w = io.Discard
	}

	return work, func() {
		cleanup()
		if _, err := os.Lstat(work); !os.IsNotExist(err) {
			fmt.Fprintf(w, "⚠️ 作業ディレクトリ %s を消せませんでした\n", work)
		}
	}, nil
}

// ciCredentials は、OIDC の token で role を1回だけ assume し、資格情報を GitHub Actions のログで伏せる指示を
// 出してから、~/.aws に役割の profile（ci.mk の CI_PROFILE と同じ名前）を置きます。
func ciCredentials(args []string, d deps, stdout io.Writer) (string, error) {
	fs := flag.NewFlagSet("ci-credentials", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	role := fs.String("role", "", "")
	base := fs.String("base", "", "")
	workParent := fs.String("work", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *project == "" || *role == "" || *base == "" || *workParent == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	c, err := ciPrepare(d, stdout, *project, *env, *base)
	if err != nil {
		return "", err
	}
	work, cleanup, err := ciWorkUnder(d.root, *workParent, "assume-", d.stderr)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if err := c.assumeInput(work, *role, "gha-"+c.run); err != nil {
		return "", err
	}
	out, err := runTool(d.tool, work, ciAssumeEnv(d.environ, c.region), stepAssumeRole, d.stderr)
	if err != nil {
		return "", err
	}
	creds, err := parseCredentials(out)
	if err != nil {
		return "", err
	}
	for _, v := range []string{creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken} {
		if _, err := fmt.Fprintf(stdout, "::add-mask::%s\n", v); err != nil {
			return "", err
		}
	}
	profile := fmt.Sprintf("%s-%s-ci-%s", c.project, c.env, *role)
	if err := writeProfile(envValue(d.environ, "HOME"), profile, creds, c.region); err != nil {
		return "", err
	}

	return "✅ " + profile + " の profile に資格情報を置きました", nil
}

// credentials は、assume の応答の資格情報です。
type credentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"`
}

// parseCredentials は、`aws sts assume-role-with-web-identity --output json` の応答から資格情報を返します。
// どれかが空か、改行や空白・節の見出しの括弧を含めば失敗します（profile のファイルを壊さないため）。文には値を入れない。
// `=` は拒まない: SessionToken は base64 で末尾に詰め物の `=` が付くことがあり、INI は行の最初の `=` で分けるので値の `=` は壊さない。
func parseCredentials(raw []byte) (credentials, error) {
	var resp struct {
		Credentials credentials `json:"Credentials"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return credentials{}, xerrors.Wrap(errCredResponse, "応答が JSON ではありません")
	}
	c := resp.Credentials
	for _, v := range []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken} {
		if v == "" || strings.ContainsAny(v, " \t\r\n[]") {
			return credentials{}, xerrors.Wrap(errCredResponse, "資格情報が空か、profile に書けない文字を含みます")
		}
	}

	return c, nil
}

// writeProfile は、home の .aws/credentials と .aws/config の profile の節を置き換えます（aws configure set と
// 同じ置き場と節の名前）。ほかの節は残します。ファイルは所有者だけが読める権限で書きます。
func writeProfile(home, profile string, c credentials, region string) error {
	if !filepath.IsAbs(home) {
		return xerrors.Wrap(errCredProfile, "HOME が絶対パスではありません")
	}
	dir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return xerrors.Wrap(errCredProfile, err.Error())
	}
	files := []struct {
		name, section string
		body          []string
	}{
		{"credentials", profile, []string{"aws_access_key_id = " + c.AccessKeyID, "aws_secret_access_key = " + c.SecretAccessKey, "aws_session_token = " + c.SessionToken}},
		{"config", "profile " + profile, []string{"region = " + region}},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		old, err := os.ReadFile(filepath.Clean(path))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return xerrors.Wrap(errCredProfile, err.Error())
		}
		if err := writeFileAtomic(path, replaceSection(old, f.section, f.body)); err != nil {
			return xerrors.Wrap(errCredProfile, err.Error())
		}
	}

	return nil
}

// replaceSection は、INI の本文 src から [section] の節を除き、末尾に新しい節を足します。
func replaceSection(src []byte, section string, body []string) []byte {
	var out bytes.Buffer
	skip := false
	for line := range strings.Lines(string(src)) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			skip = strings.TrimSpace(trimmed[1:len(trimmed)-1]) == section
		}
		if !skip {
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteString("\n")
			}
		}
	}
	fmt.Fprintf(&out, "[%s]\n%s\n", section, strings.Join(body, "\n"))

	return out.Bytes()
}

// writeFileAtomic は、同じディレクトリの一時ファイルに 0600 で書いてから置き換えます。既存のファイルの権限は
// 引き継ぎません（資格情報と state を書くため）。
func writeFileAtomic(path string, data []byte) error {
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	name := filepath.Base(path)
	tmp := "." + name + ".tmp"
	_ = r.Remove(tmp)
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return r.Rename(tmp, name)
}

// ciAssumeDenied は、同じ job の OIDC token で、cientry.DeniedRoles の role を1つずつ assume し、すべてが
// AccessDenied で拒否されることを確かめます。最初の拒否されなかった role で失敗し、後の role を試しません。
// assume の応答（資格情報）は stdout ごと捨て、stderr はアカウント ID を含みうるのでログへ出しません。
func ciAssumeDenied(args []string, d deps, stdout io.Writer) (string, error) {
	fs := flag.NewFlagSet("ci-assume-denied", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	base := fs.String("base", "", "")
	workParent := fs.String("work", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *project == "" || *base == "" || *workParent == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	roles := cientry.DeniedRoles()
	if len(roles) == 0 {
		// 確かめる role が無いまま成功すれば、拒否の確認が外れたことと区別できない。
		return "", xerrors.Wrap(errUsage, "拒否を確かめる role が登録簿に1つもありません")
	}
	c, err := ciPrepare(d, stdout, *project, *env, *base)
	if err != nil {
		return "", err
	}
	work, cleanup, err := ciWorkUnder(d.root, *workParent, "assume-", d.stderr)
	if err != nil {
		return "", err
	}
	defer cleanup()
	for _, role := range roles {
		if err := c.assumeInput(work, role, "gha-denied-"+c.run); err != nil {
			return "", err
		}
		code, stderr, err := runExitCode(d.tool, work, ciAssumeEnv(d.environ, c.region), stepAssumeRole)
		if err != nil {
			return "", err
		}
		if code == 0 {
			return "", xerrors.Wrapf(errAssumeSucceeded, "%s の role", role)
		}
		line, err := assumeDenied(role, stderr)
		if xerrors.Is(err, errAssumeUnreadable) {
			// stderr は文に入れられないので、STS に届かなかった失敗の手掛かりは終了コードだけが運ぶ。
			return "", xerrors.Wrapf(err, "aws の終了コード %d", code)
		}
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("✅ %d 個の role の assume がすべて拒否されました", len(roles)), nil
}

// runExitCode は、道具を起動して終了コードと stderr を返します。stdout は捨てます。起動できない・シグナルで
// 終わったときは toolrun.ErrTool です。
func runExitCode(tool toolFunc, dir string, env []string, step string) (int, []byte, error) {
	var stderr bytes.Buffer
	cmd := tool(context.Background(), dir, env, step)
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return 0, nil, xerrors.Wrap(err, step)
	}

	return code, stderr.Bytes(), nil
}
