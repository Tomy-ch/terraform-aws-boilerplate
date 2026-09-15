# ADR-0001: ADRの採用とADR配置・所有権ポリシー

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0002, ADR-0003, ADR-0005

## Context

本リポジトリは、限定されたAWSアーキテクチャをTerraformへエンコードする。Terraformコードは「何を作るか」は表現できるが、「なぜその構成にしたか」「どの自由度を意図的に捨てたか」「何を保証しないと決めたか」は表現できない。

これらが記録されないと、次の変更時に同じ議論を再実行するか、前提を無自覚に破壊する。特に本リポジトリは自由度を制限することで保証範囲を確保する戦略を採るため（ADR-0002）、「制限した理由」の消失は設計そのものの消失に等しい。

また、設計判断には適用範囲の差がある。

- リポジトリ全体で維持すべき不変条件
- 特定ユースケース内部でのみ成立する判断

これらを同一の場所・同一の番号体系で管理すると、次の問題が発生する。

- 局所的な判断が全体ルールとして誤って参照される
- 全体ルールが特定moduleの実装詳細へ引きずられる
- ユースケースを削除しても、対応する設計判断が残留する
- 実装を読む際に、関連する設計判断へ到達できない

さらに、ADRの主語をAWS resourceに置くと、Provider側の都合（resource分割・統合・置換）でADR体系そのものが揺れる。

## Decision

### ADRの採用

1. 設計判断はADRとして記録する。コード、コードコメント、PR説明のみで設計判断を伝達しない。
2. ADRはimmutableとして扱う。内容の変更は、既存ADRの書き換えではなく新規ADRによるsupersedeで行う。誤字修正および参照リンク修正はこの限りではない。
3. Statusは `Proposed` / `Accepted` / `Superseded by <scope>/ADR-NNNN` / `Deprecated` のいずれかとする。
4. ADR番号は採番後に再利用しない。欠番を許容する。

### 配置

5. ADRは適用範囲に応じて2階層で管理する。

   - repository-wide ADR: `docs/adr/`
   - use-case-local ADR: `modules/<use-case>/docs/adr/`

6. 原則は以下とする。

   > Repository-wide decisions are stored at the repository level.
   > Use-case-specific decisions are colocated with the owning use case.

7. ADRの配置場所そのものが、その設計判断の所有範囲を表す。配置はADRレビューの対象とする。

### 所有権と主語

8. ADRの主語はAWS resourceではなく、architecture invariantまたはuse caseとする。「Security Groupをどう作るか」ではなく「当該ユースケースのネットワーク境界をどう保証するか」を主題とする。
9. 特定resourceに関する判断であっても、そのresourceが特定ユースケースの内部実装である場合、ADRは当該ユースケース配下へ置く。resource単位のroot ADRを新設しない。

### 番号体系

10. 番号体系はscopeごとに独立させる。リポジトリ全体で一意である必要はない。
11. ADRのidentityは `<scope>/ADR-<number>` とする。`repository-wide/ADR-0003` と `ecs-web-service/ADR-0003` は別のADRである。
12. ファイル名は `NNNN-kebab-case-title.md` とする。

### 依存方向

13. 依存方向は root ADR → use-case ADR とする。
14. use-case ADRはroot ADRを前提条件として参照してよい。
15. root ADRから特定ユースケースの実装詳細へ依存しない。root ADRが例示として特定ユースケースに触れる場合も、その存在を前提としない記述とする。

### 昇格

16. use-case ADRとして始まった判断は、以下のいずれかを満たす場合にroot ADRへの昇格を検討する。

    - 2つ以上のユースケースへ同一原則が適用される
    - セキュリティ上のrepository-wide invariantである
    - module間の公開契約に影響する
    - リポジトリ全体のtest policyへ影響する
    - 今後追加するユースケースでも原則として維持すべき
    - 各ユースケースで個別判断させること自体がリスクになる

