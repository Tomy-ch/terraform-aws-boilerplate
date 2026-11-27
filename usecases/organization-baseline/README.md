# organization-baseline

AWS Organizations の組織と OU、SCP・RCP・declarative policy、root の資格情報の中央管理、break-glass を、管理アカウントに持つユースケース。統制の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、管理アカウントの identity root だけが呼ぶ（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3、[organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md)）。

## 解決する問題

環境の境界をアカウントで分け（[scope.md 第10.1節](../../docs/v1.0-plan/scope.md)）、メンバーアカウントの中の主体（root と CI の role を含む）が外せない禁止を、組織の側から掛ける。人の通常の到達経路（IAM Identity Center）が使えないときの緊急の経路（break-glass）を持つ（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。管理アカウントの CI の apply-identity の role が、Organizations と IAM の API を呼ぶ。最初の作成だけは、bootstrap の段4 の一時 IAM user が呼ぶ（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1・9）
- Organizations の API は global の endpoint（`organizations.us-east-1.amazonaws.com`）を、IAM は `iam.amazonaws.com` を呼ぶ
- 委任管理者の指定は、GuardDuty・Inspector・Security Hub の API を [repository-wide/ADR-0209](../../docs/adr/0209-default-regions-for-regional-controls.md) の集合の各リージョンの endpoint で、CloudTrail の API を provider のリージョンの endpoint で呼ぶ。宛先は `.github/egress.toml` の `aws-governance` が持つ
- 外への egress は無い

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、38文字以内） | policy の名前 `<project>-<policy>` と break-glass の role の名前 `<project>-break-glass` の先頭。SCP はメンバーの state Bucket と CI の role を `deployment-identity`・`state-backend` と同じ token で名指すので、それらと同じ値を渡す。最も長いアカウント名 `<project>-log-archive` を Organizations の上限の50文字に収める |
| `bootstrapping` | `set(string)`（`security`・`log-archive` のいずれか。既定 `[]`） | 立ち上げ中のメンバーアカウントの env。ここに無いアカウントでは、`OrganizationAccountAccessRole` と立ち上げの読み取りの role の使用を SCP（`deny-bootstrap-role`）が拒む。管理アカウントの identity root が、メンバーの `state-backend`・`deployment-identity` を作り終えるまで入れておき、外す PR で立ち上げを閉じる |
| `bootstrap_read_principal_arn` | `string`（wildcard を含まない IAM role の ARN） | メンバーの立ち上げの読み取りの role（`<project>-bootstrap-read`）を引き受けてよい、管理アカウントの role。管理アカウントの identity root が `deployment-identity` の plan の role の ARN を渡す（[organization-baseline/ADR-0203](docs/adr/0203-member-bootstrap-read-role.md)） |

| 出力 | 使う側 |
| --- | --- |
| `break_glass_arns` | 管理アカウントの break-glass の IAM user 2つ・group・role の ARN。管理アカウントの identity root が `deployment-identity` へ渡し、apply の role にこれらの変更を拒ませる |
| `account_ids` | メンバーアカウントの env から ID への map。メンバーの root と、アカウントを名指す usecase が使う |
| `bootstrapping_account_ids` | `bootstrapping` に入れたメンバーアカウントの env から ID への map。管理アカウントの identity root が、立ち上げの provider の引き受け先に使い、`deployment-identity` の `bootstrap_member_account_ids` へ渡して、引き受けをこのアカウントに限る |
| `bootstrap_read_role` | メンバーへ配る立ち上げの読み取りの role の名前と path（`object({ name, path })`）。管理アカウントの identity root が `deployment-identity` へ渡し、その role を配る StackSet の権限と、plan の role の引き受けの Resource にさせる |
| `member_break_glass_role` | メンバーへ配る break-glass の role の名前と path（`object({ name, path })`）。管理アカウントの identity root が、立ち上げの中のメンバーの `deployment-identity` に渡す break-glass の ARN を組む |

