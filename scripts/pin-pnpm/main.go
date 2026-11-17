// Package main は、docker/tools/Dockerfile の node_tools 段が取る pnpm の単体バイナリを、
// 版と sha256 の組で固定するツール。
//
//	resolve  mise.toml が宣言する pnpm の版の GitHub Release から、配布物の digest を取り、
//	         Dockerfile の版と照合値を書き換える
//	check    Dockerfile の版が mise.toml の宣言と一致し、照合値が sha256 の形をしていることを
//	         検査する（通信しない）
//
// 照合そのものはイメージのビルドが `sha256sum -c` で行う。ここが守るのは、照合値がどの版の
// ものかである —— 版だけを上げて照合値を据え置くと、ビルドは照合で落ちるまで古い値を持ち回る。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	miseFile       = "mise.toml"
	dockerfile     = "docker/tools/Dockerfile"
	filePerm       = 0o644
	pnpmTool       = "aqua:pnpm/pnpm"
	pnpmRepo       = "pnpm/pnpm"
	defaultAPIBase = "https://api.github.com"
	requestTimeout = 60 * time.Second
)

var (
	errUsage    = xerrors.New("usage: pin-pnpm <resolve|check>")
	errShape    = xerrors.New("pnpm の宣言または Dockerfile の写しの形が想定と異なります")
	errDrift    = xerrors.New("Dockerfile の pnpm が mise.toml の宣言からずれています")
	errRelease  = xerrors.New("pnpm の GitHub Release から配布物の digest を取れません")
	errReplaced = xerrors.New("同じ版の pnpm の配布物の digest が、固定済みの値から変わっています")
)

