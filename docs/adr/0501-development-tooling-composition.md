# ADR-0501: 検証と運用の道具を既存ツールの組み合わせで構成する

- Status: Accepted
- Date: 2026-09-16
- Scope: repository-wide
- Related: ADR-0101, ADR-0202, ADR-0205, ADR-0206, ADR-0301, ADR-0401, ADR-0601

## Context

ADR-0401 はテストレイヤーと検出すべき故障モードを定義したが、決定19 で実装形式を意図的に固定していない。実装形式が決まらない限り、各レイヤーは「誰が検査するのか」を持たず、決定として不完全な状態が残る（ADR-0401 決定3）。

Terraform 本体が提供するのは `fmt` / `validate` / `test` / `plan` / `show` であり、次はいずれも持たない。

- Provider 固有の誤設定検出
- security misconfiguration の検出
- リポジトリ固有の architecture policy の強制
- コスト影響の可視化
- resource の rename / move に伴う state 移行候補の生成
- 依存 version の継続的な更新
- 実システムの振る舞いの検証

これらを自前実装で埋めると、ADR-0101 が優先する対象（安全性・再現性・テスト可能性）とは無関係な実装資産を継続保守することになる。一方、道具を無方針に並べると同一の故障モードを複数の道具が報告し、どの道具がそのレイヤーの権威なのかが決まらない。ADR-0401 決定2 は、裏返せば「1つの故障モードに1つの権威」を要求する。

さらに、AWS Provider の schema は version によって変化する。argument を記憶から推測すると、実行するまで検出されない誤りになる。

## Decision

### 原則

1. Terraform 本体だけで品質保証・保守・セキュリティ・テスト・リファクタリング・依存管理を実現しようとしない。成熟した既存ツールを組み合わせる。
2. Terraform CLI が提供する機能を外部ツールで再実装しない。
3. 独自ツールの実装は、次の3つをすべて満たす場合に限り検討する。

   - 既存ツールで実現できない
   - 本リポジトリ固有の価値がある
   - 継続的に利用される

### レイヤーと道具の割当

4. ADR-0401 決定4 のレイヤーへ、次のとおり道具を割り当てる。各レイヤーの権威を1つとする。

   | レイヤー | 道具 | 実 AWS |
   | --- | --- | --- |
   | Syntax | `terraform fmt` / `terraform validate` | 不要 |
   | Static Analysis | TFLint（AWS Provider 利用時は tflint-ruleset-aws を併用） | 不要 |
   | Security | Trivy | 不要 |
   | Unit | `terraform test`（`command = plan` + mock provider） | 不要 |
   | Contract | `terraform test` | 不要 |
   | Policy | Conftest / OPA（入力は `terraform show -json`） | 不要 |
   | Integration | `terraform test`（実 provider） | 必要 |
   | E2E | Terratest（Go） | 必要 |
   | Cost | Infracost | 不要 |
   | Drift | `terraform plan -refresh-only` | 必要 |

5. TFLint は `terraform validate` の代替ではなく補完として置く。
6. Security scan の第一候補を Trivy とする。Checkov は、Trivy に存在しないルールが必要な場合に限り併用する。Trivy と重複する検査を無目的に二重化しない。
7. Static Analysis / Security / Unit / Contract / Policy は AWS credentials なしで完了すること（ADR-0401 決定6）。Unit Test では `mock_provider` / `mock_resource` / `mock_data` を用い、実 AWS API へ依存しない。
8. Policy Test の入力を `terraform show -json` の出力とする。これにより ADR-0401 決定19 の要件を満たす。
9. Unit Test と Policy Test を混同しない。入力から構成を導出するロジックの検証（既定値、条件分岐、`for_each`、ARN 組み立て）は Unit、アーキテクチャ invariant の強制（public access 禁止、wildcard 禁止、暗号化必須）は Policy とする。同一の性質を両方へ書かない。
10. 実システムの振る舞いの検証は、`terraform test` で表現できないものに限り Terratest へ移す。Terraform 内部の構造検証のために Terratest を使用しない。