- メンバーアカウントのメールは入力に取らない。管理アカウントの SSM の String の parameter `/<project>/organization-baseline/member-account-emails`（JSON の object。env からメールへ）を読む。人が置く（[`docs/setup/organization-baseline.md`](../../docs/setup/organization-baseline.md)）。key の集合が対応表と一致しないか、値がメールの形でなければ plan が落ちる

- ログの所在: 無い。Organizations と IAM の API の記録は CloudTrail が持ち、その配置は `security-tooling` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。break-glass の user のサインイン、break-glass の role の引き受け、root の使用、`sts:AssumeRoot` の呼び出しの通知は `security-tooling` と `ops-notification` が持つ

## 持つもの

- 組織（all features。SCP・RCP・EC2 の declarative policy を有効にする）と、IAM の信頼アクセス、root の資格情報の中央管理（`RootCredentialsManagement`・`RootSessions`）
- OU の木: Root の直下に Security・Workloads・Sandbox・PolicyStaging、Workloads の下に Prod・NonProd（[organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md)）
- SCP 10個・RCP 2つ・EC2 の declarative policy 1つと、その付け先。Root には組織からの離脱とアカウントの閉鎖の拒否だけを付け、ほかは PolicyStaging に付ける。最終の付け先は [organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) 決定5-7 が持つ
- 管理アカウントの break-glass の IAM user 2つ、group とその policy、break-glass の role（`AdministratorAccess`）、IAM の password policy（[organization-baseline/ADR-0201](docs/adr/0201-break-glass.md)）
- group の policy の `aws login` の2文（同じ端末の流れの `oauth2/public-client/localhost` に限る。[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19 の期間だけ） <!-- v1.0.0 で削除: #141 -->
- メンバーアカウント security と log-archive（Security OU）。env と OU の対応表は `internal/organization/accounts.tf` の1つの local が持つ。`OrganizationAccountAccessRole`・請求情報への IAM の到達・閉じない削除は書かず、AWS と Provider の既定に任せる（Provider の Read はこの3つを state に書かないので、書くと import の後に作り直しの差分が出る）。名前は `<project>-<env>` で変えない
- 立ち上げを終えたメンバーアカウントに付ける `deny-bootstrap-role`（アカウントごと）
- セキュリティのサービス（GuardDuty・Inspector・Security Hub・CloudTrail・AWS Config・IAM Access Analyzer）の委任管理者を security のアカウントにする指定（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）。GuardDuty・Inspector・Security Hub はリージョンごとで、[repository-wide/ADR-0209](../../docs/adr/0209-default-regions-for-regional-controls.md) の集合の各リージョンに置く。CloudTrail は CloudTrail の API で、Config と Access Analyzer は Organizations の API で登録する。委任管理者の側の設定（組織の trail、GuardDuty の組織の設定など）は `security-tooling`（#473）が持つ
- メンバーアカウントの break-glass の role を配る CloudFormation の StackSet（service-managed、自動展開）と、組織の Root を対象にした stack instance（[organization-baseline/ADR-0202](docs/adr/0202-member-break-glass-via-stacksets.md)）
- メンバーアカウントの立ち上げの読み取りの role（`<project>-bootstrap-read`、path `/bootstrap/`）を配る StackSet と stack instance（同じ形。[organization-baseline/ADR-0203](docs/adr/0203-member-bootstrap-read-role.md)）

組織、OU、メンバーアカウント、break-glass の IAM user と role、StackSet と stack instance に `prevent_destroy` を付ける。組織、OU、メンバーアカウント、IAM user、StackSet と stack instance は `conftest/plan/decl.json` の `stateful_protection.protected_types` に載り、CI の plan と apply が削除・置換・管理からの除外を落とす（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定11）。

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
- 信頼アクセスの一覧を Terraform で持つこと。各サービスの側で有効化する。CloudTrail・Config・Access Analyzer の信頼アクセスは、委任管理者の指定より前に人が有効にする（[`docs/setup/organization-baseline.md`](../../docs/setup/organization-baseline.md) の 3.9）
- `aws` のほかの partition。委任管理者を置くリージョンの集合（repository-wide/ADR-0209）を持たないので、plan を止める
- ユーザーとグループを Terraform で持つこと。IdP か Identity Center の directory が持つ
- 管理アカウントの root による変更。SCP が効かない（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11）
- メンバーアカウントの名前の変更。apply-identity の role に `account:PutAccountName` を与えないので、名前が変わる変更は apply で拒まれる
- メンバーアカウントのメールを Terraform で変えること。メールの変更は作り直しになり、`prevent_destroy` が止める。AWS の側で変えてから parameter を合わせる（[`docs/setup/organization-baseline.md`](../../docs/setup/organization-baseline.md)）
- メールを plan から隠すこと。parameter の値は plan の JSON に平文で入る。plan の JSON は暗号化した tfplan の Bucket にだけ置く

## 所有者

- この usecase: 組織・OU・policy とその付け先、root の中央管理、管理アカウントの break-glass と password policy、メンバーアカウントの break-glass の role を配る StackSet
- `account-baseline`: アカウント単位の安全側の設定の停止の拒否（`deny-baseline-stop`）と、opt-in のリージョンの有効化の拒否（`deny-enable-region`）の中身（Security・Workloads・Sandbox。[account-baseline/ADR-0201](../account-baseline/docs/adr/0201-baseline-settings-changed-only-by-ci-apply.md)）。付け先はこの usecase が持つ。アカウントの代替の連絡先（Security Hub の Account.1）。メンバーアカウントの password policy（IAM.7・15・16）。メンバーの apply の role がそれを書く。deployment-identity の `DenyGovernanceChange` は password policy の更新を管理アカウントでだけ拒む（deployment-identity/ADR-0201 決定5）
- `security-tooling`（#473）: 組織の trail・GuardDuty・Inspector を守る拒否の中身
- `audit-evidence`: 監査ログの保管庫と鍵を守る拒否（Security。`protect-log-vault`）の中身

## 依存するユースケースと判断 Issue

- `deployment-identity`: 管理アカウントの apply-identity の role に、この usecase の resource を変える権限と、apply の role にそれを拒ませる Deny が要る。初回は bootstrap の段4 が、権限とこの usecase の resource を同じ apply で作る（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）。以後、この usecase の resource を足す変更は、その権限の変更と同じ昇格でよい（deployment-identity/ADR-0201 の影響）
- 判断 Issue: #21（7つのアカウント、OU、break-glass、Identity Center、データ境界の決定）、#99、#142、#156、#128

## 検証方法

区分は「破棄すると認証や組織の構造を失う」（[scope.md 第11.1節](../../docs/v1.0-plan/scope.md)、[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14）。sandbox での apply と destroy、残存検出を行わない。代わりに次を行う。

- Unit Test（`terraform test` + `mock_provider`）: `internal/organization` が OU の木、policy の文書と付け先、文書の大きさ、メンバーアカウントの名前・メール・置く OU と立ち上げの SCP の付け先を、`internal/break-glass` が user・group・role・password policy と、メンバーの role を配る StackSet を、`internal/bootstrap-access` が立ち上げの読み取りの role を配る StackSet を固定する。`tests/` が入力の検査と出力の配線を固定する
- Policy Test: `plan.organization_baseline_scp`（`PLAN-ORG-*`）と `plan.organization_baseline_break_glass`（`PLAN-BG-*`）と `plan.organization_baseline_bootstrap_read`（`PLAN-BR-*`）と `plan.iam_role_attachment_pairing`（`PLAN-ATTACH-*`）が、reference の mock の plan で見る。実 plan には掛けない
- 拒まれるべき操作が拒まれることを人が確かめる（下の手順）。結果を Pull Request に記録し、行わなかったものは行わなかったと書く
- drift が0であること: 昇格の後の次の plan が変更を示さないこと

人が行う手順は [`docs/setup/organization-baseline.md`](../../docs/setup/organization-baseline.md) が持つ。break-glass を実際に使えることは、45日より短い間隔（毎月）で人が確かめる。

破棄しない形で検出できなくなる故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

| 故障モード | 起きたときの症状 | 代わりに検出するもの |
| --- | --- | --- |
| root の中央管理の有効化が、アカウントの作成より後になる | 既存のメンバーに root の資格情報が残る | `depends_on` と、人の確認（`aws iam list-organizations-features`） |
| アカウントの作成が途中で失敗し、アカウントは在るが state に無い | 再実行がメールの重複で落ちる | 人が手順書の回復（`import` block の Pull Request）で取り込む |
| 新しいアカウントへの break-glass の StackSet の自動展開が失敗する | 新しいアカウントへ break-glass で入れない | 昇格の後の人の確認（stack instance が `CURRENT`、引き受け） |
| 立ち上げの読み取りの role の読み取りが、立ち上げの plan に足りない | 立ち上げの CI の plan が `AccessDenied` で落ちる（書き込みには広がらない） | `PLAN-BR-04`（plan の role の読みとの突き合わせ）と `PLAN-BR-07`（reference のメンバーの形の instance の構成物の ARN ごとの、Action と Resource の組）と、最初の立ち上げの CI の plan |
| `aws:PrincipalArn` の条件が、assumed-role のセッションや root のセッションに一致しない | SCP が拒まない、または回復の経路を拒む | Policy Test は文書の形までしか見ない。PolicyStaging での人の確認 |
| 実 API が読み戻した policy の文書が宣言と違う | plan が毎回差分を出す | 昇格の後の plan |
| RCP が想定しない AWS のサービスの経路を止める | 配送や連携が止まる | PolicyStaging での確認と、付けた直後の読み取りの確認 |
| declarative policy が既存の EC2 の設定と衝突する | 起動や API が拒まれる | 付ける前の account status report |

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19（break-glass の role の attachment の項は 2026-11-20、`aws login` の項は 2026-11-27）

- AWS / HashiCorp が publisher の Registry module: `aws`・`aws-ia`・`hashicorp` の namespace の一覧から引いた
- 参照した一次資料: AWS Provider v6.70.0 の文書と source（`organizations_organization`・`organizations_aws_service_access`・`iam_organizations_features`・`organizations_policy`）、AWS Provider v6.73.0 の `aws_iam_role`・`aws_iam_role_policy_attachment`・`aws_iam_role_policy_attachments_exclusive` の文書と `internal/service/iam/role.go`、AWS Organizations User Guide（管理アカウントのベストプラクティス、SCP、RCP、declarative policy、既定の統制、信頼アクセス、クォータ）、IAM User Guide（root の中央管理、`aws:AssumedRoot`）、whitepaper「Organizing Your AWS Environment Using Multiple Accounts」、AWS Security Reference Architecture、Well-Architected Framework（2026-11-16 の版。FND01・FND02・FND05）、Security Hub の標準（FSBP、CIS AWS Foundations Benchmark v3.0.0・v5.0.0）

採用した知見:

- `aws_service_access_principals` は `Optional` で `Computed` でないので書かず、変更を無視する。IAM の信頼アクセスだけ `aws_organizations_aws_service_access` で持つ。policy の `type` に `RESOURCE_CONTROL_POLICY`・`DECLARATIVE_POLICY_EC2`（AWS Provider v6.70.0 の文書と source）
- Root への `DenyLeaveOrganizationAndCloseAccount` と同じ文、新しい policy を Root に試験なしに付けない、信頼アクセスをサービスの側で有効化する、SCP の文書は10,240文字・RCP は5,120文字まで（AWS Organizations User Guide）
- `deny-root-user` を `Null aws:AssumedRoot` の形で書く（IAM User Guide）
- OU の木（Security・Workloads（Prod・NonProd）・Sandbox・Policy Staging）、break-glass を管理アカウントの IAM user で持つ形（whitepaper）
- Security Tooling・Log Archive のアカウントの分離（SRA）
- メンバーの root と IAM user を SCP で縛る、データ境界の SCP と RCP、検知の停止の拒否（Well-Architected Framework）
- break-glass の role に `AdministratorAccess` を `aws_iam_role_policy_attachment` で付け、排他の resource を併用して同じ ARN を載せる（AWS Provider v6.73.0、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定21）
- IAM.2（group で権限を与える）、IAM.7・IAM.15・IAM.16（password policy）（Security Hub の標準）
- `aws login` の権限を `signin:AuthorizeOAuth2Access` と `signin:CreateOAuth2Token` の2つにし、Resource を resource type `oauth2-public-client-localhost`（`arn:${Partition}:signin:${Region}:${Account}:oauth2/public-client/localhost`）に限る。`signin:OAuthGrantType` は `CreateOAuth2Token` にだけ在る条件キーなので、文を分ける（AWS CLI User Guide の `aws login`、Service Authorization Reference の AWS Sign-In、AWS Sign-In condition keys reference） <!-- v1.0.0 で削除: #141 -->
- `aws-ia/iam-identity-center` を permission set と割当の形の参照先とする（Registry module）

採用しなかった知見と理由:

- `aws_service_access_principals` の一覧: 併用で互いに上書きする
- allow-list の SCP: [organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) 案B
- Infrastructure・Suspended の OU（whitepaper）: 要るまで作らない
- Identity Center と root の中央管理の委任、Infrastructure の OU（SRA）: [organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md)、[organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md)
- 専用の非常用アカウント（FND05-BP02）: [organization-baseline/ADR-0201](docs/adr/0201-break-glass.md)
- `force_detach_policies`: role の削除だけに効く。role は `prevent_destroy`
- `aws-ia/control_tower_account_factory`: AWS Control Tower を前提とする。この usecase は Control Tower を使わない
- Registry module を依存として使うこと: [ADR-0205](../../docs/adr/0205-module-dependency-policy.md)
- `signin:MaxSigninSessionDuration` で sign-in のセッションの長さを限る Deny: 要求を拒むのか長さを縮めるのかが文書から一意に読めない。role の `MaxSessionDuration`（1時間）と `aws logout` に頼る <!-- v1.0.0 で削除: #141 -->
- `signin:OAuthRedirectUri` の限定: CLI が使う redirect URI の形を文書で確かめていない <!-- v1.0.0 で削除: #141 -->
- managed policy `SignInLocalDevelopmentAccess`: Resource を `localhost` の client に限れない <!-- v1.0.0 で削除: #141 -->

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

| 外れる対象 | 記録 |
| --- | --- |
| SRA: Identity Center と root の中央管理の委任、identity source を企業の IdP に限ること、Identity Center の multi-Region | [organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md) |
| AWS Organizations: 信頼アクセスをサービスの側で有効化する（IAM だけ Terraform で持つ） | [organization-baseline/ADR-0001](docs/adr/0001-governance-in-management-identity-root.md) 決定5 |
| SRA: Infrastructure の OU。allow-list の SCP の併用 | [organization-baseline/ADR-0101](docs/adr/0101-organizational-units-and-policies.md) |
| Well-Architected FND05-BP02: 専用の非常用アカウント。SRA: IAM user を使わない | [organization-baseline/ADR-0201](docs/adr/0201-break-glass.md) |
| FSBP CloudFormation.4: 立ち上げの読み取りの role の StackSet の stack が service role を持たない | [organization-baseline/ADR-0203](docs/adr/0203-member-bootstrap-read-role.md) 決定7 |

## 版

- Terraform: `>= 1.7.0, < 2.0.0`
- AWS Provider: `>= 6.68.0, < 7.0.0`（reference の lock の版は `providers/` の正本が持つ）
