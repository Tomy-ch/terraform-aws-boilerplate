# ADR-0201: CI の role の権限を、Provider が呼ぶ API から導いた Action の列挙で与える

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301, repository-wide/ADR-0402

## Context

repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定16 は、apply の role の権限の与え方（Action の列挙か permissions boundary か）を use-case の ADR に委ねる。同 決定13 は、plan の role に変更系の権限を与えず、apply の role に bootstrap の構成物を変更する権限を与えないことを求める。repository-wide/ADR-0604 (state-split-by-apply-role) 決定8-10 は、plan の role が state にも lock file にも書かないこと、apply の role が自分の root の key と lock file にだけ書けること、apply の role が state Bucket の設定を変更する有効な権限を持たないことを求める。同 決定13 は、lock の解除の role に、lock file の一覧・読み取り・削除と、その lock file の復号だけを持たせる。

AWS Provider は、resource の refresh で、設定していない属性の API も呼ぶ。6.70.0 の `aws_s3_bucket` の Read は、Bucket の Policy・ACL・CORS・website・版管理・accelerate・request payment・logging・lifecycle・replication・暗号化・Object Lock・tag を読み、拒否を許容しない。読み取りの権限が1つ欠けると、plan は AccessDenied で失敗する。apply-identity の role の読み取りが欠けると、その role 自身の policy を直す apply が refresh で失敗し、repository-wide/ADR-0602 決定9 の回復の契機になる。

管理アカウントの identity root は、統制の構成物（repository-wide/ADR-0602 決定1）を持つ `organization-baseline` も呼ぶ。apply-identity の role は、それを変更する権限を持つ。apply の role は持たない（同 決定13、repository-wide/ADR-0604 決定10）。Organizations と root の資格情報の中央管理には、resource-level の権限を持たない書き込みの Action（`organizations:CreateOrganization`・`EnableAWSServiceAccess`、`iam:EnableOrganizationsRoot*` など）がある。これらは Resource の ARN ごとの Deny では拒めない。

## Decision

