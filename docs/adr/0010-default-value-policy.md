# ADR-0010: 設定値は公式ベストプラクティスを根拠に決定し、利用者へ公開しない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0004, ADR-0005, ADR-0006, ADR-0009, ADR-0011, ADR-0012

## Context

設定値の決定には2つの方向がある。

1. 利用者へ選択肢として公開し、判断を委ねる
2. boilerplate側で決定し、公開しない

(1) を既定にすると、公開variableは増え続ける。しかし多くの設定値には、AWSまたはTerraform側に明確な推奨が存在し、利用者が判断する余地は実際には存在しない。TLS policy、暗号化の有効化、public access blockなどがこれに当たる。これらを公開することは、「利用者に判断させている」のではなく「誤設定の経路を提供している」に近い。

一方で、Terraform Providerのdefault値をそのまま使うことは、公式ベストプラクティスに従うことと同義ではない。Provider defaultは以下の理由で決まっていることがある。

- 後方互換性の維持
- AWS API側のdefaultの反映
- historicalな挙動の維持
- optional fieldの省略

したがって「明示的に設定しない」という選択は、「推奨値を採用した」ことを意味しない。

また、公式推奨は不変ではない。AWSサービス、Provider、公式実装の更新により推奨構成は変化する。この変化をboilerplate内部で吸収できる設計にしておく必要がある。

## Decision

### 基本方針

1. 設定値は、原則として公式ドキュメントおよび公式実装で推奨されるベストプラクティスに従う。

   > Prefer official best-practice defaults over user configurability.

2. 「公式」の定義はADR-0009 決定3に従う。

### 決定優先順位

3. 設定値は以下の順で決定する。

   1. AWS公式の明示的な推奨値・ベストプラクティス
   2. AWS Well-Architected等の公式設計指針
   3. AWS公式Terraform moduleの最新実装
   4. HashiCorp / Terraform AWS Provider の公式ドキュメント
   5. AWSサービス固有の公式セキュリティガイド
   6. 明確な公式推奨が存在しない場合のみ、独自判断

4. 独自判断（優先順位6）を採用した場合、その理由を当該ユースケースの設計記録またはADRへ残す。

### Provider defaultの扱い

5. Provider defaultへ無条件に依存しない。boilerplateとして保証したい値は、Provider defaultと一致する場合であっても明示的に設定する。
6. 明示設定の対象は、少なくとも以下とする。

   - security invariantに関わる設定
   - reliability invariantに関わる設定
   - Provider majorversion更新で変化し得る設定

### 公開の基準

7. 公式推奨で一意に決まる値は、利用者へ公開しない。boilerplate内部で固定する。対象の例は以下。

   - TLS policy
   - 暗号化の有効・無効
   - public access block
   - log の暗号化
   - secure transport の要求

8. 利用者へ公開するのは、原則として利用者固有の要件に属する入力に限る。

   - workload size
   - business requirement
   - availability requirement
   - retention requirement
   - scaling requirement
   - domain固有の設定（ドメイン名、識別子など）

9. すなわち、利用者に入力させるのは「インフラの正しい設定方法」ではなく「利用者固有の要件」とする。
10. 新しいvariableまたは設定値を追加する前に、以下を確認する。

    1. AWS公式の推奨値は存在するか。
    2. 公式推奨値で固定できない理由は何か。
    3. この値は本当に利用者固有の要件か。
    4. Provider resourceのparameterをそのまま露出しようとしていないか。
    5. enum等の有限な型へ閉じられないか。
    6. 安全なdefaultをboilerplate側で決められないか。
    7. 利用者が誤設定した際に security / reliability invariant を壊さないか。
    8. この設定を Contract Test または Policy Test で検証できるか。
    9. 公式推奨が将来変わった場合に、boilerplate内部で追従できるか。

### 推奨から外れる場合

11. 公式推奨から外れる構成を採用する場合、暗黙に行わない。以下を当該ユースケースのADRへ記録する。

    - どの公式推奨から外れるのか
    - なぜ外れる必要があるのか
    - どのユースケース上の制約によるものか
    - security / availability / cost への影響
    - 代替案
    - 見直し条件

