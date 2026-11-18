# organization-baseline 手順書

## 1. 設定の概要

管理アカウントに AWS Organizations の組織と OU、SCP・RCP・declarative policy、root の資格情報の中央管理、break-glass を置く（[`usecases/organization-baseline/README.md`](../../usecases/organization-baseline/README.md)）。resource は CI の apply-identity の role が apply する。人が行うのは、Terraform が持たない有効化と資格情報の設定、apply の後の確認である。

| 実施者 | 担当 |
| --- | --- |
| リポジトリの管理者 | merge の前の確認と、昇格の Pull Request |
| `organization_admin` の permission set を持つ人 | Identity Center の有効化の後の作業、break-glass の資格情報の設定、apply の後の確認 |
| break-glass の資格情報を持つ人 | 定期の試験 |

`bootstrap.md` の段5 が終わった後に行う。この usecase を足す変更より先に、`deployment-identity` の権限を足す変更が昇格していること。

## 2. 設定完了後の状態

- 管理アカウントが組織の管理アカウントで、Security・Workloads（Prod・NonProd）・Sandbox・PolicyStaging の OU が在る
- Root には組織からの離脱とアカウントの閉鎖の拒否だけが付き、ほかの policy は PolicyStaging に付いている
- root の資格情報の中央管理が有効で、この後に作るメンバーアカウントは root の資格情報を持たない
- 管理アカウントに break-glass の IAM user が2つ在り、それぞれにパスワードとハードウェア MFA が設定されている
- 管理アカウントの password policy が、14文字以上・24世代の再利用の禁止・4種の文字を求める

## 3. 手順

### 3.1 merge の前: 組織が無いことを確かめる

この usecase は組織を作る。既に組織を持つ管理アカウントは unsupported である。

```sh
aws organizations describe-organization
```

`AWSOrganizationsNotInUseException` が返ることを確かめ、結果を Pull Request に書く。組織が返ったら merge しない。

### 3.2 昇格の後: 組織と policy を確かめる

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
2. identity source を選ぶ。外部 IdP を SAML 2.0 でつなぎ、SCIM でユーザーとグループを送ることを勧める（AWS Well-Architected FND02-BP03、SRA）。IdP を持たなければ Identity Center の directory を使う
3. すべてのサインインで MFA を必須にする。フィッシングに強い方式（FIDO2・passkey）を勧める。外部 IdP なら IdP の側で強制する
4. 管理アカウントへ入る人の user を作り、ID を控える

### 3.4 break-glass の資格情報を設定する

Identity Center から `organization_admin` で管理アカウントへ入れるようになった後に行う。メンバーアカウントを作る変更より前に終える。

1. `break-glass-1` と `break-glass-2` のそれぞれに、コンソールのパスワードを設定する（アクセスキーは作らない。group の policy が拒む）
2. それぞれにハードウェア MFA を登録する
3. パスワードと MFA の装置を、別々の安全な場所に保管する
4. 3.5 の試験を1回行う

### 3.5 break-glass を試す（毎月）

45日より短い間隔で行う。CIS AWS Foundations Benchmark は、45日使われていない資格情報を無効にするよう求める。

1. break-glass の user でコンソールにサインインし、MFA を使う
2. 管理アカウントの break-glass の role（`/break-glass/<project>-break-glass`）へ切り替えられることを確かめる
3. サインインと role の引き受けの通知が届くことを確かめる
4. 試した日と結果を記録する

### 3.6 policy を広げる前に PolicyStaging で試す

PolicyStaging に付けた policy の付け先を広げる変更の前に行う。

1. sandbox のアカウントを PolicyStaging へ移す変更を昇格する
2. 拒まれるべき操作が拒まれること、Identity Center と GitHub の OIDC と break-glass の到達が通ることを確かめる
3. sandbox のアカウントを Sandbox へ戻す変更を昇格する
4. 結果を、付け先を広げる Pull Request に書く

declarative policy は、付ける前に account status report で影響を見る。RCP は PolicyStaging では本番の文を試せないので、付けた直後に読み取りで確かめ、通らなければ外す変更を昇格する。