1. 5つの role の権限を、role ごとの inline policy に Action を列挙して与える。Allow の文に `*` と `service:*` の Action、`"*"` の Resource、部分的なワイルドカード（`s3:GetBucket*` 等）を使わない。permissions boundary を課さない。ただし、resource-level 権限を持たない Action（Service Authorization Reference で判定する。`kms:ListAliases`）は、`"*"` の Resource を Action ごとの別の文で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定9）。Key Policy の書き込みでの lockout の安全チェックの迂回（`kms:BypassPolicyLockoutSafetyCheck`）は Deny で拒む。
2. 列挙は、identity root の resource について AWS Provider 6.70.0 の source（`internal/service/s3`・`internal/service/iam`・`internal/service/kms`、管理アカウントの apply-identity と plan の role では `internal/service/organizations` も）の Read・Create・Update・Delete が呼ぶ API から導く。対応は下の表が持つ。Provider の版を上げる変更は、この表を見直す。
3. 権限は段4 の構成物（OIDC provider、5つの role と inline policy、state Bucket の段1-4 の設定とその lifecycle、plan の出力の Bucket とその lifecycle、workload root の apply の出力の Bucket（CI の apply-identity が作る）、state の鍵（鍵、Key Policy、alias））に要る分に限る。identity root に構成物を足す変更は、その構成物に要る権限を足す変更を先に merge する。state の鍵と alias を作る権限は与えない。新しいアカウントでは段4 の apply が一時 IAM user の権限で作る。鍵と alias の作り直しは CI では行えず、要るときは作成の権限を足す変更を先に merge する。5つの role の暗号操作は IAM では与えず、state の鍵の Key Policy が role を名指して許す（repository-wide/ADR-0301 (least-privilege-policy) 決定12）。 管理アカウント（env が `admin`）の apply-identity の role には、加えて `organization-baseline` の構成物に要る分を与える。対象は、組織・OU・policy とその付け先、IAM の信頼アクセス、root の資格情報の中央管理、break-glass の IAM user・group・role、IAM の password policy である。次の3つは意図して与えない: 組織の削除（`organizations:DeleteOrganization`）、メンバーの閉鎖（`organizations:CloseAccount`・`account:CloseAccount`）、組織からの除外（`organizations:RemoveAccountFromOrganization`）。`prevent_destroy` と `plan.stateful_protection` に加えて、権限の側でも止める。サービスが service-linked role を作る Action のために、`iam:CreateServiceLinkedRole` を `iam:AWSServiceName` の条件で該当するサービスに限って与える。env が `admin` でない role には、これらを与えない。統制の構成物の作成と設定の権限は、決定7 の customer managed policy に置く。読み取りは plan の role と同じ文で inline policy に置く。
4. Resource は、state Bucket と plan の出力の Bucket と apply の出力の Bucket の ARN とその key、5つの role と OIDC provider、state の鍵で限定する。role の ARN は、resource 参照ではなく、partition・アカウント・role 名から組んだ完全な ARN で書く。prefix の pattern ではないので、repository-wide/ADR-0301 (least-privilege-policy) 決定11 の対象ではない。apply-identity の role の inline policy は、他の role より先に書き込む。他の role の作成は、apply-identity の inline policy の書き込みに依存させる。role を足す変更は、1回の apply で、その role を管理する権限を apply-identity へ足してから role を作る。state の鍵の ARN は入力の `state_key_arn`（`state-backend` の出力）で受け、role と同じ partition・リージョン・アカウントの鍵であることを plan で（ARN が plan で決まらなければ apply で）確かめる。 `organization-baseline` の構成物の Resource は、作る前に組織の ID が決まらないので、ARN の pattern で書く。例: `arn:<partition>:organizations::<管理アカウント>:ou/o-*/ou-*`。pattern は repository-wide/ADR-0301 決定11 の対象で、その根拠をこの決定に置く。break-glass の IAM の Resource は、path（`/break-glass/`）と名前から組んだ完全な ARN で書く。
5. apply の role に、次を Deny で与える（repository-wide/ADR-0301 (least-privilege-policy) 決定8 は Deny の wildcard を許す）。5つの role と OIDC provider、identity root の key と lock file、plan の出力の Bucket とそのすべての object への、すべての Action（`*`）。state Bucket への、`s3:ListBucket` と `s3:GetEncryptionConfiguration` を除くすべての Action（`NotAction`）。state Bucket のすべての object への `s3:DeleteObjectVersion`。state の鍵への、`modules/kms` が Key Policy でアカウントへ委任する管理の Action のすべて（repository-wide/ADR-0604 (state-split-by-apply-role) 決定10）。Deny を Action の列挙やサービスの wildcard（`s3:Put*`、`iam:*`）で書くと、AWS が足す変更系の Action（tag や metadata の設定）と、別のサービスの名前で同じ ARN を対象とする Action（role の ARN への `sts:AssumeRole`）を覆わない。S3・IAM の構成物への Deny は、`*` か、許す読み取りの名前だけを除く `NotAction` で書く。`s3:GetEncryptionConfiguration` を除くのは、init の前に運用機構が Bucket の既定の暗号化と鍵の一致を確かめるためである（state-backend/ADR-0101 決定3）。workload root の key と lock file への書き込みは apply の role が行うので拒まない。鍵の Deny は `kms:*` にしない。Key Policy が role を名指して許す暗号操作（決定3）まで塞ぐからである。KMS では IAM の Allow は Key Policy がアカウントへ委任した Action にしか効かないので、委任の Action を覆えば足りる。alias の削除と付け替えは鍵への権限も要るので、鍵の Deny が塞ぐ。以後 workload の usecase のための Allow を足しても、bootstrap の構成物、identity root の state、3つの Bucket の設定、state の版、plan の出力、state の鍵の設定を変えられないようにする。apply の出力の Bucket（決定12）への、すべての Action（`*`）。apply の出力の Bucket のすべての object への、`s3:PutObject` を除くすべての Action（`NotAction`）。ただし、apply の role の書き込み先として宣言した構成物の子（apply の出力の Bucket の object）への Deny は、その書き込みの名前だけを除く `NotAction` で書き、読み取りも除かない。 apply の role に、統制の構成物を変更する Action を Deny で与える（`DenyGovernanceChange`）。Resource は `"*"`、条件は付けない。対象は次の書き込みの Action である。Organizations は、書き込みの動詞（Accept・Attach・Cancel・Close・Create・Decline・Delete・Deregister・Detach・Disable・Enable・Invite・Leave・Move・Put・Register・Remove・Tag・Terminate・Untag・Update）を接頭辞で拒む。IAM は `iam:EnableOrganizationsRoot*`・`iam:DisableOrganizationsRoot*`。Identity Center は permission set と割当の書き込み。STS は `sts:AssumeRoot`。読み取り（Describe・List・Get）は拒まない。委任管理者のアカウントの CI が、組織を読んで行う運用を塞がないためである。この Deny は、管理アカウントの外でも同じ文で置く。メンバーで Organizations の書き込みを呼ぶ正当な経路は、委任管理者の設定に限る。どの Action がそれに当たるかは、#22 の交差の表で確かめ、当たる Action をこの列挙から外すか、その委任をこの usecase の外で行うかを、その時点で決める。break-glass の IAM user・group・role の ARN には、`DenyBreakGlassChange` として Action `"*"` を Deny で与える。ARN は `organization-baseline` の出力 `break_glass_arns` で受ける。管理アカウントでは、決定7 の統制の customer managed policy の ARN も、5つの role と OIDC provider と同じ Deny（Action `*`）の対象に含める。workload の apply の role が、apply-identity の role に付く統制の権限を書き換えられないようにするためである。接頭辞で拒む Deny は、AWS が後から足す書き込みの Action の名前が、その接頭辞に当たる限りで覆う。当たらない名前の Action を覆わないことは、repository-wide/ADR-0602 の保証しないことに在る。
6. apply-identity の role は、自分を含む5つの role の信頼条件と policy を変更できる。誤った信頼条件が apply されると CI は締め出され、回復は repository-wide/ADR-0602 決定9 の回復の契機になる。
7. CI の role に AWS managed policy を付けない。customer managed policy は、管理アカウント（env が `admin`）の apply-identity の role にだけ、統制の構成物（決定3）の作成と設定の権限を持つ1つ（`<project>-admin-ci-apply-identity-governance`）を付ける。ほかの4つの role と、env が `admin` でない apply-identity の role には、managed policy を付けない。 統制の権限を inline policy に置かないのは、IAM の role の inline policy の合計が 10,240 文字を上限とし、統制の分を足すと入力の境界でそれを超えるためである。AWS も、多くの場合に inline policy より managed policy を使うことを勧める（IAM User Guide「Choose between managed policies and inline policies」）。統制の権限が増えるとき（Identity Center の permission set と割当など）は、同じ役割の customer managed policy を足して伸ばす。そのときはこの決定の「1つ」を改める。 apply-identity の role には、その customer managed policy の作成・読み取り・版の更新・削除の Action を、Resource をその policy の ARN に限って inline policy で与える。対象は `iam:CreatePolicy`・`iam:GetPolicy`・`iam:GetPolicyVersion`・`iam:ListPolicyVersions`・`iam:ListPolicyTags`・`iam:CreatePolicyVersion`・`iam:SetDefaultPolicyVersion`・`iam:DeletePolicyVersion`・`iam:DeletePolicy` である。 自分の role への付け外しとして、`iam:AttachRolePolicy` を Resource を apply-identity の role の ARN に、条件 `iam:PolicyARN` をその policy の ARN に限って与える（`iam:DetachRolePolicy` は CI の role に既に与えている）。apply-identity の role が自分に付く policy を自分で変えられる性質は、inline policy と同じである（決定6）。 plan の role には、その policy の読み取り（`iam:GetPolicy`・`iam:GetPolicyVersion`・`iam:ListPolicyVersions`・`iam:ListPolicyTags`）を同じ ARN に限って与える。 customer managed policy の文書にも、決定1 と決定4 の Action と Resource の規則を当てる。 `aws_iam_role_policies_exclusive` と `aws_iam_role_policy_attachments_exclusive` で、帯域外で足された policy を次の apply で剥がす。apply-identity の role の `aws_iam_role_policy_attachments_exclusive` には、その customer managed policy の ARN だけを載せる。 AWS managed policy は、管理アカウントの apply-identity の role が break-glass の role に `AdministratorAccess` を付けるもの（repository-wide/ADR-0301 決定19）のほかは付けない。そのために `iam:AttachRolePolicy`・`iam:DetachRolePolicy` を、Resource を break-glass の role の ARN に、条件 `iam:PolicyARN` を `AdministratorAccess` に限って与える。
8. plan の role に、plan の出力の Bucket の `pr/*` と `branch/*` の key への `s3:PutObject` を与える（repository-wide/ADR-0602 決定13 の例外）。Pull Request の plan と保護ブランチの plan を key の接頭辞で分ける。`s3:DeleteObject` と `s3:DeleteObjectVersion` は与えない。plan の出力を state の鍵で暗号化するのに要る `kms:GenerateDataKey` は IAM では与えず、Key Policy が plan の role を名指して許す（決定3）。
9. unlock-identity の role に、state Bucket への `s3:ListBucket`（Condition `s3:prefix` を identity root の lock file の key に `StringEquals` で限る）と、identity root の lock file への `s3:GetObject` と `s3:DeleteObject` だけを与える。列挙は Provider ではなく、lock を外す手順が呼ぶ API から導く。ListObjectsV2 は `s3:ListBucket` を、GetObject は `s3:GetObject` を要する。If-Match の条件付きの DeleteObject は `s3:DeleteObject` と `s3:GetObject` を要する。`s3:PutObject` と `s3:DeleteObjectVersion` を与えない。lock file の復号に要る `kms:Decrypt` は IAM では与えない。state の鍵の Key Policy が、この role を、暗号化コンテキスト `aws:s3:arn` で identity root の lock file の object に限って名指す（決定3）。この role は Deny を持たない。Allow を足すことを Policy Test が拒む。
10. apply-identity の role に、plan の出力の Bucket の `apply/*` の key への `s3:PutObject` を与える（repository-wide/ADR-0602 決定13）。plan の role の `pr/*`・`branch/*` と接頭辞を分け、Pull Request のコードが apply の出力を偽れないようにする。`s3:DeleteObject` と `s3:DeleteObjectVersion` は与えない。暗号化に要る `kms:GenerateDataKey` は、state の鍵の Key Policy が既に apply-identity の role に許している。列挙は Provider ではなく、apply の出力を置く手順が呼ぶ API（1回の PutObject）から導く。決定3 の構成物に要る分の外の権限である。
11. unlock の role に、state Bucket への `s3:ListBucket`（Condition `s3:prefix` を workload root の lock file の key に `StringEquals` で限る）と、workload root の lock file への `s3:GetObject` と `s3:DeleteObject` だけを与える。列挙と与えないもの、Deny を持たないことは決定9 の unlock-identity の role と同じとする。identity root と workload root の lock の解除を別の role に分け、片方の environment から他方の root の lock に届かないようにする（repository-wide/ADR-0602 決定13 の「対応する root の lock file」）。
12. apply の role に、apply の出力の Bucket（state-backend/ADR-0201 決定7）の object への `s3:PutObject` だけを与える（repository-wide/ADR-0602 決定13）。その object へのほかの Action は決定5 で拒む。列挙は Provider ではなく、apply の出力を置く手順が呼ぶ API（1回の PutObject）から導く。apply-identity と plan の role は、この Bucket の設定を、state Bucket と plan の出力の Bucket と同じ列挙で読み書きする。

