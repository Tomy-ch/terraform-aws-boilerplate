# ADR-0102: ユースケース単位でスコープを定義する

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0001, ADR-0101, ADR-0201, ADR-0204, ADR-0401

## Context

再利用資産の責務境界を「AWSサービス単位」で切ると、1つのmoduleが複数アーキテクチャを表現しようとする。ECSを例にすると、web service・worker・scheduled taskは同じ `aws_ecs_service` 系resourceを使うが、ネットワーク境界・スケーリング・可用性要件・observabilityの要求が異なる。これらを1moduleへ統合すると `enable_*` と optional variable が増加し、実際に成立する構成の組合せをテストできなくなる。

一方で「利用者から要求が出たら設定項目を追加する」運用を採ると、boilerplateの責務境界は要求の総和として際限なく拡大する。個別要求は個別には正当でも、総和としては保証不能な構成空間を生む。

したがって、スコープの単位と、スコープ拡大の判断基準を明示的に決める必要がある。

## Decision

### 単位

1. 責務の単位をAWSサービスではなくユースケースとする。ユースケースは「システム上の役割」で定義する。
2. ユースケースの定義には、最低限以下を含める。

   - そのユースケースが解決する問題
   - 前提とする実行形態とネットワーク境界
   - 公開する意味論的interface（ADR-0201）
   - サポートする範囲（supported）
   - 明示的にサポートしない範囲（unsupported）
   - 品質保証の方法（ADR-0401）

3. 1つのユースケースは1つのアーキテクチャを表現する。1moduleで複数アーキテクチャを表現しない。
4. 本ADRはサポート対象ユースケースの一覧を固定しない。個々のユースケースの採否は、そのユースケース自身の追加時に判断する。想定する粒度の例は次のとおりとする（例示であり、実装を約束するものではない）。

   - ECS Web Service
   - ECS Worker
   - Scheduled Job
   - Static CDN
   - Private Database
   - Bastion
   - Event-driven Lambda

### スコープ判断

5. 利用者から新しい設定要求が発生した場合、次の順で判断する。

   1. 既存のサポート対象ユースケースの契約として一般化できるか。可能なら正式な契約へ追加する。
   2. 一般化できないが、独立したアーキテクチャとして成立するか。可能なら新規ユースケースとして分離する。
   3. いずれでもない場合、boilerplateの責務外とする。

6. 「特定の利用者が必要としている」ことのみを理由に設定項目を追加しない。
7. 責務外と判断した場合、その判断はユースケースのunsupported一覧へ記録する。判断を記録しないまま放置しない。

### 合成（composability）

8. ユースケースは、他のユースケースまたは外部システムとの接続点を、意味論的なinputとoutputでのみ持つ（ADR-0201）。
9. 内部で共有する実装単位（primitive module）はリポジトリ内部の実装詳細とし、公開契約としない。外部から直接参照されることを前提としない。
10. 複数ユースケースの組み合わせ方そのもの（どのユースケースをどう並べるか）は、boilerplateの保証対象としない。

### experimental

11. 契約が安定していないユースケースは、experimentalとして明示したうえで追加してよい。
12. experimentalであっても、以下の原則は免除しない。

    - 型付き有限な公開interface（ADR-0203）
    - Generic Escape Hatchの禁止（ADR-0204）
    - 最小権限とSecure by Default（ADR-0301）
    - Static Analysis / Contract Test / Policy Test / Unit Test の実施（ADR-0401）

13. experimentalが免除されるのは「公開契約の後方互換性」のみとする。
14. experimentalなユースケースは、stableへの昇格条件と、満たせない場合の削除を、そのユースケースのADRへ記録する。無期限のexperimentalを許容しない。

## 検討した代替案

### 案A: AWSサービス単位でmoduleを切る

`ecs` / `rds` / `s3` のような単位。resourceの再利用性は高いが、1moduleが複数アーキテクチャを抱え、optional variableと `enable_*` が増える。ADR-0101の警戒兆候に直接該当する。

### 案B: 単一の巨大moduleで全体構成を受け取る

利用者の入力箇所は1つになるが、入力型が巨大化し、変更影響範囲がリポジトリ全体になる。部分的なテストと部分的な変更ができない。

### 案C: primitive moduleを公開し、利用者に合成させる

表現力は高いが、合成の正しさ（ネットワーク境界、IAM、observabilityの整合）が利用者責務となり、boilerplateが保証する対象が実質的に消える。

### 評価

| 評価軸 | 案A: service単位 | 案B: 単一巨大module | 案C: primitive公開 | 採用案: use case単位 |
| --- | --- | --- | --- | --- |
| Security | 中 | 中 | 低（合成が利用者責務） | 高 |
| Testability | 低（組合せ発散） | 低（部分検証不可） | 低 | 高（有限な構成） |
| Contract Clarity | 低 | 低 | 中 | 高 |
| Scope Control | 低 | 低 | 低 | 高（判断フローを規定） |
| Cognitive Load | 中 | 高 | 高 | 低 |
| Upgradeability | 中 | 低 | 低 | 高 |

## 意図的に捨てるもの

- AWSサービス単位での再利用性
- primitive moduleの外部公開による合成の自由度
- サポート対象外ユースケースへの部分的な対応

## 保証範囲

- 保証すること: supportedと宣言されたユースケースの完成度（安全なdefault、テスト、契約の明確さ）
- 保証しないこと: unsupportedと宣言された構成、ユースケース同士の任意の組み合わせ

## 検証方法

- 各ユースケースは supported / unsupported を記述したドキュメントを持つこと（Static Analysisで存在を検査）
- 各ユースケースは最低1つのexampleを持ち、Contract TestおよびPolicy Testの対象とすること（ADR-0401）
- experimentalなユースケースは、その旨と昇格・削除条件がADRとして存在すること

## 影響

- 利用者要求の一部は明示的に「対象外」と回答される。
- ユースケース追加の判断は、設定項目追加の判断より重い手続きになる。
- 実装の重複がユースケース間で発生し得る。重複のみを理由に統合しない（ADR-0205 決定11）。

## 見直し条件

- unsupportedへ分類した要求が、複数ユースケースで反復して発生した場合
- ユースケース間の実装重複が、セキュリティ修正の追従を阻害するレベルに達した場合
