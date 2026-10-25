# ADR-0501: 検証と運用の道具を既存ツールの組み合わせで構成する

- Status: Accepted
- Date: 2026-10-21
- Scope: repository-wide
- Related: ADR-0101, ADR-0202, ADR-0205, ADR-0206, ADR-0301, ADR-0302, ADR-0401, ADR-0402, ADR-0503, ADR-0601, ADR-0603

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

Security は事情が違う。誤設定の規則は道具ごとに独立に書かれており、ある道具に無い規則は他の道具にしか無い（例: S3 の ACL の無効化（BucketOwnerEnforced）は Trivy に無く、Checkov には在る）。1つの道具を権威にすると、その道具の規則集合の欠けが、そのまま保証範囲の欠けになる。

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

4. ADR-0401 決定4 のレイヤーへ、次のとおり道具を割り当てる。各レイヤーの権威を1つとする。ただし Static Analysis は決定5 のとおり範囲で2つに分け、Security は決定6 のとおり複数の道具をそれぞれゲートとして併用する。

   | レイヤー | 道具 | 実 AWS |
   | --- | --- | --- |
   | Syntax | `terraform fmt` / `terraform validate` | 不要 |
   | Static Analysis | TFLint（AWS Provider 利用時は tflint-ruleset-aws を併用）。本リポジトリのソースの構造の規約は Conftest / OPA（入力は HCL のソース） | 不要 |
   | Security | Trivy / Checkov / KICS / Semgrep CE（決定6） | 不要 |
   | Unit | `terraform test`（mock provider。`command = plan` を基本とし、plan で値が決まらない値の検査に限って `command = apply` を用いる） | 不要 |
   | Contract | `terraform test` | 不要 |
   | Policy | Conftest / OPA（入力は `terraform show -json`） | 不要 |
   | Integration | `terraform test`（実 provider） | 必要 |
   | E2E | Terratest（Go） | 必要 |
   | Cost | Infracost | 不要 |
   | Drift | `terraform plan -refresh-only` | 必要 |

5. TFLint は `terraform validate` の代替ではなく補完として置く。

   Static Analysis の権威を範囲で分ける。TFLint は一般的な誤設定と書式外の規約を持つ。Conftest は、本リポジトリの
   ソースの構造の規約を持つ —— ADR-0102 決定14・17・18 の層の規則と、backend 設定の不変条件。同じ規約を両方に
   書かない。この用途の Conftest の入力は HCL のソース（`--parser hcl2`）とし、Policy 層の入力（決定8、ADR-0402
   決定11）とは分ける。これらの規約は `terraform show -json` の出力に現れないか（backend 設定）、plan した root から
   届く module に検査が限られるためである。Conftest は Policy 層で既に用いており、道具は増えない。
6. Security は、**規則の出どころが異なる道具を併用し、そのすべてをゲートとする。** 対象が同じでも、規則を書いた主体が違えば別の道具として入れる。

   | 道具 | 規則の出どころ |
   | --- | --- |
   | Trivy | Aqua Security（trivy-checks） |
   | Checkov | Prisma Cloud（Bridgecrew） |
   | KICS | Checkmarx |
   | Semgrep CE | semgrep-rules（リポジトリに置かず、実行時に取得する） |

   各道具のゲートの閾値は検査の定義（`.makefiles/`）が持つ（決定14）。同じ誤設定を複数の道具が報告した場合、どれか1つを権威として他を黙らせない。値を直すか、報告した道具それぞれに決定13 の抑止を置く。

   道具を足すかどうかの基準は、規則の出どころが既存のどれとも違うことである。出どころが同じ道具（同じ規則集合を取り込んだもの）は足さない。

   <!-- sample:begin -->
   SonarCloud は、決定4 の表のどの層も持たない保守性の観点（重複・複雑度）を報告するために、権威を持たない補助として併用する。必須検査にせず、quality gate の結果で Pull Request を止めない。所見が Security の道具（決定6）・TFLint と重なる場合、判定は決定4 の表の道具に従う。SonarCloud は外部の資格情報を要するので、派生リポジトリが外せる手段を運用機構として持つ。
   <!-- sample:end -->
7. Static Analysis / Security / Unit / Contract / Policy は AWS credentials なしで完了すること（ADR-0401 決定6）。Unit Test では `mock_provider` / `mock_resource` / `mock_data` を用い、実 AWS API へ依存しない。
8. Policy Test の入力を `terraform show -json` の出力とする。これにより ADR-0401 決定19 の要件を満たす。
9. Unit Test と Policy Test を混同しない。入力から構成を導出するロジックの検証（既定値、条件分岐、`for_each`、ARN 組み立て）は Unit、アーキテクチャ invariant の強制（public access 禁止、wildcard 禁止、暗号化必須）は Policy とする。同一の性質を両方へ書かない。
10. 実システムの振る舞いの検証は、`terraform test` で表現できないものに限り Terratest へ移す。Terraform 内部の構造検証のために Terratest を使用しない。

### テストの配置

