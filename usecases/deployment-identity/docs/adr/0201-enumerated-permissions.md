# ADR-0201: CI の role の権限を、Provider が呼ぶ API から導いた Action の列挙で与える

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301, repository-wide/ADR-0402

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定16 は、apply の role の権限の与え方（Action の列挙か permissions boundary か）を use-case の ADR に委ねる。同 決定13 は、plan の role に変更系の権限を与えず、apply の role に bootstrap の構成物を変更する権限を与えないことを求める。repository-wide/ADR-0604 (state-split-by-apply-role) 決定8-10 は、plan の role が state にも lock file にも書かないこと、apply の role が自分の root の key と lock file にだけ書けること、apply の role が state Bucket の設定を変更する有効な権限を持たないことを求める。同 決定13 は、lock の解除の role に、lock file の一覧・読み取り・削除と、その lock file の復号だけを持たせる。

AWS Provider は、resource の refresh で、設定していない属性の API も呼ぶ。6.70.0 の `aws_s3_bucket` の Read は、Bucket の Policy・ACL・CORS・website・版管理・accelerate・request payment・logging・lifecycle・replication・暗号化・Object Lock・tag を読み、拒否を許容しない。読み取りの権限が1つ欠けると、plan は AccessDenied で失敗する。apply-identity の role の読み取りが欠けると、その role 自身の policy を直す apply が refresh で失敗し、repository-wide/ADR-0602 決定9 の回復の契機になる。

## Decision

1. 4つの role の権限を、role ごとの inline policy に Action を列挙して与える。Allow の文に `*` と `service:*` の Action、`"*"` の Resource、部分的なワイルドカード（`s3:GetBucket*` 等）を使わない。permissions boundary を課さない。ただし、resource-level 権限を持たない Action（`kms:ListAliases`）は、`"*"` の Resource を Action ごとの別の文で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定9）。Key Policy の書き込みでの lockout の安全チェックの迂回（`kms:BypassPolicyLockoutSafetyCheck`）は Deny で拒む。
2. 列挙は、identity root の resource について AWS Provider 6.70.0 の source（`internal/service/s3`・`internal/service/iam`・`internal/service/kms`）の Read・Create・Update・Delete が呼ぶ API から導く。対応は下の表が持つ。Provider の版を上げる変更は、この表を見直す。
3. 権限は段4 の構成物（OIDC provider、4つの role と inline policy、state Bucket の段1-4 の設定とその lifecycle、plan の出力の Bucket とその lifecycle、state の鍵（鍵、Key Policy、alias））に要る分に限る。state の鍵と alias を作る権限は与えない。新しいアカウントでは段4 の apply が一時 IAM user の権限で作る。鍵と alias の作り直しは CI では行えず、要るときは作成の権限を足す変更を先に merge する。段5 のログの権限は、ログの resource を足す変更が先に足す。4つの role の暗号操作は IAM では与えず、state の鍵の Key Policy が role を名指して許す（repository-wide/ADR-0301 (least-privilege-policy) 決定12）。
4. Resource は、state Bucket と plan の出力の Bucket の ARN とその key、4つの role と OIDC provider、state の鍵で限定する。role の ARN は、resource 参照ではなく、partition・アカウント・role 名から組んだ完全な ARN で書く。prefix の pattern ではないので、repository-wide/ADR-0301 (least-privilege-policy) 決定11 の対象ではない。apply-identity の role の inline policy は、他の role より先に書き込む。他の role の作成は、apply-identity の inline policy の書き込みに依存させる。role を足す変更は、1回の apply で、その role を管理する権限を apply-identity へ足してから role を作る。state の鍵の ARN は入力の `state_key_arn`（`state-backend` の出力）で受け、role と同じ partition・リージョン・アカウントの鍵であることを plan で（ARN が plan で決まらなければ apply で）確かめる。
5. apply の role に、次を Deny で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定8 は Deny の wildcard を許す）。4つの role と OIDC provider、identity root の key と lock file、plan の出力の Bucket とそのすべての object への、すべての Action（`*`）。state Bucket への、`s3:ListBucket` を除くすべての Action（`NotAction`）。state Bucket のすべての object への `s3:DeleteObjectVersion`。state の鍵への、`modules/kms` が Key Policy でアカウントへ委任する管理の Action のすべて（repository-wide/ADR-0604 (state-split-by-apply-role) 決定10）。Deny を Action の列挙やサービスの wildcard（`s3:Put*`、`iam:*`）で書くと、AWS が足す変更系の Action（tag や metadata の設定）と、別のサービスの名前で同じ ARN を対象とする Action（role の ARN への `sts:AssumeRole`）を覆わない。拒む側は `*` か、許す読み取りの名前だけを除く `NotAction` で書く。workload root の key と lock file への書き込みは apply の role が行うので拒まない。鍵の Deny は `kms:*` にしない。Key Policy が role を名指して許す暗号操作（決定3）まで塞ぐからである。KMS では IAM の Allow は Key Policy がアカウントへ委任した Action にしか効かないので、委任の Action を覆えば足りる。alias の削除と付け替えは鍵への権限も要るので、鍵の Deny が塞ぐ。以後 workload の usecase のための Allow を足しても、bootstrap の構成物、identity root の state、2つの Bucket の設定、state の版、plan の出力、state の鍵の設定を変えられないようにする。
6. apply-identity の role は、自分を含む4つの role の信頼条件と policy を変更できる。誤った信頼条件が merge されると CI は締め出され、回復は repository-wide/ADR-0602 決定9 の回復の契機になる。
7. managed policy を付けない。`aws_iam_role_policies_exclusive` と `aws_iam_role_policy_attachments_exclusive` で、帯域外で足された policy を次の apply で剥がす。
8. plan の role に、plan の出力の Bucket の `pr/*` と `branch/*` の key への `s3:PutObject` を与える（repository-wide/ADR-0602 決定13 の例外）。Pull Request の plan と保護ブランチの plan を key の接頭辞で分ける。`s3:DeleteObject` と `s3:DeleteObjectVersion` は与えない。plan の出力を state の鍵で暗号化するのに要る `kms:GenerateDataKey` は IAM では与えず、Key Policy が plan の role を名指して許す（決定3）。
9. unlock-identity の role に、state Bucket への `s3:ListBucket`（Condition `s3:prefix` を identity root の lock file の key に `StringEquals` で限る）と、identity root の lock file への `s3:GetObject` と `s3:DeleteObject` だけを与える。列挙は Provider ではなく、lock を外す手順が呼ぶ API から導く。ListObjectsV2 は `s3:ListBucket` を、GetObject は `s3:GetObject` を要する。If-Match の条件付きの DeleteObject は `s3:DeleteObject` と `s3:GetObject` を要する。`s3:PutObject` と `s3:DeleteObjectVersion` を与えない。lock file の復号に要る `kms:Decrypt` は IAM では与えない。state の鍵の Key Policy が、この role を、暗号化コンテキスト `aws:s3:arn` で identity root の lock file の object に限って名指す（決定3）。この role は Deny を持たない。Allow を足すことを Policy Test が拒む。

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
| `aws_kms_key` | DescribeKey、GetKeyPolicy、GetKeyRotationStatus、ListResourceTags | 同名の `kms:` | plan、apply-identity |
| 同 | EnableKeyRotation | `kms:EnableKeyRotation` | apply-identity |
| `aws_kms_key_policy` | PutKeyPolicy（Read は DescribeKey と GetKeyPolicy） | `kms:PutKeyPolicy` | apply-identity |
| `aws_kms_alias` | ListAliases | `kms:ListAliases` | plan、apply-identity |
| lock を外す手順（`ci-unlock-*`） | ListObjectsV2（prefix 指定）、GetObject、DeleteObject（If-Match） | `s3:ListBucket`、`s3:GetObject`、`s3:DeleteObject` | unlock-identity（identity root の lock file だけ） |