### Action の導出

| resource（Provider 6.70.0） | 呼ぶ API | Action | 与える role |
| --- | --- | --- | --- |
| S3 backend（state） | ListObjects、GetObject、PutObject | `s3:ListBucket`、`s3:GetObject`、`s3:PutObject` | 3つ（書き込みは apply 系の自分の key だけ） |
| S3 backend（lock file） | GetObject、PutObject、DeleteObject | `s3:GetObject`、`s3:PutObject`、`s3:DeleteObject` | apply 系の自分の `.tflock` だけ |
| plan の出力 | PutObject | `s3:PutObject` | plan（plan の出力の Bucket の `pr/*` と `branch/*` の object だけ） |
| apply の出力 | PutObject | `s3:PutObject` | apply-identity（plan の出力の Bucket の `apply/*` の object だけ。決定10） |
| `aws_s3_bucket_lifecycle_configuration`（3つの Bucket） | Get / Put / DeleteBucketLifecycle | `s3:GetLifecycleConfiguration`、`s3:PutLifecycleConfiguration` | 読みは plan、apply-identity。書きは apply-identity |
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
| lock を外す手順（`ci-unlock-*-identity`） | ListObjectsV2（prefix 指定）、GetObject、DeleteObject（If-Match） | `s3:ListBucket`、`s3:GetObject`、`s3:DeleteObject` | unlock-identity（identity root の lock file だけ） |
| lock を外す手順（`ci-unlock-*-workload`） | ListObjectsV2（prefix 指定）、GetObject、DeleteObject（If-Match） | `s3:ListBucket`、`s3:GetObject`、`s3:DeleteObject` | unlock（workload root の lock file だけ。決定11） |
| init の前の鍵の解決（`TF_STATE_KEY`） | ListAliases、GetBucketEncryption | `kms:ListAliases`、`s3:GetEncryptionConfiguration` | plan、apply-identity、apply |
| `aws_s3_bucket`（apply の出力の Bucket）の Create | CreateBucket（ACL は private）、HeadBucket | `s3:CreateBucket`、`s3:ListBucket` | apply-identity（その Bucket の ARN だけ。決定3） |
| workload root の apply の出力 | PutObject | `s3:PutObject` | apply（apply の出力の Bucket の object だけ。決定12） |
| `aws_organizations_organization` | DescribeOrganization、ListAccounts、ListRoots、ListAWSServiceAccessForOrganization | 同名の `organizations:` | plan、apply-identity（管理アカウント。以下この表の統制の行は同じ） |
| 同 | CreateOrganization（`organizations.amazonaws.com` の service-linked role を作る）、EnablePolicyType / DisablePolicyType | 同名の `organizations:`、`iam:CreateServiceLinkedRole`（`iam:AWSServiceName` を `organizations.amazonaws.com` に限る） | apply-identity（統制の policy） |
| `aws_organizations_aws_service_access` | ListAWSServiceAccessForOrganization、EnableAWSServiceAccess / DisableAWSServiceAccess | 同名の `organizations:`（書きは `organizations:ServicePrincipal` を `iam.amazonaws.com` に限る） | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_organizations_organizational_unit` | DescribeOrganizationalUnit、ListParents、ListAccountsForParent、ListTagsForResource、CreateOrganizationalUnit、UpdateOrganizationalUnit | 同名の `organizations:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_organizations_policy` | DescribePolicy、ListTagsForResource、CreatePolicy、UpdatePolicy、DeletePolicy | 同名の `organizations:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_organizations_policy_attachment` | ListTargetsForPolicy、AttachPolicy、DetachPolicy | 同名の `organizations:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_organizations_features` | ListOrganizationsFeatures、Enable / DisableOrganizationsRootCredentialsManagement、Enable / DisableOrganizationsRootSessions | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_user`（break-glass） | GetUser、ListUserTags、CreateUser、UpdateUser | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_group`（break-glass） | GetGroup、CreateGroup、UpdateGroup、DeleteGroup（ListGroupPolicies・ListAttachedGroupPolicies・DeleteGroupPolicy を伴う） | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_group_membership` | GetGroup、AddUserToGroup、RemoveUserFromGroup | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_group_policy` | GetGroupPolicy、PutGroupPolicy、DeleteGroupPolicy | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_role`（break-glass） | GetRole、ListRolePolicies、GetRolePolicy、ListAttachedRolePolicies、ListRoleTags、CreateRole、UpdateRole、UpdateRoleDescription、UpdateAssumeRolePolicy | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_role_policy_attachments_exclusive`（break-glass） | ListAttachedRolePolicies、AttachRolePolicy、DetachRolePolicy | 同名の `iam:`（付け外しは `iam:PolicyARN` を `AdministratorAccess` に限る） | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_account_password_policy` | GetAccountPasswordPolicy、UpdateAccountPasswordPolicy、DeleteAccountPasswordPolicy | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（統制の policy） |
| `aws_iam_policy`（統制の customer managed policy。決定7） | GetPolicy、GetPolicyVersion、ListPolicyTags、ListPolicyVersions、CreatePolicy、CreatePolicyVersion、SetDefaultPolicyVersion、DeletePolicyVersion、DeletePolicy | 同名の `iam:` | 読みは plan、apply-identity。書きは apply-identity（inline。その policy の ARN だけ） |
| `aws_iam_role_policy_attachments_exclusive`（apply-identity） | AttachRolePolicy | `iam:AttachRolePolicy`（`iam:PolicyARN` を統制の policy に限る） | apply-identity（inline。自分の role だけ） |

