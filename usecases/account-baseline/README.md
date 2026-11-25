# account-baseline

アカウントかリージョンに1つしか置けない安全側の設定を、全アカウントの workload root から置くユースケース。組織の側の検知は持たない（[account-baseline/ADR-0001](docs/adr/0001-workload-root-in-every-account.md)）。**experimental** であり、昇格と削除の条件は同 ADR が持つ。

## 解決する問題

Security Hub の AWS Foundational Security Best Practices（FSBP）と CIS AWS Foundations Benchmark には、resource ではなくアカウントかアカウント×リージョンを単位に評価するコントロールがある。対応する設定はアカウントかリージョンに1つしか置けず、2つの宣言は互いに上書きする。このユースケースは、それらの設定の唯一の持ち主として、7つのアカウント（`docs/v1.0-plan/scope.md` 第10.1節）に同じ値で置く。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。アカウントとリージョンの設定だけを持つ
- 外への egress は無い。AWS Config は、AWS のサービスとして log-archive のアカウントの `audit-evidence` の Bucket へ配信する
- リージョンごとの設定は、partition ごとの既定で有効な全リージョン（aws では17）に置く。provider は1つで、resource の `region` 引数で置く（[account-baseline/ADR-0101](docs/adr/0101-default-regions-per-partition.md)）

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字で始まり、英小文字と数字だけ、234文字以内） | 名前の先頭に置くプロジェクトの token |
| `env` | `admin` / `security` / `log-archive` / `sandbox` / `dev` / `stg` / `prd` | 置くアカウントの env の token。`sandbox` では AWS Config を置かず、`admin` では IAM の password policy を置かない |
| `config_delivery` | `object({ bucket_name = string, kms_key_arn = string })` | Config の配信先。log-archive の `audit-evidence` の `bucket_name` と `kms_key_arn`。鍵は単一リージョンの鍵の ARN だけを受け、alias と multi-Region key（`mrk-`）を受けない。`sandbox` では使わない |
| `config_history_retention_days` | `number`（30 以上 2557 以下の整数） | Config の構成の履歴を保持する日数。`sandbox` では使わない |
| `security_contact` | `object({ name, title, email_address, phone_number })`（すべて `string`、`sensitive`） | セキュリティの代替の連絡先。長さと形は PutAlternateContact の API の制約に合わせる。`name` は1文字以上64文字以内、`title` は1文字以上50文字以内、`email_address` は254文字以内のメールアドレス、`phone_number` は `+` と数字で始まる国際形式で25文字以内 |

すべての入力は既定値を持たない。リージョン、Config の記録の範囲と頻度、各設定の値は入力にしない。

名前: Access Analyzer の analyzer は `<project>-<env>-external`、Config の recorder と delivery channel は `<project>-<env>` である。

出力は無い。

- ログの所在: 無い。Config の配信物は `audit-evidence` の Bucket が、設定の変更の記録は組織の trail が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。通知に値する metric を出す resource を持たない（`docs/v1.0-plan/scope.md` 第7節）

## 持つもの

| 設定 | 単位 | 値 | 対応するコントロール | 置かない env |
| --- | --- | --- | --- | --- |
| S3 Public Access Block | アカウント | 4項目とも `true` | S3.1 | - |
| セキュリティの代替の連絡先 | アカウント | `security_contact` | Account.1 | - |
| IAM の password policy | アカウント | 長さ14以上、大文字・小文字・数字・記号、再利用の防止24、90日。`organization-baseline` の break-glass と同じ値 | IAM.7・15・16 | `admin` |
| EBS の既定の暗号化 | リージョン | 有効 | EC2.7 | - |
| IAM Access Analyzer | リージョン | type `ACCOUNT` の外部アクセスの analyzer | IAM.28 | - |
| AWS Config の記録 | アカウント（SLR）とリージョン（recorder・delivery channel・status・retention） | すべての resource を `CONTINUOUS` で記録。グローバルな resource は provider のリージョンだけ（[account-baseline/ADR-0102](docs/adr/0102-config-recorder-except-sandbox.md)） | Config.1 | `sandbox` |
| VPC Block Public Access | リージョン | `block-bidirectional` | EC2.172 | - |
| SSM document の公開共有 | リージョン | `Disable` | SSM.7 | - |
| SSM Automation のログの出力先 | リージョン | `CloudWatch` | SSM.6 | - |
| EMR の block public access | リージョン | 有効。公開を許す port は 22 だけ | EMR.2 | - |