### コストとの関係

12. 公式ベストプラクティスが常に最小コストであるとは限らない。以下を分離して扱う。

    | 分類 | 扱い |
    | --- | --- |
    | Security / Reliability invariant | 原則として維持する |
    | Performance recommendation | ユースケース依存で判断する |
    | Cost optimization | ユースケースのSLOと規模に応じて判断する |

13. Multi-AZ、NAT Gateway、backup retention、cross-region replication、enhanced monitoring、log retention 等については、公式推奨を理解したうえで対象ユースケースの要件に合わせて採否を決定する。
14. コストを理由に推奨構成を外す場合、「高コストだから削る」ではなく「どの保証を削ることになるか」を明示して判断する。

### versioning

15. 設定値の決定時には、以下をboilerplate内部の記録として残す（ADR-0009 決定8と同一の記録へまとめてよい）。

    - 参照した公式ドキュメント
    - 参照した公式実装のversion
    - AWS Provider version
    - 判断時点
    - 採用した推奨設定

16. 参照したversionそのものを利用者への公開契約として露出しない。
17. 公式推奨の変化に伴うdefault値の変更は、公開契約を変えない限り破壊的変更としない（ADR-0005 決定5）。ただし、適用時にresource置換を伴う場合はADR-0005 決定8に従う。

## 検討した代替案

### 案A: 設定値を可能な限り利用者へ公開する

利用者の制御範囲は最大になるが、公開variableが増加し、誤設定経路が増える。ADR-0002・ADR-0004と整合しない。

### 案B: Provider defaultへ委ねる（明示設定しない）

コード量は最小。ただしProviderの都合で挙動が変化し得るため、再現性とsecurity invariantを保証できない。「設定していない」と「推奨値を選んだ」が区別できなくなる。

### 案C: 安全なdefaultを設定しつつ、全項目を上書き可能にする

移行しやすいが、上書き経路が既定の利用形態になった時点で保証が失われる（ADR-0002 案B、ADR-0007と同じ帰結）。

### 評価

| 評価軸 | 案A: 全公開 | 案B: Provider default | 案C: default + 上書き可 | 採用案: 内部固定 |
| --- | --- | --- | --- | --- |
| Security | 低 | 中（不定） | 低 | 高 |
| Reproducibility | 中 | 低 | 中 | 高 |
| Contract Clarity | 低 | 低 | 中 | 高 |
| Cognitive Load | 高 | 低 | 高 | 低 |
| Upgradeability | 低 | 低 | 低 | 高（内部で吸収） |
| Testability | 低 | 中 | 低 | 高 |

## 意図的に捨てるもの

- 利用者が推奨値を上書きできること
- 設定値の判断を利用者へ委ねる選択肢
- Provider defaultに任せることによる実装量の削減

## 保証範囲

- 保証すること: 公開していない設定値が、記録された根拠に基づいて決定されていること
- 保証しないこと: 利用者ごとの個別最適化された設定値

## 検証方法

- Policy Test: security / reliability invariant に関わる設定が、明示的に期待値で設定されていることを検証する（Provider defaultへの暗黙依存を検出する）。
- Contract Test: 公開variableの数と内容をsnapshotとして固定し、「推奨で一意に決まる値」の公開を検出可能にする。
- Unit Test: default値から導出される構成をplanレベルで検証する。
- Static Analysis: 各ユースケースに設定値の根拠記録が存在することを検査する。

## 影響

- 公開variableの追加には、決定10の確認を通過する必要がある。
- 公式推奨の更新は、boilerplate内部のdefault変更として反映される。
- 利用者側の入力は、要件の宣言に近い形になる。

## 見直し条件

- 公式推奨で一意に決まると判断した値について、ユースケース間で要求が分岐した場合
- 公式推奨の変化により、default変更がresource置換を頻繁に伴うようになった場合
