# ADR-0102: ユースケース単位でスコープを定義する

- Status: Accepted
- Date: 2026-10-21
- Scope: repository-wide
- Related: ADR-0001, ADR-0101, ADR-0103, ADR-0201, ADR-0202, ADR-0204, ADR-0205, ADR-0301, ADR-0401, ADR-0402, ADR-0501, ADR-0601, ADR-0602

## Context

再利用資産の責務境界を「AWSサービス単位」で切ると、1つのmoduleが複数アーキテクチャを表現しようとする。ECSを例にすると、web service・worker・scheduled taskは同じ `aws_ecs_service` 系resourceを使うが、ネットワーク境界・スケーリング・可用性要件・observabilityの要求が異なる。これらを1moduleへ統合すると `enable_*` と optional variable が増加し、実際に成立する構成の組合せをテストできなくなる。

一方で「利用者から要求が出たら設定項目を追加する」運用を採ると、boilerplateの責務境界は要求の総和として際限なく拡大する。個別要求は個別には正当でも、総和としては保証不能な構成空間を生む。

スコープの単位をユースケースに決めても、それを実装する層が決まっていなければ、同じ問題は一段下で起きる。ユースケースが resource を直接宣言すれば、ユースケースごとの差異と不変条件の強制が同じ場所に混ざる。複数のユースケースが共有する部品に用途ごとの差異を持ち込めば、その部品はサービス単位のmoduleと同じ形になる。共有をやめれば、同じ不変条件をユースケースの数だけ書くことになる。環境ごとの差異をどこに置くかも決まらない。

したがって、スコープの単位と、スコープ拡大の判断基準、およびそれを実装する層の構成を明示的に決める必要がある。

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

3. 1つのユースケースは1つのアーキテクチャを表現する。1つのユースケースで複数アーキテクチャを表現しない。
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
9. module（一般 module と、ユースケースの `internal/` の module。決定12）はリポジトリ内部の実装詳細とし、公開契約としない。外部から直接参照されることを前提としない。
10. 複数ユースケースの組み合わせ方そのもの（どのユースケースをどう並べるか）は、boilerplateの保証対象としない。

### 構成の層

11. 構成を env → usecase → module の三層とする。env は usecase だけを呼び、module を直接呼ばない。
12. 各層の置き場を次とする。

    | 層 | 置き場 | 持つもの |
    | --- | --- | --- |
    | env | `env/<env>/` | 環境ごとの値と、usecase の組み合わせ。apply の起点 |
    | usecase | `usecases/<use-case>/` | 公開契約（決定2）と、module の合成 |
    | 一般 module | `modules/<module>/` | どのユースケースでも同じ形で成立する機構の resource と、その不変条件 |
    | usecase の module | `usecases/<use-case>/internal/<module>/` | 一般 module で表せない、そのユースケースに固有の機構 |

    `internal/` は必要なときにだけ置く。ユースケースの推奨構成は `usecases/<use-case>/reference/` に置く（ADR-0401 決定11-13）。リポジトリ直下に置かない。

13. 公開契約と semver の保証の対象は usecase だけとする。env は利用者が値を持つ層であり、module は決定9 のとおり内部実装である。
14. `usecases/<use-case>/` 直下の `*.tf` に、`resource` block と `provider` block を置かない。対象は直下に限り、`internal/` と `reference/` は含まない。ユースケースごとの差異は module の合成と入出力で表し、不変条件の強制は module が持つ。usecase は env から呼ばれる子 module なので、root module にしか置けない block（`import` 等）もここには置かない。
15. 1つの module は1つの機構をまとめる。一般 module は、どのユースケースでも同じ形で成立する機構だけを持ち、ユースケースごとの差異を入力の切り替え（用途の種別、`enable_*` 等）で抱えない。値が変わるだけで構造が変わらない入力（grantee の集合、保持日数等）は切り替えに当たらない。入力によって resource の有無や種類が変わるなら切り替えに当たる。一般 module で表せない機構は、それを必要とするユースケースの `internal/` に置く。attachment 型の resource は親と同じ module に置くので（決定18）、一般 module の resource に固有の attachment が要るなら、親の resource ごと `internal/` に置く。
16. ユースケースの間で共有するのは、module のコードと resource の実体の2つであり、扱いを分ける。

    - module のコード: 一般 module として `modules/` に置き、各ユースケースが呼ぶ。共有のための別の置き場を設けない
    - resource の実体: 持ち主のユースケースが1つだけ作り、他のユースケースへは ARN、ID、Endpoint、DNS 名などの識別子で渡す。受け側のユースケースは識別子を `variable` で受け、自分が呼ぶ module へ渡す。env が送り側の output をそこへ渡す。root をまたぐ受け渡しは remote state または SSM Parameter で行う。remote state は送り側の state 全体の読み取りを要するので、どちらを使うかと、受け側に許す権限は ADR-0301 に従う
    - 他のユースケースの内部 resource の address へ依存しない

