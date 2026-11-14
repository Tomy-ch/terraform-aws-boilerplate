# ADR-0201: CI の role の権限を、Provider が呼ぶ API から導いた Action の列挙で与える

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301, repository-wide/ADR-0402

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定16 は、apply の role の権限の与え方（Action の列挙か permissions boundary か）を use-case の ADR に委ねる。同 決定13 は、plan の role に変更系の権限を与えず、apply の role に bootstrap の構成物を変更する権限を与えないことを求める。repository-wide/ADR-0604 (state-split-by-apply-role) 決定8-10 は、plan の role が state にも lock file にも書かないこと、apply の role が自分の root の key と lock file にだけ書けること、apply の role が state Bucket の設定を変更する有効な権限を持たないことを求める。同 決定13 は、lock の解除の role に、lock file の一覧・読み取り・削除と、その lock file の復号だけを持たせる。

AWS Provider は、resource の refresh で、設定していない属性の API も呼ぶ。6.70.0 の `aws_s3_bucket` の Read は、Bucket の Policy・ACL・CORS・website・版管理・accelerate・request payment・logging・lifecycle・replication・暗号化・Object Lock・tag を読み、拒否を許容しない。読み取りの権限が1つ欠けると、plan は AccessDenied で失敗する。apply-identity の role の読み取りが欠けると、その role 自身の policy を直す apply が refresh で失敗し、repository-wide/ADR-0602 決定9 の回復の契機になる。

段5 の S3 サーバーアクセスログは、CloudWatch Logs の V2 permissions の配送で Log Group へ送る。配送の呼び出し主体には、Provider が呼ぶ API の権限に加えて、AWS が求める権限が要る。送り側の Bucket の `s3:AllowVendedLogDeliveryForResource` である。

## Decision