与えないもの: `s3:CreateBucket` と `s3:DeleteBucket`（2つの Bucket の作り直しを権限の側でも止める）、`iam:AttachRolePolicy`（managed policy を付けない）、`iam:TagRole` などの tag の書き込み（tag を付けない）、`iam:PutRolePermissionsBoundary` と `iam:DeleteRolePermissionsBoundary`、`iam:PassRole`、`iam:RemoveRoleFromInstanceProfile`（instance profile を持たない）。`kms:CreateKey`・`kms:CreateAlias`（state の鍵と alias を CI で作らない。決定3）、`kms:ScheduleKeyDeletion`・`kms:DisableKey`・`kms:DisableKeyRotation`・`kms:EnableKey`・`kms:UpdateKeyDescription`・`kms:DeleteAlias`・`kms:UpdateAlias`（鍵の作り直しと alias の付け替えを権限の側でも止める）、`kms:TagResource`（tag を付けない）、暗号操作（決定3）。`aws_caller_identity` の `sts:GetCallerIdentity` は権限を要さない。

## 検討した代替案

### 案A: permissions boundary を課し、Allow を広く与える

今の4つの role は IAM の role を新しく作る主体ではなく、boundary の中身を決める材料が無い。boundary の内側の Allow を広く与えると、repository-wide/ADR-0301 決定8 の Action wildcard に当たる。

### 案B: AWS managed policy（`ReadOnlyAccess` 等）を plan の role に付ける

plan の role は Pull Request の作者が任意のコードで使える資格情報であり、アカウントの全 resource を読める。repository-wide/ADR-0301 決定10 に反する。

### 案C: 段5 の権限をまとめて先に入れる

ログの resource の形（#108）が無く、正しく列挙できない。使わない権限を先に与えることになる。鍵の権限は、鍵の形（`modules/kms`）から Provider が呼ぶ API を列挙できるので、鍵を作る変更と同じか、それより前に入れる（決定3）。

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

