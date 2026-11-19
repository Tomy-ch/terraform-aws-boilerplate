# ADR-0601: 変更経路を Git に限定し、apply を merge 後の CI/CD で行う

- Status: Accepted
- Date: 2026-10-21
- Scope: repository-wide
- Related: ADR-0102, ADR-0103, ADR-0202, ADR-0301, ADR-0401, ADR-0402, ADR-0501, ADR-0602, ADR-0603, ADR-0604

## Context

Terraform は、誰がどこから実行しても同じ state を書き換えられる。実行経路を決めないと次が起きる。

- ローカルからの apply と CI からの apply が競合する
- 適用されている構成に対応する commit が特定できない
- レビューを経ていない構成が実環境に存在する
- Git と state と AWS の三者が互いに食い違い、どれが正かを判定できない

ADR-0401 は検証レイヤーを定義したが、検証がどの時点で実行され、何が通ったときに実環境へ反映されるかは決めていない。ADR-0202 は公開契約の安定性を保証するが、その保証は「適用される構成が Git 上の構成と一致する」ことを前提にしている。前提が成立しなければ、契約の保証は実環境に対して意味を持たない。

また、AWS Console や外部システムによる Git 外の変更は、リポジトリ側の設計では防止できない。検出できるかどうかだけが設計の対象になる。

アプリケーションの成果物（コンテナイメージ）の版は、インフラの構成とは別の主体が、別の頻度で変える。利用側のリポジトリの CI（発火側）が成果物を作り、マイグレーション等の単発のタスクを実行し、その成功を確かめてから Service を更新する。ECS では、新しいイメージで単発のタスクを実行するには、そのイメージを指す Task 定義の revision が要る。RunTask の override はコンテナのイメージを変えられない（AWS API Reference の `ContainerOverride`）。版の選択まで Git の経路に載せると、発火側は単発のタスクと Service の更新を1つの発火で続けて行えない。

## Decision

### Desired State

1. インフラの正となる状態を、Git リポジトリ上の Terraform コードが宣言する資源とその属性とする。ただし、実行中のアプリケーションの成果物の版の選択は、発火側が所有する外部の状態として除く。除く属性は決定20 の一覧に限る。
2. 基本の流れを次とする。

   ```text
   Pull Request（リリース線へ）
       ↓
   Static Analysis / Security / Unit / Contract / Policy
       ↓
   terraform plan
       ↓
   Review
       ↓
   Merge（リリース線）
       ↓
   Pull Request（リリース線からデプロイブランチへ）
       ↓
   同じ検証と terraform plan、Review
       ↓
   Merge（デプロイブランチ）
       ↓
   terraform apply
   ```

3. Pull Request で実行するのは `terraform plan` までとする。Pull Request の時点で apply しない。例外は決定4 の sandbox への apply だけとする。その apply が当てる木は、リリース線か hotfix の線へ merge され、レビューを経た commit の木であり、そのことを ADR-0603 決定31 の検査（デフォルトブランチからの到達性と木の一致）が保証する。レビューを経ていない構成を Pull Request の時点で当てないという本決定の理由は、この例外でも保たれる。

### 実行の主体

4. env ごとに、固定名の保護ブランチを1つ、その env のデプロイブランチとして置く。デプロイブランチへの merge の後、CI/CD がその env へ apply する。リリース線と hotfix の線は変更を集める線であり、デプロイブランチを持つ env へ apply しない。デプロイブランチは、宣言がそのブランチに名指す線のどれかがデフォルトブランチである間に、デフォルトブランチから到達できる commit を運ぶ Pull Request の merge だけで変更を受け取る（ADR-0603 決定31）。どの env にどのデプロイブランチを置くかは、分岐のパターンの単一宣言（ADR-0603 決定1）が持つ。例外として、検証の器（sandbox）はデプロイブランチを置かない。リリース線か hotfix の線からデプロイブランチへの Pull Request で、CI がその head の木を sandbox へ apply し、その成功をその Pull Request の merge の条件とする（ADR-0603 決定33）。sandbox は、リリースされていない変更を、デプロイブランチへ入る前に実際に apply して確かめる場所だからである。sandbox への apply は次に従う。sandbox の apply を1つの concurrency の group で直列にする。apply する head が、その Pull Request の head の線の先頭でなくなった run は、資格情報を取る前に失敗させる。ADR-0603 決定31 の検査の成功に依存させる。fork と Dependabot の Pull Request では、資格情報を取る前に失敗させる。
5. 人がローカル環境から本番環境へ直接 apply しない。
6. apply 対象の Terraform コードは、merge された commit と一致しなければならない。決定4 の sandbox への apply では、線へ merge された Pull Request の head の commit がそれに当たる。plan を取得した commit と apply する commit が異なる経路を作らない。
7. 本番相当の環境への適用に、承認を要する機構（environment protection 等）を用いてよい。

