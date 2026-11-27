// Package harden は、各 job の先頭に置く bullfrog の step の三つ組（本体・回復・再試行）の形を持ちます（ADR-0702 決定20）。
// 本体は落ちても job を進め（continue-on-error）、回復の step は本体が成功しなかったときだけ待って apt の索引を
// 更新し、再試行は同じ条件で本体と同じ設定で走り、落ちれば job を落とします。生成は egress が、検査の job と
// 書き込みの job で if:・continue-on-error:・run: を拒む規則の例外は oidc-job-lint と contents-write-lint が、
// この形を読みます。
package harden

import (
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const (
	// Action は step を見つける錨。SHA は pin-actions が書くので、名前だけを見る。
	Action = "bullfrogsec/bullfrog"
	// ID は本体の step の id。回復と再試行の step の if: がこれを名指す。
	ID = "harden-runner"
	// RetryID は再試行の step の id。Guard がこれを名指す。
	RetryID = "harden-runner-retry"
	// RetryIf は回復と再試行の step の条件。`!= 'success'` にする理由は scripts/README.md の lib/harden の行が持つ。
	RetryIf = "steps." + ID + ".outcome != 'success'"
	// RecoveryRun は回復の step の本文。bullfrog は apt の索引を update せずに install するので、待ってから更新する
	// （ADR-0702 決定20）。秒数に根拠は無く、効き目は観測で確かめる。
	RecoveryRun = "sleep 30 && sudo apt-get update"
	// Guard は、三つ組の後ろで status の関数（always()・failure()・cancelled()）を持つ step の if: が先頭に持つ条件
	// （ADR-0702 決定20）。
	Guard = "(steps." + ID + ".outcome == 'success' || steps." + RetryID + ".outcome == 'success')"
)

// StatusFunc は、暗黙の success() を外す status の関数の呼び出し。式の関数名は大小を区別しないので、大小を問わない。
var StatusFunc = regexp.MustCompile(`(?i)\b(?:always|failure|cancelled)\s*\(`)

// Head は本体の step の、uses: の前の行です。
func Head() []string {
	return []string{
		"      - name: Harden the runner",
		"        id: " + ID,
		"        continue-on-error: true",
	}
}

// Recovery は回復の step の全体です。timeout-minutes は、遮断の後か不調のミラーで apt-get が止まっても、
// 再試行に届くための上限。
func Recovery() []string {
	return []string{
		"      - name: Prepare to retry hardening the runner",
		"        if: " + RetryIf,
		"        continue-on-error: true",
		"        timeout-minutes: 2",
		"        run: " + RecoveryRun,
	}
}

// RetryHead は再試行の step の、uses: の前の行です。
func RetryHead() []string {
	return []string{
		"      - name: Harden the runner (retry)",
		"        id: " + RetryID,
		"        if: " + RetryIf,
	}
}

// Render は、本体の uses: 行と with: の行から、三つ組の全体を返します。
func Render(uses string, with []string) []string {
	out := Head()
	out = append(out, uses)
	out = append(out, with...)
	out = append(out, Recovery()...)
	out = append(out, RetryHead()...)
	out = append(out, uses)
	out = append(out, with...)
	return out
}

// GeneratedLines は、job の先頭の3つの step が生成した三つ組の頭（Head・Recovery・RetryHead）と一字一句同じで、
// 本体と再試行が bullfrog を uses に持つとき、lint が見逃す行の番号を返します。本体の continue-on-error:、
// 回復の step の if:・continue-on-error:・timeout-minutes:・run:、再試行の if: である。形が違えば空を返す。
// uses: と with: が本体と再試行で揃っていることは egress の検査（必須の check）が持つ。この除外はそれを前提にする。
func GeneratedLines(job workflow.Job) map[int]bool {
	steps := workflow.SplitSteps(job)
	if len(steps) < 3 {
		return nil
	}
	head, retry := Head(), RetryHead()
	if !startsWith(steps[0], head) || !IsRecovery(steps[1]) || !startsWith(steps[2], retry) {
		return nil
	}
	if !usesBullfrog(steps[0].Lines[len(head)]) || !usesBullfrog(steps[2].Lines[len(retry)]) {
		return nil
	}
	out := map[int]bool{steps[0].Lines[2].Number: true, steps[2].Lines[2].Number: true}
	for _, l := range steps[1].Lines[1:] {
		out[l.Number] = true
	}
	return out
}

// IsRecovery は、step が Recovery と一字一句同じ行だけを持つかを返します。後ろの空行とコメントも許さない。
func IsRecovery(st workflow.Step) bool {
	want := Recovery()
	if len(st.Lines) != len(want) {
		return false
	}
	for i, w := range want {
		if st.Lines[i].Text != w {
			return false
		}
	}
	return true
}

func startsWith(st workflow.Step, head []string) bool {
	if len(st.Lines) <= len(head) {
		return false
	}
	for i, h := range head {
		if st.Lines[i].Text != h {
			return false
		}
	}
	return true
}

func usesBullfrog(l workflow.Line) bool {
	return strings.HasPrefix(l.Text, "        uses: "+Action+"@")
}
