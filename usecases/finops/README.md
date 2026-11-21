# finops

1つのアカウントに、月の予算（AWS Budgets）と費用の異常の検知（AWS Cost Anomaly Detection）を持ち、どちらの通知も同じアカウントの `ops-notification` の Topic へ送るユースケース。常駐層に属し（`docs/v1.0-plan/scope.md` 第10.2節）、7つのアカウントのそれぞれの workload root から呼ばれる。

## 解決する問題

消し残しや想定外の利用による費用の増加に、人が気づけるようにする。予算は超過と超過の予測を、異常の検知は期待値から外れた費用を知らせる。どちらも止める仕組みではない。課金データの反映に数時間から1日の遅れがあり、検知まで最大24時間かかる（scope.md 第10.2節）。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。Budgets と Cost Anomaly Detection が、AWS の側で費用を評価して Topic へ送る
- 外への egress は無い。apply は Budgets と Cost Explorer の API を呼ぶ

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、75文字以内） | 名前の先頭に置くプロジェクトの token。最も長い予算の名前（`<project>-log-archive-monthly-cost`）を Budgets の上限の100文字に収める |
| `env` | `admin` / `security` / `log-archive` / `sandbox` / `dev` / `stg` / `prd` | 置くアカウントの env の token。`admin` の monitor はメンバーアカウントごと、ほかは AWS のサービスごとに費用を分ける（[finops/ADR-0001](docs/adr/0001-monitor-dimension-by-account.md)） |
| `notification_topic_arn` | `string`（SNS の Topic の ARN。FIFO の Topic は受けない） | 同じアカウントの `ops-notification` の `topic_arn`。partition とアカウントが provider のものと違えば plan で落ちる。Budgets は別のアカウントの Topic へ通知できない |
| `monthly_budget_usd` | `number`（0より大きく、小数2桁まで） | このアカウントの月の予算の額（USD）。管理アカウントでは組織全体の費用に対する予算になる |
| `anomaly_threshold_usd` | `number`（0より大きく、10000000000 以下） | 通知する異常の影響額の下限（USD）。影響額がこの額以上で、かつ期待値に対して40%以上の異常を通知する |

出力は無い。

名前は入力から決まる。予算は `<project>-<env>-monthly-cost`、monitor は管理アカウントで `<project>-admin-linked-accounts`、ほかで `<project>-<env>-services`、subscription は `<project>-<env>-immediate` である。

- ログの所在: 無い。予算と monitor の変更の記録は CloudTrail が持ち、その配置は `account-baseline` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: `notification_topic_arn` に受ける。metric alarm は持たない

## 持つもの

- 予算1つ: 費用・月次・USD。アカウントの費用の全体を見て、filter を持たない。実績が85%と100%を超えたときと、予測が100%を超えたときに Topic へ通知する（[finops/ADR-0101](docs/adr/0101-notify-through-ops-notification.md)）
- AWS managed monitor 1つと、それを指す subscription 1つ。subscription は即時（`IMMEDIATE`）に Topic へ通知する

## supported / unsupported

supported:

- 7つのアカウントのそれぞれに1つずつ置き、そのアカウントの Topic へ通知する構成
- 管理アカウントで組織全体の予算と、メンバーアカウントごとの異常の検知を持つ構成

unsupported:

- 予算で止めること（予算の action）。予算は消し残しの検知に使う（scope.md 第10.2節、[finops/ADR-0101](docs/adr/0101-notify-through-ops-notification.md)）
- email の宛先と、日次・週次の異常の要約
- 予算を filter（サービス、タグ、アカウント等）で絞ることと、タグで切ったワークロードの単位の予算
- タグ・コストカテゴリ・customer managed の monitor
- 予算と閾値の通知の並びを選ぶこと。閾値の割合（85%・100%・予測100%、異常の40%）は固定する
- 別のアカウントの Topic への通知
- 不正利用の即時の検知。遅れが大きいので、侵害の検知の代わりにならない（GuardDuty は `account-baseline` の責務）
- 管理アカウントに bootstrap の前提として在る AWS services monitor と、その email の subscription を Terraform で持つこと（[finops/ADR-0001](docs/adr/0001-monitor-dimension-by-account.md)）。それらは人が tag `<project>:bootstrap` を付け、CI の apply の role に、その変更・削除を拒む Deny を置く。実際の要求での評価は確かめていない（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) の保証範囲）
- 1つのアカウントに複数の env を置く構成（scope.md 第10.1節）

