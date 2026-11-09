# ADR-0201: CI の role の権限を、Provider が呼ぶ API から導いた Action の列挙で与える

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301, repository-wide/ADR-0402

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定16 は、apply の role の権限の与え方（Action の列挙か permissions boundary か）を use-case の ADR に委ねる。同 決定13 は、plan の role に変更系の権限を与えず、apply の role に bootstrap の構成物を変更する権限を与えないことを求める。repository-wide/ADR-0604 (state-split-by-apply-role) 決定8-10 は、plan の role が state にも lock file にも書かないこと、apply の role が自分の root の key と lock file にだけ書けること、apply の role が state Bucket の設定を変更する有効な権限を持たないことを求める。

AWS Provider は、resource の refresh で、設定していない属性の API も呼ぶ。6.70.0 の `aws_s3_bucket` の Read は、Bucket の Policy・ACL・CORS・website・版管理・accelerate・request payment・logging・lifecycle・replication・暗号化・Object Lock・tag を読み、拒否を許容しない。読み取りの権限が1つ欠けると、plan は AccessDenied で失敗する。apply-identity の role の読み取りが欠けると、その role 自身の policy を直す apply が refresh で失敗し、repository-wide/ADR-0602 決定9 の回復の契機になる。

## Decision

1. 3つの role の権限を、role ごとの inline policy に Action を列挙して与える。Allow の文に `*` と `service:*` の Action、`"*"` の Resource、部分的なワイルドカード（`s3:GetBucket*` 等）を使わない。permissions boundary を課さない。ただし、resource-level 権限を持たない Action（`kms:CreateKey`・`kms:ListAliases`）は、`"*"` の Resource を Action ごとの別の文で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定9）。`kms:CreateKey` は鍵の種類（`kms:KeySpec`・`kms:KeyUsage`）の Condition で限る。多リージョンの鍵、外部の鍵素材の鍵（`kms:KeyOrigin` が `AWS_KMS` 以外）、lockout の安全チェックの迂回は Deny で拒む。Provider はこれらの値を既定のとき要求に載せない。KMS の文書は、要求に無い値も既定値で評価するとしているが、実際の要求では確かめていないので、どちらの評価でも既定の作成を妨げない Deny の形にする。
2. 列挙は、identity root の resource について AWS Provider 6.70.0 の source（`internal/service/s3`・`internal/service/iam`・`internal/service/kms`）の Read・Create・Update・Delete が呼ぶ API から導く。対応は下の表が持つ。Provider の版を上げる変更は、この表を見直す。
3. 権限は段4 の構成物（OIDC provider、3つの role と inline policy、state Bucket の段1-4 の設定とその lifecycle、plan の出力の Bucket とその lifecycle）と、段5 の state の鍵（鍵、Key Policy、alias）に要る分に限る。段5 のログの権限は、ログの resource を足す変更が先に足す。3つの role の暗号操作は IAM では与えず、state の鍵の Key Policy が role を名指して許す（repository-wide/ADR-0301 (least-privilege-policy) 決定12）。
4. Resource は、state Bucket と plan の出力の Bucket の ARN とその key、3つの role と OIDC provider の resource 参照で限定する。state の鍵の ARN は、鍵を作る apply より前には無い。鍵の Action は、鍵の ARN を参照できるようになるまで、このアカウントとリージョンの鍵の pattern（`arn:<partition>:kms:<region>:<account>:key/*`）に与える（repository-wide/ADR-0301 (least-privilege-policy) 決定11）。plan の role の読み取りは、`kms:ResourceAliases` の Condition で state の鍵の alias が付いた鍵に限る。apply-identity の role は、鍵を作る apply の途中（alias より前の rotation の設定と、作成直後の読み取り）でも同じ鍵を扱うので、`ForAllValues` の `kms:ResourceAliases` で、alias が無い鍵か state の鍵の alias だけが付いた鍵に限る。alias の無いほかの鍵は限れない。この条件が alias の無い鍵で真になることは KMS の文書によるもので、実際の要求では鍵を作る変更の apply が初めて確かめる。`state-backend` が鍵の ARN を出力した後の変更が、Resource を鍵の ARN に絞る。alias は、入力の `state_key_alias_name`（`state-backend` の出力）の ARN に限る。
5. apply の role に、3つの role と OIDC provider への `iam:*`、identity root の key と lock file への `s3:Put*` と `s3:Delete*`、state Bucket と plan の出力の Bucket への `s3:Put*` と `s3:Delete*`、state の object と plan の出力の object への `s3:Put*` と `s3:DeleteObjectVersion` を Deny で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定8 は Deny の wildcard を許す）。以後 workload の usecase のための Allow を足しても、bootstrap の構成物、identity root の state、2つの Bucket の設定、plan の出力を変えられないようにする。
6. apply-identity の role は、自分を含む3つの role の信頼条件と policy を変更できる。誤った信頼条件が merge されると CI は締め出され、回復は repository-wide/ADR-0602 決定9 の回復の契機になる。
7. managed policy を付けない。`aws_iam_role_policies_exclusive` と `aws_iam_role_policy_attachments_exclusive` で、帯域外で足された policy を次の apply で剥がす。
8. plan の role に、plan の出力の Bucket の `pr/*` と `branch/*` の key への `s3:PutObject` を与える（repository-wide/ADR-0602 決定13 の例外）。Pull Request の plan と保護ブランチの plan を key の接頭辞で分ける。`s3:DeleteObject` と `s3:DeleteObjectVersion` は与えない。plan の出力を state の鍵で暗号化するのに要る `kms:GenerateDataKey` は IAM では与えず、Key Policy が plan の role を名指して許す（決定3）。

