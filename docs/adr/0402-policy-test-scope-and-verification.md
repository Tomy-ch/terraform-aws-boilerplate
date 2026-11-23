# ADR-0402: Policy Testの適用範囲と、その検証方法を定める

- Status: Accepted
- Date: 2026-10-21
- Scope: repository-wide
- Related: ADR-0102, ADR-0103, ADR-0202, ADR-0204, ADR-0301, ADR-0401, ADR-0501, ADR-0604

## Context

ADR-0501 決定4・6 は Security レイヤーを Trivy / Checkov / KICS / Semgrep CE へ、Policy レイヤーを Conftest / OPA へ割り当てた。しかし両者が検出する対象は重なる。S3 の public access、暗号化の未設定、RDS の public access、IAM の wildcard は、いずれも Security の道具が既定ルールとして持ち、同時に本リポジトリが invariant として持ちたいものでもある。

境界を決めないと、同一の違反が2つの ID で報告される。抑止（ADR-0501 決定13）が2箇所へ分散し、片方だけを消した状態が「対応済み」に見える。ADR-0501 決定9 は Unit と Policy の境界を定めたが、Security と Policy の境界は定めていない。

次に、Policy 自体が検証されていない。Rego はコードであり、誤った Policy は違反を見逃す。見逃しは「全件緑」として現れるため、実行結果からは正常と区別できない。ADR-0401 決定3 は検証できない主張を保証として扱わないとしているが、検査器そのものがこの規律の外に置かれている。

最後に、Policy が何を入力の根拠にしてよいかが決まっていない。ADR-0301 決定15 は `0.0.0.0/0` からの ingress を「インターネット公開点として設計されたコンポーネント」に限っているが、plan の出力から「これは公開点である」をどう判定するかは定義されていない。tag で宣言する案は、tag が利用者の入力であるため（ADR-0204 決定5）、invariant の根拠を利用者が上書きできる値へ置くことになる。命名規約による判定は、偶然の一致で穴が開く。

## Decision

### Security と Policy の境界

1. 両レイヤーの境界を次とする。

   | レイヤー | 対象 |
   | --- | --- |
   | Security（ADR-0501 決定6 の道具） | AWS 一般のベストプラクティスとして成立する misconfiguration |
   | Policy（Conftest / OPA） | 本リポジトリのアーキテクチャ invariant であり、外部のルールセットが知り得ないもの |

2. 判定の基準を「本リポジトリの ADR を読まなければ書けないルールか」とする。読まずに書けるものは Security、読まなければ書けないものは Policy とする。
3. Security の道具（ADR-0501 決定6）の**いずれか**が既定ルールとして持つ検査を、Policy として重複して書かない。重複が生じた場合、Security を権威とし、Policy 側を削る。Security の道具の間の重複は本決定の対象外とする（ADR-0501 決定6・13）。
4. Security の道具のルールが本リポジトリの invariant より緩い場合、その道具を無効化せず、Policy を追加して厳格化する。**抑止と厳格化を混同しない。**
5. 抑止は、その検査の権威を持つ層の設定にのみ書く。Security の所見は、報告した道具それぞれに書く（ADR-0501 決定13）。
6. 境界の適用例を次とする。この表は例示であり、Policy の一覧ではない。

   | 検査 | 権威 | 理由 |
   | --- | --- | --- |
   | S3 の public access の遮断 | Security | ADR を読まずに書ける |
   | 暗号化の有効化 | Security | 同上 |
   | RDS の public access の禁止 | Security | 同上 |
   | IAM の Action wildcard の禁止 | Security | 同上 |
   | `0.0.0.0/0` からの ingress を、インターネット公開点として設計された module に限ること | Policy | 何が公開点かは本リポジトリの設計が決める（ADR-0301 決定15） |
   | 意味論的な権限指定から導出された Action の集合と、生成された policy が完全一致すること | Policy | 導出規則は当該ユースケースの ADR が持つ（ADR-0301 決定7） |
   | OIDC の信頼条件にワイルドカードが存在しないこと | Policy | 対象と条件の形が本リポジトリの設計に属する |

   Security の道具の既定のルールが、本リポジトリの書き方の文書を検出しない場合は、決定4 により Policy が持つ。IAM の Action wildcard はその例である。

### Policy 自体の検証

7. Policy はコードであり、テストを持つ。Policy のテストを CI の必須検査とする。
8. Policy のテストは、**違反する入力と違反しない入力の両方**を持つ。違反しない入力のみのテストを、テスト済みとして扱わない。
9. Policy の追加・変更は、対応するテストの追加・変更を伴う。テストを持たない Policy を配線しない。
10. ADR-0401 決定4 の Policy レイヤーに、Policy 自身のテストを含める。Policy Test が「保証されている」と言えるのは、その Policy 自身が検証されている場合に限る。

### Policy が依存してよい入力