### テストの配置

11. module のテストは当該 module 配下へ置く。

    ```text
    modules/<use-case>/
    ├── main.tf
    ├── variables.tf
    ├── outputs.tf
    └── tests/
        ├── defaults.tftest.hcl
        ├── validation.tftest.hcl
        ├── contract.tftest.hcl
        └── behavior.tftest.hcl
    ```

12. 実 AWS に resource を作成するテストを、作成しないテストと同一ファイルへ混在させない。

### 抑止

13. 検査結果の抑止（TFLint / Trivy / Checkov / Conftest / Terraform の warning）は、次をすべて満たす場合に限る。

    - 抑止の単位を、ルールやスキャナ単位ではなく、検出 ID と対象パスの組とする
    - 各エントリに「なぜ許容できるか」を記述する。理由を書けないものは抑止せず、値そのものを直す
    - 条件が変われば削除する。恒久 allowlist にしない

14. 検査を無条件に skip しない。テストや開発を容易にする目的で、安全側の設定（public access の遮断、暗号化、IAM の限定、security group の限定、`validation`）を弱めない。必要な場合は設計変更として扱う（ADR-0102 決定5）。

### リファクタリング

15. resource / module の rename・移動に伴う `moved` block の候補生成に tfautomv を利用してよい。候補の採否は人が判断する。ADR-0202 決定6 が要求する移行手段の提供責任は、道具へ移らない。

### 依存の更新

16. Terraform 本体・provider・GitHub Actions・ツール pin の更新を Renovate で継続的に提案する。
17. Renovate の terraform module manager は無効化する。ADR-0205 決定1-3 により外部 module への依存が存在せず、有効なまま置くと常に0件の検査になる。
18. 更新 PR を自動 merge しない。ADR-0601 決定2 の検証を通すことを条件とする。

### version の固定と実行

19. 本 ADR が挙げる道具の version を、リポジトリ直下の単一 manifest で pin する。pin されていない道具を CI の必須検査に用いない。
20. 道具はそのまま（bare）実行する。version manager のサブコマンドで包む形（`<manager> exec -- <command>`）を、手元・hook・CI recipe のいずれでも用いない。包みが必要に見える場合の対処は `PATH` の修正であり、wrapper の追加ではない。

### Provider 仕様の参照

21. AWS Provider の resource / data source の argument を記憶から推測しない。不明な点は次の順で現在の仕様を確認する。

    1. Terraform MCP Server
    2. Terraform Registry
    3. Provider Documentation

22. 本決定は参照手段を定めるものであり、ADR-0206 決定7・8 が課す記録義務を置き換えない。

### CI ゲートにしない道具

23. 次は開発補助とし、CI の必須検査に含めない。

    - terraform-ls（エディタ連携）
    - `terraform console`（expression の評価）
    - Rover / Inframap / `terraform graph`（可視化）
    - terraform-docs（ドキュメント生成）

24. Infracost はコスト差分の可視化に用い、deployment 拒否の絶対条件としない。意図しない高コスト resource の検出とレビュー支援として扱う。
25. terraform-docs が生成するドキュメントは、本リポジトリの品質保証機構ではない。生成可能なドキュメントを手作業で重複管理しない。
26. ローカルの git hook は fast feedback のために置き、CI の代替としない。強制は CI が持つ（ADR-0601）。
27. 複雑な expression を推測だけで実装しない。`terraform console` で評価する。

### 変更時の必須実行

28. Terraform コードを変更したあと、最低限次を実行する。

    - `terraform fmt`
    - `terraform validate`
    - TFLint
    - `terraform test`

    security / policy の道具が配線されている場合、それらも実行する。

29. 既存 resource を変更する場合、変更の種類に対応するテスト層（Unit / Contract / Policy / Integration）を先に追加または更新する。
30. Unit Test で確認できる内容について、実 AWS へ resource を作成しない。優先順位を mock → plan → 実 provider → 実 AWS とする。

### 導入順序