1. 4つの role の権限を、role ごとの inline policy に Action を列挙して与える。Allow の文に `*` と `service:*` の Action、`"*"` の Resource、部分的なワイルドカード（`s3:GetBucket*` 等）を使わない。permissions boundary を課さない。ただし、resource-level 権限を持たない Action（Service Authorization Reference で判定する。`kms:ListAliases`・`logs:DescribeLogGroups`・`logs:DescribeResourcePolicies`・`logs:PutResourcePolicy`）は、`"*"` の Resource を Action ごとの別の文で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定9）。Logs の3つは条件キーを持たないので、Condition で限れない。Key Policy の書き込みでの lockout の安全チェックの迂回（`kms:BypassPolicyLockoutSafetyCheck`）は Deny で拒む。
2. 列挙は、identity root の resource について AWS Provider 6.70.0 の source（`internal/service/s3`・`internal/service/iam`・`internal/service/kms`・`internal/service/logs`）の Read・Create・Update・Delete が呼ぶ API から導く。対応は下の表が持つ。Provider の版を上げる変更は、この表を見直す。AWS がその API の呼び出し主体に追加で求める権限は、AWS の文書から導き、表の行に出典を書く（`s3:AllowVendedLogDeliveryForResource` は Amazon S3 User Guide）。Log Group の ARN の形（`:*` の有無）は、CloudWatch Logs API Reference の LogGroup 型に従う。
3. 権限は段4 の構成物（OIDC provider、4つの role と inline policy、state Bucket の段1-4 の設定とその lifecycle、plan の出力の Bucket とその lifecycle、state の鍵（鍵、Key Policy、alias））と段5 の構成物に要る分に限る。段5 の構成物は、2つの Bucket の S3 サーバーアクセスログの配送（Log Group、その resource policy、delivery destination、2つの delivery source と delivery）である。Log Group の暗号化の構成は `state-backend` が決める。この決定は、Log Group に鍵を付ける権限（鍵の作成、`logs:AssociateKmsKey`、state の鍵以外の鍵への `kms:DescribeKey`）を与えない。Log Group に鍵を付ける変更は、その権限と apply の role の Deny を足す変更を先に merge する。鍵と alias を作る権限は与えない。新しいアカウントでは段4 の apply が一時 IAM user の権限で作る。鍵と alias の作り直しは CI では行えず、要るときは作成の権限を足す変更を先に merge する。4つの role の暗号操作は IAM では与えず、state の鍵の Key Policy が role を名指して許す（repository-wide/ADR-0301 (least-privilege-policy) 決定12）。
4. Resource は、state Bucket と plan の出力の Bucket の ARN とその key、4つの role と OIDC provider、state の鍵で限定する。role の ARN は、resource 参照ではなく、partition・アカウント・role 名から組んだ完全な ARN で書く。prefix の pattern ではないので、repository-wide/ADR-0301 (least-privilege-policy) 決定11 の対象ではない。apply-identity の role の inline policy は、他の role より先に書き込む。他の role の作成は、apply-identity の inline policy の書き込みに依存させる。role を足す変更は、1回の apply で、その role を管理する権限を apply-identity へ足してから role を作る。state の鍵の ARN は入力の `state_key_arn`（`state-backend` の出力）で受け、role と同じ partition・リージョン・アカウントの鍵であることを plan で（ARN が plan で決まらなければ apply で）確かめる。段5 の構成物の Resource は、入力の `access_log_group_arn`・`access_log_destination_arn`・`access_log_source_arns`（`state-backend` が組む）で書き、role と同じ partition・リージョン・アカウントであることを plan で確かめる。Log Group は、`ListTagsForResource` には `:*` の無い ARN を、ほかの Action には `:*` を付けた ARN を使う（API Reference の LogGroup）。delivery の ARN は AWS が付ける ID を含み、作るまで決まらないので、作るまでは `delivery:*` の pattern で与え、作った後の変更で delivery の ARN へ絞り、`logs:CreateDelivery` を外す（repository-wide/ADR-0301 (least-privilege-policy) 決定11）。delivery を限れる条件キーは `aws:ResourceTag` だけで、このユースケースは tag を付けないので限れない。`PutDeliveryDestination` は、`logs:DeliveryDestinationResourceArn` の Condition で宛先を Log Group に限る。`PutDeliverySource` は、送り側の resource を `logs:LogGeneratingResourceArns` で限らない。送り側に要る `s3:AllowVendedLogDeliveryForResource` が2つの Bucket に限られているからである。
5. apply の role に、次を Deny で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定8 は Deny の wildcard を許す）。4つの role と OIDC provider への `iam:*`。identity root の key と lock file への `s3:Put*` と `s3:Delete*`。state Bucket と plan の出力の Bucket への `s3:Put*` と `s3:Delete*`。state Bucket のすべての object への `s3:DeleteObjectVersion`。plan の出力の Bucket のすべての object への `s3:Put*` と `s3:DeleteObjectVersion`。state の鍵への、`modules/kms` が Key Policy でアカウントへ委任する管理の Action のすべて（repository-wide/ADR-0604 (state-split-by-apply-role) 決定10）。workload root の key と lock file への書き込みは apply の role が行うので拒まない。鍵の Deny は `kms:*` にしない。Key Policy が role を名指して許す暗号操作（決定3）まで塞ぐからである。KMS では IAM の Allow は Key Policy がアカウントへ委任した Action にしか効かないので、委任の Action を覆えば足りる。alias の削除と付け替えは鍵への権限も要るので、鍵の Deny が塞ぐ。Log Group、delivery destination、2つの delivery source への `logs:*`（Log Group は `:*` の有無の2つの形）。2つの Bucket への `s3:AllowVendedLogDeliveryForResource`。resource type を持たず、Log Group を名指して拒めない Logs の変更系（`logs:PutAccountPolicy`・`logs:DeleteAccountPolicy`・`logs:PutIndexPolicy`・`logs:DeleteIndexPolicy`・`logs:PutResourcePolicy`・`logs:DeleteResourcePolicy`）への、Resource `"*"` の Deny。delivery の ARN への `logs:*` は、作った後の変更で足す。以後 workload の usecase のための Allow を足しても、bootstrap の構成物、identity root の state、2つの Bucket の設定、state の版、plan の出力、state の鍵の設定、Log Group・delivery destination・delivery source の設定を変えられないようにする。
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
| `aws_cloudwatch_log_group` | DescribeLogGroups、ListTagsForResource | `logs:DescribeLogGroups`（Resource `"*"`）、`logs:ListTagsForResource`（`:*` の無い ARN） | plan、apply-identity |
| 同 | CreateLogGroup、PutRetentionPolicy | 同名の `logs:`（`:*` を付けた ARN） | apply-identity |
| `aws_cloudwatch_log_resource_policy` | DescribeResourcePolicies | `logs:DescribeResourcePolicies`（Resource `"*"`） | plan、apply-identity |
| 同 | PutResourcePolicy | `logs:PutResourcePolicy`（Resource `"*"`） | apply-identity |
| `aws_cloudwatch_log_delivery_destination` | GetDeliveryDestination、ListTagsForResource | 同名の `logs:` | plan、apply-identity |
| 同 | PutDeliveryDestination | `logs:PutDeliveryDestination`（宛先の Condition） | apply-identity |
| `aws_cloudwatch_log_delivery_source` | GetDeliverySource、ListTagsForResource | 同名の `logs:` | plan、apply-identity |
| 同 | PutDeliverySource | `logs:PutDeliverySource` | apply-identity |
| `aws_cloudwatch_log_delivery` | GetDelivery、ListTagsForResource | 同名の `logs:`（`delivery:*`） | plan、apply-identity |
| 同 | CreateDelivery | `logs:CreateDelivery`（delivery と、その source と destination） | apply-identity |
| 配送の呼び出し主体への追加の要求（Amazon S3 User Guide「Delivering server access logs to CloudWatch Logs」） | PutDeliverySource ほか | `s3:AllowVendedLogDeliveryForResource`（2つの Bucket） | apply-identity |
| lock を外す手順（`ci-unlock-*`） | ListObjectsV2（prefix 指定）、GetObject、DeleteObject（If-Match） | `s3:ListBucket`、`s3:GetObject`、`s3:DeleteObject` | unlock-identity（identity root の lock file だけ） |

