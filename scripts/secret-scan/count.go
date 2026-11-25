package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 数え方を読んだ gitleaks の版。
const gitleaksVersion = "8.30.1"

var (
	errNoCount  = xerrors.New("走査した commit の数を gitleaks の出力から読めません")
	errNumstat  = xerrors.New("git log --numstat の出力を解釈できません")
	errMismatch = xerrors.New("走査した commit の数が git の数えた数と合いません。git を読めていないか、範囲を取り違えています")
)

var scannedPattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]+) commits scanned`)

const commitHeader = "commit "

// runCount は、gitleaks の出力 out から走査した commit の数を読み、git が独立に数えた数と突き合わせます
// （ADR-0702 決定15）。**gitleaks は git を読めなくても「0 commits scanned」で 0 を返す**ので、走査した数を
// 比べなければ、何も見なかった走査が所見なしの走査として通ります。
//
// minimum が1以上なら、走査した数がそれ以上であることだけを求めます（全履歴の走査。ref の数え方が
// git rev-list と揃わない）。0 なら、numstat（走査と同じ範囲の numstatArgs の出力）から数えた範囲に入ることを
// 求めます。
//
// gitleaks が数えるのは、走査の断片を1つ以上出した commit です。断片を出すのは、削除されていない
// ファイルの hunk と、削除されていないアーカイブのバイナリだけです。空の commit、削除だけ・
// mode の変更だけ・行の変わらない rename だけの commit は数えません。この規則は gitleaksVersion の
// 版のソース（detect/detect.go の addCommit と sources/git.go の Fragments）から読んだものです。
// 期待値は次の2つに分けて数えます。
//
//   - 必ず数える commit: 削除されていないテキストのファイルで、1行以上が変わったものを持つ
//   - 数えるかを git の出力から決められない commit: それ以外で、削除されていないバイナリか、
//     行の変わらない rename・copy を持つ。アーカイブかどうかは gitleaks がファイル名で判じ、
//     rename として読むかは gitleaks を走らせる側の git の設定が決める
//
// 走査した数が、必ず数える数以上、両者の和以下であることを求めます。下限を割れば gitleaks が
// 範囲を読み落としており、上限を超えれば範囲の取り違えか数え方の変化です。
func runCount(out string, numstat []byte, minimum int) error {
	n, err := scanned(out)
	if err != nil {
		return err
	}
	if minimum > 0 {
		if n < minimum {
			return xerrors.Wrapf(errMismatch, "走査 %d 件、下限 %d 件", n, minimum)
		}

		return nil
	}
	must, maybe, err := expected(numstat)
	if err != nil {
		return err
	}
	if n < must || n > must+maybe {
		return xerrors.Wrapf(errMismatch, "走査 %d 件、git の数えた範囲 %d-%d 件", n, must, must+maybe)
	}

	return nil
}

// scanned は gitleaks の出力から走査した commit の数を取り出します。数の行がちょうど1つでなければ
// エラーです。
func scanned(out string) (int, error) {
	m := scannedPattern.FindAllStringSubmatch(out, -1)
	if len(m) != 1 {
		return 0, xerrors.Wrapf(errNoCount, "数の行が %d 個", len(m))
	}
	n, err := strconv.Atoi(m[0][1])
	if err != nil {
		return 0, xerrors.Wrap(errNoCount, err.Error())
	}
	return n, nil
}

// expected は `git log --numstat -z` の出力から、gitleaks が必ず数える commit の数と、数えるかを
// 決められない commit の数を返します。
func expected(raw []byte) (must, maybe int, err error) {
	var (
		seen    bool
		hasText bool
		hasOpen bool
	)
	flush := func() {
		switch {
		case hasText:
			must++
		case hasOpen:
			maybe++
		}
		hasText, hasOpen = false, false
	}

	tokens := bytes.Split(raw, []byte{0})
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimPrefix(string(tokens[i]), "\n")
		if tok == "" && i == len(tokens)-1 {
			break
		}
		if hash, ok := strings.CutPrefix(tok, commitHeader); ok {
			if !gitrev.IsHash(hash) {
				return 0, 0, xerrors.Wrapf(errNumstat, "commit の行 %q", tok)
			}
			if seen {
				flush()
			}
			seen = true
			continue
		}
		if !seen {
			return 0, 0, xerrors.Wrapf(errNumstat, "commit の行より前の %q", tok)
		}

		cols := strings.SplitN(tok, "\t", 3)
		if len(cols) != 3 {
			return 0, 0, xerrors.Wrapf(errNumstat, "numstat の行 %q", tok)
		}
		changed, binary, err := lineCounts(cols[0], cols[1])
		if err != nil {
			return 0, 0, err
		}
		rename := cols[2] == ""
		if rename {
			// rename と copy は、元と先の path が続く2つの区切りに入る。
			if i+2 >= len(tokens) || len(tokens[i+1]) == 0 || len(tokens[i+2]) == 0 {
				return 0, 0, xerrors.Wrapf(errNumstat, "rename の path が欠けています: %q", tok)
			}
			i += 2
		}

		switch {
		case binary:
			hasOpen = true
		case changed:
			hasText = true
		case rename:
			hasOpen = true
		}
	}
	if !seen {
		return 0, 0, nil
	}
	flush()
	return must, maybe, nil
}

// lineCounts は numstat の追加と削除の列を読みます。バイナリは両方が "-" です。
func lineCounts(added, deleted string) (changed, binary bool, err error) {
	if added == "-" && deleted == "-" {
		return false, true, nil
	}
	a, errA := strconv.Atoi(added)
	d, errD := strconv.Atoi(deleted)
	if errA != nil || errD != nil || a < 0 || d < 0 {
		return false, false, xerrors.Wrap(errNumstat, fmt.Sprintf("行数の列 %q %q", added, deleted))
	}
	return a+d > 0, false, nil
}
