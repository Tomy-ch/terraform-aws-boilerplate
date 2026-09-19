// Package workflow は、workflow 定義を「桁」で読むための共通の切り出しを提供します。
//
// YAML パーサを使わないのは、違反の位置を行番号で報告する必要があるためです。パーサを通すと
// 元の行が失われ、「どこを直すか」を返せなくなります。
package workflow

import (
	"regexp"
	"sort"
	"strings"
)

// Line は行番号（1始まり）付きの1行です。
type Line struct {
	Number int
	Text   string
}

// Job は jobs: 直下の1ジョブです。Number はジョブ見出し行の行番号です。
type Job struct {
	ID     string
	Number int
	Lines  []Line
}

// Step は steps: 配下の1ステップです。Number は "- " で始まる先頭行の行番号です。
type Step struct {
	Number int
	Lines  []Line
}

// Split は workflow 本文を切り出した結果です。
type Split struct {
	// Found は jobs: が見つかったかを表します。見つからないのは検査対象の取り違えなので、
	// ジョブが0件であることとは区別します。
	Found bool
	Jobs  []Job
	// Preamble は jobs: の外側（トップレベルの env: など）です。ジョブ本文には現れませんが、ジョブへ届きます。
	Preamble []Line
}

var (
	jobsKey = regexp.MustCompile(`^jobs:[ \t]*(?:#.*)?$`)
	// 行末コメントや引用符といった書式差で検出が外れると、規約が破られた瞬間に検査が沈黙します。
	jobHeader = regexp.MustCompile(`^ {2}(?:"([A-Za-z0-9_-]+)"|'([A-Za-z0-9_-]+)'|([A-Za-z0-9_-]+)):[ \t]*(?:#.*)?$`)
	stepsKey  = regexp.MustCompile(`^ {4}steps:[ \t]*(?:#.*)?$`)
	stepItem  = regexp.MustCompile(`^ {6}- `)
	topLevel  = regexp.MustCompile(`^\S`)
	shallow   = regexp.MustCompile(`^ {0,4}\S`)
	comment   = regexp.MustCompile(`^\s*#`)
)

// SelectFiles は、ディレクトリの読み取り結果から検査対象の workflow を選び出してパスへ組み立てます。
//
// 拡張子は .yaml / .yml の両方を採ります。GitHub がどちらも読むため、片方だけを見ると拡張子を
// 替えただけの workflow が検査から静かに外れます。並びを固定するのは、違反の出力順が実行ごとに
// 揺れると CI の失敗差分が読めなくなるためです。
func SelectFiles(names []string, dir string) []string {
	var picked []string
	for _, n := range names {
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			picked = append(picked, n)
		}
	}
	sort.Strings(picked)

	out := make([]string, 0, len(picked))
	for _, n := range picked {
		out = append(out, dir+"/"+n)
	}
	return out
}

// SplitJobs は workflow 本文をジョブ単位に切り出します。
//
// 桁0のコメント行では jobs: を打ち切りません。トップレベルキーではないため、ここで打ち切ると
// 以降のジョブが丸ごと検査対象から外れます。
func SplitJobs(source string) Split {
	lines := strings.Split(source, "\n")

	jobsIndex := -1
	for i, ln := range lines {
		if jobsKey.MatchString(ln) {
			jobsIndex = i
			break
		}
	}
	if jobsIndex == -1 {
		return Split{Found: false, Preamble: toLines(lines, 0)}
	}

	var jobs []Job
	current := -1
	end := len(lines)

	for i := jobsIndex + 1; i < len(lines); i++ {
		text := lines[i]

		if topLevel.MatchString(text) && !strings.HasPrefix(text, "#") {
			end = i
			break
		}

		if m := jobHeader.FindStringSubmatch(text); m != nil {
			id := m[1]
			if id == "" {
				id = m[2]
			}
			if id == "" {
				id = m[3]
			}
			jobs = append(jobs, Job{ID: id, Number: i + 1})
			current = len(jobs) - 1
			continue
		}

		if current >= 0 {
			jobs[current].Lines = append(jobs[current].Lines, Line{Number: i + 1, Text: text})
		}
	}

	preamble := append(toLines(lines[:jobsIndex], 0), toLines(lines[end:], end)...)
	return Split{Found: true, Jobs: jobs, Preamble: preamble}
}

// SplitSteps はジョブ本文をステップ単位に切り出します。
//
// if: や name: は「そのステップのもの」だけを見る必要があるため、"- " で区切ります。
func SplitSteps(job Job) []Step {
	start := -1
	for i, ln := range job.Lines {
		if stepsKey.MatchString(ln.Text) {
			start = i
			break
		}
	}
	if start == -1 {
		return nil
	}

	var steps []Step
	current := -1

	for _, line := range job.Lines[start+1:] {
		switch {
		case stepItem.MatchString(line.Text):
			steps = append(steps, Step{Number: line.Number})
			current = len(steps) - 1
		case shallow.MatchString(line.Text) && !comment.MatchString(line.Text):
			// steps: と同じかそれより浅い桁のキーが来たらステップ列の終わり。
			return steps
		}

		if current >= 0 {
			steps[current].Lines = append(steps[current].Lines, line)
		}
	}

	return steps
}

// UsesActionPattern は、uses: にこのローカルアクションを指定している行へ当たる正規表現を組み立てます。
//
// anchored が true のときは、ステップの先頭行（"      - uses:"）とその続き（"        uses:"）の桁だけに
// 当てます。false のときは行中のどこでも当てます。
func UsesActionPattern(actionPath string, anchored bool) *regexp.Regexp {
	escaped := regexp.QuoteMeta(actionPath)
	if anchored {
		return regexp.MustCompile(`^(?: {6}- | {8})uses:[ \t]*["']?` + escaped + `["']?[ \t]*(?:#.*)?$`)
	}
	return regexp.MustCompile(`uses:[ \t]*["']?` + escaped + `["']?[ \t]*(?:#.*)?$`)
}

func toLines(src []string, offset int) []Line {
	out := make([]Line, 0, len(src))
	for i, t := range src {
		out = append(out, Line{Number: offset + i + 1, Text: t})
	}
	return out
}
