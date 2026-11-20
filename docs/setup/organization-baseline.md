# organization-baseline 手順書

## 1. 設定の概要

管理アカウントに AWS Organizations の組織と OU、SCP・RCP・declarative policy、root の資格情報の中央管理、break-glass を置く（[`usecases/organization-baseline/README.md`](../../usecases/organization-baseline/README.md)）。resource は bootstrap の段4 が管理アカウントの identity root の apply で作り（[ADR-0602](../adr/0602-bootstrap-and-ci-authentication.md) 決定1）、以後の変更は CI の apply-identity の role が apply する。人が行うのは、Terraform が持たない有効化と資格情報の設定、apply の後の確認である。

| 実施者 | 担当 |
| --- | --- |
| リポジトリの管理者 | 昇格の Pull Request |
| `organization_admin` の permission set を持つ人 | Identity Center の有効化の後の作業、break-glass の資格情報の設定、apply の後の確認 |
| break-glass の資格情報を持つ人 | 定期の試験 |

[`bootstrap.md`](bootstrap.md) の段5 が終わった後に、3.2 から行う。

## 2. 設定完了後の状態

- 管理アカウントが組織の管理アカウントで、Security・Workloads（Prod・NonProd）・Sandbox・PolicyStaging の OU が在る
- Root には組織からの離脱とアカウントの閉鎖の拒否だけが付き、ほかの policy は PolicyStaging に付いている
- root の資格情報の中央管理が有効で、この後に作るメンバーアカウントは root の資格情報を持たない
- 管理アカウントに break-glass の IAM user が2つ在り、それぞれにパスワードとハードウェア MFA が設定されている
- 管理アカウントの password policy が、14文字以上・24世代の再利用の禁止・4種の文字を求める

## 3. 手順

### 3.1 段4 の前: 組織が無いことを確かめる

この usecase は組織を作る。既に組織を持つ管理アカウントは unsupported である。組織と break-glass の IAM user・group・role がまだ無いことは、`bootstrap.md` の段2 と段4 の `verify.sh before-apply` が確かめる。

### 3.2 段5 の後: 組織と policy を確かめる

```sh
aws organizations describe-organization --query 'Organization.[Id,FeatureSet]'
aws organizations list-roots --query 'Roots[0].PolicyTypes'
aws organizations list-organizational-units-for-parent --parent-id <Root の ID>
aws iam list-organizations-features
```

- `FeatureSet` が `ALL`、Root の policy の種類に `SERVICE_CONTROL_POLICY`・`RESOURCE_CONTROL_POLICY`・`DECLARATIVE_POLICY_EC2` が `ENABLED` で在る
- Root の直下に Security・Workloads・Sandbox・PolicyStaging が在る
- `EnabledFeatures` に `RootCredentialsManagement` と `RootSessions` が在る

次の CI の plan が変更を示さないことを確かめる。

### 3.3 Identity Center を有効にする

1. 管理アカウントのコンソールで、リージョンを `ap-northeast-1` にして IAM Identity Center を有効にする。組織のインスタンスとし、鍵は AWS owned key のままにする
   - 「インスタンス設定」は「単一リージョン」を選ぶ。「マルチリージョン」と「カスタム」は選ばない（[organization-baseline/ADR-0001](../../usecases/organization-baseline/docs/adr/0001-governance-in-management-identity-root.md) 決定4。multi-Region は customer managed key を要する）
2. identity source を選ぶ。外部 IdP を SAML 2.0 でつなぎ、SCIM でユーザーとグループを送ることを勧める（AWS Well-Architected FND02-BP03、SRA）。IdP を持たなければ Identity Center の directory を使う
   - 「設定」→「アイデンティティソース」タブ →「アクション」→「アイデンティティソースを変更」で選び、確認の画面で `ACCEPT` と入力する。有効にした直後の identity source は「Identity Center ディレクトリ」である
   - 現在の値が「Identity Center ディレクトリ」以外（外部 ID プロバイダーや Active Directory を以前につないだ組織）で directory へ戻すときは、同じ操作で「Identity Center ディレクトリ」を選ぶ。IAM Identity Center User Guide の「Change your identity source」と「Considerations for changing your identity source」は、次のように述べる
     - ユーザー・グループ・メンバーシップ・割当は残る。ただし同じ文書は、構成によっては割当と permission set の IAM role が消えうるとも警告する。確認の画面の説明を読んでから `ACCEPT` を入力する
     - 外部 ID プロバイダーから戻す場合、Identity Center にパスワードを持たなかったユーザーは、管理者がパスワードのリセットを強制するまでサインインできない
     - Active Directory から戻す場合、`d-xxxxxxxxxx.awsapps.com/start` の URL とその別名は使えなくなる。MFA の設定は残る
   - 組織として管理している IdP（独自ドメインの Google Workspace、Microsoft Entra ID、Okta など）があれば「外部 ID プロバイダー」を選ぶ。個人の Gmail や Microsoft アカウントは SAML の IdP にならない
   - 「Active Directory」は AWS Managed Microsoft AD か AD Connector を要し、常に費用がかかるので選ばない
   - 同じメニューの「AWS アクセスポータル URL をカスタマイズ」は任意である。サブドメインは一度決めると変えられない
3. すべてのサインインで MFA を必須にする。フィッシングに強い方式（FIDO2・passkey）を勧める。外部 IdP なら IdP の側で強制する
   - 「設定」→「認証」タブ →「多要素認証」の「設定」で選ぶ
   - MFA を求めるタイミング: 「サインインごと（常時オン）」
   - 使える MFA のタイプ: 「セキュリティキーと組み込みの認証子」。セキュリティキーも passkey も持たない間は「認証アプリ」も許してよい
   - MFA デバイスを持たないユーザー: 「サインイン時に MFA デバイスの登録を求める」
