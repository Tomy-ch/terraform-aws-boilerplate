package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type toolFunc = toolrun.Func

const stepMiseLock = "mise-lock"

var (
	// errLock は mise.lock が mise.toml の宣言と合わないことです。
	errLock = xerrors.New("mise.lock が mise.toml の宣言と合いません")
	// errLockFailed は mise lock が 0 以外で終わったことです。
	errLockFailed = xerrors.New("mise lock が失敗しました")
)

// offGitHubHosts は、GitHub Release の外から配布物を取る backend と、その配布元です。ほかの backend の url は
// github.com の、backend が名指す owner/repo の Release でなければなりません。
var offGitHubHosts = map[string]string{
	"core:go":                  "dl.google.com",
	"core:node":                "nodejs.org",
	"aqua:hashicorp/terraform": "releases.hashicorp.com",
	"aqua:aws/aws-cli":         "awscli.amazonaws.com",
}

func command(ctx context.Context, dir string, env []string, _ string, operands ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "mise", append([]string{"lock", "--"}, operands...)...) //nolint:gosec // G204: operand は mise.toml から読んだ道具のキーで、境の後ろだけに置く。道具と option はリテラル。gosec が境の後ろの operand を区別できるようになれば外す
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// lockTools は mise.toml の [tools] のうち、disable_tools に入らない道具のキーを、宣言の順で返します。
// disable_tools の道具（npm:）は mise が入れないので lock に置かない。素の `mise lock` はそれも lock し、
// .mise/locks/ に npm の lockfile まで書く。
func lockTools(root string) ([]misetoml.Entry, error) {
	path := filepath.Join(root, miseFile)
	entries, err := misetoml.ParseFile(path)
	if err != nil {
		return nil, err
	}
	disabled, _, err := misetoml.DisableToolsFile(path)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(entries, func(e misetoml.Entry) bool { return slices.Contains(disabled, e.Key) }), nil
}

// writeLock は mise.lock を `mise lock` で書き直します。取得元と照合値の解決には上流の API が要るので、
// これを走らせるのは手元だけです。
func writeLock(root string, out io.Writer, tool toolFunc) error {
	tools, err := lockTools(root)
	if err != nil {
		return err
	}
	if len(tools) == 0 {
		return xerrors.Wrap(errShape, miseFile+" の [tools] に lock する道具がありません")
	}
	keys := make([]string, 0, len(tools))
	for _, t := range tools {
		keys = append(keys, t.Key)
	}

	cmd := tool(context.Background(), root, os.Environ(), stepMiseLock, keys...)
	cmd.Stdout, cmd.Stderr = out, out
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return xerrors.Wrapf(errLockFailed, "mise lock の終了コード %d", code)
	}

	return checkLock(root)
}

// checkLock は、mise.lock が mise.toml の宣言と同じ道具・版を持ち、lockfile_platforms の各 platform の取得元が
// 宣言の指す配布元であることを検めます。mise.lock は CI とイメージのビルドが道具を引く唯一の出所なので
// （ADR-0503 決定2）、ここで見ない取得元は誰も見ません。
func checkLock(root string) error {
	tools, err := lockTools(root)
	if err != nil {
		return err
	}
	platforms, found, err := misetoml.LockfilePlatformsFile(filepath.Join(root, miseFile))
	if err != nil {
		return err
	}
	if !found || len(platforms) == 0 {
		return xerrors.Wrap(errShape, miseFile+" の [settings] に lockfile_platforms がありません")
	}
	locked, err := misetoml.ParseLockFile(filepath.Join(root, misetoml.LockFile))
	if err != nil {
		return err
	}

	byKey := map[string]misetoml.LockEntry{}
	for _, e := range locked {
		byKey[e.Key] = e
	}
	var problems []string
	for _, t := range tools {
		e, ok := byKey[t.Key]
		if !ok {
			problems = append(problems, t.Key+" の entry がありません")

			continue
		}
		delete(byKey, t.Key)
		if e.Version != t.Version {
			problems = append(problems, fmt.Sprintf("%s の版が %s（宣言は %s）", t.Key, e.Version, t.Version))

			continue
		}
		problems = append(problems, checkSources(t.Key, e, platforms)...)
	}
	for _, e := range locked {
		if _, extra := byKey[e.Key]; extra {
			problems = append(problems, e.Key+" は lock する道具ではありません")
		}
	}
	if len(problems) > 0 {
		return xerrors.Wrap(errLock, strings.Join(problems, "; "))
	}

	return nil
}