与えないもの: `s3:CreateBucket` と `s3:DeleteBucket`（2つの Bucket の作り直しを権限の側でも止める）、`iam:AttachRolePolicy`（managed policy を付けない）、`iam:TagRole` などの tag の書き込み（tag を付けない）、`iam:PutRolePermissionsBoundary` と `iam:DeleteRolePermissionsBoundary`、`iam:PassRole`、`iam:RemoveRoleFromInstanceProfile`（instance profile を持たない）。`kms:CreateKey`・`kms:CreateAlias`（鍵と alias を CI で作らない。決定3）、`kms:ScheduleKeyDeletion`・`kms:DisableKey`・`kms:DisableKeyRotation`・`kms:EnableKey`・`kms:UpdateKeyDescription`・`kms:DeleteAlias`・`kms:UpdateAlias`（鍵の作り直しと alias の付け替えを権限の側でも止める）、`kms:TagResource`（tag を付けない）、暗号操作（決定3）。ログの構成物の削除（`logs:DeleteLogGroup`・`logs:DeleteResourcePolicy`・`logs:DeleteDeliveryDestination`・`logs:DeleteDeliverySource`・`logs:DeleteDelivery`。Bucket と鍵と同じく作り直しを権限の側でも止める。改名と巻き戻しは、削除の権限を足す変更を先に入れる）。保護を弱める変更と鍵の付け替え（`logs:DeleteRetentionPolicy`・`logs:DisassociateKmsKey`・`logs:AssociateKmsKey`・`logs:PutLogGroupDeletionProtection`・`logs:UpdateDeliveryConfiguration`）。`logs:PutDeliveryDestinationPolicy`（別のアカウントからの配送は責務外）。Logs の tag の書き込み（`logs:TagResource`・`logs:TagLogGroup`・`logs:UntagResource`）。ログの event を読む Action。`logs:CreateLogStream` と `logs:PutLogEvents`（書くのは `delivery.logs.amazonaws.com` で、Log Group の resource policy が許す）。`aws_caller_identity` の `sts:GetCallerIdentity` は権限を要さない。

## 検討した代替案

### 案A: permissions boundary を課し、Allow を広く与える

今の4つの role は IAM の role を新しく作る主体ではなく、boundary の中身を決める材料が無い。boundary の内側の Allow を広く与えると、repository-wide/ADR-0301 決定8 の Action wildcard に当たる。

### 案B: AWS managed policy（`ReadOnlyAccess` 等）を plan の role に付ける

plan の role は Pull Request の作者が任意のコードで使える資格情報であり、アカウントの全 resource を読める。repository-wide/ADR-0301 決定10 に反する。