4. 管理アカウントへ入る人の user を作り、ID を控える
   - user は組織全体で1人につき1回だけ作る。どのアカウントへ入れるかは permission set の割当が決めるので、user の名前に環境の接尾辞（`-admin`・`-dev` など）を付けない
   - 「ユーザーを追加」では、ユーザー名・メールアドレス・名・姓・表示名だけを入れる。任意の「お問い合わせ方法」「ジョブ関連情報」「アドレス」「環境設定」「追加の属性」（日本語のコンソールでの表記）は空のままにする。追加の属性は属性で権限を分ける ABAC のためのもので、この構成は使わない。CIS AWS Foundations Benchmark 1.1・1.2 の連絡先は AWS アカウントの代替連絡先で、[`bootstrap.md`](bootstrap.md#2-1-連絡先を登録する) の 2-1 で登録する
   - 招待のメールから AWS access portal でパスワードと MFA を登録し、サインインできることを確かめる。割り当てたアカウントが無い間、ポータルには何も表示されない
   - user ID は access portal では見られない。管理アカウントのコンソールへ戻り、IAM Identity Center の「ユーザー」から作った user を開き、そのユーザー ID（`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` の形）を控える

### 3.4 break-glass の資格情報を設定する

Identity Center から `organization_admin` で管理アカウントへ入れるようになった後に行う。メンバーアカウントを作る変更より前に終える。

1. `break-glass-1` と `break-glass-2` のそれぞれに、コンソールのパスワードを設定する（アクセスキーは作らない。group の policy が拒む）
2. それぞれにハードウェア MFA を登録する
3. パスワードと MFA の装置を、別々の安全な場所に保管する
4. 3.6 の試験を1回行う

### 3.5 StackSets の信頼アクセスを有効にする

このリポジトリを新しく使う管理アカウントでは、組織の作成と StackSet の作成が同じ apply に入る。その apply は、信頼アクセスが無いために StackSet の作成で止まる（組織と他の resource は作られ、StackSet は state に残らない）。止まったら次の手順で信頼アクセスを有効にし、同じ apply をやり直す（[bootstrap 手順書](bootstrap.md) の段4 の手順7）。

メンバーアカウントへ break-glass の role を配る変更（CloudFormation の StackSet を作る変更）の merge より前に行う。`deployment-identity` に StackSets の権限を足す変更が昇格した後でよい。StackSet は、組織への信頼アクセスが無いと作れない。

1. 管理アカウントのコンソールで、`ap-northeast-1` の CloudFormation を開き、「StackSets」を選ぶ
2. 「AWS Organizations との信頼されたアクセスを有効にする」を選ぶ（API の `ActivateOrganizationsAccess`）
3. IAM の role の一覧に `AWSServiceRoleForCloudFormationStackSetsOrgAdmin` が在ることを確かめ、結果を Pull Request に書く

AWS CLI を使える管理アカウントの資格情報が在るなら、`aws cloudformation activate-organizations-access` と `aws cloudformation describe-organizations-access`（`Status` が `ENABLED`）でもよい。

メンバーアカウントを作る変更が昇格した後に、次を確かめる。

1. CloudFormation の「StackSets」で `<project>-break-glass` を開き、「スタックインスタンス」のすべてのメンバーアカウントの状態が `CURRENT` であることを確かめる
2. 3.6 の試験で、メンバーアカウントの break-glass の role へも切り替えられることを確かめ、結果を Pull Request に書く

StackSet か stack instance の作成で昇格の apply が失敗したときは、次の順に直す。失敗した resource は state に tainted で残り、次の plan は作り直しを求めて `prevent_destroy` で止まる。CI の role は削除の権限を持たないので、tainted は人が外す。apply は人が行わず、CI の経路でやり直す。

1. CloudFormation の「StackSets」で `<project>-break-glass` の「オペレーション」を開き、失敗した操作とアカウントの理由を見る
2. 原因を直す。コードの変更が要るなら、それを Pull Request で直して昇格する
3. `organization_admin` の資格情報で `env/admin/identity` を `terraform init` し、失敗した resource の tainted を外す（`terraform untaint 'module.organization_baseline.module.break_glass.aws_cloudformation_stack_instances.this'`。StackSet が失敗したなら `aws_cloudformation_stack_set.this` も）
4. CI の plan が作り直しを示さないことを確かめ、昇格の apply をやり直す

### 3.6 break-glass を試す（毎月）

45日より短い間隔で行う。CIS AWS Foundations Benchmark は、45日使われていない資格情報を無効にするよう求める。

1. break-glass の user でコンソールにサインインし、MFA を使う
2. 管理アカウントの break-glass の role（`/break-glass/<project>-break-glass`）へ切り替えられることを確かめる。メンバーアカウントが在れば、そのうち1つの同じ名前の role へも切り替える
3. サインインと role の引き受けの通知が届くことを確かめる
4. 試した日と結果を記録する

### 3.7 policy を広げる前に PolicyStaging で試す

PolicyStaging に付けた policy の付け先を広げる変更の前に行う。

1. sandbox のアカウントを PolicyStaging へ移す変更を昇格する
2. 拒まれるべき操作が拒まれること、Identity Center と GitHub の OIDC と break-glass の到達が通ることを確かめる
3. sandbox のアカウントを Sandbox へ戻す変更を昇格する
4. 結果を、付け先を広げる Pull Request に書く

declarative policy は、付ける前に account status report で影響を見る。RCP は PolicyStaging では本番の文を試せないので、付けた直後に読み取りで確かめ、通らなければ外す変更を昇格する。