var (
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	sha256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type pin struct {
	name string
	// asset は照合値の出所となる Release の配布物の名前。版の変数では空。
	asset string
}

// pins は Dockerfile が持つ pnpm の写しの一覧。**ARG ではなくシェル変数にする** —— `--build-arg`
// で外から差し替えられる照合値は、照合しているふりをしているだけになる。
var pins = []pin{
	{name: "pnpm_version"},
	{name: "pnpm_sha256_amd64", asset: "pnpm-linux-x64-musl.tar.gz"},
	{name: "pnpm_sha256_arm64", asset: "pnpm-linux-arm64-musl.tar.gz"},
}

// shellVarRe は `<名前>="<値>"` を捉える。`[^#\n]*?` でコメント行を除き、前置きを第1群に入れる
// 理由は scripts/versions の bakedRe と同じである。
func shellVarRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^([^#\n]*?\b` + regexp.QuoteMeta(name) + `=")([^"\n]*)(")`)
}

func main() {
	log.SetFlags(0)

	client := &http.Client{Timeout: requestTimeout}
	if err := run(context.Background(), os.Args[1:], ".", defaultAPIBase, client, os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(ctx context.Context, args []string, root, apiBase string, client *http.Client, out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}

	version, err := declaredVersion(filepath.Join(root, miseFile))
	if err != nil {
		return err
	}
	path := filepath.Join(root, dockerfile)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return xerrors.Wrap(err, dockerfile)
	}
	current, err := readPins(string(data))
	if err != nil {
		return err
	}

	switch args[0] {
	case "check":
		return check(version, current, out)
	case "resolve":
		want, err := fetchDigests(ctx, client, apiBase, version)
		if err != nil {
			return err
		}
		want["pnpm_version"] = version
		if err := detectReplaced(current, want); err != nil {
			return err
		}

		return write(path, string(data), current, want, out)
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}
}

func declaredVersion(path string) (string, error) {
	entries, err := misetoml.ParseFile(path)
	if err != nil {
		return "", err
	}
	version, ok := misetoml.Lookup(entries, pnpmTool)
	if !ok {
		return "", xerrors.Wrap(errShape, miseFile+" の [tools] に "+pnpmTool+" がありません")
	}
	// 移動する版（`12` や `latest`）は Release の tag へ一意に解決できない。
	if !versionRe.MatchString(version) {
		return "", xerrors.Wrap(errShape, pnpmTool+" の版が x.y.z の形をしていません: "+version)
	}

	return version, nil
}

// readPins は Dockerfile から pins の値を読みます。各変数はちょうど1件でなければなりません ——
// 0件は照合が消えたこと、2件以上はどちらが照合に使われるかが読み手に決まらないことを意味します。
func readPins(content string) (map[string]string, error) {
	got := map[string]string{}
	for _, p := range pins {
		m := shellVarRe(p.name).FindAllStringSubmatch(content, -1)
		if len(m) != 1 {
			return nil, xerrors.Wrap(errShape,
				fmt.Sprintf("%s の %s: 一致が %d 件、期待は 1 件", dockerfile, p.name, len(m)))
		}
		got[p.name] = m[0][2]
	}

	return got, nil
}

// check は Dockerfile の写しを宣言と照らします。照合値の正しさは通信なしには確かめられないので、
// 形だけを見ます。値が配布物と食い違えば、イメージのビルドが照合で落ちます。
func check(version string, current map[string]string, out io.Writer) error {
	var bad []string
	for _, p := range pins {
		v := current[p.name]
		switch {
		case p.asset == "" && v != version:
			bad = append(bad, fmt.Sprintf("%s=%q（宣言は %s）", p.name, v, version))
		case p.asset != "" && !sha256Re.MatchString(v):
			bad = append(bad, fmt.Sprintf("%s=%q（sha256 の形ではない）", p.name, v))
		}
	}
	if len(bad) > 0 {
		fmt.Fprintf(out, "❌ pin-pnpm: %s（make pin-pnpm-resolve で反映）\n", strings.Join(bad, ", "))

		return errDrift
	}

	fmt.Fprintf(out, "✅ pin-pnpm: pnpm %s の版と照合値 %d 件が Dockerfile に固定されています\n", version, len(pins)-1)

	return nil
}

type release struct {
	Assets []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// fetchDigests は Release `v<version>` の配布物の sha256 を、pins の変数名をキーにして返します。
func fetchDigests(ctx context.Context, client *http.Client, apiBase, version string) (map[string]string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/tags/v%s", strings.TrimRight(apiBase, "/"), pnpmRepo, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, xerrors.Wrap(errRelease, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, xerrors.Wrap(errRelease, fmt.Sprintf("%s: HTTP %d", url, resp.StatusCode))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, xerrors.Wrap(errRelease, err.Error())
	}

	digests := map[string]string{}
	for _, a := range rel.Assets {
		digests[a.Name] = a.Digest
	}

	got := map[string]string{}
	for _, p := range pins {
		if p.asset == "" {
			continue
		}
		// 配布物が無い、または digest を持たないなら、照合値を空や古い値のまま残さずに止める。
		sum, ok := strings.CutPrefix(digests[p.asset], "sha256:")
		if !ok || !sha256Re.MatchString(sum) {
			return nil, xerrors.Wrap(errRelease,
				fmt.Sprintf("v%s の %s: digest が sha256 の形ではありません: %q", version, p.asset, digests[p.asset]))
		}
		got[p.name] = sum
	}

	return got, nil
}

// detectReplaced は、版が同じまま照合値だけが変わったことを拒みます。同じ版の配布物が
// 差し替えられたことを意味し、版上げではなく供給網の事象である —— 書き換えずに止め、人が
// 調べる（/supply-chain-triage）。
func detectReplaced(current, want map[string]string) error {
	if current["pnpm_version"] != want["pnpm_version"] {
		return nil
	}
	for _, p := range pins {
		if p.asset == "" || !sha256Re.MatchString(current[p.name]) || current[p.name] == want[p.name] {
			continue
		}

		return xerrors.Wrap(errReplaced,
			fmt.Sprintf("v%s の %s: 固定済み %s、Release は %s", want["pnpm_version"], p.asset, current[p.name], want[p.name]))
	}

	return nil
}

// write は want を Dockerfile へ書きます。変化が無ければ書きません。
func write(path, content string, current, want map[string]string, out io.Writer) error {
	next := content
	var changed []string
	for _, p := range pins {
		if current[p.name] == want[p.name] {
			continue
		}
		changed = append(changed, p.name)
		// 置換は関数で組む。Go の置換文字列は `$` を後方参照として読むので、値を連結すると
		// `$1` が別の群へ化ける。
		re := shellVarRe(p.name)
		v := want[p.name]
		next = re.ReplaceAllStringFunc(next, func(m string) string {
			g := re.FindStringSubmatch(m)

			return g[1] + v + g[3]
		})
	}
	if len(changed) == 0 {
		fmt.Fprintf(out, "✅ pin-pnpm-resolve: pnpm %s は固定済みです\n", want["pnpm_version"])

		return nil
	}

	if err := atomicwrite.Apply(map[string]string{path: next}, filePerm); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ pin-pnpm-resolve: pnpm %s の %s を %s へ書きました\n",
		want["pnpm_version"], strings.Join(changed, ", "), dockerfile)

	return nil
}