11. Policy の入力を、Terraform の JSON の plan 表現の `resource_changes` とする（ADR-0501 決定8）。資格情報なしの検査では mock provider の下の `terraform test -json -verbose` が出す `test_plan` を、実 provider の plan では `terraform show -json` の出力を用いる。Policy が判定に読む入力は、両者に共通する `resource_changes` だけである（形の版の検査は ADR-0501 決定8）。入力のほかに読むのは、宣言と、決定18 の Provider の schema の一覧である。
12. Policy は resource の Terraform address（`module.<name>.<type>.<name>`）に依存してよい。ADR-0202 が address を実装詳細としているのは**公開契約に対して**であり、リポジトリ内部の検査には及ばない。
13. 「インターネット公開点である」等のアーキテクチャ上の役割を、address によって判定する。tag、名前の規約、および利用者が入力できる値を、役割判定の根拠にしない。
14. 決定13 の理由を次とする。

    - tag は利用者が指定する入力であり（ADR-0204 決定5）、invariant の根拠を利用者が触れる値に置くことになる
    - 命名規約は文字列の一致であり、偶然の一致で判定が通る
    - address は boilerplate が所有する。変更時は ADR-0202 決定6 が `moved` block を要求するため、Policy の更新漏れが plan に現れる

15. address に依存する Policy は、依存する address を1箇所へ集約する。module の構造を変更したときに、更新すべき Policy を特定できる形とする。module はユースケースから呼ばれるので（ADR-0102 決定11-12）、address は `module.<use-case>.module.<module>.<type>.<name>` の入れ子になる。同じ一般 module が複数のユースケースから呼ばれ、同じ名前の `internal/` の module もユースケースごとに在りうるので、集約はユースケースを含む address で行う。
16. 役割判定のための専用ツールを実装しない。ADR-0501 決定3 の3条件のうち、第1条件（既存ツールで実現できない）を満たさない。

### Policy が読まない経路で設定を書く引数

17. Policy は、設定を、それを書く resource の宣言した型と属性から読む。親の resource の引数が別の resource と同じ設定を書く場合、その引数は、Policy が読まない経路で同じ設定を書き換える。そのような引数を持つ型を、Policy Test が読む宣言に載せる。載せる型は、Policy Test が stateful な resource の保護の対象として宣言した型（ADR-0604 決定11）に限る。attachment 型と同じ設定を書く引数と、attachment 型の親として宣言した型の非推奨の引数は、ADR-0102 決定18 の宣言が持ち、Policy Test はそこから導く。Policy Test が持つ宣言には、それ以外の引数だけを置く。載せた型については、別の resource と同じ設定を書くかを問わず、Provider の schema が非推奨とする書ける引数（属性と block）をすべて載せる。網羅を機械で確かめるためである。resource の NOTE が別の resource との併用で互いに上書きすると告げる引数も載せる。各引数には、書かないときの値（属性は `null`、block は空の list）を添える。
18. reference の plan を入力とする Policy Test は、決定17 で宣言した引数が書かないときの値でない変更と、その値が plan で決まらない変更を失敗させる。宣言した型と引数が入力のどこにも現れない場合と、宣言した型の、Provider の schema が非推奨とする書ける引数が宣言に無い場合も失敗させる。後者の schema は、mock provider の下の `test_plan` の `provider_schemas` から型ごとの非推奨の引数の一覧として取り出し、宣言と同じく data として Policy に渡す。取り出しは `provider_format_version` を検査し、期待する版でなければ失敗する。
19. 決定17・18 の検査は、Policy が設定をどの resource から読むかという本リポジトリの入力の設計に依る。外部のルールセットはこの設計を知り得ないので、決定2 により Policy に置く。

### 値を固定と読む判定

20. Policy が、要求元・暗号化コンテキスト・Resource の値を「1つの値に固定している」と読む判定は、wildcard（`*`・`?`）と IAM の policy 変数（`${...}`）を含む値を固定と読まない。policy 変数は要求の時点で置き換わり、どの値に当たるかが plan では決まらない。エスケープ（`${$}` など）も読み分けず、含むものとする。

### 届く主体を数える判定

21. Policy が、Allow の Resource がある resource に当たるかを読み、その resource へ権限が届く主体を数える判定は、IAM の policy 変数（`${...}`）を含む Resource を、当たるとも当たらないとも読まない。policy 変数を任意の文字列と読んで当たりうる Resource を、違反として報告する。届く主体の集合を別の文書と突き合わせる判定では、どちらに読んでも片側へ誤るためである。決定20 と同じく、エスケープ（`${*}` など）と既定値を持つ変数も読み分けない。Resource の policy 変数を一律に拒む判定は、これを満たす。

## 検討した代替案

### 案A: Security と Policy を分けず、いずれか一方へ寄せる

Trivy へ寄せると、リポジトリ固有の invariant を表現できない（カスタムルールを書くことは可能だが、それは Policy を Trivy の記法で書くことに等しい）。Conftest へ寄せると、AWS 一般のベストプラクティスを自前の Rego で再実装することになり、ADR-0501 決定1 と決定3 の双方に反する。