11. テストは対象の配下の `tests/` へ置く。usecase、一般 module、`internal/` の module のいずれも同じとする（ADR-0102 決定12）。公開契約を持つのは usecase だけなので（ADR-0102 決定13）、Contract Test は usecase の `tests/` に置く。

    ```text
    usecases/<use-case>/
    ├── main.tf
    ├── variables.tf
    ├── outputs.tf
    ├── internal/<module>/
    │   └── tests/
    └── tests/
        ├── defaults.tftest.hcl
        ├── validation.tftest.hcl
        ├── contract.tftest.hcl
        └── behavior.tftest.hcl
    modules/<module>/
    └── tests/
    ```

12. 実 AWS に resource を作成するテストを、作成しないテストと同一ファイルへ混在させない。

### 抑止

13. 検査結果の抑止（TFLint / Trivy / Checkov / KICS / Semgrep / Conftest / Terraform の warning）は、次をすべて満たす場合に限る。

    - 抑止の単位を、ルールやスキャナ単位ではなく、検出 ID と対象パスの組とする
    - 各エントリに「なぜ許容できるか」を記述する。理由を書けないものは抑止せず、値そのものを直す
    - 条件が変われば削除する。恒久 allowlist にしない
    - 道具が検出 ID と対象パスの組を外部の設定ファイルで表せない場合は、対象のファイルか resource に付ける inline の抑止を用いる。inline の抑止も検出 ID を名指し、理由と撤回条件を同じ箇所に書く。ID を取らない形（パスだけ、ブロックだけを外すもの）と、ID も対象も読めない形（結果のハッシュ）は用いない
    - 同じ誤設定を複数の道具が報告し、値を直さずに許容する場合は、報告した道具それぞれに抑止を置き、理由は同じものとする。理由の正本を1箇所（use-case ADR か module の `README.md`）に置き、各抑止はそこを指す

14. 検査を無条件に skip しない。テストや開発を容易にする目的で、安全側の設定（public access の遮断、暗号化、IAM の限定、security group の限定、`validation`）を弱めない。必要な場合は設計変更として扱う（ADR-0102 決定5）。

    CI と道具構成の単純さ —— 道具の数、job の数、実行時間、設定の短さ —— を理由に、検査を減らしたり、ゲートの閾値を緩めたりしない。閾値の値は検査の定義（`.makefiles/`）が持ち、本 ADR は持たない。閾値を緩める変更は本決定の変更として扱う。必須検査に何を入れるかの基準は ADR-0603 決定9-11 が持つ。権威を1つにするための重複の排除（決定4・5）は本項の対象外である。Security の道具（決定6）を減らすことは、本項の対象に含む。

### リファクタリング

15. resource / module の rename・移動に伴う `moved` block の候補生成に tfautomv を利用してよい。候補の採否は人が判断する。ADR-0202 決定6 が要求する移行手段の提供責任は、道具へ移らない。

### 依存の更新

16. Terraform 本体・provider・GitHub Actions・ツール pin の更新を Renovate で継続的に提案する。
17. Renovate の terraform module manager は無効化する。ADR-0205 決定1-3 により外部 module への依存が存在せず、有効なまま置くと常に0件の検査になる。
18. 更新 PR を自動 merge しない。ADR-0601 決定2 の検証を通すことを条件とする。

### version の固定と実行