### 案C: 段5 の権限をまとめて先に入れる

段4 の構成物の権限と同じ変更で、段5 の構成物の権限を先に入れる。段5 の構成物の形が決まる前は列挙できず、使わない権限を先に与えることになる。鍵の権限は、鍵の形（`modules/kms`）から Provider が呼ぶ API を列挙できるので、鍵を作る変更と同じか、それより前に入れる（決定3）。

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
- apply の role で、Logs の resource policy・アカウント単位の policy・index policy を管理すること（どの Log Group に対しても。決定5 の Resource `"*"` の Deny による）

## 保証範囲

- 保証すること: 4つの role の policy の文書が、上の表と決定5 の Deny と決定9 から組まれていること（Unit Test）。決定5 の鍵の Deny の Action が、`modules/kms` が Key Policy でアカウントへ委任する Action を覆うこと（Policy Test）。unlock-identity の role の Allow が決定9 の範囲に限られ、Key Policy がその復号を同じ lock file の object に限ること（Policy Test）。他の role の作成が、apply-identity の inline policy の書き込みに依存すること（Static Analysis）。plan の role の Allow の `logs:` の Action が、`conftest/plan/decl.json` の `deployment_identity.plan_role.logs_actions` の部分集合であり、その宣言が規則の持つ閉じた集合の部分集合で、plan の role の Allow と一致すること（Policy Test。`PLAN-ROLE-05`〜`08`）。apply の role が、決定5 の Resource `"*"` の Deny を Condition 無しで持つこと（Policy Test。`PLAN-APPLY-02`）。apply-identity の `logs:PutDeliveryDestination`・`logs:PutDeliverySource` の Resource と、`logs:CreateDelivery` の source と destination の Resource が、入力で名指した ARN に限られること（Unit Test。delivery の Resource は `delivery:*`）
- 保証しないこと: 列挙が Provider の実際の呼び出しを漏れなく覆うこと（段4 の後の人の確認と、CI の plan が見る）。apply の role の有効な権限が bootstrap の構成物の変更を許さないことの検査（Policy Test は未配線）。apply-identity の inline policy の書き込みが、同じ apply の role の作成までに IAM の評価へ反映されること。反映が遅れると、その apply は `iam:CreateRole` の AccessDenied で失敗する。CloudWatch Logs が、resource type を持たない Action を `"*"` で認可すること、`logs:DeliveryDestinationResourceArn` に入る ARN の形（段5 の構成物を作る CI の apply が初めて確かめる）。apply の role の Deny は、決定5 で名指した6つのほかの、resource type を持たない Logs の変更系と、作った後の変更までの delivery を覆わない。apply-identity は、アカウントのすべての Log Group の resource policy を書き換えられ、Log Group の保持期間を短くして既存の event を失効させられる。plan の role（Pull Request の作者のコードが使える資格情報）と apply-identity は、アカウントのすべての Log Group の一覧と設定、すべての resource policy、作った後の変更まではすべての delivery を読める。`resource_wildcard_actions` の宣言はどの role の identity-based policy にも効くので、apply-identity と apply の role のほかの role が `logs:PutResourcePolicy` を Resource `"*"` で持つことは、Policy Test では落ちない。段5 の構成物の作成が途中で落ちて taint が残ったとき、CI の role は削除の権限を持たないので、CI だけでは回復できない。段5 の列挙は、Log Group に鍵を付けない形に対するものである

## 検証方法