統制の行の Action のうち resource-level の権限を持たないもの（Service Authorization Reference の resource 型が空の行。`organizations:CreateOrganization`・`DescribeOrganization`・`ListAccounts`・`ListRoots`・`ListAWSServiceAccessForOrganization`・`EnableAWSServiceAccess`・`DisableAWSServiceAccess`、`iam:ListOrganizationsFeatures`・`Enable/DisableOrganizationsRoot*`・`Get/Update/DeleteAccountPasswordPolicy`）は、決定1 のとおり Resource `"*"` の別の文で与え、`conftest/plan/decl.json` の `resource_wildcard_actions` に宣言する。`EnableAWSServiceAccess`・`DisableAWSServiceAccess` は条件キー `organizations:ServicePrincipal` で限る。

与えないもの: `s3:CreateBucket`（state Bucket と plan の出力の Bucket の作り直しを権限の側でも止める。apply の出力の Bucket には、Resource をその ARN に限って apply-identity に与える。CI が作る構成物だからである）と `s3:DeleteBucket`（3つの Bucket のどれにも与えない）、`iam:AttachRolePolicy`（決定7 の2つ（apply-identity の role への統制の customer managed policy、break-glass の role への `AdministratorAccess`）のほかには与えない。条件 `iam:PolicyARN` でその policy に限る）、`iam:CreatePolicy` などの customer managed policy の管理（決定7 の統制の policy の ARN のほかには与えない）、`iam:TagRole` などの tag の書き込み（tag を付けない）、`iam:PutRolePermissionsBoundary` と `iam:DeleteRolePermissionsBoundary`、`iam:PassRole`、`iam:RemoveRoleFromInstanceProfile`（instance profile を持たない）。`kms:CreateKey`・`kms:CreateAlias`（鍵と alias を CI で作らない。決定3）、`kms:ScheduleKeyDeletion`・`kms:DisableKey`・`kms:DisableKeyRotation`・`kms:EnableKey`・`kms:UpdateKeyDescription`・`kms:DeleteAlias`・`kms:UpdateAlias`（鍵の作り直しと alias の付け替えを権限の側でも止める）、`kms:TagResource`（tag を付けない）、暗号操作（決定3）。`organizations:DeleteOrganization`・`organizations:CloseAccount`・`account:CloseAccount`・`organizations:RemoveAccountFromOrganization`（決定3）、`iam:CreateAccessKey`・`iam:CreateLoginProfile`（break-glass の資格情報は人が作る。CI に作らせない）、`organizations:DeleteOrganizationalUnit` と break-glass の `iam:DeleteUser`・`iam:DeleteRole`（OU・break-glass の user と role は `prevent_destroy` を持ち、決定3 の「権限の側でも止める」と同じ扱いにする。user の削除が呼ぶ資格情報の削除（`iam:DeleteLoginProfile`・`iam:DeactivateMFADevice` など）も与えない）。`aws_caller_identity` の `sts:GetCallerIdentity` は権限を要さない。

