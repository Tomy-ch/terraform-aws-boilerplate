# ADR-0601: 変更経路を Git に限定し、apply を merge 後の CI/CD で行う

- Status: Accepted
- Date: 2026-09-16
- Scope: repository-wide
- Related: ADR-0202, ADR-0301, ADR-0401, ADR-0501

## Context

Terraform は、誰がどこから実行しても同じ state を書き換えられる。実行経路を決めないと次が起きる。

- ローカルからの apply と CI からの apply が競合する
- 適用されている構成に対応する commit が特定できない
- レビューを経ていない構成が実環境に存在する
- Git と state と AWS の三者が互いに食い違い、どれが正かを判定できない

ADR-0401 は検証レイヤーを定義したが、検証がどの時点で実行され、何が通ったときに実環境へ反映されるかは決めていない。ADR-0202 は公開契約の安定性を保証するが、その保証は「適用される構成が Git 上の構成と一致する」ことを前提にしている。前提が成立しなければ、契約の保証は実環境に対して意味を持たない。

また、AWS Console や外部システムによる Git 外の変更は、リポジトリ側の設計では防止できない。検出できるかどうかだけが設計の対象になる。

## Decision

### Desired State

1. インフラの正となる状態を、Git リポジトリ上の Terraform コードとする。
2. 基本の流れを次とする。

   ```text
   Pull Request
       ↓
   Static Analysis / Security / Unit / Contract / Policy
       ↓
   terraform plan
       ↓
   Review
       ↓
   Merge
       ↓
   terraform apply
   ```

3. Pull Request で実行するのは `terraform plan` までとする。Pull Request の時点で apply しない。

### 実行の主体

4. 保護ブランチへの merge 後、CI/CD が apply を行う。
5. 人がローカル環境から本番環境へ直接 apply しない。
6. apply 対象の Terraform コードは、merge された commit と一致しなければならない。plan を取得した commit と apply する commit が異なる経路を作らない。
7. 本番相当の環境への適用に、承認を要する機構（environment protection 等）を用いてよい。

### 検証との関係

8. Pull Request 契機で実行するレイヤーは ADR-0401 決定8 に従う。本 ADR はそこへ `terraform plan` と Infracost（ADR-0501 決定24）を加える。
9. AWS credentials を要する検証を、要さない検証より先に実行しない。credentials の取得失敗によって ADR-0401 決定6 のレイヤーが巻き添えで失敗する順序を採らない。
10. 検証が失敗した Pull Request を merge しない。merge 後に apply が起動する以上、merge の可否が実環境への適用の可否と同義になる。

### Drift

11. Drift Detection を deployment 機構の代替として使用しない。通常の変更反映は Git → CI/CD → AWS の経路のみで行う。
12. Drift Detection を次の検出に用いる。

    - AWS Console からの手動変更
    - Terraform 管理外の変更
    - import 漏れ
    - 外部システムによる変更
    - AWS 側で変更された属性

13. drift の解消も Git を経由する。Git 側を実態へ合わせるか、実態を Git の宣言へ戻すかのいずれかを Pull Request として提出する。検出箇所へ直接 apply して差分を黙らせない。
14. drift の自動修復を行わない。検出は通知であり、適用の起動条件としない。

### 本 ADR で決定しない事項

15. state backend の選定、state の分割単位、環境（dev / staging / production 等）の表現方法は本 ADR の対象外とし、別 ADR で決定する。
16. 本 ADR が前提とするのは、apply が排他制御（lock）のもとで実行されることのみとする。この前提を満たさない backend を採用する場合、決定6 の保証は成立しない。

## 検討した代替案

### 案A: ローカルからの apply を許容する

緊急時の反映が速く、CI の配線に依存しない。ただし適用された構成に対応する commit が特定できず、レビューを経ない構成が実環境に存在し得る。ADR-0202 の公開契約の保証が、実環境に対して意味を持たなくなる。

### 案B: Pull Request 上で apply まで行い、merge を事後手続きとする

実環境での結果を merge 前に確認できる。ただし main に存在しない構成が実環境へ適用され、Git が Desired State である前提が崩れる。Pull Request を閉じた場合に実環境へ何が残るかが定義できない。

### 案C: drift を検出したら自動的に修復する

Git と実態の乖離が自動的に収束する。ただし drift の原因（意図的な緊急対応か、外部システムの正当な変更か、攻撃か）を判定せずに上書きすることになり、インシデント対応中の変更を巻き戻し得る。検出と適用を分離しないため、Drift Detection が deployment 機構になる。

### 評価

| 評価軸 | 案A: ローカル apply | 案B: merge 前 apply | 案C: 自動修復 | 採用案: merge 後 apply |
| --- | --- | --- | --- | --- |
| Security | 低（未レビュー構成が通る） | 中 | 低（判定なしの上書き） | 高 |
| Testability | 低（検証の通過が前提でない） | 中 | 中 | 高（merge が検証通過と同義） |
| Contract Clarity | 低（適用 commit が不定） | 低（Git が正でなくなる） | 中 | 高 |
| Scope Control | 低 | 中 | 低（Drift が deployment 化） | 高 |
| Reproducibility | 低 | 中 | 中 | 高 |
| 反映の即時性 | 高 | 高 | 高 | 低（意図的） |

## 意図的に捨てるもの

- 緊急時にローカルから即座に適用できること
- merge 前に実環境での適用結果を確認できること
- AWS 側で行われた手動変更がそのまま維持されること
- drift の自動的な収束

## 保証範囲

- 保証すること: 実環境へ適用された構成が、保護ブランチ上の commit に対応すること。適用が検証を通過した構成であること
- 保証しないこと: Git 外で行われた変更が発生しないこと（検出のみを保証する）。検出から解消までの間、実態が Git と一致していること

## 検証方法

- Static Analysis: apply を行う workflow の契機が、保護ブランチへの push に限定されていること
- Static Analysis: Pull Request 契機の workflow に apply が含まれないこと
- Static Analysis: 検証ジョブの実行順序が決定9 を満たすこと
- 定期実行: drift 検出ジョブが存在し、結果が観測可能な形で残ること

## 影響

- 反映の最短時間は、レビューと merge を含む時間になる。
- 実環境で先に手を入れて後から Git へ写す運用は、drift として検出され、Pull Request として処理される。
- state backend を決める ADR が別途必要になる（決定15）。

## 見直し条件

- 検証の所要時間が、インシデント対応で許容できる時間を超えた場合
- 環境の数が増え、単一の apply 経路では表現できなくなった場合
- state backend の決定が、決定16 の前提（排他制御）を満たせない方向へ動いた場合
