# ADR-0101: 保証可能性を自由度より優先する

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0001, ADR-0102, ADR-0201, ADR-0203, ADR-0204, ADR-0401

## Context

Terraformの再利用資産には、大きく2つの設計方向がある。

1. AWS Providerの各resourceを薄くラップし、利用者がresourceと同等の自由度を得られるようにする汎用module collection
2. 限定されたアーキテクチャを、安全かつ再現可能な形でエンコードした参照実装

(1) は任意構成を表現できるが、構成の正しさ・安全性・再現性は利用者側の責務として残る。moduleが保証できるのは「Terraformとして妥当なresourceを作ること」までであり、「そのアーキテクチャが安全であること」は保証できない。自由度が高いほど、moduleが取り得る構成空間は組合せ的に増大し、テスト可能な範囲は相対的に縮小する。

本リポジトリは (2) を採る。したがって、設計上の判断基準を「利用者が何をできるか」ではなく「boilerplateが何を保証できるか」に置く必要がある。この基準を各ADRで繰り返し再定義しないため、リポジトリ全体の評価軸としてここで固定する。

## Decision

### 位置づけ

1. 本リポジトリが提供するものは次の一点とする。

   > 限定されたAWSアーキテクチャおよびユースケースを、安全かつ再現可能な形でTerraformへエンコードした参照実装。

2. 以下を目的としない。

   - 汎用的なTerraform module collection
   - AWS Provider resourceの薄いラップ
   - 任意のAWS構成を表現可能にすること

### 優先順位

3. 設計判断が競合した場合、以下を優先する。

   - 安全性
   - 再現性
   - テスト可能性
   - 契約の明確さ
   - 認知負荷の低減
   - 最小権限
   - ユースケース単位での完成度

4. 以下は主要な評価軸としない。

   - 利用者の自由度
   - 汎用性
   - 任意構成への対応

5. 自由度を増やす提案は、その自由度によって失われる保証との比較を必須とする。比較なしに自由度を追加しない。

### 評価軸

6. すべてのADRは、代替案を最低限以下の評価軸で比較する。各ADRは関連する軸を選択してよいが、Security・Testability・Contract Clarity・Scope Control は常に評価する。

   | 評価軸 | 意味 |
   | --- | --- |
   | Security | 最小権限および安全なdefaultを維持できるか |
   | Testability | 自動テストで振る舞いを保証できるか |
   | Contract Clarity | moduleの契約が明確か |
   | Type Safety | 非型付きデータを排除できるか |
   | Maintainability | Provider / AWSの変更へ追従可能か |
   | Cognitive Load | 利用者がAWS内部構造を意識せず利用できるか |
   | Reproducibility | 同じ入力から同等構成を再現できるか |
   | Upgradeability | 内部実装変更を利用者へ波及させずに済むか |
   | Observability | 構築・運用状態を観測可能か |
   | Scope Control | boilerplateの責務が際限なく拡大しないか |

### 設計原則

7. リポジトリ全体の基本原則を以下とする。各原則の詳細および例外条件は、対応するADRで定義する。

   1. Test everything that can reasonably be tested.（ADR-0401）
   2. Do not expose raw AWS resources as the public interface.（ADR-0201）
   3. Do not depend on external Terraform modules, including official modules.（ADR-0205）
   4. Use the latest official implementations as reference implementations before building our own.（ADR-0206）
   5. Optimize for supported use cases, not configurability.（ADR-0102）
   6. Least privilege must be the default.（ADR-0301）
   7. Configuration must be semantic, typed, and finite.（ADR-0203）
   8. Raw JSON and untyped configuration are prohibited by default.（ADR-0203）
   9. Generic escape hatches are prohibited by default.（ADR-0204）
   10. Expose intent, not provider schema.（ADR-0201）
   11. Treat the concrete AWS resource topology as an implementation detail whenever possible.（ADR-0202）

### 設計レビュー時の確認事項

8. 新しい設計判断をADR化する際、および公開interfaceを追加・変更する際は、最低限以下を確認する。

   1. これはどのユースケースを解決するのか。
   2. そのユースケースはboilerplateがサポートすべきものか。
   3. AWS resourceのschemaをそのまま利用者へ露出していないか。
   4. より意味論的なinterfaceへ変換できないか。
   5. Raw JSONや `map(any)` へ逃げていないか。
   6. Generic Escape Hatchを作ろうとしていないか。
   7. Least Privilegeを維持できるか。
   8. 安全な設定がdefaultになっているか。
   9. どのようにテストするのか。
   10. Contract Testを書けるinterfaceになっているか。
   11. Static Analysis可能な構造になっているか。
   12. 最新の公式実装を確認したか。
   13. 公式実装のどの知見を採用し、何を捨てたか。
   14. Providerの都合をboilerplateの公開契約へ漏らしていないか。
   15. 内部resource構成を将来変更できるか。
   16. この設定項目を一度公開した場合、将来維持できるか。
   17. 「念のため」という理由だけで設定項目を追加していないか。
   18. この機能を削った場合、本当にユースケースが成立しないか。