## 所有者

- 予算・monitor・subscription は、このユースケースが所有する
- 通知の Topic、その Topic Policy と鍵は `ops-notification` が所有する。このユースケースは Topic へ送るだけで、Topic Policy を持たない

## 依存するユースケースと判断 Issue

- ユースケース: `ops-notification` の `topic_arn` を `notification_topic_arn` に受ける。Topic Policy と鍵の Key Policy は、`budgets.amazonaws.com` と `costalerts.amazonaws.com` の送信を、同じアカウントの `aws:SourceAccount` で許していなければならない（`ops-notification` の側の宣言）
- CI の apply の role は、Budgets と Cost Explorer の権限を `deployment-identity` から受けていなければならない
- 判断 Issue: #97（通知の Topic のアカウント配置）、#98（常駐層の完了条件）、#109（alarm の置き場）。いずれも閉じている
- Cost Explorer は管理アカウントで有効にする（`docs/setup/bootstrap.md` 2-2）。メンバーアカウントは既定で自分の費用を見られる

## 検証方法

- Unit Test（mock）: `internal/budget/tests/` が予算の種類・期間・単位・額の文字列・通知の3つの閾値と宛先の導出を完全一致で、Topic の partition とアカウントの precondition を、`internal/cost-anomaly/tests/` が monitor の種類と次元、subscription の宛先・閾値・monitor への配線と precondition を確かめる。`tests/validation.tftest.hcl` が公開 variable の検査を確かめる
- Contract Test: 公開 output を持たないので、`tests/contract.tftest.hcl` が各公開入力の届き先（Topic が予算と異常の通知の宛先に、額が予算に、閾値が異常の閾値に、`env` が monitor の次元に）を固定する
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、予算の action の作成と更新が無いこと、予算が通知を持ち、予算の通知と異常の subscription の宛先が同じ Topic 1つで email を持たないこと、頻度が `IMMEDIATE` であること、予算が費用を filter で絞らないこと、usecase の instance ごとに予算と subscription が1つずつあることを確かめる。plan で決まらない値は 02-04 が落とす（`PLAN-FINOPS-*`。一覧は [`conftest/plan/README.md`](../../conftest/plan/README.md)）
- Static Analysis（`make layer-check`・`make infra-code-gate`）: 層の規則と、resource の型の分類を、HCL のソースで確かめる
- 実環境テストの形: scope.md 第11.1節の区分「破棄できるが、仕様で残るものがある」に当たる。sandbox では常駐の実体と AWS managed monitor の枠を共有して複製を作れないので、[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の既定（作成して破棄する）を常駐の実体で行い、destroy して作り直す。残存は決定16 の期待残存と照合する

env の root、CI の apply の経路、`ops-notification`、sandbox の掃除 CI は無い。いずれも配線された後に遡って検証する。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- 通知が Topic に届くこと。Budgets が暗号化した Topic へ送るための鍵の policy の文例を AWS は示していない。Budgets は課金データの反映を、Cost Anomaly Detection は検知までの最大24時間と10日の履歴を待つので、apply の後の検証の時間内には観測できない
- API が名前・Topic の region・AWS managed monitor の枠で拒むこと。メンバーアカウントに手で作った AWS services monitor が在ると、apply が枠で落ちる
- `limit_amount` の読み戻しと、期間を書かない予算の読み戻しで、plan が毎回差分を出すこと
- 管理アカウントの予算と `LINKED_ACCOUNT` の monitor が、管理アカウント自身の費用を含むこと
- 管理アカウントで Cost Explorer の「linked account access」を外したとき、メンバーアカウントの予算と monitor が費用を見られること
- CI の role の権限が、Provider の実際の呼び出しを覆うこと

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19

- AWS / HashiCorp が publisher の Budgets と Cost Anomaly Detection の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `budgets` / `cost anomaly`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`hashicorp` が無い）
- 参照した一次資料: AWS Well-Architected の COST02-BP05 と COST01-BP05（Cost Optimization Pillar の版。Framework の本体の同じ BP は URL を開けず、文言を確かめていない）、Cost Anomaly Detection User Guide の「Getting started」「Quotas and restrictions」「Creating an Amazon SNS topic for anomaly notifications」「Using EventBridge with Cost Anomaly Detection」、AWS Budgets User Guide の「Creating an Amazon SNS topic for budget notifications」「Using budget templates」、Budgets と Cost Explorer の API Reference、Cost Management の「Controlling access to Cost Explorer」、AWS Organizations の「Best practices for the management account」、AWS SRA の管理アカウントの節、Security Hub の FSBP と CIS AWS Foundations Benchmark のコントロールの一覧、AWS Provider 6.70.0 の `aws_budgets_budget`・`aws_ce_anomaly_monitor`・`aws_ce_anomaly_subscription` の文書

採用した知見:

- アカウントごとに、アカウントの費用の全体を見る予算を置く（COST02-BP05、COST01-BP05）
- 組織の主たる次元の AWS managed monitor と、AWS services monitor を並べて持つ。メンバーアカウントごとの monitor は管理アカウントだけに置ける（Getting started、Quotas）
- Budgets の通知の Topic は同じアカウントに置く（budgets の SNS の文書）
- Cost Anomaly Detection を SNS で受けるのは即時の通知だけで、subscription が持てる Topic は1つ
- 管理アカウントには、管理アカウントでしかできないもの（組織全体の予算、メンバーアカウントごとの monitor）だけを置く

採用しなかった知見と理由:

- 予算の action（COST02-BP05）: scope.md 第10.2節が予算を停止機構にしない（[finops/ADR-0101](docs/adr/0101-notify-through-ops-notification.md)）
- タグで切ったワークロードの単位の予算（COST01-BP05）: 予算の filter を Provider と API の表現のまま受けることになる（[ADR-0204](../../docs/adr/0204-no-generic-escape-hatch.md)）
- EventBridge で異常を受ける経路: subscription はどちらでも要り、event は Cost Anomaly Detection の home region に出るので、Topic へ届けるにはリージョンをまたぐ rule と role が要る。subscription が無いときに event が出るかは文書に無い
- Provider の SNS の例の、同じアカウントの全主体へ Topic の操作を開く文: Topic Policy は `ops-notification` が持ち、このユースケースは写さない

Security Hub の FSBP と CIS AWS Foundations Benchmark に、Budgets・Cost Explorer・Cost Anomaly Detection を対象にするコントロールは無い。`make trivy-config`・`checkov`・`kics`・`semgrep` は、この3つの resource の型に所見を出さない。KICS の e592a0c5（IAM Access Analyzer）は型によらず出る所見で、ほかの module と同じ理由で `internal/` の2つのファイルで抑止している。

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

- 予算をタグで切ったワークロードの単位に持たない（AWS Well-Architected COST01-BP05 の後半）: [finops/ADR-0001](docs/adr/0001-monitor-dimension-by-account.md)

予算の通知の閾値（85%・100%・予測100%）と異常の通知の割合（40%）は、明確な公式推奨が無いので独自判断とした（[finops/ADR-0101](docs/adr/0101-notify-through-ops-notification.md) 決定6）。

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.17.1（`mise.toml`）だけである
- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた。reference の lock は 6.70.0 を固定する