### 検証との関係

8. Pull Request 契機で実行するレイヤーは ADR-0401 決定8 に従う。本 ADR はそこへ `terraform plan` と Infracost（ADR-0501 決定24）を加える。
9. AWS credentials を要する検証を、要さない検証より先に実行しない。credentials の取得失敗によって ADR-0401 決定6 のレイヤーが巻き添えで失敗する順序を採らない。`terraform` を AWS credentials 付きで実行する job（`terraform plan` を含む）は、ADR-0103 のコード実行の検出の成功に依存させる。
10. 検証が失敗した Pull Request を merge しない。デプロイブランチへの merge は apply を起動するので、その merge の可否は実環境への適用の可否と同義である。リリース線へ merge した commit は、昇格でそのままデプロイブランチへ入る（決定4）ので、リリース線への merge にも同じ検証を課す。

### Drift

11. Drift Detection を deployment 機構の代替として使用しない。通常の変更反映は Git → CI/CD → AWS の経路のみで行う。例外は、決定20 の一覧の属性を発火側が変えることだけとする。
12. Drift Detection を次の検出に用いる。

    - AWS Console からの手動変更
    - Terraform 管理外の変更
    - import 漏れ
    - 外部システムによる変更
    - AWS 側で変更された属性

    決定20 の一覧の属性の変化は発火側の正規の変更であり、検出の対象としない。その属性が指す先の中身は除かない。中身の検出は決定23 による。

13. drift の解消も Git を経由する。Git 側を実態へ合わせるか、実態を Git の宣言へ戻すかのいずれかを Pull Request として提出する。検出箇所へ直接 apply して差分を黙らせない。
14. drift の自動修復を行わない。検出は通知であり、適用の起動条件としない。

### 本 ADR で決定しない事項

15. state backend の選定は本 ADR の対象外とし、別 ADR で決定する。state の分割単位は ADR-0604 が決める。環境（dev / stg / prd 等）の表現方法は ADR-0102 決定11-12 が決める。
16. 本 ADR が前提とするのは、apply が排他制御（lock）のもとで実行されることのみとする。この前提を満たさない backend を採用する場合、決定6 の保証は成立しない。

### apply の失敗

17. merge の後の apply が、commit の宣言を AWS に対して評価した後に失敗した場合も、その状態からの回復は Git の経路で行う。CI は最後に成功した apply の commit を apply し直さない（決定6）。CI が用意するのは、最後に成功した apply の commit へ宣言を戻す revert のブランチまでとし、Pull Request を開いて merge するのは人である（決定10）。revert のブランチは、失敗した apply の merge が運んだリリース線の commit から切り、リリース線へ向ける。デプロイブランチの上では revert しない。そこでだけ戻すと、次にリリース線から運ぶ merge が戻した変更を運び直さず、2本の木が分かれる。リリース線へ merge した revert は、決定4 の経路でデプロイブランチへ運ぶ。
18. revert のブランチを用意するために CI が持つリポジトリへの書き込みの資格情報は、その run の間だけ有効な `GITHUB_TOKEN` の `contents: write` に限る。それを持つ job は判定を持たず、道具を導入せず、決まった commit の revert と push だけを行う。戻すかどうかと戻す範囲は、読み取りの権限だけを持つ job が決める。
19. apply の失敗は、revert が merge されるまで後続の apply を止めない。apply は常にデプロイブランチの先頭の木全体を適用するので、後続の apply が成功すれば実環境は Git と一致する。後続の apply はそのブランチへの merge でだけ起きるので、止める必要があるときは、人がそこへの merge を止める。

### 発火側が所有する状態

20. Desired State から除く属性を、次の一覧に限る。一覧に無い属性を `lifecycle.ignore_changes` に入れない。属性の全体を表す `all` を用いない。一覧への追加と変更は、本 ADR の変更とする。

    | resource type | 属性 | 発火側が行う操作 |
    | --- | --- | --- |
    | `aws_ecs_service` | `task_definition` | 成果物の版を差し替えた Task 定義の revision の登録と、Service の更新 |

21. 除いた属性が指す先の中身は、Terraform が宣言する。発火側が登録する revision は、Terraform が最後に登録した revision から、成果物の版（コンテナイメージの digest）だけを差し替えたものとする。決定20 の一覧を使う use-case は、元にする revision の識別子を出力として発火側へ渡し、Terraform の宣言の変更が実行中の Service へ届く経路（発火側の次の更新）を、公開契約として README に持つ。
22. 発火側の資格情報は、決定20 の操作と、成果物の push と、デプロイの前のスキャン結果の読み取りに要るものに限る。Task 定義へ渡せる Role を当該 use-case の Task Role と Execution Role に限り、渡し先を ECS のタスクに限る。Service の更新を当該 use-case の Service に限る。
23. 発火側が登録した revision の中身と Terraform の宣言との差は、plan に現れない。これを、当該 use-case の `check` block と、AWS Config の評価による Security Hub の ECS のコントロールで、事後に検出する。Config を常駐させない環境では `check` block だけが残る。