## 検討した代替案

### 案A: permissions boundary を課し、Allow を広く与える

今の5つの role は IAM の role を新しく作る主体ではなく、boundary の中身を決める材料が無い。boundary の内側の Allow を広く与えると、repository-wide/ADR-0301 決定8 の Action wildcard に当たる。

### 案B: AWS managed policy（`ReadOnlyAccess` 等）を plan の role に付ける

plan の role は Pull Request の作者が任意のコードで使える資格情報であり、アカウントの全 resource を読める。repository-wide/ADR-0301 決定10 に反する。

### 案C: 後から足す構成物の権限を、まとめて先に入れる

構成物の形が決まる前は正しく列挙できず、使わない権限を先に与えることになる。鍵の権限は、鍵の形（`modules/kms`）から Provider が呼ぶ API を列挙できるので、鍵を作る変更と同じか、それより前に入れる（決定3）。

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

- 保証すること: 5つの role の policy の文書が、上の表と決定5 の Deny と決定9 から組まれていること（Unit Test）。reference の plan で、apply の role に付く inline policy が、決定5 の S3・IAM の各 ARN への読み取り以外の Action（identity root の key と lock file へはすべての Action）と、state Bucket のすべての object への `s3:DeleteObjectVersion` を条件なしの Deny で拒み、鍵ごとの ARN への Deny が `modules/kms` が Key Policy でアカウントへ委任する Action を覆うこと。bootstrap の構成物の設定を変える resource が、bootstrap の構成物を指すこと。apply の role に、その inline policy のほかの policy が付かないこと（role の引数、`aws_iam_role_policy_attachment`、`aws_iam_policy_attachment` を含む）（Policy Test）。plan の role の Allow の Action の service の名前が wildcard を含まないこと（Policy Test。`PLAN-ROLE-05`）。unlock-identity の role の Allow が決定9 の範囲に限られ、Key Policy がその復号を同じ lock file の object に限ること。unlock の role の Allow が決定11 の範囲に限られること（Policy Test）。他の role の作成が、apply-identity の inline policy の書き込みに依存すること（Static Analysis）。apply-identity の role の、plan の出力の Bucket の object に届く Allow が、`apply/*` への `s3:PutObject` だけであること。宣言した inline policy の外から apply-identity の role に権限が付かないこと。apply の role に付く inline policy が、apply の出力の Bucket の object へ、`s3:PutObject` のほかのすべての Action を条件なしの Deny で拒むこと（Policy Test）。apply の role に付く inline policy が、宣言した統制の書き込みの Action のそれぞれを、Resource `"*"` の条件なしの Deny で拒むこと（Policy Test。`PLAN-APPLY-06`）
- 保証しないこと: 列挙が Provider の実際の呼び出しを漏れなく覆うこと（段4 の後の人の確認と、CI の plan が見る）。apply の role の Deny が実際の要求で文書どおりに評価されることと、Deny の形から導く保証の外（repository-wide/ADR-0602 の保証範囲の「保証しないこと」）。resource type を持たず Resource `"*"` でしか与えられない Action を経た、bootstrap の構成物の変更（決定5 の Deny は構成物の ARN を名指す）。Policy Test の宣言（`conftest/plan/decl.json`）の型の分類のうち、親の resource の ARN で守られるとした型の設定を変える API が実際に親の ARN への Action であること（検査は無く、宣言の変更のレビューに依る）。apply-identity の inline policy の書き込みが、同じ apply の role の作成までに IAM の評価へ反映されること。反映が遅れると、その apply は `iam:CreateRole` の AccessDenied で失敗する。plan の role の Allow が、読み取りの動詞を持ちながら data を返す Action（ログの event、secret の値など）を含まないこと。Policy Test は動詞で読み取りを判定する