- Unit Test: `internal/github-oidc/tests/permissions.tftest.hcl` が、4つの role の policy の文書と、inline policy と managed policy の排他を完全一致で確かめる
- 手順: `docs/setup/bootstrap.md` の段4 の手順9 と段5 の手順4 が、apply の後に `aws iam simulate-principal-policy` で、表の Action が plan と apply-identity の role に許され、与えないものと apply の role の bootstrap の構成物への変更が拒否されることを確かめる
- Policy Test（`make tf-policy`）: Allow の Action の `*`・`service:*` の不在（repository-wide/ADR-0301 (least-privilege-policy) 決定8）と、`NotAction`・`NotResource` の Allow の不在、Allow の Resource `"*"` が決定1 の Action（`kms:ListAliases`・`logs:DescribeLogGroups`・`logs:DescribeResourcePolicies`・`logs:PutResourcePolicy`。`resource_wildcard_actions` の宣言）に限られること（同 決定9）、KMS の暗号操作と `kms:CreateGrant` の Allow の不在（決定3、同 決定12）
- Policy Test（`make tf-policy`）: plan の role の Allow が読み取りと、plan の出力の Bucket の `pr/*`・`branch/*` への `s3:PutObject` だけであることと、宣言した policy の外から plan の role に権限が付かないこと（決定8）
- Policy Test（`make tf-policy`）: apply の role の inline policy が、state の鍵の Key Policy がアカウントへ委任する Action のすべてを、鍵の ARN への Deny で拒むこと（決定5。`plan.deployment_identity_apply_role`）
- Policy Test（`make tf-policy`）: unlock-identity の role の Allow が決定9 の3つの Action と Resource に限られ、宣言した policy の外から権限が付かず、state の鍵の Key Policy がこの role に `kms:Decrypt` だけを、IAM が許す lock file の object の ARN に限って許すこと（決定9。`plan.deployment_identity_unlock_role`）
- Policy Test（`make tf-policy`）: plan の role の Allow の `logs:` の Action が宣言した一覧の部分集合で（`PLAN-ROLE-05`）、宣言が配列で wildcard を含まず（`PLAN-ROLE-07`）、規則の持つ閉じた集合の部分集合で（`PLAN-ROLE-08`。ログの event を読む Action を与えない）、plan の role の Allow に使われていること（`PLAN-ROLE-06`）。plan の role の Action の service の名前が wildcard を含まないこと（`PLAN-ROLE-09`）
- Policy Test（`make tf-policy`）: apply の role の inline policy が、宣言した resource type を持たない Logs の変更系を、Resource `"*"` の Condition の無い Deny で拒むこと（決定5。`PLAN-APPLY-02`）
- 手順: `docs/setup/bootstrap.md` の段4 の手順9 が、段5 の構成物の Action が plan と apply-identity の role に許され、名指した外の ARN と削除と apply の role への変更が拒否されることを、`aws iam simulate-principal-policy` で確かめる
- Static Analysis（`make resource-order-check`）: apply-identity を除く role の resource が、apply-identity の inline policy の resource に依存すること（決定4）
- Policy Test（未配線。#209）: repository-wide/ADR-0602 の検証方法の、apply の role の有効な権限の項目

## 影響

- 段5 と、管理アカウントに workload の usecase を足す変更は、まずこの policy を広げる変更を merge してから、その権限を使う変更を apply する。CI の role を足す変更は、決定4 の順序により1回の apply に収まる。権限の変更と、その権限で作る resource を同じ apply に入れると、両者の間に依存が無く失敗しうる
- inline policy は role あたり 10,240 文字（空白を除く）が上限である。入力の境界での長さは、`permissions.tftest.hcl` の `policy_length` が上限以下であることを固定する

## 見直し条件

- AWS Provider の版を上げるとき
- apply の role が IAM の role を作る usecase が入るとき（permissions boundary を決める）
- inline policy の文字数が上限に近づいたとき
- CI の plan か apply が AccessDenied で失敗したとき
- `modules/kms` が Key Policy でアカウントへ委任する Action を変えるとき（決定5 の鍵の Deny の列挙を合わせる）
- Service Authorization Reference が Logs の Action に resource type か条件キーを足したとき
- tag 以外で delivery を限る条件キーが現れたとき
- 段5 の構成物を作る CI の apply が AccessDenied で落ちたとき
- apply-identity の policy が、入力の境界で 10,240 文字を超える変更を入れるとき（policy を割り、決定4 の順序を見直す）
- Log Group に鍵を付けるとき（鍵の読み取り・`logs:AssociateKmsKey`・apply の role の Deny を併せて決める）
- state の鍵の作成の権限を足すとき（鍵の種類 `kms:KeySpec`・`kms:KeyUsage`、多リージョン `kms:MultiRegion`、鍵素材 `kms:KeyOrigin` を限る条件と、`resource_wildcard_actions` への `kms:CreateKey` の追加を併せて決める）
- Logs の resource policy・アカウント単位の policy・index policy を要る workload の usecase が入るとき（決定5 の Resource `"*"` の Deny を見直す）
