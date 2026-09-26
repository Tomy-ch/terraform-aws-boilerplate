# ADR-0505: CIランナーのイメージを単一の宣言へ固定する

- Status: Accepted
- Date: 2026-09-26
- Scope: repository-wide
- Related: [0501](0501-development-tooling-composition.md), [0503](0503-tool-execution-form.md), [0702](0702-repository-operations-substrate.md)

## Context

GitHub Actions の `runs-on:` が受け取る `ubuntu-latest` のような label は、GitHub の都合で指す先が
入れ替わる。入れ替えは GitHub 側で告知されるが、**このリポジトリの履歴には現れない**。ある日から
別の OS の上で検査していたことに、何かが壊れるまで気づけない。

このリポジトリは既に2種類の版を単一の宣言へ固定している —— 道具の版は `mise.toml`
（[0501](0501-development-tooling-composition.md) 決定19）、ベースイメージは digest
（[0503](0503-tool-execution-form.md) 決定10）。CIランナーだけが浮動のまま残っており、しかも
それは**その2つを実行する土台**である。土台が動けば、固定した上の層の意味も動く。

故障モードは、版が動くこと自体ではない。**動いたことが diff に出ないこと**である。移行の日に
何かが壊れたとき、原因の候補としてはリポジトリの変更しか挙がらない。

GitHub の公式文書は `-latest` と版付き label のどちらを推奨するとも述べていない。述べているのは
「`-latest` は GitHub が提供する最新の安定版であって、OSベンダの最新とは限らない」ことだけである。
したがってどちらを採っても公式推奨からの逸脱ではなく、[0207](0207-default-value-policy.md) 決定11
の記録を要しない。

## Decision

1. `runs-on:` が名指しする runner の label を、単一の宣言 `.github/runners-pin.toml` で固定する。
   宣言は `"<浮動の label>" = "<固定先の label>"` の対を持つ。
2. **workflow へ label を直接書かない。** 写しは `make pin-runners-apply` が書き込み、
   `make pin-runners-check` が宣言とのずれで落とす。
3. 宣言のどちらの側にも無い label は**エラーとする**。列・`group:`・`${{ }}` の形も同様に扱う。
   読めない形を取りこぼしとして通さない（[0702](0702-repository-operations-substrate.md) 決定14）。
4. 宣言が0件、または走査した `runs-on:` が0件のとき、成功で返さない
   （[0702](0702-repository-operations-substrate.md) 決定13）。
5. 版を上げるのは、宣言を書き換えて `apply` を実行することによる。**その回の diff が移行そのもので
   ある。**

## 検討した代替案

### 案A: `-latest` のまま使う

労力が要らず、新しいイメージへ自動で乗る。採らないのは、乗り換えがこちらの履歴に現れないため
である。壊れたときに参照できる変更が無く、切り分けが「GitHub 側で何か変わったのだろう」で止まる。

### 案B: 各 workflow へ版付き label を直接書く

宣言も道具も要らない。採らないのは、同じ版が20ファイル28箇所へ散り、それを突き合わせる手段が
無くなるためである。このリポジトリが `mise.toml` と lockfile で解いている問題を、ランナーについて
だけ解かないことになる。上げ忘れた1箇所は、落ちるのではなく別の OS で走り続ける。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 入れ替わりが無告知で入る | 固定されるが、散った写しがずれる | 固定され、ずれが落ちる |
| Testability | 検査対象が無い | 写し同士を突き合わせる手段が無い | 宣言と写しの一致を機械で判定する |
| Contract Clarity | どの OS で検査しているかが宣言に無い | 28箇所のどれが正本か決まらない | 正本が1つ |
| Maintainability | 手間ゼロ | 版上げが28箇所の編集 | 版上げが宣言1行の編集 |
| Scope Control | — | — | 対象は `.github/workflows` の `runs-on:` に限る |

評価軸の定義は [0101](0101-architecture-principles.md) を参照する。

## 意図的に捨てるもの

- **新しいランナーイメージが配られた日に、何もせず乗ること。** 移行は明示の操作になる。
- **`-latest` が持つ「GitHub が安定と判断した版に自動で追随する」性質。** 追随するかどうかを
  こちらが決める代わりに、決めなければ古いままになる。

## 保証範囲

- 保証すること: すべての `runs-on:` が宣言と一致していること。宣言のどちらの側にも無い label と、
  読めない形の `runs-on:` が黙って通らないこと。走査対象を失った実行が成功で返らないこと。
- 保証しないこと: 固定先の label が GitHub に実在すること。固定先が EOL に達したことの検出。
  self-hosted runner（`runs-on` が label の集合を取る形は本決定の対象外である）。

## 検証方法

`make pin-runners-check`（`scripts/pin-runners`）が、宣言と各 workflow の `runs-on:` を突き合わせる。
pre-commit フックと CI の `pin-runners-check` job が実行する。判定は `apply` と同じ経路を書き換え
なしで走らせたものであり、別実装を持たない（[0702](0702-repository-operations-substrate.md) 決定10-12）。

## 影響

- `.github/workflows/**` の `runs-on:` は生成された写しになる。手で編集するとフックが落ちる。
- 版を上げる変更は、宣言1行と、そこから生成された写しの差分として現れる。
- 新しい workflow を足すときは `make pin-runners-apply` を実行する。実行しなければフックが落ちる。

## 見直し条件

- GitHub が版付き label の提供をやめたとき。
- self-hosted runner、または `runs-on` が label の集合を取る形を導入したとき。
- 固定先が EOL に達したことを検出する必要が出たとき —— `pin-actions` / `pin-images` が持つような
  鮮度の仕組みが要る。本決定はそれを持たない。