### 設計上の警戒兆候

9. 以下が観測された場合、実装を進める前に設計を再検討する。これらは違反そのものではなく、契約境界が壊れ始めている可能性を示す指標として扱う。

   | 兆候 | 示唆 |
   | --- | --- |
   | Provider resourceの引数とvariableが1:1対応している | 薄いwrapperになっている |
   | optional variableが大量に存在する | 複数ユースケースを1moduleへ詰め込んでいる |
   | `map(any)` が必要になる | 公開interfaceの意味論が定義できていない |
   | Raw JSON入力が必要になる | resource実装詳細を利用者へ漏らしている |
   | `extra_*` が増える | moduleの責務境界が壊れ始めている |
   | `enable_*` が大量に増える | 1moduleで複数アーキテクチャを表現しようとしている |
   | 利用者がAWS Providerのdocumentationを読まないとvariableを設定できない | 抽象化が不足している |
   | テスト方法を説明できない設定が存在する | boilerplateがその設定を保証できない |

### 到達目標

10. 利用者が考えるべき事項を「どのAWS resourceをどう設定するか」ではなく「どのサポート済みアーキテクチャを利用し、システムとして何を実現するか」とする。
11. AWS Providerの複雑性、resource間の接続、標準的なセキュリティ設定、IAM、observability、Terraform固有の実装詳細は、可能な限りboilerplate側で吸収する。

## 検討した代替案

### 案A: 汎用Terraform module collectionとする

AWS Provider resourceを薄くラップし、利用者が任意構成を組めるようにする。表現力は最大だが、構成空間が組合せ的に増大し、安全性・再現性を保証できない。テストは「resourceが作成できること」に限定される。

### 案B: 自由度と保証を両立させる（安全なdefault + 全項目の上書き可能）

安全なdefaultを提供しつつ、すべての設定を上書き可能にする。実運用では上書き経路が既定の利用形態になり、boilerplateの保証は「初期値の提案」に縮退する。テスト対象は上書きされていない構成に限られ、実際に稼働する構成を検証できない。

### 評価

| 評価軸 | 案A: 汎用collection | 案B: default + 全上書き可 | 採用案: 保証優先 |
| --- | --- | --- | --- |
| Security | 低（利用者責務） | 低（上書きで無効化可能） | 高（invariantとして維持） |
| Testability | 低（構成空間が発散） | 低（実構成を検証できない） | 高（有限な構成空間） |
| Contract Clarity | 低（schema再公開） | 中（二重契約） | 高 |
| Reproducibility | 低 | 中 | 高 |
| Scope Control | 低 | 低 | 高 |
| Cognitive Load | 高 | 高 | 低 |
| 表現力 | 高 | 高 | 低（意図的） |

## 意図的に捨てるもの

- 任意のAWS構成を表現できること
- 単一のmoduleで広範な利用者要求を吸収できること
- サポート対象外の構成に対する「とりあえず動く」経路

## 保証範囲

- 保証すること: サポート対象として定義されたユースケースについて、安全なdefault・再現性・テストによる裏付け
- 保証しないこと: サポート対象外の構成、および本リポジトリの外で行われるresource操作の結果

## 検証方法

本ADRは上位原則であり、単体で機械検証しない。実効性は各下位ADRの検証手段（ADR-0201 / 0006 / 0007 / 0008 / 0011 / 0012）によって担保する。警戒兆候のうち以下は静的検査の対象とする。

- 公開variableにおける `any` 型の出現
- `extra_` / `additional_` / `override` を含むvariable名の出現
- moduleあたりのoptional variable数の閾値超過（警告として扱う）

## 影響

- 利用者の個別要求は、正式なユースケース化・新規ユースケース化・対象外のいずれかへ分類される（ADR-0102）。
- 「できない」ことが設計上の正しい結果になり得る。

## 見直し条件

- サポート対象ユースケースの大半で、契約外構成が必要という実証が積み上がった場合
- 優先順位（決定3）のいずれかが、実際の運用で維持不能と判明した場合