17. 以下は昇格の理由としない。

    - 偶然同じAWS resourceを利用している
    - 現時点で実装が似ている
    - Providerのdefaultが同じ
    - コード重複を減らしたい
    - 将来共通化できるかもしれない

18. 昇格の根拠は「同じresourceを利用しているから」ではなく「同じarchitecture invariantを共有しているから」とする。ADRの共通化はコードのDRYとは別問題として扱う。
19. 昇格時は、元のuse-case ADRを `Superseded by repository-wide/ADR-NNNN` とし、root ADRからは元ADRを参照しない（依存方向の維持）。

### レビュー

20. ADR追加・変更時は、内容に加えて以下を確認する。

    1. この判断はリポジトリ全体へ適用されるか。
    2. 特定ユースケースのみの判断ではないか。
    3. AWS resourceそのものを主語にしていないか。
    4. より上位のarchitecture invariantとして表現できないか。
    5. 他ユースケースへ強制する必要が本当にあるか。
    6. 同じ判断が既にroot ADRとして存在しないか。
    7. use-case ADRで十分なのにrootへ昇格させていないか。
    8. 複数ユースケースに共通する不変条件なのに各所へ重複していないか。

## 検討した代替案

### 案A: すべてのADRをリポジトリ直下へ集約する

一覧性と番号の一意性は得られるが、ユースケース実装からの到達性が悪く、module削除時に残留ADRが発生する。root ADRが特定moduleの実装詳細で肥大化する。

### 案B: ADRを作らず、README・コードコメント・PR説明で代替する

記録コストは最小だが、「意図的に捨てたもの」が残らない。本リポジトリの中心戦略（自由度の制限）を次の変更者が復元できない。

### 案C: AWS resource単位でADRを管理する

Provider側の変更（resource分割・統合・置換・新resource登場）でADR体系が直接揺れる。設計意図とresource実装の寿命が一致しない。

### 評価

| 評価軸 | 案A: root集約 | 案B: 記録しない | 案C: resource単位 | 採用案: 2階層 + use-case主語 |
| --- | --- | --- | --- | --- |
| Contract Clarity | 中（適用範囲が曖昧） | 低 | 低（契約とresourceが混同） | 高（配置が適用範囲を表す） |
| Maintainability | 低（root肥大化） | 低 | 低（Provider変更で崩れる） | 高 |
| Scope Control | 低（昇格が既定値化） | 低 | 中 | 高（昇格条件を明示） |
| Cognitive Load | 中（全読が必要） | 低（読む物がない／判断は再現不能） | 高 | 低（local reasoning可能） |
| Testability | 中 | 低 | 中 | 中（配置規約を静的検査可能） |

## 意図的に捨てるもの

- リポジトリ全体でのADR番号の一意性
- 単一ディレクトリを読めば全設計判断が把握できる一覧性
- ADRの物理的な重複排除（同一invariantがroot昇格前に複数箇所へ現れる期間を許容する）

## 保証範囲

- 保証すること: すべてのrepository-wide invariantが `docs/adr/` に存在すること
- 保証しないこと: use-case ADRが他ユースケースへ自動的に適用されること

## 検証方法

以下を静的検査の対象とする（ADR-0012の Static Analysis レイヤー）。

- ファイル名が `NNNN-kebab-case-title.md` に適合すること
- 同一scope内で番号が重複しないこと
- 先頭メタデータに Status / Date / Scope が存在すること
- Status が `Superseded by` の場合、参照先ADRが存在すること
- root ADR本文が `modules/<use-case>/` 配下のpathへ規範的に依存していないこと

## 影響

- 新規ユースケース追加時は `modules/<use-case>/docs/adr/` を同時に作成する。
- ユースケース削除時は配下のADRも削除する。昇格済みの判断はroot側に残る。
- ADRのownershipはコードのownershipと一致する。

## 見直し条件

- use-case数が増え、root ADRの前提条件参照が実務上追えなくなった場合
- `modules/<use-case>/` というディレクトリ構造自体を変更する場合
- 複数リポジトリへ設計判断を共有する必要が生じた場合