### Action の導出

| resource（Provider 6.70.0） | 呼ぶ API | Action | 与える role |
| --- | --- | --- | --- |
| S3 backend（state） | ListObjects、GetObject、PutObject | `s3:ListBucket`、`s3:GetObject`、`s3:PutObject` | 3つ（書き込みは apply 系の自分の key だけ） |
| S3 backend（lock file） | GetObject、PutObject、DeleteObject | `s3:GetObject`、`s3:PutObject`、`s3:DeleteObject` | apply 系の自分の `.tflock` だけ |
| plan の出力 | PutObject | `s3:PutObject` | plan（plan の出力の Bucket の `pr/*` と `branch/*` の object だけ） |
| `aws_s3_bucket_lifecycle_configuration`（2つの Bucket） | Get / Put / DeleteBucketLifecycle | `s3:GetLifecycleConfiguration`、`s3:PutLifecycleConfiguration` | 読みは plan、apply-identity。書きは apply-identity |
| `aws_s3_bucket` の Read | HeadBucket（region の解決を含む） | `s3:ListBucket` | plan、apply-identity |
| 同 | GetBucketPolicy / GetBucketAcl / GetBucketCors / GetBucketWebsite / GetBucketVersioning / GetBucketAccelerateConfiguration / GetBucketRequestPayment / GetBucketLogging / GetBucketLifecycleConfiguration / GetBucketReplication / GetBucketEncryption / GetObjectLockConfiguration / GetBucketTagging | `s3:GetBucketPolicy`、`s3:GetBucketAcl`、`s3:GetBucketCORS`、`s3:GetBucketWebsite`、`s3:GetBucketVersioning`、`s3:GetAccelerateConfiguration`、`s3:GetBucketRequestPayment`、`s3:GetBucketLogging`、`s3:GetLifecycleConfiguration`、`s3:GetReplicationConfiguration`、`s3:GetEncryptionConfiguration`、`s3:GetBucketObjectLockConfiguration`、`s3:GetBucketTagging` | plan、apply-identity |
| `aws_s3_bucket_public_access_block` | Get / Put / DeletePublicAccessBlock | `s3:GetBucketPublicAccessBlock`、`s3:PutBucketPublicAccessBlock` | 読みは plan、apply-identity。書きは apply-identity |
| `aws_s3_bucket_ownership_controls` | Get / Put / DeleteBucketOwnershipControls | `s3:GetBucketOwnershipControls`、`s3:PutBucketOwnershipControls` | 同上 |
| `aws_s3_bucket_versioning` | Get / PutBucketVersioning | `s3:GetBucketVersioning`、`s3:PutBucketVersioning` | 同上 |
| `aws_s3_bucket_server_side_encryption_configuration` | Get / Put / DeleteBucketEncryption | `s3:GetEncryptionConfiguration`、`s3:PutEncryptionConfiguration` | 同上 |
| `aws_s3_bucket_policy` | Get / Put / DeleteBucketPolicy | `s3:GetBucketPolicy`、`s3:PutBucketPolicy`、`s3:DeleteBucketPolicy` | 同上 |
| `aws_iam_openid_connect_provider` | GetOpenIDConnectProvider、ListOpenIDConnectProviderTags | 同名の `iam:` | plan、apply-identity |
| 同 | Create / Delete、AddClientIDTo / RemoveClientIDFrom、UpdateOpenIDConnectProviderThumbprint | 同名の `iam:` | apply-identity |
| `aws_iam_role` | GetRole、ListRolePolicies、GetRolePolicy、ListAttachedRolePolicies、ListRoleTags | 同名の `iam:` | plan、apply-identity |
| 同 | CreateRole、DeleteRole、UpdateRole、UpdateRoleDescription、UpdateAssumeRolePolicy、ListInstanceProfilesForRole、DetachRolePolicy、DeleteRolePolicy | 同名の `iam:` | apply-identity |
| `aws_iam_role_policy` | Get / Put / DeleteRolePolicy | `iam:GetRolePolicy`、`iam:PutRolePolicy`、`iam:DeleteRolePolicy` | 読みは plan、apply-identity。書きは apply-identity |
| `aws_iam_role_policies_exclusive` | ListRolePolicies、DeleteRolePolicy | 同名の `iam:` | 同上 |
| `aws_iam_role_policy_attachments_exclusive` | ListAttachedRolePolicies、DetachRolePolicy | 同名の `iam:` | 同上 |
| `aws_kms_key` | CreateKey | `kms:CreateKey` | apply-identity |
| 同 | DescribeKey、GetKeyPolicy、GetKeyRotationStatus、ListResourceTags | 同名の `kms:` | plan、apply-identity |
| 同 | EnableKeyRotation | `kms:EnableKeyRotation` | apply-identity |
| `aws_kms_key_policy` | PutKeyPolicy（Read は DescribeKey と GetKeyPolicy） | `kms:PutKeyPolicy` | apply-identity |
| `aws_kms_alias` | ListAliases | `kms:ListAliases` | plan、apply-identity |
| 同 | CreateAlias（alias と鍵の両方に要る） | `kms:CreateAlias` | apply-identity |

