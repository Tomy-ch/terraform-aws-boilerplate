// Package branches は、分岐のパターンの単一宣言。
//
// **保護対象のブランチ、リリース線の形、ブランチ名の接頭辞はここだけが持つ**
// （ADR-0603 決定1-3）。読む側は次の4つで、いずれもパターンを自分で持たない。
//
//	base-branch   最新のリリース線の判定
//	release       タグを打つ先と、切るブランチの接頭辞
//	repo-setup    初期化で用意するブランチと、破棄してよいブランチの判定
//	branches      .github/settings/branch-protection.json の生成と突合
//
// **宣言をデータファイルではなく Go に置く理由。** 読む側の4つすべてが Go であり、
// 生成先の `branch-protection.json` は正規表現も既定ブランチも表現できない。外部ファイルに
// すると、それを読むパーサと、パーサが読み違える経路と、値が実行時に不正でありうる状態を
// 同時に持ち込むことになる —— 定数なら、捕捉群の個数も値の形もコンパイル時に決まる。
package branches

import "regexp"

const (
	// Default はリリースの起点であり、GitHub のデフォルトブランチでもある。
	Default = "production"

	// ReleasePrefix は、ReleasePattern の前方一致アンカーと、repo-setup の部分一致の
	// 両方に使われる。
	ReleasePrefix = "release/"

	// HotfixPrefix は release 線を迂回して Default へ入る経路の接頭辞。
	// 保護対象に含めるのは、この経路もレビューを通すためである。
	HotfixPrefix = "hotfix/"
)

// ReleasePattern はリリース線の形。**捕捉群は major / minor / patch の順**で、
// base-branch はこの順序で版を比較する。
var ReleasePattern = regexp.MustCompile(`^` + ReleasePrefix + `v(\d+)\.(\d+)\.(\d+)$`)

// Deploy は実環境へ届くブランチ（ADR-0601 決定10）。並びは下流から上流で、
// repo-setup が作る順序でもある。
var Deploy = []string{"develop", "staging", Default}

// GatePush は、push 側で走らせる検査の起動対象。**required context を報告しない側なので
// 絞ってよい** —— 絞ってはならないのは pull_request 側である（ADR-0603 決定16-18）。
var GatePush = []string{"develop", "staging", Default, ReleasePrefix + "*"}

// ReleasePush は、リリース線と Default だけを見る検査の起動対象。
var ReleasePush = []string{ReleasePrefix + "*", Default}

// Protected は保護設定の適用対象。GitHub の ruleset へは refs/heads/ を前置した
// fnmatch で渡り、`**/*` は階層を跨ぐ。
//
// Deploy に glob を足した形だが、**両者を導出関係にしない。** 保護対象へ入れたいものと
// 実環境へ届くものは別の問いであり、片方の変更が黙ってもう片方を動かす形にしない。
var Protected = []string{
	"develop",
	"staging",
	Default,
	ReleasePrefix + "**/*",
	HotfixPrefix + "**/*",
}

// Line はブランチを切る線の種類。呼び出し側は接頭辞ではなく種類を渡す ——
// 接頭辞そのものを渡す形にすると、呼び出し側が文字列を持つことになる。
type Line string

const (
	// LineRelease は通常のリリース線。
	LineRelease Line = "release"
	// LineHotfix は HotfixPrefix に対応する線。
	LineHotfix Line = "hotfix"
)

// Prefix は線に対応する接頭辞を返します。未知の線は空文字列を返すので、
// 呼び出し側は Valid で先に確かめること。
func (l Line) Prefix() string {
	switch l {
	case LineRelease:
		return ReleasePrefix
	case LineHotfix:
		return HotfixPrefix
	default:
		return ""
	}
}

// Valid は既知の線かどうかを返します。
func (l Line) Valid() bool { return l.Prefix() != "" }

// Lines は指定できる線の一覧を返します。usage の文言をここから組むためのもので、
// **一覧を別に書くと、線を足したときに片方だけが古くなる。**
func Lines() []Line { return []Line{LineRelease, LineHotfix} }