### 案B: アーキテクチャ上の役割を tag で宣言する

plan の出力から役割を読めるようになる。ただし tag は利用者の入力であり、invariant の根拠が利用者の側へ移る。「security invariant を利用者が緩和できない」という ADR-0301 決定2 の前提が、判定の入口で崩れる。

### 案C: 役割判定のための専用ツールを実装する

判定の意味論を自前で定義できる。ただし address による判定で足りる範囲であり、ADR-0501 決定3 の第1条件を満たさない。判定器そのものが新たな検証対象になる。

### 評価

| 評価軸 | 案A: 一方へ寄せる | 案B: tag で宣言 | 案C: 専用ツール | 採用案: 境界 + address 判定 |
| --- | --- | --- | --- | --- |
| Security | 中（表現できないか再実装） | 低（根拠が利用者側） | 中 | 高 |
| Testability | 中 | 中 | 低（判定器が未検証） | 高（Policy 自体を検証） |
| Contract Clarity | 低（権威が不定） | 中 | 中 | 高（権威が1つ） |
| Scope Control | 中 | 中 | 低（実装資産が増える） | 高 |
| Maintainability | 低（重複の保守） | 中 | 低 | 中（構造変更時に Policy の更新が必要） |

## 意図的に捨てるもの

- 同一の invariant を複数の道具で二重に検査することによる保険
- 利用者が宣言によって役割を指定できること
- module の内部構造を変えても Policy が無変更で済むこと

## 保証範囲

- 保証すること: Security と Policy の間で、1つの invariant に検査の権威が1つであること。配線された Policy がテストを持つこと。reference の plan で、宣言した引数が書かないときの値であり、宣言した型の、Provider の schema が非推奨とする書ける引数が宣言に在ること（決定17・18）。値を固定と読む判定が、wildcard と policy 変数を含む値を固定と読まないこと（決定20）。届く主体を数える判定が、policy 変数を含み当たりうる Resource を数えずに違反とすること（決定21）
- 保証しないこと: Security レイヤーのルールセットの網羅性。Policy が対象としていない invariant の充足。resource の NOTE だけが告げる引数を宣言に載せたこと。保護の対象でない型の、別の resource と同じ設定を書く引数（attachment 型と同じ設定を書く引数は、ADR-0102 決定18 が書くことを禁じ、層の検査が HCL のソースで落とす）。保護の対象に足した型を宣言に載せたこと

## 検証方法

- Static Analysis: Policy のテストが CI の必須検査として存在すること（決定7）
- Static Analysis: テストを持たない Policy が存在しないこと（決定9）
- Policy のテスト: 各 Policy が、違反する入力に対して違反を報告すること（決定8）
- Policy のテスト: 決定17 で宣言した引数ごとに、書いた値と plan で決まらない値で違反を報告し、書かないときの値で報告しないこと。宣言した型と引数が入力に無い場合と、Provider の schema が非推奨とする書ける引数が宣言に無い場合に違反を報告すること（決定18）
- Policy のテスト: 値を固定と読む判定ごとに、wildcard と policy 変数を含む値で違反を報告すること（決定20）
- Policy のテスト: 届く主体を数える判定ごとに、policy 変数を含み対象に当たりうる Resource で違反を報告し、変数を任意の文字列と読んでも当たらない Resource で報告しないこと（決定21）
- Static Analysis（`make tf-policy` が本走査の前に走らせる canary）: mock provider の plan で、書いた文字列の属性の値が override に隠されずに Policy の入力へ現れること。Provider の schema から取り出した非推奨の引数の一覧が Policy に届くこと（決定18）
- Unit Test（`scripts/provider-schema`）: Provider の schema が非推奨とする書ける引数を、層の検査（ADR-0102 決定18）と同じ関数で取り出し、同じ schema から同じ一覧を得ること（決定18）
- レビュー: 決定2 の基準による配置の判断、決定3 の重複の不在、および決定17 の resource の NOTE に依る引数の選定。いずれも機械判定できないため、Policy を追加・変更する Pull Request のレビュー観点とする

## 影響

- Policy の追加は、Policy のテストの追加を伴う。
- module の address を変更する変更は、`moved` block（ADR-0202 決定6）と Policy の更新の両方を伴う。
- ADR-0301 決定15 の「インターネット公開点」は、module 境界として実装される。公開点を新設することは module を追加することと同義になる。

## 見直し条件

- Security の道具のいずれかがカスタムルールの記述において Conftest と同等の表現力を持ち、両レイヤーを統合できると判断できる場合
- address による役割判定が、module の粒度の変更に追随できなくなった場合
- Policy の数が、レビューによる重複判定（決定3）では追えない規模に達した場合
- Provider が、別の resource と同じ設定を書く引数を非推奨としなくなり、決定18 の schema との突き合わせが宣言の網羅を表さなくなった場合