## 検討した代替案

### 案A: ローカルからの apply を許容する

緊急時の反映が速く、CI の配線に依存しない。ただし適用された構成に対応する commit が特定できず、レビューを経ない構成が実環境に存在し得る。ADR-0202 の公開契約の保証が、実環境に対して意味を持たなくなる。

### 案B: Pull Request 上で apply まで行い、merge を事後手続きとする

実環境での結果を merge 前に確認できる。ただし main に存在しない構成が実環境へ適用され、Git が Desired State である前提が崩れる。Pull Request を閉じた場合に実環境へ何が残るかが定義できない。

### 案C: drift を検出したら自動的に修復する

Git と実態の乖離が自動的に収束する。ただし drift の原因（意図的な緊急対応か、外部システムの正当な変更か、攻撃か）を判定せずに上書きすることになり、インシデント対応中の変更を巻き戻し得る。検出と適用を分離しないため、Drift Detection が deployment 機構になる。

### 案D: apply の失敗の後、CI が最後に成功した commit を apply し直す

実環境を速く直前の状態へ近づけられる。ただし apply する commit がデプロイブランチの先頭と一致しなくなり、決定6 と古い commit の拒否（ADR-0602 決定14）に反する。Terraform は部分的に適用された変更を取り消す手段を持たないので、直前の commit の apply も同じ理由で失敗しうる。

### 案E: CI が revert の Pull Request まで作る

人の手順が1つ減る。ただし GitHub Actions に Pull Request の作成を許す設定は承認も同時に許し、長期の資格情報（fine-grained PAT）は ADR-0602 の「CI が長期の資格情報を保持しない」に反する。どちらも、承認者数の要件を CI が満たしうる状態を作る。

### 案F: リリース線への merge で apply する

デプロイブランチを置かずに済む。ただし変更を集めることと実環境へ当てることを分けられず、止めるにはデフォルトブランチを動かすしかない。リリース線は版ごとに名前が変わるので、apply の environment を固定名へ狭められず、保護パターンに当たる新しいブランチから apply 系の environment に届く。

### 案G: feature のブランチをデプロイブランチへ直接 merge する

リリース線を待たずに当てられる。ただし同じ変更がリリース線とデプロイブランチへ別々に merge され、2本の木が分かれる。どちらの木が Desired State か（決定1）が決まらなくなる。hotfix の線から直接入れる形も同じである。

### 案H: 成果物の版を env の値として Git に置き、CI の apply が反映する

決定1・11 を例外なしに保てる。実行中の版に対応する commit が常に1つあり、plan と Policy Test が実際にデプロイされる Task 定義に掛かる。ただし、マイグレーション等の単発のタスクを Service の更新より先に実行するには、単発のタスクの版と Service の版を別の値にし、env ごとに2回の変更の経路（リリース線とデプロイブランチへの merge と apply）を通す必要がある。発火側は単発のタスクと Service の更新を1つの発火で続けて行えない。

### 案I: 発火側が版を SSM Parameter へ書き、Terraform が読んで CI が apply する

`ignore_changes` が要らず、Service を変えるのは常に Terraform である。ただし Git は版を持たず（決定1）、plan の時点で読む値が apply までに変わりうる（決定6）。単発のタスクの順序のために案H と同じく2回の apply が要り、発火側が apply の workflow を起動する資格情報も要る。案H と本決定の欠点を併せ持つ。

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
- リリース線への merge が、そのまま実環境へ届くこと。当てるのは、リリース線からデプロイブランチへの Pull Request の merge である
- hotfix の修正を、リリース線を経ずに当てること。hotfix の間（デフォルトブランチがリリース線でない間）は、デプロイブランチへ昇格できない
- 1つの env へ、複数のブランチから apply すること
- 実行中のアプリケーションの成果物の版を Git が知っていること。版の記録は、発火側のパイプラインと ECS の Service の履歴が持つ
- Terraform の apply の時点で、Task 定義の変更が実行中の Service へ届くこと。届くのは発火側の次の更新の時点である（決定21）

## 保証範囲