service-linked role は、Config のものだけを持つ（[account-baseline/ADR-0103](docs/adr/0103-service-linked-roles-by-arn-reference.md)）。

## supported / unsupported

supported:

- 7つのアカウントの workload root から1つずつ呼ぶ構成
- aws の partition の、既定で有効な17のリージョンだけが有効なアカウント
- `sandbox` で Config を置かない構成と、`admin` で password policy を置かない構成

unsupported:

| 要求 | 理由 |
| --- | --- |
| 組織の側の検知（組織の trail、GuardDuty、Config の集約、組織の analyzer、Inspector、Security Hub、検知の通知） | 委任管理者で持つ `security-tooling`（#473）の責務（[account-baseline/ADR-0001](docs/adr/0001-workload-root-in-every-account.md)） |
| Macie（Security Hub の Macie.1・2） | アカウント単位の安全側の設定ではなく、データの検知である。持つかどうかは `security-tooling`（#473）で決める |
| AWS Support の role（Security Hub の IAM.18） | 人の到達の経路は `organization-baseline` が持つ。満たす形は permission set（#21） |
| Access Analyzer の unused access の analyzer | Security Hub のコントロールに無く、有料で、要求する利用者が無い（[ADR-0102](../../docs/adr/0102-use-case-centric-scope.md) 決定6） |
| Config rule と conformance pack | 組織に配る形は委任管理者が持つ（#362） |
| EBS の既定の暗号化の鍵を customer managed key にすること | 公式推奨に一意の値が無い。一般 module は region を受けない（[ADR-0208](../../docs/adr/0208-us-east-1-resources-by-region-argument.md) 決定2） |
| Config の配信先の鍵に multi-Region key（`mrk-`）を使うこと | `audit-evidence` の鍵は単一リージョンの鍵である。`config_delivery.kms_key_arn` の検査は、鍵の ID が UUID の形の ARN だけを受ける |
| aws の外の partition | 集合を持たない。plan で落ちる（[account-baseline/ADR-0101](docs/adr/0101-default-regions-per-partition.md)） |
| opt-in のリージョンが有効なアカウント | 同上 |
| VPC Block Public Access の例外（exclusion） | 例外は Internet Gateway を通す subnet の持ち主の判断で、このユースケースは両向きを止める形だけを持つ |
| Config の記録の範囲と頻度を選ぶこと | 公式推奨で一意に決まるので固定する（[account-baseline/ADR-0102](docs/adr/0102-config-recorder-except-sandbox.md)） |
| `env` によらず Config や password policy の有無を選ぶこと | 機能のフラグを置かない（[ADR-0204](../../docs/adr/0204-no-generic-escape-hatch.md)） |
| 1つのアカウントに複数の env を置く構成 | `docs/v1.0-plan/scope.md` 第10.1節 |

## 所有者

| resource | 所有 |
| --- | --- |
| S3 Public Access Block、代替の連絡先、password policy | [`internal/account-controls`](internal/account-controls/) |
| EBS の既定の暗号化、analyzer、VPC Block Public Access、SSM の2つのサービス設定、EMR の block public access | [`internal/regional-controls`](internal/regional-controls/) |
| Config の SLR・recorder・delivery channel・status・retention | [`internal/config-recorder`](internal/config-recorder/) |
| 管理アカウントの password policy | `organization-baseline` の break-glass |
| Config の配信先の Bucket と鍵、その Bucket Policy と Key Policy | `audit-evidence` |
| AWS Chatbot の service-linked role | `deployment-identity`（#472） |
| 設定の停止と変更を拒む SCP（`deny-baseline-stop`）、opt-in のリージョンの有効化を拒む SCP（`deny-enable-region`） | 中身の正本はこのユースケースの [account-baseline/ADR-0201](docs/adr/0201-baseline-settings-changed-only-by-ci-apply.md)。付け先は `organization-baseline` |