17. module block の `source` が指してよい先を、層ごとに次へ限る。

    - env は `usecases/<use-case>/` だけを指す
    - usecase は `modules/` と、自分の `internal/` だけを指す
    - `internal/` の module は、`modules/` と、同じユースケースの `internal/` だけを指す
    - 一般 module は `modules/` だけを指す。一般 module の間の参照は循環させない
    - `reference/` は、それを持つユースケースだけを指す

    `source` はリポジトリ内の相対パスに限り（ADR-0205 決定3）、正規化した後のパスで判定する。

18. ユースケース内の配線（Security Group のルール、IAM Policy、DNS レコード、resource-level policy 等）は、独立した resource として置かない。受け側の module の意味論的な入力として表す。attachment 型の resource（bucket policy、queue policy、Security Group のルール、role policy 等）は、親の resource と同じ module に置く。

### experimental

19. 契約が安定していないユースケースは、experimentalとして明示したうえで追加してよい。
20. experimentalであっても、以下の原則は免除しない。

    - 型付き有限な公開interface（ADR-0203）
    - Generic Escape Hatchの禁止（ADR-0204）
    - 最小権限とSecure by Default（ADR-0301）
    - Static Analysis / Contract Test / Policy Test / Unit Test の実施（ADR-0401）

21. experimentalが免除されるのは「公開契約の後方互換性」のみとする。
22. experimentalなユースケースは、stableへの昇格条件と、満たせない場合の削除を、そのユースケースのADRへ記録する。無期限のexperimentalを許容しない。

## 検討した代替案

### 案A: AWSサービス単位でmoduleを切る

`ecs` / `rds` / `s3` のような単位を公開面とする。resourceの再利用性は高いが、1moduleが複数アーキテクチャを抱え、optional variableと `enable_*` が増える。ADR-0101の警戒兆候に直接該当する。公開面をユースケースにしても、その下の一般 module がユースケースごとの差異を入力の切り替えで抱えれば、同じ問題がmoduleの層で起きる —— 決定15 が、一般 module に持たせる範囲を限る理由である。

### 案B: 単一の巨大moduleで全体構成を受け取る

利用者の入力箇所は1つになるが、入力型が巨大化し、変更影響範囲がリポジトリ全体になる。部分的なテストと部分的な変更ができない。

### 案C: primitive moduleを公開し、利用者に合成させる

表現力は高いが、合成の正しさ（ネットワーク境界、IAM、observabilityの整合）が利用者責務となり、boilerplateが保証する対象が実質的に消える。

### 案D: Capability 単位の module を公開し、Composition Preset で組み合わせを提供する

公開面が Capability と Preset の2種類になる。Preset に無い組み合わせは、利用者が Capability を合成することになり、案C と同じく合成の正しさが利用者責務へ移る。

### 案E: 一般 module とは別に、共有のための置き場を設ける

`_shared/` のように、複数のユースケースが使う部品の置き場を一般 module の外に設ける。一般 module の層がすでに共有の層であるため、置き場が2つになり、どちらに置くかの判断が残る。どちらにも属さない部品の受け皿になりやすい。

### 案F: module をすべてユースケースの私有物とし、一般 module を設けない

用途ごとの差異が module に流れ込む経路は消える。ただし、どのユースケースでも同じ形の機構（Public Access Block、暗号化、TLS の強制を伴う bucket 等）を、ユースケースの数だけ書くことになる。不変条件の修正が全ユースケースへ追従する必要があり、ADR-0205 決定12 が認める invariant の共有の場所が無くなる。

### 評価