19. 本 ADR が挙げる道具の version を、リポジトリ直下の単一 manifest で pin する。pin されていない道具を CI の必須検査に用いない。

    ただし、上流がコンテナイメージとして配布し、ADR-0503 決定4 によりそのイメージで実行する道具は manifest に載せない。その版と digest の置き場は ADR-0503 決定10 が持つ。Semgrep CE のルール集合は、semgrep-rules の commit をその検査の定義（`.makefiles/security/semgrep.mk`）で固定し、リポジトリへ置かずに実行時に取得する。[Semgrep Rules License v1.0](https://semgrep.dev/legal/rules-license)がルールの配布を許さないためである。
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

31. 初期導入を Terraform CLI / TFLint / `terraform test` / Trivy / Checkov / KICS / Semgrep CE / Renovate / Terraform MCP Server とする。
32. 次段階を Conftest（OPA）/ Infracost / tfautomv / Terratest / ローカル hook とする。
33. terraform-docs / Rover / Inframap は必要が生じた時点で導入する。未使用の道具を pin だけ置かない。

## 検討した代替案

### 案A: Terraform 本体のみを用い、不足する検査を自前実装する

依存する外部ツールが無くなり、検査の内容を完全に所有できる。ただし Static Analysis / Security / Policy / Cost / Drift の各層に独自実装を持つことになり、ADR-0101 が優先する対象と無関係な保守負債が発生する。security ルールの網羅性は、専門ツールのルールセットに追随できない。

### 案B: すべてのレイヤーで重複を許容して道具を並べる

取りこぼしは減るが、Static Analysis・Unit・Policy のように本リポジトリが規則を書く層では、同じ規則を2つの記法で保守することになり、どちらが正かが決まらない。ADR-0401 決定2 の「故障モードごとに検出手段を割り当てる」構造が崩れる。**Security だけはこの欠点を持たない** —— 規則を書くのは外部であり、本リポジトリが保守するのは抑止だけである。採用案は Security に限って重複を許容する。

### 案C: マネージドプラットフォームへ検証と実行を寄せる

Terraform Cloud / Spacelift 等が提供する検証と実行に統合する。配線コストは最小になるが、検証の構成がプラットフォームの機能セットに従属し、ADR-0401 のレイヤー定義を維持できるかがベンダー側の都合で決まる。ローカルと CI で同一の検査を再現する手段も、プラットフォームの提供範囲に依存する。

### 案D: Security も1つの道具を権威とする

抑止が1箇所に集まり、所見の数も最小になる。ただし保証範囲がその道具の規則集合の欠けをそのまま引き継ぐ。Trivy は S3 の ACL の無効化を検査しない。規則を足すにはカスタムルールを自前で保守することになり、決定1・3 に反する。

### 評価

| 評価軸 | 案A: 自前実装 | 案B: 全層で重複許容 | 案C: プラットフォーム | 案D: Security も1権威 | 採用案: 1層1権威、Security は併用 |
| --- | --- | --- | --- | --- | --- |
| Security | 低（網羅性を追随できない） | 中（抑止が分散する） | 中（提供範囲に従属） | 中（1つの規則集合の欠けを引き継ぐ） | 高 |
| Testability | 中 | 中 | 中 | 高 | 高（canary で各ゲートが落ちうることを示す） |
| Contract Clarity | 中 | 低（権威が不定） | 中 | 高 | 中（Security は道具ごとに判定し、抑止も道具ごと） |
| Scope Control | 低（実装資産が増える） | 低（道具が増え続ける） | 中 | 高 | 中（Security の道具は出どころの基準でだけ増える） |
| Maintainability | 低 | 低 | 中（外部都合で変動） | 中 | 中（同じ理由の抑止が道具の数だけ要る） |
| Reproducibility | 中 | 中 | 低（手元で再現しにくい） | 高 | 高（pin + digest + bare 実行） |
| 実装コスト | 高 | 低 | 低 | 低 | 中 |

## 意図的に捨てるもの

- 道具の総数を最小にすること
- CI と道具構成の単純さ（決定14）
- Security 以外のレイヤーで、検査の重複による「取りこぼしの保険」
- Security の所見について、どの道具が正かを1つに決めること
- 同じ誤設定の抑止が1箇所で済むこと
- 単一ベンダーに統合された実行体験
- 検査内容を完全に所有すること

## 保証範囲

- 保証すること: ADR-0401 の各レイヤーに実行可能な道具が割り当てられ、各範囲の権威が1つであること（Static Analysis は決定5 の範囲で2つ、Security は決定6 の道具がそれぞれゲートであること）。抑止が理由とともに記録されていること
- 保証しないこと: 各道具が検出する範囲の網羅性と、Security の道具の和集合の網羅性。道具が報告しない誤りが存在しないこと

## 検証方法

- Static Analysis: 決定4 の各レイヤーに対応する CI ジョブが存在すること
- Static Analysis: 決定5 の構造の規約の Rego が、違反する入力と違反しない入力の両方のテストを持つこと（ADR-0402 決定8 と同じ規律）
- Static Analysis: 抑止エントリが、検出 ID と対象の組かつ理由付きであること（決定13）
- Static Analysis: CI の必須検査で用いる道具が、すべて manifest に pin されているか、イメージの digest が lockfile に固定されていること（決定19）
- Static Analysis: 決定6 の各道具に、本走査と同じ条件で落ちることを示す canary があること
- Static Analysis: 決定6 の各道具のジョブが required context として宣言されていること
- Static Analysis: `<manager> exec` を含む recipe / hook 定義が存在しないこと（決定20）
- Static Analysis: Renovate 設定で terraform module manager が無効化されていること（決定17）
- レビュー: 決定28 の実行結果を Pull Request に記録すること
- レビュー: 検査の削除または閾値の緩和を含む Pull Request が、本 ADR の変更を伴うこと（決定14）

## 影響

- 道具の追加は本 ADR の更新を伴う。決定4 の表が、どの検査を誰が持つかの唯一の記述になる。
- 検出結果の抑止が、道具ごとの設定ファイルか、決定13 の inline の形で、理由付きで残る。Security の同じ誤設定の抑止は、道具の数だけ置かれる。
- 実 AWS を必要とする検証は Integration / E2E に限られ、それ以外は credentials なしで完結する。

## 見直し条件

- `terraform test` が Policy Test 相当の表明を、外部 policy engine なしで表現できるようになった場合
- 割り当てた道具のいずれかが保守されなくなった場合
- Security 以外の同一レイヤーに2つ目の道具が必要という判断が反復して発生した場合
- Security の道具のいずれかが、他の道具と同じ規則集合を取り込むようになった場合（出どころの基準を満たさなくなる）
- Security の道具のいずれかがライセンスを変え、本リポジトリでの利用条件を満たさなくなった場合
