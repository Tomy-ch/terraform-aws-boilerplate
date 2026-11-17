// notify-channel-lint は、notify.yaml を呼ぶ job が、その job の系統の secret を渡していることを検査します。
//
// 解いている問題:
//
// 通知の宛先は、呼び出し側が notify.yaml の `slack-webhook-url` へ渡す secret で決まります。
// notify.yaml には別の系統へ代わりに送る経路が無いので、取り違えが起きるのは呼び出し側だけです。
// 本番の apply が dev の secret を渡しても、actionlint も他の検査も何も言わず、本番の通知が
// dev のチャンネルへ届きます。割当は .github/notify-channels.toml が宣言し、この検査が
// workflow の実物と突き合わせます。
//
// 宣言は2種類の鍵を持ちます。`"channel:<系統>" = "<secret>"` が系統の secret を、
// `"job:<file>:<job>" = "<系統>"` が job の系統を決めます。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lockfile"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	notifyWorkflow      = "./.github/workflows/notify.yaml"
	defaultWorkflowsDir = ".github/workflows"
	defaultDecl         = ".github/notify-channels.toml"
	channelPrefix       = "channel:"
	jobPrefix           = "job:"
)

var (
	notifyUse = workflow.UsesActionPattern(notifyWorkflow, false)
	// 渡し方として認めるのは、secret を1つ名指す形だけです。式で組むと、どの secret が渡るかを
	// 字面から決められません。
	webhookLine = regexp.MustCompile(`^\s*slack-webhook-url:\s*\$\{\{\s*secrets\.([A-Za-z0-9_]+)\s*\}\}\s*(?:#.*)?$`)
	webhookKey  = regexp.MustCompile(`slack-webhook-url`)
	inherit     = regexp.MustCompile(`^\s*secrets:\s*inherit\s*(?:#.*)?$`)
	// 呼び出しを数え直す独立の経路。表記（引用符、フロー記法、`..` を含む path、@ref）を問わず、
	// uses と notify.yaml を同じ行に持つものを数える。notifyUse が読めない形の呼び出しは、
	// ここと件数が合わず、検査から黙って外れずに違反になる。
	notifyMention = regexp.MustCompile(`\buses\b.*notify\.ya?ml`)
	commentLine   = regexp.MustCompile(`^\s*#`)

	declFormat = lockfile.Format{Line: regexp.MustCompile(`^"([^"]+)"\s*=\s*"([A-Za-z0-9_-]+)"$`)}
)

var (
	errNoCaller   = xerrors.New("notify.yaml を呼ぶ job が1件も見つかりません")
	errViolations = xerrors.New("違反があります")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoCaller) {
			log.Printf("❌ notify-channel-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ notify-channel-lint: %v", err)
	}
}

type decl struct {
	channels map[string]string // 系統 → secret
	jobs     map[string]string // "<file>:<job>" → 系統
}