与えないもの: `s3:CreateBucket` と `s3:DeleteBucket`（2つの Bucket の作り直しを権限の側でも止める）、`iam:AttachRolePolicy`（managed policy を付けない）、`iam:TagRole` などの tag の書き込み（tag を付けない）、`iam:PutRolePermissionsBoundary` と `iam:DeleteRolePermissionsBoundary`、`iam:PassRole`、`iam:RemoveRoleFromInstanceProfile`（instance profile を持たない）。`kms:ScheduleKeyDeletion`・`kms:DisableKey`・`kms:DisableKeyRotation`・`kms:EnableKey`・`kms:UpdateKeyDescription`・`kms:DeleteAlias`・`kms:UpdateAlias`（鍵の作り直しと alias の付け替えを権限の側でも止める）、`kms:TagResource`（tag を付けない）、暗号操作（決定3）。`aws_caller_identity` の `sts:GetCallerIdentity` は権限を要さない。

## 検討した代替案

### 案A: permissions boundary を課し、Allow を広く与える

今の3つの role は IAM の role を新しく作る主体ではなく、boundary の中身を決める材料が無い。boundary の内側の Allow を広く与えると、repository-wide/ADR-0301 決定8 の Action wildcard に当たる。

### 案B: AWS managed policy（`ReadOnlyAccess` 等）を plan の role に付ける