一般 module は呼ばない。

## 依存するユースケースと判断 Issue

- `audit-evidence`: `bucket_name` と `kms_key_arn` を `config_delivery` に受ける。`audit-evidence` の `writers.config_source_accounts` に、`sandbox` の外の各アカウントの ID が要る
- `deployment-identity`: 各アカウントの apply の role が、このユースケースの resource を作る権限を持つ。管理は `<project>-<env>-ci-apply-workload-2`、読み取りは `<project>-<env>-ci-apply-workload-read`（plan の role にも付く）に置く。ARN にリージョンを持つ Action と EC2 の Action は provider のリージョンと us-east-1 に限り、Resource `"*"` の S3・Config の delivery channel と保持期間・EMR の Action はリージョンで限らない。password policy の更新は、`DenyGovernanceChange` が管理アカウントでだけ拒む（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定5・13・17）
- 呼ぶ env の root: 管理アカウントは `env/admin/`、メンバーは #21・#330・#331 が配線する
- 全リージョンを CI の経路に乗せる egress の宣言と apply の role の権限: #474
- 判断 Issue: #86（sandbox の Config を常駐させない）、#98（常駐層の完了条件）、#472（Chatbot の SLR の持ち主）

## 検証方法

区分は「アカウントかリージョンに1つしか置けない安全側の設定を常駐させる」（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14、`docs/v1.0-plan/scope.md` 第11.1節）。常駐させる設定は破棄せず、Policy Test、拒まれるべき操作が拒まれることの確認、drift 0 の3つで代える。sandbox に置かない Config はこの区分に当たらず、`internal/config-recorder` を sandbox で作成・破棄する。

| 層 | 何を確かめるか | 道具 |
| --- | --- | --- |
| Unit | `internal/account-controls` の PAB の4項目・代替の連絡先・password policy の値と有無、`internal/regional-controls` の各リージョンの6つの設定の値と件数・集合の外の有効なリージョンで plan が落ちること・空の集合の拒否、`internal/config-recorder` の SLR・記録の範囲・home だけのグローバルな記録・配信先・status・保持日数・provider のリージョンが集合の外にあるときに recorder の precondition で plan が落ちること・空の集合の拒否 | `make tf-test`（`internal/*/tests/`） |
| Contract | env ごとの合成（Config と password policy の件数）、aws の集合が届くこと、`config_delivery` の届き先、aws の外の partition で plan が落ちること、入力の検査（`project` の形と234文字の境界、代替の連絡先の長さと形の境界） | `tests/contract.tftest.hcl`、`tests/validation.tftest.hcl` |
| Policy | `PLAN-ACB-00`-`06`（宣言の形とキーの集合、見る型の変更の存在、単一の型が層の宣言の `singletons` に持ち主 `account-baseline` として在ること、リージョンごとの型と、Config の型を1つでも持つ instance の Config の型と、SSM の2つの設定のそれぞれが宣言のリージョンに過不足なく在ること、設定の値・recorder の `role_arn`・status、グローバルな記録が1つのリージョン、PAB の4項目、password policy の値が `organization-baseline` の break-glass と一致すること）。01-04 の形の検査は account-baseline の reference の plan にだけ掛ける | `make tf-policy`（reference の root の mock の plan） |
| Policy（削除の保護） | `PLAN-STATE-*`（`stateful_protection.protected_types` の13型。PAB・代替の連絡先・password policy・EBS の既定の暗号化・analyzer・SSM のサービス設定・EMR の block public access・VPC Block Public Access・SLR・recorder・delivery channel・recorder の status・retention の削除と置換）。reference の plan は作成しか持たないので、ここでは効かない。型が宣言から外れることは `conftest/plan/stateful_protection_test.rego` が落とす | CI の実 plan（`plan.stateful_protection`）。account-baseline を呼ぶ env の root が配線されるまで効かない |
| Static | 層の規則（アカウントかリージョンに1つの型を、ほかのユースケース・env・一般 module が宣言しないこと（`LY401`）を含む）、型の分類 | `make layer-check`、`make infra-code-gate` |
| Integration（sandbox） | 拒まれるべき操作が拒まれること、drift 0、`internal/config-recorder` の作成と破棄 | 未実施。sandbox の apply の経路（#331）の後に行う |