func readDecl(path string) (decl, []lintreport.Finding, error) {
	raw, err := declFormat.Read(path)
	if err != nil {
		return decl{}, nil, xerrors.Wrapf(err, "%s の読み取り", path)
	}

	d := decl{channels: map[string]string{}, jobs: map[string]string{}}
	var findings []lintreport.Finding
	for key, value := range raw {
		switch {
		case strings.HasPrefix(key, channelPrefix):
			d.channels[strings.TrimPrefix(key, channelPrefix)] = value
		case strings.HasPrefix(key, jobPrefix):
			d.jobs[strings.TrimPrefix(key, jobPrefix)] = value
		default:
			findings = append(findings, lintreport.Finding{File: path, Line: 1,
				Message: fmt.Sprintf("鍵 %q は %q でも %q でも始まりません", key, channelPrefix, jobPrefix)})
		}
	}

	// 2つの系統が同じ secret を持つと、系統を分けた意味が無くなります。
	owner := map[string]string{}
	for _, name := range sortedKeys(d.channels) {
		secret := d.channels[name]
		if other, dup := owner[secret]; dup {
			findings = append(findings, lintreport.Finding{File: path, Line: 1,
				Message: fmt.Sprintf("系統 %s と %s が同じ secret %s を持ちます", other, name, secret)})
			continue
		}
		owner[secret] = name
	}
	for _, job := range sortedKeys(d.jobs) {
		if _, ok := d.channels[d.jobs[job]]; !ok {
			findings = append(findings, lintreport.Finding{File: path, Line: 1,
				Message: fmt.Sprintf("job %s の系統 %s は宣言されていません", job, d.jobs[job])})
		}
	}

	return d, findings, nil
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("notify-channel-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("workflows", defaultWorkflowsDir, "workflow を探すディレクトリ")
	declPath := fs.String("decl", defaultDecl, "系統の割当の宣言")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	d, findings, err := readDecl(*declPath)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", *dir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	seen := map[string]bool{}
	for _, path := range workflow.SelectFiles(names, *dir) {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", path)
		}
		split := workflow.SplitJobs(string(raw))
		if !split.Found {
			findings = append(findings, lintreport.Finding{File: path, Line: 1, Message: "jobs: が見つかりません"})
			continue
		}
		callers := 0
		for _, job := range split.Jobs {
			if !callsNotify(job) {
				continue
			}
			callers++
			key := filepath.Base(path) + ":" + job.ID
			seen[key] = true
			findings = append(findings, checkCaller(path, key, job, d)...)
		}
		if mentions := countMentions(string(raw)); mentions != callers {
			findings = append(findings, lintreport.Finding{File: path, Line: 1,
				Message: fmt.Sprintf("notify.yaml を uses: で指す行が %d 行あるのに、job として読めた呼び出しは %d 件です。"+
					"`  <job>:` の見出しの下に `    uses: ./.github/workflows/notify.yaml` と書いてください", mentions, callers)})
		}
	}

	if len(seen) == 0 {
		return errNoCaller
	}

	// 宣言に在って実物に無い job は、消えた job の割当か名前の書き違いです。残すと、
	// 同じ名前で足し直した job が、誰も確かめていない系統を受け継ぎます。
	for _, key := range sortedKeys(d.jobs) {
		if !seen[key] {
			findings = append(findings, lintreport.Finding{File: *declPath, Line: 1,
				Message: fmt.Sprintf("job %s は notify.yaml を呼ぶ job として見つかりません", key)})
		}
	}

	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ notify-channel-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}

	fmt.Fprintf(out, "✅ notify-channel-lint: notify.yaml を呼ぶ job %d 件の系統を確認しました\n", len(seen))
	return nil
}

// countMentions は、コメント行を除き、uses と notify.yaml を同じ行に持つ行を数えます。
func countMentions(source string) int {
	n := 0
	for line := range strings.Lines(source) {
		if !commentLine.MatchString(line) && notifyMention.MatchString(line) {
			n++
		}
	}
	return n
}

func callsNotify(job workflow.Job) bool {
	for _, ln := range job.Lines {
		if notifyUse.MatchString(ln.Text) {
			return true
		}
	}
	return false
}

// checkCaller は、notify.yaml を呼ぶ job 1つが、宣言した系統の secret をちょうど1回渡しているかを見ます。
func checkCaller(path, key string, job workflow.Job, d decl) []lintreport.Finding {
	channel, declared := d.jobs[key]
	if !declared {
		return []lintreport.Finding{{File: path, Line: job.Number,
			Message: fmt.Sprintf("job %s の系統が %s に宣言されていません", key, defaultDecl)}}
	}
	want, known := d.channels[channel]
	if !known {
		// 宣言に無い系統は readDecl が指摘する。ここで空の secret と比べると、意味の無い指摘が重なる。
		return nil
	}

	var findings []lintreport.Finding
	passed := 0
	for _, ln := range job.Lines {
		if inherit.MatchString(ln.Text) {
			findings = append(findings, lintreport.Finding{File: path, Line: ln.Number,
				Message: fmt.Sprintf("job %s は secrets: inherit で、どの系統の secret も notify.yaml へ渡します", key)})
			continue
		}
		if !webhookKey.MatchString(ln.Text) {
			continue
		}
		m := webhookLine.FindStringSubmatch(ln.Text)
		if m == nil {
			findings = append(findings, lintreport.Finding{File: path, Line: ln.Number,
				Message: fmt.Sprintf("job %s の slack-webhook-url は ${{ secrets.<名前> }} の形で書いてください。どの secret が渡るかを読めません", key)})
			continue
		}
		passed++
		if m[1] != want {
			findings = append(findings, lintreport.Finding{File: path, Line: ln.Number,
				Message: fmt.Sprintf("job %s は系統 %s なので secrets.%s を渡してください（secrets.%s を渡しています）", key, channel, want, m[1])})
		}
	}
	if passed > 1 {
		findings = append(findings, lintreport.Finding{File: path, Line: job.Number,
			Message: fmt.Sprintf("job %s は slack-webhook-url を %d 回渡しています。1回にしてください", key, passed)})
	}
	if passed == 0 && len(findings) == 0 {
		findings = append(findings, lintreport.Finding{File: path, Line: job.Number,
			Message: fmt.Sprintf("job %s は slack-webhook-url を渡していません。系統 %s の secrets.%s を渡してください", key, channel, want)})
	}
	return findings
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