31. 初期導入を Terraform CLI / TFLint / `terraform test` / Trivy / Renovate / Terraform MCP Server とする。
32. 次段階を Conftest（OPA）/ Infracost / tfautomv / Terratest / ローカル hook とする。
33. Checkov / terraform-docs / Rover / Inframap は必要が生じた時点で導入する。未使用の道具を pin だけ置かない。

## 検討した代替案

### 案A: Terraform 本体のみを用い、不足する検査を自前実装する

依存する外部ツールが無くなり、検査の内容を完全に所有できる。ただし Static Analysis / Security / Policy / Cost / Drift の各層に独自実装を持つことになり、ADR-0101 が優先する対象と無関係な保守負債が発生する。security ルールの網羅性は、専門ツールのルールセットに追随できない。

### 案B: 重複を許容して道具を並べる

Trivy と Checkov と TFLint の security ルールを同時に有効化する等。取りこぼしは減るが、同一の故障モードを複数の道具が別の ID で報告するため、抑止が道具ごとに分散し、どれが権威かが決まらない。ADR-0401 決定2 の「故障モードごとに検出手段を割り当てる」構造が崩れる。

### 案C: マネージドプラットフォームへ検証と実行を寄せる

Terraform Cloud / Spacelift 等が提供する検証と実行に統合する。配線コストは最小になるが、検証の構成がプラットフォームの機能セットに従属し、ADR-0401 のレイヤー定義を維持できるかがベンダー側の都合で決まる。ローカルと CI で同一の検査を再現する手段も、プラットフォームの提供範囲に依存する。

### 評価

| 評価軸 | 案A: 自前実装 | 案B: 重複許容 | 案C: プラットフォーム | 採用案: 既存ツールを1層1つ |
| --- | --- | --- | --- | --- |
| Security | 低（網羅性を追随できない） | 中（抑止が分散する） | 中（提供範囲に従属） | 高 |
| Testability | 中 | 中 | 中 | 高（層と権威が1:1） |
| Contract Clarity | 中 | 低（権威が不定） | 中 | 高 |
| Scope Control | 低（実装資産が増える） | 低（道具が増え続ける） | 中 | 高（導入順序を規定） |
| Maintainability | 低 | 低 | 中（外部都合で変動） | 中 |
| Reproducibility | 中 | 中 | 低（手元で再現しにくい） | 高（pin + bare 実行） |
| 実装コスト | 高 | 低 | 低 | 中 |

## 意図的に捨てるもの

- 道具の総数を最小にすること
- 検査の重複による「取りこぼしの保険」
- 単一ベンダーに統合された実行体験
- 検査内容を完全に所有すること

## 保証範囲

- 保証すること: ADR-0401 の各レイヤーに、実行可能な道具が1つ割り当てられていること。抑止が理由とともに記録されていること
- 保証しないこと: 各道具が検出する範囲の網羅性。道具が報告しない誤りが存在しないこと

## 検証方法

- Static Analysis: 決定4 の各レイヤーに対応する CI ジョブが存在すること
- Static Analysis: 抑止エントリが、検出 ID と対象の組かつ理由付きであること（決定13）
- Static Analysis: CI の必須検査で用いる道具が、すべて manifest に pin されていること（決定19）
- Static Analysis: `<manager> exec` を含む recipe / hook 定義が存在しないこと（決定20）
- Static Analysis: Renovate 設定で terraform module manager が無効化されていること（決定17）
- レビュー: 決定28 の実行結果を Pull Request に記録すること

## 影響

- 道具の追加は本 ADR の更新を伴う。決定4 の表が、どの検査を誰が持つかの唯一の記述になる。
- 検出結果の抑止が、道具ごとの設定ファイルへ理由付きで残る。
- 実 AWS を必要とする検証は Integration / E2E に限られ、それ以外は credentials なしで完結する。

## 見直し条件

- `terraform test` が Policy Test 相当の表明を、外部 policy engine なしで表現できるようになった場合
- 割り当てた道具のいずれかが保守されなくなった場合
- 同一レイヤーに2つ目の道具が必要という判断が反復して発生した場合