KICS の e592a0c5（IAM Access Analyzer）は、analyzer を宣言しない `internal/account-controls/main.tf` と `internal/config-recorder/main.tf` で抑止する。KICS はファイルごとに analyzer の宣言を求める。理由と撤回の条件は各ファイルの先頭が持つ。

破棄しない形と mock で検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

| 故障モード | 起きたときの症状 | 代わりに検出するもの |
| --- | --- | --- |
| 公開の ACL の要求や暗号化を指定しない EBS volume が、実際には拒まれないか暗号化されない | 設定は在るが効かない | apply の後の人の確認（未実施） |
| Config の SLR が既に在るアカウントで、SLR の作成が失敗する | apply が落ちる | 配線の前の人の確認（`iam get-role`） |
| 既存の代替の連絡先と password policy に対する作成が、上書きでなく失敗する | apply が落ちる | apply の失敗 |
| SLR の主体での、別のアカウントの Bucket と鍵への配信が拒まれる | Config の配信が届かない | 無い。`audit-evidence` の配信の途絶の alarm は、組織の trail が書き込んでいる間は鳴らない |
| 実 API が読み戻す設定の値が宣言と違う | plan が毎回差分を出す | 配線の後の plan |
| SSM の `setting_id` を `/ssm/...` の形で書いたのに、実 API が ARN の形で読み戻す（未確認） | plan が毎回差分を出す | 配線の後の plan |
| 別の経路（Security Hub の central configuration、Control Tower など）が先に recorder か delivery channel を作っている | 作成が止まり、apply が落ちる | 配線の前の人の確認（全リージョンで `aws configservice describe-configuration-recorders`） |
| mock の SLR の ARN は手書きで、実 provider の値の形と一致することを確かめていない | `PLAN-ACB-02` の `role_arn` の検査が、実際の値と違う形を見て通る | 無い。どの層も見ていない |
| opt-in のリージョンの判定を、mock の `aws_regions` の値でしか確かめていない | 実 provider の `aws_regions` が返す集合が mock と違えば、有効な opt-in のリージョンを見逃す | 無い |
| 適用の後にアカウントで opt-in のリージョンを有効にする | `aws_regions` の postcondition で、destroy の plan も止まる | plan の失敗 |
| EMR の block public access・SSM の2つの設定・VPC Block Public Access・analyzer が、実際には効かない | 設定は在るが効かない | apply の後の人の確認（未実施） |

## 保証しないもの

