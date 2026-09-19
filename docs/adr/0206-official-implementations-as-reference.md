# ADR-0206: 公式実装を参照実装（Knowledge Source）として利用する

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0201, ADR-0203, ADR-0205, ADR-0207, ADR-0301

## Context

外部moduleを実行時依存として採用しない（ADR-0205）ことは、外部moduleが蓄積した知見を捨てることを意味しない。

公式moduleおよび公式ドキュメントには、自前実装では容易に得られない情報が含まれる。

- AWS API側の制約（作成順序、更新時の置換条件、同時指定できない属性）
- Provider固有の制約とlifecycleの扱い
- edge caseと、それに対する既知の回避策
- security関連設定の既定と推奨
- version差分への対応方法

これらを調査せずに実装すると、既知の問題を再発見する形になる。特にIAM、暗号化、ネットワーク境界に関する設定は、誤りがそのままsecurity invariantの破綻になる。

一方で、公式moduleをそのまま複製すると、ADR-0205で回避したはずの汎用性がコードとして流入する。公式moduleは多様な利用者を対象とするため、本リポジトリより高い自由度を持つ。

## Decision

### 基本方針

1. 外部の公式実装は、dependencyとしてではなくKnowledge Source / Reference Implementationとして利用する。

   > Dependency としては利用しない。Knowledge Source / Reference Implementation として利用する。

2. AWS resourceの実装を開始する際は、原則として着手時点における最新の公式実装を確認する。

### 「公式」の定義

3. 本ADRにおける「公式」を以下とする。

   1. AWS または HashiCorp が publisher である Terraform Registry module
   2. AWS公式ドキュメント（サービスのUser Guide、API Reference、Security Guide、Best Practices）
   3. AWS Well-Architected Framework を含む公式設計指針
   4. Terraform AWS Provider の公式ドキュメントおよびソース
   5. AWSまたはHashiCorpが公開するsecurity advisory / bulletin

4. 上記以外（community module、ブログ、第三者の解説）は参照してもよいが、決定の根拠として単独で採用しない。

### 参照する内容

5. 参照時は、少なくとも以下を確認する。

   - resource構成と依存関係
   - AWS Provider固有の制約
   - default値と、その根拠
   - lifecycle設定（`create_before_destroy`、`ignore_changes` 等）とその理由
   - IAM構成
   - validation
   - edge case
   - optional resourceの必要条件
   - AWS API上の制約
   - version差分への対応
   - security関連設定

### 取り込み手順

6. 以下の手順を基本とする。複製を目的としない。

   1. 公式実装を読む
   2. 必要な知見を抽出する
   3. 対象ユースケースへ限定する（ADR-0102）
   4. 不要な自由度を削る（ADR-0101、ADR-0203）
   5. boilerplate独自の契約へ再構成する（ADR-0201）

7. 採用した知見と、意図的に採用しなかった知見（およびその理由）を記録する。「参照した」だけの記録は不十分とする。

### 記録

8. 参照記録は、対象ユースケース配下のドキュメント（設計ノートまたは当該use-case ADR）へ残す。記録項目は以下とする。

   - 参照した公式ドキュメントまたはmoduleと、その version / 日付
   - 参照時点の AWS Provider version
   - 採用した設定とその根拠
   - 採用しなかった設定と、その理由

9. 参照元をコードコメントとして残すことを原則としない。コードコメントは、値が非自明である理由の説明に限って記述する。参照元の一覧はコードから分離し、陳腐化を局所化する。
10. 参照した公式実装のversionを、利用者への公開契約として露出しない（ADR-0207 決定7と同様）。これはboilerplate内部の実装判断の根拠として扱う。

### upstreamの追従

11. 公式実装の更新を自動追従の対象としない（自動追従は実行時依存と同義になるため）。
12. 追従は次の契機で行う。

    - security advisory が公開された場合（当該サービスを利用するユースケースを対象にレビューする）
    - AWS Provider の major version 更新時
    - 定期レビュー（対象ユースケースごとに、少なくとも年1回）

13. 追従レビューの結果、設定を変更する場合はADR-0207（default値の決定）に従う。変更しない場合も、判断とその時点を記録する。

## 検討した代替案

### 案A: 公式実装を参照しない（完全独自実装）

外部の影響を完全に排除できるが、AWS API制約やedge caseを実装者の経験にのみ依存して発見することになる。security関連設定の見落としリスクが高い。

### 案B: 公式実装をvendoringし、必要箇所のみ改変する

知見を最大限取り込めるが、汎用性がコードとして流入し、upstream差分の追従義務も残る。ADR-0205 決定4に該当する。

### 案C: 公式実装の更新を継続監視し、差分を自動的に取り込む

追従漏れは減るが、実質的に実行時依存と同じ追従コストを負う。かつ取り込み判断が機械的になり、ADR-0101の優先順位（自由度を削る）を適用する機会が失われる。

### 評価

| 評価軸 | 案A: 参照しない | 案B: vendoring | 案C: 自動追従 | 採用案: 参照実装として利用 |
| --- | --- | --- | --- | --- |
| Security | 低（見落としリスク） | 中 | 中 | 高 |
| Scope Control | 高 | 低 | 低 | 高 |
| Maintainability | 中 | 低 | 低 | 中 |
| Contract Clarity | 中 | 低 | 低 | 高 |
| Reproducibility | 中 | 中 | 低（外部変更で変動） | 高 |
| 実装コスト | 高 | 低 | 中 | 中 |

## 意図的に捨てるもの

- 公式実装の更新を自動的に取り込むこと
- 公式moduleと同等の網羅性
- 参照元とboilerplate実装の対応関係をコード上で追跡できること

## 保証範囲

- 保証すること: 実装着手時点で公式実装を確認したこと、および採否の根拠が記録されていること
- 保証しないこと: 公式実装の最新状態との継続的な一致

## 検証方法

- Static Analysis: 各ユースケースに参照記録のドキュメントが存在することを検査する。
- レビュー: ADR-0101 決定8の確認事項12・13（最新の公式実装を確認したか、何を採用し何を捨てたか）を、実装レビューの必須項目とする。
- Policy Test: 参照から採用したsecurity関連設定を、invariantとして機械検証する（ADR-0301、ADR-0401）。

## 影響

- 実装着手前に調査フェーズが発生する。
- 参照記録の更新が、定期レビューの作業単位になる。
- security advisory への対応はレビュー契機として明示される。

## 見直し条件

- 定期レビューの周期では追従できないほど、公式推奨の変化が速いサービスを扱う場合
- security advisory の発生頻度が、契機ベースのレビューでは処理しきれなくなった場合
