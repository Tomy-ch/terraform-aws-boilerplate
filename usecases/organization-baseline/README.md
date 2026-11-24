# organization-baseline

AWS Organizations の組織と OU、SCP・RCP・declarative policy、root の資格情報の中央管理、break-glass を、管理アカウントに持つユースケース。統制の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、管理アカウントの identity root だけが呼ぶ（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3、[organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md)）。

## 解決する問題

環境の境界をアカウントで分け（[scope.md 第10.1節](../../docs/v1.0-plan/scope.md)）、メンバーアカウントの中の主体（root と CI の role を含む）が外せない禁止を、組織の側から掛ける。人の通常の到達経路（IAM Identity Center）が使えないときの緊急の経路（break-glass）を持つ（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。管理アカウントの CI の apply-identity の role が、Organizations と IAM の API を呼ぶ。最初の作成だけは、bootstrap の段4 の一時 IAM user が呼ぶ（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1・9）
- Organizations の API は global の endpoint（`organizations.us-east-1.amazonaws.com`）を、IAM は `iam.amazonaws.com` を呼ぶ
- 外への egress は無い

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、38文字以内） | policy の名前 `<project>-<policy>` と break-glass の role の名前 `<project>-break-glass` の先頭。SCP はメンバーの state Bucket と CI の role を `deployment-identity`・`state-backend` と同じ token で名指すので、それらと同じ値を渡す。最も長いアカウント名 `<project>-log-archive` を Organizations の上限の50文字に収める |

| 出力 | 使う側 |
| --- | --- |
| `break_glass_arns` | 管理アカウントの break-glass の IAM user 2つ・group・role の ARN。管理アカウントの identity root が `deployment-identity` へ渡し、apply の role にこれらの変更を拒ませる |

- ログの所在: 無い。Organizations と IAM の API の記録は CloudTrail が持ち、その配置は `security-tooling` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。break-glass の user のサインイン、break-glass の role の引き受け、root の使用、`sts:AssumeRoot` の呼び出しの通知は `security-tooling` と `ops-notification` が持つ

## 持つもの

- 組織（all features。SCP・RCP・EC2 の declarative policy を有効にする）と、IAM の信頼アクセス、root の資格情報の中央管理（`RootCredentialsManagement`・`RootSessions`）
- OU の木: Root の直下に Security・Workloads・Sandbox・PolicyStaging、Workloads の下に Prod・NonProd（[organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md)）
- SCP 10個・RCP 2つ・EC2 の declarative policy 1つと、その付け先。Root には組織からの離脱とアカウントの閉鎖の拒否だけを付け、ほかは PolicyStaging に付ける。最終の付け先は [organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) 決定5-7 が持つ
- 管理アカウントの break-glass の IAM user 2つ、group とその policy、break-glass の role（`AdministratorAccess`）、IAM の password policy（[organization-baseline/ADR-0201](docs/adr/0201-break-glass.md)）
- メンバーアカウントの break-glass の role を配る CloudFormation の StackSet（service-managed、自動展開）と、組織の Root を対象にした stack instance（[organization-baseline/ADR-0202](docs/adr/0202-member-break-glass-via-stacksets.md)）

組織、OU、break-glass の IAM user と role、StackSet と stack instance に `prevent_destroy` を付ける。組織、OU、IAM user、StackSet と stack instance は `conftest/plan/decl.json` の `stateful_protection.protected_types` に載り、CI の plan と apply が削除・置換・管理からの除外を落とす（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定11）。

## supported / unsupported

supported:

- 管理アカウントが1つの組織で、7つのアカウント（管理・security・log-archive・sandbox・dev・stg・prd）を持つ構成（[scope.md 第10.1節](../../docs/v1.0-plan/scope.md)）
- 組織をこの usecase の apply で作る構成

unsupported:

- 同一アカウント構成（1つのアカウントに複数の環境の役割を持たせる構成と、組織を作らない構成。#99）
- 既に組織を持つ管理アカウント。組織を `import` で取り込む手順は持たない
- IAM Identity Center を provider のリージョン（`ap-northeast-1`）のほかで有効にした組織
- Identity Center の管理、root の資格情報の中央管理の委任（[organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md)）
- 利用者が SCP・RCP・declarative policy の中身や付け先を入力で変えること
- 信頼アクセスの一覧を Terraform で持つこと。各サービスの側で有効化する
- ユーザーとグループを Terraform で持つこと。IdP か Identity Center の directory が持つ
- 管理アカウントの root による変更。SCP が効かない（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11）

## 所有者

- この usecase: 組織・OU・policy とその付け先、root の中央管理、管理アカウントの break-glass と password policy、メンバーアカウントの break-glass の role を配る StackSet
- `account-baseline`: アカウント単位の安全側の設定の停止の拒否（`deny-baseline-stop`）と、opt-in のリージョンの有効化の拒否（`deny-enable-region`）の中身（Security・Workloads・Sandbox。[account-baseline/ADR-0201](../account-baseline/docs/adr/0201-baseline-settings-changed-only-by-ci-apply.md)）。付け先はこの usecase が持つ。アカウントの代替の連絡先（Security Hub の Account.1）。メンバーアカウントの password policy（IAM.7・15・16）。メンバーの apply の role がそれを書く。deployment-identity の `DenyGovernanceChange` は password policy の更新を管理アカウントでだけ拒む（deployment-identity/ADR-0201 決定5）
- `security-tooling`（#473）: 組織の trail・GuardDuty・Inspector を守る拒否の中身
- `audit-evidence`: 監査ログの保管庫と鍵を守る拒否（Security。`protect-log-vault`）の中身

## 依存するユースケースと判断 Issue

- `deployment-identity`: 管理アカウントの apply-identity の role に、この usecase の resource を変える権限と、apply の role にそれを拒ませる Deny が要る。初回は bootstrap の段4 が、権限とこの usecase の resource を同じ apply で作る（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）。以後、この usecase の resource を足す変更は、その権限の変更を先に昇格してから昇格する
- 判断 Issue: #21（7つのアカウント、OU、break-glass、Identity Center、データ境界の決定）、#99、#142、#156、#128

## 検証方法