- 保証すること: 4つの role の policy の文書が、上の表と決定5 の Deny と決定9 から組まれていること（Unit Test）。reference の plan で、apply の role に付く inline policy が、決定5 の S3・IAM の各 ARN への読み取り以外の Action と、state Bucket のすべての object への `s3:DeleteObjectVersion` を条件なしの Deny で拒み、state の鍵の ARN への Deny が `modules/kms` が Key Policy でアカウントへ委任する Action を覆うこと。apply の role に、その inline policy のほかの policy が付かないこと（Policy Test）。unlock-identity の role の Allow が決定9 の範囲に限られ、Key Policy がその復号を同じ lock file の object に限ること（Policy Test）。他の role の作成が、apply-identity の inline policy の書き込みに依存すること（Static Analysis）
- 保証しないこと: 列挙が Provider の実際の呼び出しを漏れなく覆うこと（段4 の後の人の確認と、CI の plan が見る）。apply の role の Deny が実際の要求で文書どおりに評価されることと、Deny の形から導く保証の外（repository-wide/ADR-0602 の保証範囲の「保証しないこと」）。Policy Test の宣言（`conftest/plan/decl.json`）の型の分類のうち、親の resource の ARN で守られるとした型の設定を変える API が実際に親の ARN への Action であることと、Deny で守らないとした型の理由が正しいこと（検査は無く、宣言の変更のレビューに依る）。apply-identity の inline policy の書き込みが、同じ apply の role の作成までに IAM の評価へ反映されること。反映が遅れると、その apply は `iam:CreateRole` の AccessDenied で失敗する

## 検証方法

- Unit Test: `internal/github-oidc/tests/permissions.tftest.hcl` が、4つの role の policy の文書と、inline policy と managed policy の排他を完全一致で確かめる
- 手順: `docs/setup/bootstrap.md` の段4 の手順9 と段5 の手順4 が、apply の後に `aws iam simulate-principal-policy` で、表の Action が plan と apply-identity の role に許され、与えないものと apply の role の bootstrap の構成物への変更が拒否されることを確かめる
- Policy Test（`make tf-policy`）: Allow の Action の `*`・`service:*` の不在（repository-wide/ADR-0301 (least-privilege-policy) 決定8）と、`NotAction`・`NotResource` の Allow の不在、Allow の Resource `"*"` が決定1 の Action（`kms:ListAliases`。`resource_wildcard_actions` の宣言）に限られること（同 決定9）、KMS の暗号操作と `kms:CreateGrant` の Allow の不在（決定3、同 決定12）
- Policy Test（`make tf-policy`）: plan の role の Allow が読み取りと、plan の出力の Bucket の `pr/*`・`branch/*` への `s3:PutObject` だけであることと、宣言した policy の外から plan の role に権限が付かないこと（決定8）
- Policy Test（`make tf-policy`）: apply の role の inline policy が、state の鍵の Key Policy がアカウントへ委任する Action のすべてを、state の鍵の ARN（`state-backend` の plan の値）への Deny で拒むこと（決定5。`plan.deployment_identity_apply_role`）
- Policy Test（`make tf-policy`）: unlock-identity の role の Allow が決定9 の3つの Action と Resource に限られ、宣言した policy の外から権限が付かず、state の鍵の Key Policy がこの role に `kms:Decrypt` だけを、IAM が許す lock file の object の ARN に限って許すこと（決定9。`plan.deployment_identity_unlock_role`）
- Static Analysis（`make resource-order-check`）: apply-identity を除く role の resource が、apply-identity の inline policy の resource に依存すること（決定4）
- Policy Test（`make tf-policy`）: apply の role の inline policy が apply の role に付き、決定5 の S3・IAM の各 ARN（role・OIDC provider・Bucket は plan の値、identity root の key と lock file は宣言の `state_backend.roots` から組む）について、`*` か、読み取りの名前（動詞が Get・List・Describe・Head で wildcard を含まないもの）だけを除く `NotAction` を、条件なしの Deny で拒み、state Bucket のすべての object への `s3:DeleteObjectVersion` を拒むこと。apply の role の inline policy と managed policy の排他が、その inline policy だけを残し managed policy を残さないこと。identity root の bootstrap の use-case の下の resource の型が、宣言ですべて分類されていること（決定5・7。`plan.deployment_identity_apply_role`）

## 影響

- 段5 と、管理アカウントに workload の usecase を足す変更は、まずこの policy を広げる変更を merge してから、その権限を使う変更を apply する。CI の role を足す変更は、決定4 の順序により1回の apply に収まる。権限の変更と、その権限で作る resource を同じ apply に入れると、両者の間に依存が無く失敗しうる
- inline policy は role あたり 10,240 文字（空白を除く）が上限である

## 見直し条件

- AWS Provider の版を上げるとき
- apply の role が IAM の role を作る usecase が入るとき（permissions boundary を決める）
- inline policy の文字数が上限に近づいたとき
- CI の plan か apply が AccessDenied で失敗したとき
- `modules/kms` が Key Policy でアカウントへ委任する Action を変えるとき（決定5 の鍵の Deny の列挙を合わせる）
- state の鍵の作成の権限を足すとき（鍵の種類 `kms:KeySpec`・`kms:KeyUsage`、多リージョン `kms:MultiRegion`、鍵素材 `kms:KeyOrigin` を限る条件と、`resource_wildcard_actions` への `kms:CreateKey` の追加を併せて決める）