| 評価軸 | 案A: service単位 | 案B: 単一巨大module | 案C: primitive公開 | 案D: Capability＋Preset | 案E: 別の共有置き場 | 案F: module は全て私有 | 採用案: use case単位、一般 module ＋ internal |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Security | 中 | 中 | 低（合成が利用者責務） | 低（Preset外は利用者責務） | 中 | 中（不変条件の修正が分散） | 高 |
| Testability | 低（組合せ発散） | 低（部分検証不可） | 低 | 中 | 中 | 高 | 高（有限な構成） |
| Contract Clarity | 低 | 低 | 中 | 中 | 中（置き場が2つ） | 高 | 高（置き場は2つだが、判定は決定15 の基準で決まる） |
| Scope Control | 低 | 低 | 低 | 中 | 低（受け皿になる） | 高 | 中（判断フローを規定するが、決定15 は機械判定できない） |
| Cognitive Load | 中 | 高 | 高 | 高 | 中 | 中 | 中（一般 module か `internal/` かの判断が残る） |
| Upgradeability | 中 | 低 | 低 | 中 | 中 | 低（同じ修正を各所へ） | 高 |
| 実装の重複 | 少 | 少 | 少 | 少 | 少 | 多 | 中（固有の attachment を持つ親は `internal/` で複製される） |

## 意図的に捨てるもの

- AWSサービス単位での公開と、それによる再利用性
- primitive moduleの外部公開による合成の自由度
- サポート対象外ユースケースへの部分的な対応
- 一般 module への、ユースケース固有の機能の追加（固有のものは `internal/` に置く）

## 保証範囲

- 保証すること: supportedと宣言されたユースケースの完成度（安全なdefault、テスト、契約の明確さ）。決定14・17・18 の層の規則が守られていること
- 保証しないこと: unsupportedと宣言された構成、ユースケース同士の任意の組み合わせ

## 検証方法

- 各ユースケースは supported / unsupported を記述したドキュメントを持つこと（Static Analysisで存在を検査）
- 各ユースケースは最低1つの reference を持ち、Contract TestおよびPolicy Testの対象とすること（ADR-0401）
- experimentalなユースケースは、その旨と昇格・削除条件がADRとして存在すること
- 次の3つを、Static Analysis 層で Conftest が HCL のソースを読んで検査する（ADR-0501 決定5）
  - `usecases/<use-case>/` 直下の `*.tf` に `resource` block と `provider` block が無いこと（決定14）
  - module block の `source` が、層ごとに許された先だけを指すこと（決定17）
  - attachment 型の resource の親を指す引数が `module.` 参照を持たないこと、すなわち親と同じ module に在ること（決定18）。attachment だけを持つ module が親の識別子を `variable` で受ける形はこの検査を通るので、レビューで見る

決定15（一般 module がユースケースごとの差異を抱えないこと）は機械判定できない。一般 module の入力に用途の種別や `enable_*` が現れる、特定のユースケースだけが使う引数が増える、は崩れ始めた徴候であり、レビューで見る。徴候が出た機構は `internal/` へ移す。

## 影響

- 利用者要求の一部は明示的に「対象外」と回答される。
- ユースケース追加の判断は、設定項目追加の判断より重い手続きになる。
- 一般 module で表せない機構は、ユースケースごとの `internal/` に作られる。その間の重複は、重複のみを理由に統合しない（ADR-0205 決定11）。
- 一般 module の resource に固有の attachment が要る場合、親の resource ごと `internal/` に置くので（決定15・18）、その親が持つ不変条件はユースケースごとに複製され、修正の追従もユースケースごとになる。案F を退けた理由と同じ形の費用を、この範囲に限って受け入れる。
- 一般 module の変更は、それを呼ぶすべてのユースケースに及ぶ。一般 module の入出力の変更は、呼ぶ側のユースケースの Contract Test で検出する（ADR-0401）。
- 環境ごとの差異は env に集まり、usecase は環境を知らない。

## 見直し条件

- unsupportedへ分類した要求が、複数ユースケースで反復して発生した場合
- 一般 module の入力が、特定のユースケースのための切り替えで増え続ける場合
- ユースケースごとの `internal/` の重複が、セキュリティ修正の追従を阻害するレベルに達した場合