区分は「破棄すると認証や組織の構造を失う」（[scope.md 第11.1節](../../docs/v1.0-plan/scope.md)、[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14）。sandbox での apply と destroy、残存検出を行わない。代わりに次を行う。

- Unit Test（`terraform test` + `mock_provider`）: `internal/organization` が OU の木、policy の文書と付け先、文書の大きさを、`internal/break-glass` が user・group・role・password policy と、メンバーの role を配る StackSet を固定する。`tests/` が入力の検査と出力の配線を固定する
- Policy Test: `plan.organization_baseline_scp`（`PLAN-ORG-*`）と `plan.organization_baseline_break_glass`（`PLAN-BG-*`）と `plan.iam_role_attachment_pairing`（`PLAN-ATTACH-*`）が、reference の mock の plan で見る。実 plan には掛けない
- 拒まれるべき操作が拒まれることを人が確かめる（下の手順）。結果を Pull Request に記録し、行わなかったものは行わなかったと書く
- drift が0であること: 昇格の後の次の plan が変更を示さないこと

人が行う手順は [`docs/setup/organization-baseline.md`](../../docs/setup/organization-baseline.md) が持つ。break-glass を実際に使えることは、45日より短い間隔（毎月）で人が確かめる。

破棄しない形で検出できなくなる故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

| 故障モード | 起きたときの症状 | 代わりに検出するもの |
| --- | --- | --- |
| root の中央管理の有効化が、アカウントの作成より後になる | 既存のメンバーに root の資格情報が残る | `depends_on` と、人の確認（`aws iam list-organizations-features`） |
| `aws:PrincipalArn` の条件が、assumed-role のセッションや root のセッションに一致しない | SCP が拒まない、または回復の経路を拒む | Policy Test は文書の形までしか見ない。PolicyStaging での人の確認 |
| 実 API が読み戻した policy の文書が宣言と違う | plan が毎回差分を出す | 昇格の後の plan |
| RCP が想定しない AWS のサービスの経路を止める | 配送や連携が止まる | PolicyStaging での確認と、付けた直後の読み取りの確認 |
| declarative policy が既存の EC2 の設定と衝突する | 起動や API が拒まれる | 付ける前の account status report |

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

2026-11-19 に確認した。

| 参照 | 採ったもの | 採らなかったもの（理由） |
| --- | --- | --- |
| AWS Provider v6.70.0 の文書と source（`organizations_organization`・`organizations_aws_service_access`・`iam_organizations_features`・`organizations_policy`） | `aws_service_access_principals` は `Optional` で `Computed` でないので書かず、変更を無視する。IAM の信頼アクセスだけ `aws_organizations_aws_service_access` で持つ。policy の `type` に `RESOURCE_CONTROL_POLICY`・`DECLARATIVE_POLICY_EC2` | `aws_service_access_principals` の一覧（併用で互いに上書きする） |
| AWS Organizations User Guide（管理アカウントのベストプラクティス、SCP、RCP、declarative policy、既定の統制、信頼アクセス、クォータ） | Root への `DenyLeaveOrganizationAndCloseAccount` と同じ文、新しい policy を Root に試験なしに付けない、信頼アクセスをサービスの側で有効化する、SCP の文書は10,240文字・RCP は5,120文字まで | allow-list の SCP（[organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) 案B） |
| IAM User Guide（root の中央管理、`aws:AssumedRoot`） | `deny-root-user` を `Null aws:AssumedRoot` の形で書く | — |
| whitepaper「Organizing Your AWS Environment Using Multiple Accounts」 | OU の木（Security・Workloads（Prod・NonProd）・Sandbox・Policy Staging）、break-glass を管理アカウントの IAM user で持つ形 | Infrastructure・Suspended の OU（要るまで作らない） |
| AWS Security Reference Architecture | Security Tooling・Log Archive のアカウントの分離 | Identity Center と root の中央管理の委任、Infrastructure の OU（[organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md)、[organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md)） |
| Well-Architected Framework（2026-11-16 の版。FND01・FND02・FND05） | メンバーの root と IAM user を SCP で縛る、データ境界の SCP と RCP、検知の停止の拒否 | 専用の非常用アカウント（FND05-BP02。[organization-baseline/ADR-0201](docs/adr/0201-break-glass.md)） |
| AWS Provider v6.73.0 の `aws_iam_role`・`aws_iam_role_policy_attachment`・`aws_iam_role_policy_attachments_exclusive` の文書と `internal/service/iam/role.go`（2026-11-20） | break-glass の role に `AdministratorAccess` を `aws_iam_role_policy_attachment` で付け、排他の resource を併用して同じ ARN を載せる（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定21） | `force_detach_policies`（role の削除だけに効く。role は `prevent_destroy`） |
| Security Hub の標準（FSBP、CIS AWS Foundations Benchmark v3.0.0・v5.0.0） | IAM.2（group で権限を与える）、IAM.7・IAM.15・IAM.16（password policy） | — |
| Terraform Registry の AWS・HashiCorp が publisher の module（`aws`・`aws-ia`・`hashicorp` の namespace の一覧から） | `aws-ia/iam-identity-center` を permission set と割当の形の参照先とする | `aws-ia/control_tower_account_factory`（AWS Control Tower を前提とする。この usecase は Control Tower を使わない）。依存として使うことはしない（[ADR-0205](../../docs/adr/0205-module-dependency-policy.md)） |

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

| 外れる対象 | 記録 |
| --- | --- |
| SRA: Identity Center と root の中央管理の委任、identity source を企業の IdP に限ること、Identity Center の multi-Region | [organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md) |
| AWS Organizations: 信頼アクセスをサービスの側で有効化する（IAM だけ Terraform で持つ） | [organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md) 決定5 |
| SRA: Infrastructure の OU。allow-list の SCP の併用 | [organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) |
| Well-Architected FND05-BP02: 専用の非常用アカウント。SRA: IAM user を使わない | [organization-baseline/ADR-0201](docs/adr/0201-break-glass.md) |

## 版

- Terraform: `>= 1.7.0, < 2.0.0`
- AWS Provider: `>= 6.68.0, < 7.0.0`（reference の lock は 6.70.0）