## 検証方法

- Unit Test: `internal/github-oidc/tests/permissions.tftest.hcl` が、5つの role の policy の文書と、inline policy と managed policy の排他を完全一致で確かめる
- 手順: `docs/setup/bootstrap.md` の段4 の手順9 と段5 の手順4 が、apply の後に `aws iam simulate-principal-policy` で、表の Action が plan と apply-identity の role に許され、与えないものと apply の role の bootstrap の構成物への変更が拒否されることを確かめる
- Policy Test（`make tf-policy`）: Allow の Action の `*`・`service:*` の不在（repository-wide/ADR-0301 (least-privilege-policy) 決定8）と、`NotAction`・`NotResource` の Allow の不在、Allow の Resource `"*"` が決定1 の Action（`kms:ListAliases`。`resource_wildcard_actions` の宣言）に限られること（同 決定9）、KMS の暗号操作と `kms:CreateGrant` の Allow の不在（決定3、同 決定12）
- Policy Test（`make tf-policy`）: plan の role の Allow が読み取りと、plan の出力の Bucket の `pr/*`・`branch/*` への `s3:PutObject` だけであることと、宣言した policy の外から plan の role に権限が付かないこと（決定8）。Action の service の名前に wildcard を使わないこと（`PLAN-ROLE-05`）
- Policy Test（`make tf-policy`、`plan.deployment_identity_apply_identity_output`）: 決定10。Bucket の object に届く S3 の Action と Resource、宣言した policy の付き先、ほかの付け方の不在を、`state-backend` の reference の plan と結合した入力で確かめる
- Policy Test（`make tf-policy`）: apply の role の inline policy が、鍵ごとに、同じ module の Key Policy がアカウントへ委任する Action のすべてを、その鍵の ARN（`state-backend` の plan の値）への Deny で拒むこと（決定5。`plan.deployment_identity_apply_role`）
- Policy Test（`make tf-policy`）: unlock-identity の role の Allow が決定9 の3つの Action と Resource に限られ、宣言した policy の外から権限が付かず、state の鍵の Key Policy がこの role に `kms:Decrypt` だけを、IAM が許す lock file の object の ARN に限って許すこと（決定9。`plan.deployment_identity_unlock_role`）
- Static Analysis（`make resource-order-check`）: apply-identity を除く role の resource が、apply-identity の inline policy の resource に依存すること（決定4）
- Policy Test（`make tf-policy`）: apply の role の inline policy が apply の role に付き、決定5 の S3・IAM の各 ARN（role・OIDC provider・Bucket は plan の値、identity root の key と lock file は宣言の `state_backend.roots` から組む）について、`*` か、読み取りの名前（動詞が Get・List・Describe・Head で wildcard を含まないもの）だけを除く `NotAction` を、条件なしの Deny で拒むこと（identity root の key と lock file は `*` だけ）。state Bucket のすべての object への `s3:DeleteObjectVersion` を拒むこと。apply の role に、その inline policy のほかの policy が付かないこと（排他の2つ、role の `inline_policy` と `managed_policy_arns`、`aws_iam_role_policy_attachment`、`aws_iam_policy_attachment`）。identity root の bootstrap の use-case の下の resource の型が宣言ですべて分類され、設定を変える resource が、宣言した属性の値で bootstrap の構成物を指すこと。宣言の形が崩れていれば落ちること（決定5・7。`plan.deployment_identity_apply_role`）。宣言が書き込みの名前（`writes`）を持つ子の ARN については、ちょうどその名前だけを除く `NotAction` で拒むこと

