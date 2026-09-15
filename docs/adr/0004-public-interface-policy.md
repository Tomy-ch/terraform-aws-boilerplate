# ADR-0004: 公開インターフェースは意図を表現し、Provider schemaを露出しない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0002, ADR-0003, ADR-0005, ADR-0006, ADR-0010, ADR-0011

## Context

module interfaceをAWS Provider resourceの引数と対応付けて設計すると、実質的にProviderのschemaを再公開することになる。例えば次の形は、`aws_ecs_service` のschemaをそのまま利用者へ渡しているに等しい。

```hcl
ecs_service = {
  deployment_controller      = ...
  enable_execute_command     = ...
  propagate_tags             = ...
  scheduling_strategy        = ...
  placement_constraints      = ...
  capacity_provider_strategy = ...
}
```

この形には次の問題がある。

- 利用者はAWS Providerのdocumentationを読まなければ設定できない（抽象化が存在しない）
- 入力の組合せ空間がProviderのschemaと同等になり、テストできない
- 内部resource構成を変更すると公開契約が壊れる（ADR-0005が成立しない）
- 安全な既定値を維持できない（ADR-0011が成立しない）
- 属性の意味がAWS側の都合で変化した場合、boilerplateが吸収できない

同様に、IAMを `actions = ["s3:GetObject", "s3:GetObjectVersion", ...]` の形で受け取ると、最小権限の保証主体が利用者側へ移る。

## Decision

### 入力

1. 公開variableは、AWS Providerのschemaではなく、ユースケース上の意味論で定義する。
2. Provider resourceの引数と1:1対応する公開variableを原則として作らない。
3. 利用者が宣言するのは「どのresourceをどう作るか」ではなく「何を実現したいか」とする。想定する形は次のとおり。

   ```hcl
   services = {
     api = {
       type           = "web"
       container_port = 8080
       cpu            = 512
       memory         = 1024
       desired_count  = 2
     }
   }
   ```

4. 権限は意味論で受け取り、必要なIAM Actionはboilerplate内部で導出する。

   ```hcl
   permissions = ["read"]
   ```

   利用者へIAM Actionの列挙を要求する形を標準としない（ADR-0011）。

5. 役割の指定（例: `service_type = "web"`）から、ALB連携・health check・service構成・security group・log deliveryなどの構成を導出する。個別の有効化フラグを並べる形を標準としない。
6. 公式推奨で一意に決まる値は、そもそも公開しない（ADR-0010）。公開するのは、利用者固有の要件に属する入力に限る。

### 出力

7. outputは内部resourceの網羅的なexportを行わない。
8. outputとして公開するのは、以下のいずれかに該当し、かつ具体的な接続先が説明できるものに限る。

   - 他のサポート対象ユースケースとの接続に必要な識別子
   - boilerplate外部のシステム（DNS、CI/CD、監視基盤など）との接続に必要な識別子
   - 利用者が運用上参照する必要がある、安定した論理的な値

9. 各outputは、それを必要とする接続ユースケースとともに正当化する。「あると便利」を理由に追加しない。
10. ARNやIDを公開する場合も、公開するのは「その識別子が表す役割」であり、内部resourceの構成ではない。命名は役割ベースとする（例: `alb_dns_name` は可、`aws_lb_main_dns_name` のような内部address由来の命名は不可）。
11. デバッグ目的の情報は公開契約としない。内部状態の観測はTerraform stateおよびAWS側の観測手段で行う。
12. outputの追加・削除・意味変更は公開契約の変更として扱う（ADR-0005）。

### 例外

13. 本ADRの例外は、当該ユースケースのADRに、露出する属性・理由・代替案・見直し条件を記録した場合に限り認める。例外を暗黙に導入しない。

## 検討した代替案

### 案A: Provider schemaをそのまま通す（pass-through）

実装コストは最小で、Providerの新機能へ即応できる。ただし抽象化が存在せず、ADR-0002が掲げる保証（安全なdefault、テスト可能性、認知負荷の低減）のいずれも成立しない。

### 案B: 意味論的interfaceを基本としつつ、内部resourceへの部分的な直接設定を併置する

移行が容易で個別要求へ対応しやすいが、直接設定経路が既定の利用形態になりやすく、実質的に案Aへ収束する。Generic Escape Hatchの禁止（ADR-0007）とも整合しない。

### 案C: 内部resourceの全属性をoutputとして公開する

利用者の観測性は最大化されるが、内部構成が公開契約へ固定され、ADR-0005が成立しない。

### 評価

| 評価軸 | 案A: pass-through | 案B: 併置 | 案C: 全output公開 | 採用案: 意味論的契約 |
| --- | --- | --- | --- | --- |
| Security | 低 | 低 | 中 | 高 |
| Testability | 低 | 低 | 中 | 高 |
| Contract Clarity | 低 | 低 | 中 | 高 |
| Cognitive Load | 高 | 高 | 中 | 低 |
| Upgradeability | 低 | 低 | 低 | 高 |
| Maintainability | 中（追従は容易） | 低 | 低 | 高 |

## 意図的に捨てるもの

- Providerの新機能へ設定変更なしで即応できること
- 利用者が内部resourceを直接調整できること
- 内部resourceの網羅的な観測をoutput経由で行えること

## 保証範囲

- 保証すること: 公開variableとoutputが、AWS Providerのschemaから独立した意味論で定義されていること
- 保証しないこと: Providerが提供する全機能へのアクセス手段

## 検証方法

- Contract Test: 公開variableとoutputのschemaをsnapshotとして固定し、意図しない追加・削除・型変更を検出する。
- Static Analysis: 公開variable名がProvider resourceの引数名と一致するパターンを検出し、警告する。
- Policy Test: 意味論的入力（例: `permissions`）から生成されたIAM policyが、想定するActionの集合と一致することを検証する。
- Unit Test: 役割指定（例: `service_type`）から導出される構成が、planレベルで期待どおりであることを検証する。

## 影響

- Providerの新機能はboilerplate側の設計判断を経てから公開される。
- 利用者はAWS Providerのdocumentationを読まずに設定できる状態を目標とする。
- 内部実装の変更自由度が確保される（ADR-0005）。

## 見直し条件

- 意味論的抽象では表現できないユースケース要求が、複数ユースケースで反復した場合
- outputに関する例外が特定ユースケースで累積し、実質的な内部構成の公開へ近づいた場合