// checkSources は1つの道具の、platform ごとの取得元を検めます。go: と npm: は配布物を持たない backend なので、
// platform を持たないことだけを見ます。
func checkSources(key string, e misetoml.LockEntry, platforms []string) []string {
	if strings.HasPrefix(e.Backend, "go:") || strings.HasPrefix(e.Backend, "npm:") {
		if len(e.Platforms) > 0 {
			return []string{key + " は platform の取得元を持たない backend です"}
		}

		return nil
	}
	if _, ok := repoOf(key); ok && e.Backend != key {
		return []string{fmt.Sprintf("%s の backend が %s です", key, e.Backend)}
	}

	var problems []string
	for _, p := range platforms {
		src, ok := e.Platforms[p]
		if !ok || src.URL == "" {
			problems = append(problems, fmt.Sprintf("%s の %s に url がありません", key, p))

			continue
		}
		if msg := checkURL(e.Backend, e.Version, src.URL); msg != "" {
			problems = append(problems, fmt.Sprintf("%s の %s の url %s: %s", key, p, src.URL, msg))
		}
	}
	for p := range e.Platforms {
		if !slices.Contains(platforms, p) {
			problems = append(problems, fmt.Sprintf("%s の %s は lockfile_platforms にありません", key, p))
		}
	}

	return problems
}

// checkURL は、url が backend の配布元を https で指し、版を含むかを見ます。問題が無ければ空を返します。
func checkURL(backend, version, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return "https の url ではありません"
	}
	if !strings.Contains(u.Path, version) {
		return "版 " + version + " を含みません"
	}
	if host, ok := offGitHubHosts[backend]; ok {
		if u.Host != host {
			return "配布元は " + host + " です"
		}

		return ""
	}
	name, ok := repoOf(backend)
	if !ok {
		return "配布元を決められない backend です"
	}
	if u.Host != "github.com" || !strings.HasPrefix(u.Path, "/"+name+"/releases/download/") {
		return "配布元は github.com/" + name + " の Release です"
	}

	return ""
}

// repoOf は `aqua:owner/repo` と `github:owner/repo` の owner/repo を返します。
func repoOf(key string) (string, bool) {
	for _, prefix := range []string{"aqua:", "github:"} {
		if name, ok := strings.CutPrefix(key, prefix); ok && strings.Count(name, "/") == 1 {
			return name, true
		}
	}

	return "", false
}

// lockedInstalls は、道具を mise.lock からだけ入れる設定を持つべきファイルと、その行です。どちらかが外れると、
// その経路は lock を読まずに上流の API で版を解決し、照合値も確かめません。
var lockedInstalls = []struct {
	path string
	re   *regexp.Regexp
}{
	{".github/actions/setup-mise/action.yaml", regexp.MustCompile(`(?m)^\s*MISE_LOCKED: "1"\s*$`)},
	{"docker/tools/Dockerfile", regexp.MustCompile(`(?m)^\s*MISE_LOCKED=1\s*$`)},
}

// checkLockedInstalls は、CI とイメージのビルドが MISE_LOCKED で道具を入れることを検めます。
func checkLockedInstalls(root string) error {
	var missing []string
	for _, l := range lockedInstalls {
		content, err := os.ReadFile(filepath.Clean(filepath.Join(root, l.path)))
		if err != nil {
			return xerrors.Wrap(err, l.path)
		}
		if !l.re.Match(content) {
			missing = append(missing, l.path)
		}
	}
	if len(missing) > 0 {
		return xerrors.Wrap(errLock, "MISE_LOCKED で道具を入れていません: "+strings.Join(missing, ", "))
	}

	return nil
}
