# ADR-0005: 内部のAWS resource構成を実装詳細として扱う

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0002, ADR-0004, ADR-0009, ADR-0010, ADR-0012

## Context

利用者が「moduleがどのresourceを何個作るか」に依存し始めると、次の変更がすべて破壊的変更になる。

- AWSの推奨構成の変更
- Providerにおけるresourceの分割・統合・置換
- 同一機能を実現する別AWS機構への移行
- 内部的なresource分割によるリファクタリング

本リポジトリは公式推奨の変化をboilerplate内部で吸収することを方針としている（ADR-0010）。そのためには、内部構成が公開契約に含まれないことを明示的に決めておく必要がある。

一方で、Terraformにはstateという実務上の制約がある。内部resourceのaddressが変わると、利用者のstateには破壊と再作成が生じ得る。「実装詳細である」ことと「利用者に影響がない」ことは同義ではない。この差を埋める手段も併せて決める必要がある。

## Decision

### 公開契約の定義

1. 公開契約は以下に限る。

   - 公開variableの名前・型・意味・既定値（ADR-0004、ADR-0006）
   - 公開outputの名前・型・意味（ADR-0004）
   - ユースケースが宣言する機能と性質（ネットワーク境界、可用性、observability、セキュリティのinvariant）
   - supported / unsupported の宣言（ADR-0003）

2. 以下は公開契約に含めない。

   - 内部AWS resourceの種類・個数
   - 内部resourceのTerraform address
   - 内部resourceの物理名（AWS上の名前）のうち、outputとして公開していないもの
   - 内部で利用するProvider機能の選択

3. ユースケースが契約するのは「Private Subnetで稼働し、ロードバランサ経由で公開され、指定されたobservability backendへtelemetryを送る」といった性質であり、「`aws_ecs_service` が1つ、`aws_security_group` が2つ、`aws_cloudwatch_log_group` が1つ」という構成ではない。

### 破壊的変更の定義

4. 以下を破壊的変更（major）とする。

   - 公開variableの削除、型変更、意味変更、既定値の意味を変える変更
   - 公開outputの削除、型変更、意味変更
   - security invariantの緩和
   - supported ユースケースまたはsupported構成の削除
   - 利用者の入力変更なしには適用できない変更

5. 以下は破壊的変更としない。

   - 公開契約を変えない内部resource構成の変更
   - 内部resource addressの変更
   - 公開契約を変えないdefault値の変更（公式推奨の更新に伴うもの、ADR-0010）

### state移行

6. 内部resource addressの変更は、`moved` block等の移行手段を同一変更内で提供することを必須とする。移行手段を提供できない変更は、破壊的変更として扱う。
7. 利用者による `terraform state` 直接操作、targeted apply、内部resourceの `import` を前提としない。これらを前提とする運用手順をドキュメント化しない。
8. resourceの置換（replace）が避けられない変更は、それ自体が公開契約を変えなくても、影響と移行手順を明示したうえでminor以上の変更として扱う。

### 物理名

9. 内部resourceの物理名は、boilerplateが決定する命名規則に従って生成する。命名規則そのものを利用者が制御する入力は公開しない。
10. 命名規則の変更はresource置換を伴うため、決定8に従う。

## 検討した代替案

### 案A: 内部resource構成を公開契約に含める

利用者はaddressを前提とした運用（targeted apply、state操作、resource単位の監視設定）が可能になる。ただしAWSの推奨構成やProviderの進化に追従できなくなり、ADR-0010と両立しない。

### 案B: 実装詳細と宣言するが、state移行手段は提供しない

宣言としては最も単純だが、実際には利用者環境で破壊と再作成が発生し、宣言が運用実態と乖離する。「実装詳細である」という主張が信頼されなくなる。

### 評価

| 評価軸 | 案A: 構成を契約化 | 案B: 移行手段なし | 採用案: 実装詳細 + 移行提供 |
| --- | --- | --- | --- |
| Security | 中 | 中 | 高（構成変更で改善を適用可能） |
| Upgradeability | 低 | 中（宣言上は高、実態は低） | 高 |
| Maintainability | 低 | 中 | 高 |
| Contract Clarity | 中 | 低（契約と実態が乖離） | 高 |
| Reproducibility | 中 | 低 | 高 |
| Testability | 中 | 中 | 高（契約をsnapshot検証可能） |

## 意図的に捨てるもの

- 利用者によるresource address前提の運用（targeted apply、state直接操作、内部resourceのimport）
- 内部resource構成の安定性に対する期待
- 内部resource物理名を利用者が決定できること

## 保証範囲

- 保証すること: 公開契約（決定1）の安定性、内部構成変更時の移行手段の提供
- 保証しないこと: 内部resourceの構成・address・物理名の安定性

## 検証方法

- Contract Test: 公開variable / outputのschemaをsnapshotとして固定し、差分を破壊的変更判定のトリガーとする。
- Unit Test: 内部構成を変更した際も、ユースケースが宣言する性質（ネットワーク境界、権限、telemetry送信先）がplanレベルで維持されることを検証する。
- Integration / E2E Test: 利用者視点の振る舞いが内部構成変更の前後で一致することを検証する。
- Static Analysis: 内部resource addressの変更を検出した場合に、対応する `moved` blockの存在を検査する。

## 影響

- 内部リファクタリングの自由度が確保される。
- 変更時のレビュー観点が「resourceが変わったか」ではなく「公開契約が変わったか」になる。
- 移行手段の提供が実装コストとして常時発生する。

## 見直し条件

- 移行手段では吸収できない構成変更が反復して必要になった場合
- Terraform側のstate移行機能に、本ADRの前提を変える変更があった場合