- 保証すること: 決定20 の一覧の属性を除き、実環境へ適用された構成が、デプロイブランチ上の commit に対応し、その木がリリース線でレビューされた commit の木と一致すること（ADR-0603 決定31 の検査が通った merge に限る）。適用が検証を通過した構成であること
- 保証しないこと: Git 外で行われた変更が発生しないこと（検出のみを保証する）。検出から解消までの間、実態が Git と一致していること
- 保証しないこと: apply の失敗から revert の merge まで、または後続の apply の成功までの間、実態が Git と一致していること。revert の apply が成功すること
- 保証しないこと: sandbox への apply の成功が、merge の時点でその Pull Request の構成が sandbox に当たっていること。sandbox の state は1つで、別の線の Pull Request の apply が後から上書きしうる。成功が示すのは、その head の木が、その時点の sandbox の状態に対して apply できたことである（決定4）
- 保証しないこと: 実行中の Task 定義の revision が、Terraform が登録した revision であること。発火側が登録した revision の中身が Terraform の宣言と一致すること（事後の検出のみ。決定23）
- 保証しないこと: Terraform の宣言の変更が、発火側の次の更新より前に、実行中の Service へ届くこと（決定21）

## 検証方法

- Static Analysis: apply を行う workflow の契機が、デプロイブランチへの push と、ref や commit を入力に取らない手動の起動に限定されていること（ADR-0602 決定14）。push のブランチが宣言から生成されたものと一致すること（make repo-settings-check）
- 整合の検査（required の deploy-source-check）: デプロイブランチへ入る Pull Request について、デフォルトブランチが宣言の線であり、head がデフォルトブランチから到達でき、merge の結果の木が head の木と一致すること（決定4、ADR-0603 決定31）
- 運用機構（make repo-settings-check）: 宣言で、デプロイブランチを持つ env がそれぞれちょうど1つのデプロイブランチを持ち、デプロイブランチが受け取る線が宣言の線であること（決定4）
- Static Analysis: sandbox の apply の job が、Pull Request だけで起動し、1つの concurrency の group に属し、ADR-0603 決定31 の検査の成功に依存し、古い head と fork・Dependabot の Pull Request を資格情報の取得の前に拒むこと（決定4）
- Static Analysis: Pull Request 契機の workflow に apply が含まれないこと。決定3 の sandbox の例外を除く
- Static Analysis: 検証ジョブの実行順序が決定9 を満たすこと
- 定期実行: drift 検出ジョブが存在し、結果が観測可能な形で残ること
- Static Analysis: リポジトリへの書き込みの資格情報（`contents: write`）を持つ job が、決定18 の形に限られること（`make contents-write-lint`）
- Static Analysis（HCL）: `lifecycle.ignore_changes` が、決定20 の一覧の resource type と属性の組にだけ現れ、`all` を含まないこと
- Policy Test: 発火側の role の `iam:PassRole` の Resource が当該 use-case の Task Role と Execution Role に限られ、`iam:PassedToService` の条件で ECS のタスクに限られること。`ecs:UpdateService` の Resource が当該 Service に限られること（決定22）
- Contract Test: 決定20 の一覧を使う use-case が、発火側が元にする revision の識別子を出力すること（決定21）
- 実環境（sandbox）: 決定20 の一覧を使う use-case で、Terraform が Task 定義を変えた後、発火側の更新を経て、その変更が実行中のタスクへ届くこと（決定21）
- 定期実行: drift 検出ジョブが決定20 の一覧の属性を検出から外す場合、外す規則が一覧から導かれ、一覧の外の属性を外さないこと

## 影響

- 反映の最短時間は、レビューと merge を含む時間になる。
- 実環境で先に手を入れて後から Git へ写す運用は、drift として検出され、Pull Request として処理される。
- state backend を決める ADR が別途必要になる（決定15）。
- 実環境へ当てるたびに、リリース線からデプロイブランチへの Pull Request が要る。apply の失敗の回復は、リリース線への revert とその昇格の2つの merge になる。
- アプリケーションの成果物の版の更新は、Pull Request を要さない。その代わり、発火側の資格情報が本番で動くコードを決める資格情報になる。その範囲を決定22 が限る。

## 見直し条件

- 検証の所要時間が、インシデント対応で許容できる時間を超えた場合
- 環境の数が増え、単一の apply 経路では表現できなくなった場合
- state backend の決定が、決定16 の前提（排他制御）を満たせない方向へ動いた場合
- GitHub が、Pull Request の作成だけを許し承認を許さない権限を `GITHUB_TOKEN` に与えた場合（決定17 の「人が開く」を見直す）
- sandbox の例外（決定3・4・6）を、ほかの env へ広げる必要が生じたとき。または、リリースされていない変更を sandbox で確かめる必要が無くなったとき
- hotfix の間に、デプロイブランチへ当てる必要が生じたとき（決定4。hotfix の間はデフォルトブランチがリリース線でない）
- 決定21 の経路（Terraform の宣言の変更が、発火側の次の更新で実行中の Service へ届くこと）が成り立たないと分かったとき。案H を再検討する
- RunTask がコンテナのイメージを override できるようになったとき（Context。発火側が revision を登録する理由が消える）
