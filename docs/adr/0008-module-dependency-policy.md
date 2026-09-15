# ADR-0008: 外部Terraform moduleへ依存しない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0002, ADR-0004, ADR-0005, ADR-0009, ADR-0010

## Context

Terraformには、AWS公式・HashiCorp公式・community製の再利用moduleが多数存在する。これらを実行時依存として採用すると、実装コストは下がる一方で、次の追従義務が発生する。

- 外部moduleのinterface変更
- 外部moduleの内部resource構成変更
- 外部moduleのdefault値変更
- 外部moduleのupgrade対応（Provider互換、Terraform version互換）
- 外部moduleがサポートしないユースケースの発生

これらはいずれも、本リポジトリの設計判断とは無関係な理由で発生する。

さらに本質的な問題として、外部moduleが持つ汎用性がそのまま流入する。汎用moduleは多様な利用者を対象とするため、自由度・default値・resource構成の選択が「誰に対しても妥当」な方向へ設計される。本リポジトリは自由度を制限することで保証範囲を確保する戦略（ADR-0002）を採るため、この性質は直接的に対立する。

また、外部moduleは内部resource構成を自身の契約として持つ。これを取り込むと、内部構成を実装詳細として扱う決定（ADR-0005）が、外部moduleの層で成立しなくなる。

## Decision

### module依存

1. 実行時依存としてのTerraform moduleを採用しない。対象は publisherを問わない（AWS公式、HashiCorp公式、community製、社内外を問わない）。
2. utility系module（命名、tag生成、CIDR計算など、resourceを生成しないものを含む）も同様に採用しない。
3. `module` blockの `source` は、リポジトリ内の相対パスに限る。Terraform Registry、Git URL、HTTP、S3等の外部sourceを使用しない。
4. 外部moduleのforkおよびvendoringも、「外部moduleへの依存」として扱い採用しない。必要な知見はADR-0009に従い、参照実装として取り込む。
5. 本ADRに例外条項を設けない。例外が必要と判断した場合、本ADRをsupersedeする新規ADRとして、許容する依存の範囲・選定基準・upgrade責務・security対応の手順を定義する。

### provider依存

6. providerはdependencyとして採用する。moduleとproviderを同一視しない。両者の差は次のとおりとする。

   | 観点 | 外部module | provider |
   | --- | --- | --- |
   | 提供するもの | resource構成と抽象化の選択 | AWS APIへのアクセス手段 |
   | 代替可能性 | 自前実装で代替可能 | 代替不可 |
   | 契約への影響 | 内部構成が外部契約に従属する | resource schemaのみに従属する |
   | 設計判断の所有 | 外部 | 本リポジトリ |

7. 各moduleは `required_providers` にversion制約を宣言する。制約は、検証済みの下限versionを起点とし、互換性を破壊し得るmajor更新を除外する形とする。上限を過度に固定しない。
8. `required_version`（Terraform本体）も同様に、検証済みの下限を宣言する。
9. lock fileは、テストおよびexampleのroot（実際に `terraform init` を行う単位）で管理する。再利用単位のmoduleディレクトリではlock fileを管理しない。
10. providerのversion更新は、ADR-0012の各テストレイヤーを通過することを条件とする。

### 重複の扱い

11. 外部moduleを使わないことにより、リポジトリ内で類似実装が生じることを許容する。実装の重複のみを理由に共通化しない（ADR-0001 決定17・18）。
12. 共通化はarchitecture invariantの共有を根拠に行い、その場合も共通化の単位はリポジトリ内部の実装詳細とする（ADR-0003 決定9）。

## 検討した代替案

### 案A: 公式moduleを実行時依存として採用する

実装コストと初期の網羅性で優位。ただし公開契約・default値・内部構成の決定権が外部へ移り、ADR-0004 / 0005 / 0006 / 0010 / 0011 のいずれも、外部module層では保証できない。security fixの適用タイミングも外部のrelease cycleに従属する。

### 案B: 公式moduleをforkして利用する

変更の自由度は得られるが、upstream追従のコストを継続的に負う。fork時点の汎用性は残り、ADR-0002の方向性と一致しない。実質的に「自前実装 + 追従義務」となり、利点が相殺される。

### 案C: utility系moduleのみ許容する

resourceを生成しないため影響は小さいように見えるが、依存管理・supply chain・version追従の仕組みを結局導入する必要がある。導入した仕組みは、resource生成moduleへの拡大圧力を持つ。

### 評価

| 評価軸 | 案A: 公式module依存 | 案B: fork | 案C: utilityのみ | 採用案: 依存しない |
| --- | --- | --- | --- | --- |
| Security | 中（外部release依存） | 中 | 中 | 高（構成を所有） |
| Contract Clarity | 低 | 中 | 中 | 高 |
| Maintainability | 中（初期は高、追従で低下） | 低 | 中 | 中 |
| Upgradeability | 低 | 低 | 中 | 高 |
| Scope Control | 低（汎用性が流入） | 低 | 中 | 高 |
| Reproducibility | 中 | 中 | 中 | 高 |
| 実装コスト | 低 | 中 | 低 | 高（意図的に受容） |

## 意図的に捨てるもの

- 外部moduleによる実装コストの削減
- 外部moduleが既に対応済みのedge caseへの無償の追従
- 外部moduleのcommunityによる継続的な改善の自動的な取り込み

## 保証範囲

- 保証すること: boilerplateが生成する全構成の決定権を本リポジトリが所有すること
- 保証しないこと: 外部moduleが提供する機能との同等性

## 検証方法

- Static Analysis: `module` blockの `source` がリポジトリ内相対パスのみであることを検査する。
- Static Analysis: `required_providers` および `required_version` の宣言が存在することを検査する。
- Contract Test: provider version更新時に、公開契約のsnapshotが変化しないことを検証する。
- Integration / E2E Test: provider version更新時の実挙動を検証する（ADR-0012）。

## 影響

- 実装コストは外部module採用時より高い。これはADR-0002の優先順位に基づく受容済みのコストである。
- 外部moduleの知見は、参照実装として取り込む（ADR-0009）。
- 依存管理の対象はprovider とTerraform本体に限定される。

## 見直し条件

- AWS APIの複雑性が、自前実装では安全な構成を維持できないレベルに達した場合
- 外部moduleに、本リポジトリと同等の制約（限定ユースケース、型付き契約、最小権限default）を持つものが現れ、かつその契約の安定性が実証された場合