- 実際の AWS での作成。このユースケースを呼ぶ root と、その apply の経路が要る
- 設定を弱める変更を、CI の実 plan で見ること。`PLAN-ACB-*` は reference の mock の plan にだけ掛かる。CI の実 plan に掛かるのは `plan.stateful_protection`・`plan.kms_key_policy`・`plan.resource_policy_exclusive` だけである（[`conftest/plan/README.md`](../../conftest/plan/README.md)）
- 設定の削除と置換を止めること。`plan.stateful_protection` は CI の実 plan に掛かり、account-baseline を呼ぶ env の root が配線されるまで、どの plan も見ない
- 管理アカウントでの設定の停止と変更を拒むこと。SCP は管理アカウントに効かない（[account-baseline/ADR-0201](docs/adr/0201-baseline-settings-changed-only-by-ci-apply.md) 決定6）
- メンバーでの設定の停止と opt-in のリージョンの有効化を、SCP を Security・Workloads・Sandbox へ広げる前に拒むこと。新しい SCP は PolicyStaging に付けて始める（同 決定5）
- 管理アカウントで opt-in のリージョンが有効にされることを拒むこと。管理アカウントには SCP が効かず、有効なリージョンの検出は plan の postcondition だけである
- SSM Automation が作る既定の Log Group（`/aws/ssm/automation/executeScript`）の暗号化と保持日数。出力先を CloudWatch にする設定だけを持つ
- provider のリージョンと us-east-1 のほかのリージョンでの apply。recorder・analyzer・SSM の設定・EBS の既定の暗号化・VPC Block Public Access の作成は拒まれる（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定13。全リージョンは #474）
- S3 Public Access Block・EMR の block public access・VPC Block Public Access を、CI の apply の role が外さないこと。これらの削除は作成と同じ Action で行うので、権限で分けられない（同 決定13）
- `project` の上限（234文字）で組んだ255文字の analyzer の名前を、AWS が実際に受けること。入力の検査の境界は `tests/validation.tftest.hcl` が確かめる
- Security Hub のコントロールの判定そのもの。Security Hub はこのユースケースが有効にしない

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-24

- AWS / HashiCorp が publisher の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `account-baseline` / `config-recorder` / `access-analyzer` / `ebs-encryption` / `security-baseline`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`aws-samples`・`hashicorp` が無い）
- 参照した一次資料: AWS SRA（管理アカウント、Security Tooling のアカウント、Log Archive のアカウント、application の節）、Security Hub の FSBP と CIS AWS Foundations Benchmark（v3.0.0・v5.0.0）の対象の一覧、Security Hub の account・config・ec2・emr・iam・macie・s3・ssm のコントロールの頁、AWS のリージョンの管理の文書（既定で有効なリージョン）、Systems Manager User Guide の document の共有と Automation のログ、AWS Provider 6.70.0 の schema（`terraform providers schema -json`）

採用した知見:

- アカウント単位の S3 Public Access Block の4項目と、EBS の既定の暗号化を、全アカウント・全リージョンで有効にする（SRA、S3.1、EC2.7）
- Config を全アカウント・全リージョンで記録し、SLR を使い、グローバルな resource は1つのリージョンでだけ記録する（SRA、Config.1）
- 外部アクセスの analyzer をアカウントごとに置く。組織の analyzer は IAM.28 に数えられない（IAM.28）
- 代替の連絡先（Account.1）、password policy（IAM.7・15・16）、VPC Block Public Access（EC2.172）、document の公開共有の無効化（SSM.7）、Automation のログの CloudWatch Logs への出力（SSM.6）、EMR の block public access（EMR.2）
- 既定のリージョンは無効にできず、opt-in のリージョンだけが集合を広げる（リージョンの管理の文書）

採用しなかった知見と理由:

- 自前の IAM role と `aws:PrincipalOrgID` による Config の配信（Config の文書）: Config.1 の SLR の検査で外れる（[account-baseline/ADR-0102](docs/adr/0102-config-recorder-except-sandbox.md)）
- 組織の trail・GuardDuty・Inspector・Security Hub・Config の集約・組織の analyzer を委任管理者で持つこと（SRA）: `security-tooling` の責務
- sandbox の Config（Config.1、CIS 3.3、SRA）: 下の逸脱
- Macie.1・2、IAM.18、unused access の analyzer、Config rule と conformance pack、EBS の既定の鍵: 上の unsupported

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

| 外れる対象 | 記録 |
| --- | --- |
| Security Hub の Config.1・CIS AWS Foundations Benchmark 3.3・SRA の Config の有効化（sandbox だけ） | [account-baseline/ADR-0102](docs/adr/0102-config-recorder-except-sandbox.md) |

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。reference の lock は 6.73.0。引数名は 6.70.0 の schema で確かめた
- Terraform は `>= 1.7.0, < 2.0.0`