- Policy Test（`make tf-policy`、`plan.deployment_identity_apply_role`）: apply の role の inline policy が、`conftest/plan/decl.json` の `deployment_identity.apply_role.governance.actions` の各 Action を、Resource `"*"` の条件なしの Deny の Action に持つこと（決定5。`PLAN-APPLY-06`）。宣言が空でない文字列の空でない配列でなければ落ちること

## 影響

- identity root に構成物を足す変更と、管理アカウントに workload の usecase を足す変更は、まずこの policy を広げる変更を apply してから、その権限を使う変更を apply する。CI の role を足す変更は、決定4 の順序により1回の apply に収まる。権限の変更と、その権限で作る resource を同じ apply に入れると、両者の間に依存が無く失敗しうる
- inline policy は role あたり 10,240 文字（空白を除く）が上限である。入力の境界での長さは、`permissions.tftest.hcl` の `policy_length` が上限以下であることを固定する

## 見直し条件

- AWS Provider の版を上げるとき
- apply の role が IAM の role を作る usecase が入るとき（permissions boundary を決める）
- inline policy の文字数が上限に近づいたとき
- CI の plan か apply が AccessDenied で失敗したとき
- `modules/kms` が Key Policy でアカウントへ委任する Action を変えるとき（決定5 の鍵の Deny の列挙を合わせる）
- apply-identity の policy が、入力の境界で 10,240 文字を超える変更を入れるとき（policy を割り、決定4 の順序を見直す）
- state の鍵の作成の権限を足すとき（鍵の種類 `kms:KeySpec`・`kms:KeyUsage`、多リージョン `kms:MultiRegion`、鍵素材 `kms:KeyOrigin` を限る条件と、`resource_wildcard_actions` への `kms:CreateKey` の追加を併せて決める）
- bootstrap の構成物に、resource type を持たない Action で設定を変えられる型が入るとき（apply の role に Resource `"*"` の Deny を足すかを決める）
