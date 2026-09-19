# ADR-0204: Generic Escape Hatchを提供しない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0101, ADR-0102, ADR-0201, ADR-0203, ADR-0301

## Context

サポート範囲外の要求が発生したとき、次のような「何でもできる設定口」を追加すると短期的には解決する。

```hcl
extra_iam_policy_json
additional_security_group_rules
resource_overrides
extra_tags_by_resource
custom_resource_arguments
```

しかしEscape Hatchは、boilerplateの契約外へ実装を流出させる。結果として次の状態になる。

- 同一ユースケースであっても、利用者ごとに実構成が異なる
- 実際に稼働している構成がboilerplateのテスト範囲外になる
- security invariantを維持できない（権限や通信経路を外部から追加できるため）
- upgrade時の影響範囲を判断できない
- 利用者がProvider resourceのschemaへ依存する（ADR-0201が成立しない）

さらに、Escape Hatchは「ユースケースの定義が不足している」という設計上の信号を隠す。要求は記録されず、正式な契約へ昇格する機会も失われる。

## Decision

1. 汎用的な設定注入口を提供しない。具体的には、以下の形式のvariableを公開しない。

   - 任意のIAM policy documentまたはpolicy statementを受け取るもの（例: `extra_iam_policy_json`、`additional_iam_statements`）
   - 任意のsecurity group ruleを受け取るもの（例: `additional_security_group_rules`）
   - 内部resourceの引数を上書きするもの（例: `resource_overrides`、`custom_resource_arguments`）
   - 内部resource単位で設定を差し込むもの（例: `extra_tags_by_resource`）
   - 任意のresourceを追加で生成させるもの

2. 「汎用的」とは、受け取る値の意味がユースケースの意味論ではなく、AWS ProviderまたはAWS APIの表現に依存している状態を指す。名前が `extra_*` でなくとも、この条件を満たすものはEscape Hatchとして扱う。
3. 新しい要求に対しては、以下の3択のいずれかを選ぶ。

   1. boilerplateの正式な機能として、意味論的な契約を追加する（ADR-0201）
   2. 新しいユースケースとして分離する（ADR-0102）
   3. boilerplateの責務外と判断する（ADR-0102 決定7に従い記録する）

4. 判断を保留したまま、暫定的なEscape Hatchを導入しない。
5. tagについては、リポジトリ共通の単一入力により、boilerplateが生成する全resourceへ一律付与する形のみを許容する。resource単位・種別単位でtagを指定する入力は提供しない。
6. IAMについて、権限の追加が必要な場合は意味論的な権限指定（ADR-0301）の語彙を拡張する。policy documentを受け取る形で解決しない。
7. ネットワークについて、通信経路の追加が必要な場合は、接続先を意味論的に指定する契約（接続対象のユースケースまたは論理的な役割）として定義する。CIDRやruleの直接指定で解決しない。
8. 本ADRに例外条項を設けない。例外が必要と判断した場合、本ADRをsupersedeする新規ADRとして、適用範囲と限界を再定義する。

## 検討した代替案

### 案A: Escape Hatchを提供し、利用は自己責任とする

未対応要求へ即座に対応でき、機能追加の圧力が下がる。ただしboilerplateが保証できる範囲が利用者ごとに異なり、「サポート対象ユースケースの完成度を保証する」という前提（ADR-0101）が成立しなくなる。

### 案B: Escape Hatchを提供し、利用時に警告・検査を行う

Policy Testで危険な注入を検出する運用。検出できるのは既知のパターンのみで、未知の構成には無力。また検査に通った構成が「サポートされている」と誤解される。

### 案C: 限定的なEscape Hatch（特定resourceのみ上書き可）

範囲を絞れば影響を限定できるように見えるが、対象resourceが公開契約へ固定され、ADR-0202が成立しなくなる。

### 評価

| 評価軸 | 案A: 自己責任 | 案B: 検査付き | 案C: 限定的上書き | 採用案: 提供しない |
| --- | --- | --- | --- | --- |
| Security | 低 | 中（既知パターンのみ） | 中 | 高 |
| Testability | 低（実構成が不明） | 低 | 中 | 高 |
| Contract Clarity | 低 | 低 | 中 | 高 |
| Upgradeability | 低 | 低 | 低 | 高 |
| Scope Control | 低 | 低 | 中 | 高 |
| 要求への即応性 | 高 | 高 | 中 | 低（意図的） |

## 意図的に捨てるもの

- 未対応要求への即時対応手段
- 利用者側でのworkaroundによる解決
- boilerplateを変更せずに構成を拡張できること

## 保証範囲

- 保証すること: あるユースケースを利用している限り、実構成がboilerplateのテスト範囲内にあること
- 保証しないこと: boilerplateが提供していない構成の実現手段

## 検証方法

- Static Analysis: 公開variable名に対する禁止パターン検査（`extra_`、`additional_`、`override`、`custom_`、`_json` 等の接頭・接尾辞）。
- Static Analysis: 公開variableの型に `string` を用いてJSONを受け取っている疑いのある定義（description または名称にpolicy/json/documentを含むもの）の検出。
- Contract Test: 公開variable schemaのsnapshotにより、Escape Hatch相当の入力追加をレビュー可能にする。
- Policy Test: 生成されるIAM policyおよびsecurity group ruleが、boilerplate内部で導出された集合と一致すること（外部由来の要素が存在しないこと）を検証する。

## 影響

- 未対応要求は、機能追加・新規ユースケース・対象外のいずれかに分類されるまで解決しない。
- 要求がEscape Hatchへ吸収されず、設計上の判断として可視化される。
- 短期的な対応速度は低下する。

## 見直し条件

- 3択（決定3）のいずれにも分類できない要求が反復して発生した場合
- ユースケースの定義粒度そのものを見直す必要が生じた場合