plan の role は Pull Request の作者が任意のコードで使える資格情報であり、アカウントの全 resource を読める。repository-wide/ADR-0301 決定10 に反する。

### 案C: 段5 の権限をまとめて先に入れる

ログの resource の形（#108）が無く、正しく列挙できない。使わない権限を先に与えることになる。鍵の権限は、鍵の形（`modules/kms`）から Provider が呼ぶ API を列挙できるので、鍵を作る変更より前に入れる（決定3）。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中（boundary の中が広い） | 低（全 resource を読める） | 中（未使用の権限） | 高 |
| Testability | 低（有効な権限の計算が要る） | 中 | 中 | 高（文書の完全一致） |
| Contract Clarity | 中 | 低 | 中 | 高 |
| Maintainability | 中 | 高 | 低 | 中（Provider の版ごとに見直す） |
| Scope Control | 中 | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- Provider の版を上げても権限を見直さずに済むこと
- apply-identity の role を、自分の権限を広げられないようにすること

## 保証範囲

- 保証すること: 3つの role の policy の文書が、上の表と決定5 の Deny から組まれていること（Unit Test）
- 保証しないこと: 列挙が Provider の実際の呼び出しを漏れなく覆うこと（段4 の後の人の確認と、CI の plan が見る）。apply の role の有効な権限が bootstrap の構成物の変更を許さないことの検査（Policy Test は未配線）。state の鍵の ARN へ絞るまで、apply-identity の role がこのアカウントの alias の無い鍵の Key Policy と rotation の状態を読み、Key Policy を書き換えられること（決定4）。apply の role が state の鍵の設定を変えられないことは、apply の role に KMS の Allow が無いことだけが支え、Deny は無い

## 検証方法

- Unit Test: `internal/github-oidc/tests/permissions.tftest.hcl` が、3つの role の policy の文書と、inline policy と managed policy の排他を完全一致で確かめる
- 手順: `docs/setup/bootstrap.md` の段4 の手順9 と段5 の手順4 が、apply の後に `aws iam simulate-principal-policy` で、表の Action が plan と apply-identity の role に許され、与えないものと apply の role の bootstrap の構成物への変更が拒否されることを確かめる
- Policy Test（`make tf-policy`）: Allow の Action の `*`・`service:*` の不在（repository-wide/ADR-0301 (least-privilege-policy) 決定8）と、`NotAction`・`NotResource` の Allow の不在、Allow の Resource `"*"` が決定1 の2つの Action（`resource_wildcard_actions` の宣言）に限られること（同 決定9）、KMS の暗号操作と `kms:CreateGrant` の Allow の不在（決定3、同 決定12）
- Policy Test（`make tf-policy`）: plan の role の Allow が読み取りと、plan の出力の Bucket の `pr/*`・`branch/*` への `s3:PutObject` だけであることと、宣言した policy の外から plan の role に権限が付かないこと（決定8）
- Policy Test（未配線。#209）: repository-wide/ADR-0602 の検証方法の、apply の role の有効な権限の項目

## 影響

- 段5 と、管理アカウントに workload の usecase を足す変更は、まずこの policy を広げる変更を merge してから、その権限を使う変更を apply する。権限の変更と、その権限で作る resource を同じ apply に入れると、両者の間に依存が無く失敗しうる
- inline policy は role あたり 10,240 文字（空白を除く）が上限である

## 見直し条件

- AWS Provider の版を上げるとき
- apply の role が IAM の role を作る usecase が入るとき（permissions boundary を決める）
- inline policy の文字数が上限に近づいたとき
- CI の plan か apply が AccessDenied で失敗したとき
- `state-backend` が state の鍵の ARN を出力したとき（決定4 の pattern を鍵の ARN に絞り、apply の role に state の鍵の管理の Action の Deny を足す。repository-wide/ADR-0604 (state-split-by-apply-role) 決定10）
- KMS が `kms:KeyOrigin` の値を足したとき（決定1 の Deny は外部の鍵素材の値を列挙している）
