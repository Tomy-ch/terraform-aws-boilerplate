package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"gopkg.in/yaml.v3"
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
func writeLock(root string, out io.Writer, tool toolFunc, tables sourceTables) error {
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

	return checkLock(root, tables)
}

// checkLock は、mise.lock が mise.toml の宣言と同じ道具・版を持ち、lockfile_platforms の各 platform の取得元が
// 宣言の指す配布元であることを検めます。mise.lock は CI とイメージのビルドが道具を引く唯一の出所なので
// （ADR-0503 決定2）、ここで見ない取得元は誰も見ません。
func checkLock(root string, tables sourceTables) error {
	tools, err := lockTools(root)
	if err != nil {
		return err
	}
	if len(tools) == 0 {
		return xerrors.Wrap(errShape, miseFile+" の [tools] に lock する道具がありません")
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
	problems = append(problems, orphanEntries(tools, locked, tables)...)
	if len(problems) > 0 {
		return xerrors.Wrap(errLock, strings.Join(problems, "; "))
	}

	return nil
}

// shortBackends は、mise.toml が短い名前で宣言する道具と、その名前が解決されるべき backend です。lock の backend を
// 宣言の側から決めた値と突き合わせるために持ちます —— lock が書いた backend を信じると、backend と url を揃えて
// 別の repo へ向けた lock が通ります。表に無い短い名前は落とします。
var shortBackends = map[string]string{
	"actionlint":    "aqua:rhysd/actionlint",
	"golangci-lint": "aqua:golangci/golangci-lint",
	"hadolint":      "aqua:hadolint/hadolint",
	"lefthook":      "aqua:evilmartians/lefthook",
	"shellcheck":    "aqua:koalaman/shellcheck",
	"go":            "core:go",
	"node":          "core:node",
}

// unverifiedSources は、配布元が照合値を公開しないので mise.lock に照合値を持てない backend と、その理由です。
// ここに載る道具は TLS だけで配布元を信じて入る。
var unverifiedSources = map[string]string{
	// 撤回: AWS CLI の配布物の照合値を宣言して照合するようになったとき（#131）。
	"aqua:aws/aws-cli": "AWS は awscli.amazonaws.com の配布物の照合値を公開しない",
}

// checksumRe は mise.lock の照合値の形です。
var checksumRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// sourceTables は、宣言の道具と突き合わせる3つの表です。孤児の検査が、どの表を宣言と突き合わせるかを
// 呼び出し側から受けるために束ねます。
type sourceTables struct {
	short, offGitHub, unverified map[string]string
}

// repoTables は、このリポジトリの mise.toml と mise.lock に当てる表です。
var repoTables = sourceTables{short: shortBackends, offGitHub: offGitHubHosts, unverified: unverifiedSources}

// orphanEntries は、shortBackends・offGitHubHosts・unverifiedSources のうち、宣言のどの道具にも当たらない
// エントリを名指します。消えた道具のエントリが残ると、同じ名前の道具が戻ったとき、照合値の免除などが黙って
// 掛かります。
func orphanEntries(tools []misetoml.Entry, locked []misetoml.LockEntry, tables sourceTables) []string {
	declared := map[string]bool{}
	for _, t := range tools {
		declared[t.Key] = true
	}
	backends := map[string]bool{}
	for _, e := range locked {
		backends[e.Backend] = true
	}
	var problems []string
	for _, key := range slices.Sorted(maps.Keys(tables.short)) {
		if !declared[key] {
			problems = append(problems, "shortBackends の "+key+" は mise.toml の [tools] にありません")
		}
	}
	for _, table := range []struct {
		name    string
		entries []string
	}{
		{"offGitHubHosts", slices.Sorted(maps.Keys(tables.offGitHub))},
		{"unverifiedSources", slices.Sorted(maps.Keys(tables.unverified))},
	} {
		for _, b := range table.entries {
			if !backends[b] {
				problems = append(problems, table.name+" の "+b+" は mise.lock のどの backend にも当たりません")
			}
		}
	}

	return problems
}

// expectedBackend は、mise.toml のキーが解決されるべき backend を返します。
func expectedBackend(key string) (string, bool) {
	if _, ok := repoOf(key); ok {
		return key, true
	}
	if strings.HasPrefix(key, "go:") || strings.HasPrefix(key, "npm:") {
		return key, true
	}
	b, ok := shortBackends[key]

	return b, ok
}

// checkSources は1つの道具の backend と、platform ごとの取得元と照合値を検めます。go: と npm: は配布物を持たない
// backend なので、platform を持たないことだけを見ます。
func checkSources(key string, e misetoml.LockEntry, platforms []string) []string {
	want, ok := expectedBackend(key)
	if !ok {
		return []string{key + " の解決されるべき backend を決められません（shortBackends に足す）"}
	}
	if e.Backend != want {
		return []string{fmt.Sprintf("%s の backend が %s です（宣言からは %s）", key, e.Backend, want)}
	}
	if strings.HasPrefix(e.Backend, "go:") || strings.HasPrefix(e.Backend, "npm:") {
		if len(e.Platforms) > 0 {
			return []string{key + " は platform の取得元を持たない backend です"}
		}

		return nil
	}

	var problems []string
	_, unverified := unverifiedSources[e.Backend]
	for _, p := range platforms {
		src, ok := e.Platforms[p]
		if !ok || src.URL == "" {
			problems = append(problems, fmt.Sprintf("%s の %s に url がありません", key, p))

			continue
		}
		if msg := checkURL(e.Backend, e.Version, src.URL); msg != "" {
			problems = append(problems, fmt.Sprintf("%s の %s の url %s: %s", key, p, src.URL, msg))
		}
		switch {
		case unverified && src.Checksum != "":
			problems = append(problems, fmt.Sprintf("%s の %s が照合値を持ちます（unverifiedSources から外す）", key, p))
		case !unverified && !checksumRe.MatchString(src.Checksum):
			problems = append(problems, fmt.Sprintf("%s の %s の照合値 %q が sha256 ではありません", key, p, src.Checksum))
		}
	}
	for p := range e.Platforms {
		if !slices.Contains(platforms, p) {
			problems = append(problems, fmt.Sprintf("%s の %s は lockfile_platforms にありません", key, p))
		}
	}

	return problems
}

// checkURL は、url が backend の配布元を https で指し、正規化した path に版を含むかを見ます。問題が無ければ
// 空を返します。
func checkURL(backend, version, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "query・fragment・利用者を持たない https の url ではありません"
	}
	if path.Clean(u.Path) != u.Path {
		return "path が正規化されていません"
	}
	if !versionIn(u.Path, version) {
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

// versionIn は、p の中に version が別の版の一部としてでなく現れるかを見ます。前は数字と `.` 以外、後ろは数字以外
// （`.` なら、その次が数字以外）でなければなりません。`go1.27.4.linux-amd64` は 1.27.4 を含み、`v0.50.0` は 0.5 を
// 含みません。
func versionIn(p, version string) bool {
	re := regexp.MustCompile(`(?:^|[^0-9.])` + regexp.QuoteMeta(version) + `(?:$|[^0-9.]|\.[^0-9])`)

	return re.MatchString(p)
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

const (
	setupMiseAction = ".github/actions/setup-mise/action.yaml"
	toolsDockerfile = "docker/tools/Dockerfile"
)

// checkLockedInstalls は、CI とイメージのビルドが MISE_LOCKED で道具を入れることを検めます。どちらかが外れると、
// その経路は lock を読まずに上流の API で版を解決し、照合値も確かめません。
func checkLockedInstalls(root string) error {
	var problems []string
	action, err := os.ReadFile(filepath.Clean(filepath.Join(root, setupMiseAction)))
	if err != nil {
		return xerrors.Wrap(err, setupMiseAction)
	}
	if msg := lockedAction(action); msg != "" {
		problems = append(problems, setupMiseAction+": "+msg)
	}
	dockerfile, err := os.ReadFile(filepath.Clean(filepath.Join(root, toolsDockerfile)))
	if err != nil {
		return xerrors.Wrap(err, toolsDockerfile)
	}
	if msg := lockedDockerfile(dockerfile); msg != "" {
		problems = append(problems, toolsDockerfile+": "+msg)
	}
	if len(problems) > 0 {
		return xerrors.Wrap(errLock, strings.Join(problems, "; "))
	}

	return nil
}

// miseInstallsRe は、run の中で mise が道具を入れる起動です。install のほか、入っていない道具を入れる use・exec と
// その略記も数えます。
var miseInstallsRe = regexp.MustCompile(`\bmise\s+(?:install|i|use|u|exec|x)\b`)

// lockedAction は、composite action の、mise が道具を入れる step がどれも env の MISE_LOCKED を "1" に持ち、
// そうした step が1つ以上あるかを見ます。問題が無ければ空を返します。
func lockedAction(content []byte) string {
	var action struct {
		Runs struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(content, &action); err != nil {
		return "読めません: " + err.Error()
	}
	installs := 0
	for _, st := range action.Runs.Steps {
		if !miseInstallsRe.MatchString(st.Run) {
			continue
		}
		installs++
		if st.Env["MISE_LOCKED"] != "1" {
			return fmt.Sprintf("step %q が MISE_LOCKED を \"1\" にしていません", st.Name)
		}
	}
	if installs == 0 {
		return "mise が道具を入れる step がありません"
	}

	return ""
}

// lockedAssignRe は、MISE_LOCKED の代入です。
var lockedAssignRe = regexp.MustCompile(`\bMISE_LOCKED=(\S*)`)

// lockedDockerfile は、Dockerfile の MISE_LOCKED の代入がちょうど1つで 1 であり、mise が最初に道具を入れるより前に
// あるかを見ます。`#` で始まる行はコメントとして読みません。問題が無ければ空を返します。
func lockedDockerfile(content []byte) string {
	var (
		assigns []string
		at      int
		install = -1
		offset  int
	)
	for line := range strings.Lines(string(content)) {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			for _, m := range lockedAssignRe.FindAllStringSubmatchIndex(line, -1) {
				assigns = append(assigns, line[m[2]:m[3]])
				at = offset + m[0]
			}
			if loc := miseInstallsRe.FindStringIndex(line); loc != nil && install < 0 {
				install = offset + loc[0]
			}
		}
		offset += len(line)
	}
	if len(assigns) != 1 {
		return fmt.Sprintf("MISE_LOCKED の代入が %d 件です（1件にする）", len(assigns))
	}
	if assigns[0] != "1" {
		return "MISE_LOCKED が " + assigns[0] + " です"
	}
	if install < 0 {
		return "mise が道具を入れる行がありません"
	}
	if at > install {
		return "MISE_LOCKED の代入が、mise が最初に道具を入れるより後ろにあります"
	}

	return ""
}
