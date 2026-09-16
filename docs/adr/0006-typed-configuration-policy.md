# ADR-0006: 公開設定は型付きかつ有限とし、Raw JSONと非型付き設定を禁止する

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0002, ADR-0004, ADR-0007, ADR-0011, ADR-0012

## Context

Terraformの型システムは `any` や `map(any)` を許容するため、公開interfaceを型付けしないまま任意の構造を受け取ることができる。同様に、IAM policyやresource policyをJSON文字列として受け取ることもできる。

これらを許容すると、次の帰結が生じる。

- 契約が不明確になる（何を渡せるかがコードから読み取れない）
- validationが困難になる（構造が確定しないため、意味的検査ができない）
- 破壊的変更を検知できない（schema差分が取得できない）
- Provider resourceの内部構造が実質的に露出する（ADR-0004が成立しない）
- 最小権限を保証できない（任意のpolicyが注入され得る、ADR-0011が成立しない）
- static analysisの精度が低下する
- 生成系ツールが、存在しない属性や誤った構造を生成しやすくなる

「型付けを省略する」判断は、実装時点では低コストだが、以後すべての保証手段を同時に無効化する。

## Decision

### 原則

1. 公開variableは、Terraformの型システム上で有限かつ明示的な契約として定義する。
2. 公開variableに以下を使用しない。

   - `any`
   - `map(any)` / `list(any)` / `set(any)`
   - object型の属性としての `any`
   - 構造を規定しない `map(string)` のうち、キーが実質的に設定名として機能するもの

3. 以下の形式のvariableを提供しない。

   ```hcl
   extra_config       = {}
   resource_overrides = {}
   additional_settings = {}
   ```

   これらはADR-0007（Generic Escape Hatch禁止）にも該当する。

4. 利用者へ任意のJSONを記述させるinterfaceを提供しない。IAM policy、resource policy、bucket policy等はboilerplate内部で生成する（ADR-0011）。

### 型の選択順序

5. 入力表現は次の優先順位で選択する。下位ほど採用を避ける。

   1. enum（`validation` で許容値を列挙した `string`）
   2. `bool`
   3. primitive（`string` / `number`）
   4. typed object
   5. typed list / typed set
   6. generic map（キーが利用者定義の識別子であり、値が typed object であるもの）
   7. raw JSON

6. 優先順位6（generic map）は、キーが利用者定義の論理名（例: service名）であり、値が完全に型付けされた object である場合に限り許容する。キーが設定名として機能するmapは許容しない。
7. 優先順位7（raw JSON）は例外条項（決定12）を満たす場合に限る。

### 型定義の規律

8. object型は `optional()` を用いて省略可能属性を明示し、既定値を型定義側に置く。
9. `null` を意味のある値として扱う場合、その意味（未指定・無効化・AWS側既定への委譲のいずれか）をvariableの description に記述する。意味が複数になる `null` を作らない。
10. 有限集合として表現できる入力は、`string` のまま放置せず enum として `validation` を付ける。
11. `validation` は型の制約に限定し、セキュリティ上のinvariantは `validation` のみに依存せず Policy Test でも検証する（ADR-0012）。

### 例外

12. 例外は、外部APIがJSONを第一級のinterfaceとして持ち、boilerplate側で意味のある型へ変換することが著しく困難である場合に限り検討する。
13. 例外を採用する場合、以下をADR（原則としてuse-case ADR）へ記録する。

    - 対象となる入力と、型化が困難である具体的理由
    - 受け入れる構造の範囲と、検証方法
    - 最小権限およびsecurity invariantを維持する手段
    - 見直し条件

14. 例外は当該入力に限定し、他の入力へ波及させない。例外の存在を理由に他の入力の型付けを緩めない。

## 検討した代替案

### 案A: `map(any)` と raw JSON を許容し、documentationで期待構造を説明する

実装は容易で、Providerの変化にも追従しやすい。ただし契約はdocumentationにしか存在せず、機械検証もschema差分検出もできない。ADR-0005の破壊的変更判定が機能しない。

### 案B: 型付きを原則とし、JSON入力を「上級者向け」として併置する

段階的移行としては自然だが、JSON経路が既定の利用形態になった時点で、最小権限もContract Testも保証できなくなる。保証範囲が利用者ごとに異なる状態を生む。

### 案C: JSON入力を受け取り、boilerplate内部でvalidationする

構造検査は可能だが、検査はTerraform実行時の文字列解析となり、型としての契約は得られない。エディタ補完・static analysis・schema snapshotのいずれも機能しない。

### 評価

| 評価軸 | 案A: 非型付き許容 | 案B: 併置 | 案C: JSON + 実行時検査 | 採用案: 型付き有限 |
| --- | --- | --- | --- | --- |
| Type Safety | 低 | 低 | 中 | 高 |
| Security | 低 | 低 | 中 | 高 |
| Testability | 低 | 低 | 中 | 高 |
| Contract Clarity | 低 | 中 | 中 | 高 |
| Maintainability | 中 | 低 | 中 | 中 |
| Cognitive Load | 高 | 高 | 高 | 低 |

## 意図的に捨てるもの

- 利用者が任意構造を渡せること
- 型定義を更新せずにProviderの新属性へ対応できること
- 型定義の記述コストの削減

## 保証範囲

- 保証すること: 公開variableの構造が型として確定し、schema差分として検出可能であること
- 保証しないこと: 型として表現されていない入力の受け入れ

## 検証方法

- Static Analysis: 公開variableに `any` が出現しないことを検査する。`extra_` / `additional_` / `override` を含むvariable名を検出する。
- Contract Test: 公開variableのschema（名前・型・optional・default）をsnapshotとして固定する。
- Unit Test: `validation` の境界値（許容値・非許容値）を検証する。
- Policy Test: 生成物（IAM policy等）が、入力の型だけでなくsecurity invariantを満たすことを検証する。

## 影響

- Providerの新属性へ対応するには、型定義とテストの更新が必要になる。
- 公開variableの追加は、その時点でContract Testの対象になる。
- 型定義がそのままdocumentationとして機能する。

## 見直し条件

- Terraformの型システムに、現在表現できない制約（判別共用体、相互排他制約など）を表現可能にする変更があった場合
- 例外条項の適用が複数ユースケースで反復し、原則の見直しが必要になった場合
