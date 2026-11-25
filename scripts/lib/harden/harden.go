// Package harden は、各 job の先頭に置く bullfrog の step の対（本体と再試行）の形を持ちます（ADR-0702 決定20）。
// 本体は落ちても job を進め（continue-on-error）、再試行は本体が成功しなかったときだけ同じ設定で走り、
// 落ちれば job を落とします。生成は egress が、検査の job と書き込みの job で if: と continue-on-error: を拒む
// 規則の例外は oidc-job-lint と contents-write-lint が、この形を読みます。
package harden

import (
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const (
	// Action は step を見つける錨。SHA は pin-actions が書くので、名前だけを見る。
	Action = "bullfrogsec/bullfrog"
	// ID は本体の step の id。再試行の step の if: がこれを名指す。
	ID = "harden-runner"
	// RetryIf は再試行の step の条件。本体の outcome が failure のほか想定外の値でも、再試行の側へ倒す
	// （status の関数を書かないので、job が cancel されれば暗黙の success() で走らない）。
	RetryIf = "steps." + ID + ".outcome != 'success'"
)

// Head は本体の step の、uses: の前の行です。
func Head() []string {
	return []string{
		"      - name: Harden the runner",
		"        id: " + ID,
		"        continue-on-error: true",
	}
}

// RetryHead は再試行の step の、uses: の前の行です。
func RetryHead() []string {
	return []string{
		"      - name: Harden the runner (retry)",
		"        if: " + RetryIf,
	}
}

// Render は、本体の uses: 行と with: の行から、対の全体を返します。
func Render(uses string, with []string) []string {
	out := Head()
	out = append(out, uses)
	out = append(out, with...)
	out = append(out, RetryHead()...)
	out = append(out, uses)
	out = append(out, with...)
	return out
}

// GeneratedLines は、job の先頭の2つの step が生成した対の頭（Head と RetryHead）と一字一句同じで、
// どちらも bullfrog を uses に持つとき、その continue-on-error: と if: の行番号を返します。形が違えば空を返す。
// uses: と with: が対の間で揃っていることは egress の検査（必須の check）が持つ。この除外はそれを前提にする。
func GeneratedLines(job workflow.Job) map[int]bool {
	steps := workflow.SplitSteps(job)
	if len(steps) < 2 {
		return nil
	}
	head, retry := Head(), RetryHead()
	if !startsWith(steps[0], head) || !startsWith(steps[1], retry) {
		return nil
	}
	if !usesBullfrog(steps[0].Lines[len(head)]) || !usesBullfrog(steps[1].Lines[len(retry)]) {
		return nil
	}
	return map[int]bool{
		steps[0].Lines[2].Number: true,
		steps[1].Lines[1].Number: true,
	}
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
