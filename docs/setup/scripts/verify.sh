#!/usr/bin/env bash
# bootstrap.md の読むだけの確認を、期待値と突き合わせて実行する。何も作らず、何も変えない。
# 使い方: docs/setup/scripts/verify.sh <stage1|before-apply|stage4|permissions|stage5|output-policy>
# 前提: make aws-login 済み。aws と jq が PATH に在る。出力のアカウント ID は伏せる。
set -euo pipefail

REGION=${REGION:-ap-northeast-1}
export AWS_PROFILE=${AWS_PROFILE:-tabp-bootstrap}
# 段3 で bootstrap_executor_user_name を渡したときだけ、その user の名前を与える。
BOOTSTRAP_EXECUTOR=${BOOTSTRAP_EXECUTOR:-}
# output-policy が評価する Bucket Policy の文書の出所。live はいま Bucket に付いている文書、reference は make tf-policy が
# 書いた state-backend の reference の plan の文書（アカウント ID を置き換える）。
POLICY_SOURCE=${POLICY_SOURCE:-live}
REFERENCE_PLAN=tmp/tf-policy/state-backend.json
REFERENCE_ACCOUNT_ID=123456789012

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
KEY_ALIAS=alias/tabp-admin-tfstate
fails=0
checks=0

# jq の式。$ を含むので、展開されない heredoc に置く。
jq_program() { cat; }
JQ_KMS_ENCRYPTION=$(jq_program <<'JQ'
.ServerSideEncryptionConfiguration.Rules | length == 1 and (.[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm == "aws:kms") and (.[0].ApplyServerSideEncryptionByDefault.KMSMasterKeyID == $key) and ((.[0].BucketKeyEnabled // false) == false) and ([.[0] | .. | strings] | index("SSE-C") != null)
JQ
)
JQ_POLICY_NAMES=$(jq_program <<'JQ'
.PolicyNames == [$p]
JQ
)
JQ_EXECUTOR_EXCEPTION=$(jq_program <<'JQ'
def arr: if type == "array" then . else [.] end;
[.Statement[] | select(.Sid == "DenyObjectWriteByOthers") | .Condition.ArnNotEquals["aws:PrincipalArn"] | arr[] | select(endswith($u))] | length > 0
JQ
)
JQ_LIFECYCLE=$(jq_program <<'JQ'
.Rules | length == 1 and (.[0] | .Status == "Enabled" and ((.Filter // {}) | . == {} or . == {"Prefix": ""}) and .Expiration == {"ExpiredObjectDeleteMarker": true} and .NoncurrentVersionExpiration.NoncurrentDays == $days and .AbortIncompleteMultipartUpload.DaysAfterInitiation == 1)
JQ
)
JQ_ALIAS_ABSENT=$(jq_program <<'JQ'
[.Aliases[] | select(.AliasName == $a)] | length == 0
JQ
)
JQ_STATE_OBJECT=$(jq_program <<'JQ'
.ServerSideEncryption == "aws:kms" and .SSEKMSKeyId == $key
JQ
)
JQ_NO_USER=$(jq_program <<'JQ'
[.. | strings | select(test(":user/"))] | length == 0
JQ
)
JQ_KEY_POLICY=$(jq_program <<'JQ'
def arr: if type == "array" then . else [.] end;
def rw($p; $o): any(.Effect == "Allow" and (.Principal.AWS | arr) == [$p] and (.Action | arr | sort) == ["kms:Decrypt", "kms:GenerateDataKey"] and ((.Condition.StringLike["kms:EncryptionContext:aws:s3:arn"] | arr | sort) == ($o | sort)) and .Condition.StringEquals["kms:ViaService"] == $via);
def rd($p; $o): any(.Effect == "Allow" and (.Principal.AWS | arr) == [$p] and (.Action | arr) == ["kms:Decrypt"] and ((.Condition.StringEquals["kms:EncryptionContext:aws:s3:arn"] | arr | sort) == ($o | sort)) and .Condition.StringEquals["kms:ViaService"] == $via);
.Statement | length == 7
and any(.Effect == "Allow" and .Principal.AWS == $root)
and rw($plan; ["\($pb)/pr/*", "\($pb)/branch/*"])
and rd($plan; ["\($sb)/identity/terraform.tfstate", "\($sb)/workload/terraform.tfstate"])
and rw($ai; ["\($sb)/identity/terraform.tfstate", "\($sb)/identity/terraform.tfstate.tflock", "\($pb)/apply/*"])
and rw($apply; ["\($sb)/workload/terraform.tfstate", "\($sb)/workload/terraform.tfstate.tflock", "\($ab)/*"])
and rd($unlock; ["\($sb)/identity/terraform.tfstate.tflock"])
and rd($wunlock; ["\($sb)/workload/terraform.tfstate.tflock"])
JQ
)

ok() {
  checks=$((checks + 1))
  printf 'ok  %s\n' "$*"
}

ng() {
  checks=$((checks + 1))
  fails=$((fails + 1))
  printf 'NG  %s\n' "$*"
}

# jq の式は最後の引数。その前に --arg などを置ける。
expect_json() {
  local desc=$1 json=$2
  shift 2
  if jq -e "$@" >/dev/null 2>&1 <<<"$json"; then ok "$desc"; else ng "$desc"; fi
}

expect_error() {
  local desc=$1 code=$2
  shift 2
  local out
  if out=$("$@" 2>&1); then
    ng "${desc}（エラーが返らなかった）"
  elif grep -q "$code" <<<"$out"; then
    ok "$desc"
  else
    local got
    got=$(grep -oE 'An error occurred \([A-Za-z0-9.]+\)' <<<"$out" | head -n 1 | sed -E 's/.*\((.*)\)/\1/') || got=""
    ng "${desc}（$code ではないエラー${got:+: $got}）"
  fi
}

role_arn() { echo "arn:aws:iam::${ACCOUNT_ID}:role/tabp-admin-ci-$1"; }

# simulate-principal-policy の各 Action の判定が want であること。--context は context entry を1つ渡し、繰り返せる。
sim() {
  local want=$1 role=$2 res=$3
  shift 3
  local entries=() ctx=()
  while [[ $1 == --context ]]; do
    entries+=("$2")
    shift 2
  done
  if ((${#entries[@]} > 0)); then
    ctx=(--context-entries "${entries[@]}")
  fi
  local out n=0 bad=()
  # 代入の失敗は set -e でスクリプトごと止めるので、NG として数えて続ける。下の代入も同じ。
  if ! out=$(aws iam simulate-principal-policy --policy-source-arn "$(role_arn "$role")" --resource-arns "$res" \
    ${ctx[@]+"${ctx[@]}"} --action-names "$@" --query 'EvaluationResults[].[EvalActionName,EvalDecision]' --output text); then
    ng "$role $res: simulate-principal-policy が失敗した"
    return 0
  fi
  local action decision
  while read -r action decision; do
    [[ -n $action ]] || continue
    n=$((n + 1))
    [[ $decision == "$want" ]] || bad+=("${action}=${decision}")
  done <<<"$out"
  if ((n == $#)) && ((${#bad[@]} == 0)); then
    ok "$role $res: $# 件が $want"
  else
    ng "$role $res: $want を期待（返った件数 $n / $#、違う判定: ${bad[*]:-なし}）"
  fi
}

# 出力の Bucket の Bucket Policy の文書（$1、JSON の文字列）を Bucket の各 object に付けたものとして、role の要求を評価する。
# Bucket Policy は aws:PrincipalArn で書き手を比べるので、その値を context で role の ARN にする。
sim_bucket_policy() {
  local want=$1 role=$2 res=$3 doc=$4
  shift 4
  local out n=0 bad=()
  if ! out=$(aws iam simulate-principal-policy --policy-source-arn "$(role_arn "$role")" --caller-arn "$(role_arn "$role")" \
    --resource-arns "$res" --resource-policy "$doc" --resource-owner "$ACCOUNT_ID" \
    --context-entries "ContextKeyName=aws:PrincipalArn,ContextKeyValues=$(role_arn "$role"),ContextKeyType=string" \
    "ContextKeyName=aws:SecureTransport,ContextKeyValues=true,ContextKeyType=boolean" \
    --action-names "$@" --query 'EvaluationResults[].[EvalActionName,EvalDecision]' --output text); then
    ng "$role $res: Bucket Policy を含む simulate-principal-policy が失敗した"
    return 0
  fi
  local action decision
  while read -r action decision; do
    [[ -n $action ]] || continue
    n=$((n + 1))
    [[ $decision == "$want" ]] || bad+=("${action}=${decision}")
  done <<<"$out"
  if ((n == $#)) && ((${#bad[@]} == 0)); then
    ok "$role $res（Bucket Policy を含む）: $# 件が $want"
  else
    ng "$role $res（Bucket Policy を含む）: $want を期待（返った件数 $n / $#、違う判定: ${bad[*]:-なし}）"
  fi
}

s3() { aws s3api "$1" --bucket "$2" --expected-bucket-owner "$ACCOUNT_ID" "${@:3}"; }

state_key_arn() { aws kms describe-key --region "$REGION" --key-id "$KEY_ALIAS" --query KeyMetadata.Arn --output text; }

check_kms_encryption() {
  local key b
  key=$(state_key_arn) || key=""
  for b in "$BUCKET" "$PLAN_BUCKET" "$APPLY_BUCKET"; do
    expect_json "$b の既定の暗号化が state の鍵の SSE-KMS（Bucket Key なし、SSE-C を拒否）" "$(s3 get-bucket-encryption "$b")" \
      --arg key "$key" "$JQ_KMS_ENCRYPTION"
  done
}

stage1() {
  expect_json "Public Access Block の4項目が true" "$(s3 get-public-access-block "$BUCKET")" \
    '.PublicAccessBlockConfiguration | .BlockPublicAcls and .IgnorePublicAcls and .BlockPublicPolicy and .RestrictPublicBuckets'
  expect_json "所有権が BucketOwnerEnforced" "$(s3 get-bucket-ownership-controls "$BUCKET")" \
    '.OwnershipControls.Rules == [{"ObjectOwnership": "BucketOwnerEnforced"}]'
  expect_json "版管理が Enabled" "$(s3 get-bucket-versioning "$BUCKET")" '.Status == "Enabled"'
  expect_json "既定の暗号化が AES256 で SSE-C を拒否（違えば段2 へ進まず PR に記録して止まる）" "$(s3 get-bucket-encryption "$BUCKET")" \
    '.ServerSideEncryptionConfiguration.Rules | length == 1 and (.[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm == "AES256") and ([.[0] | .. | strings] | index("SSE-C") != null)'
}

before_apply() {
  expect_json "OIDC provider（token.actions.githubusercontent.com）がまだ無い" "$(aws iam list-open-id-connect-providers)" \
    '[.OpenIDConnectProviderList[].Arn | select(endswith("/token.actions.githubusercontent.com"))] | length == 0'
  local r
  for r in plan apply apply-identity unlock-identity unlock; do
    expect_error "role tabp-admin-ci-$r がまだ無い" NoSuchEntity aws iam get-role --role-name "tabp-admin-ci-$r"
  done
  expect_error "state Bucket に Bucket Policy がまだ無い" NoSuchBucketPolicy s3 get-bucket-policy "$BUCKET"
  expect_error "state Bucket に lifecycle がまだ無い" NoSuchLifecycleConfiguration s3 get-bucket-lifecycle-configuration "$BUCKET"
  # 在ると、段4 の apply は鍵を作った後の alias の作成で落ち、alias の無い鍵が残る。
  expect_json "state の鍵の alias（${KEY_ALIAS}）がまだ無い" "$(aws kms list-aliases --region "$REGION")" \
    --arg a "$KEY_ALIAS" "$JQ_ALIAS_ABSENT"
  # 段4 は統制の構成物も作る（ADR-0602 決定1）。既に組織を持つ管理アカウントは organization-baseline の unsupported。
  expect_error "組織がまだ無い" AWSOrganizationsNotInUseException aws organizations describe-organization
  local u
  for u in break-glass-1 break-glass-2; do
    expect_error "IAM user ${u} がまだ無い" NoSuchEntity aws iam get-user --user-name "$u"
  done
  expect_error "IAM group break-glass がまだ無い" NoSuchEntity aws iam get-group --group-name break-glass
  expect_error "role tabp-break-glass がまだ無い" NoSuchEntity aws iam get-role --role-name tabp-break-glass
  local p
  for p in apply-identity-governance apply-workload boundary-ops-notification; do
    expect_error "policy tabp-admin-ci-$p がまだ無い" NoSuchEntity aws iam get-policy --policy-arn "arn:aws:iam::${ACCOUNT_ID}:policy/tabp-admin-ci-$p"
  done
  # 在ると、段4 の apply は import せずに上書きする。
  expect_error "account password policy がまだ無い" NoSuchEntity aws iam get-account-password-policy
}

stage4() {
  check_kms_encryption
  expect_json "OIDC provider の ClientIDList が sts.amazonaws.com だけ" \
    "$(aws iam get-open-id-connect-provider --open-id-connect-provider-arn "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com")" \
    '.ClientIDList == ["sts.amazonaws.com"]'
  local r p
  for r in plan:plan apply:apply apply-identity:apply_identity unlock-identity:unlock_identity unlock:unlock; do
    p=${r#*:}
    r=${r%%:*}
    expect_json "role tabp-admin-ci-$r の inline policy が $p だけ" "$(aws iam list-role-policies --role-name "tabp-admin-ci-$r")" \
      --arg p "$p" "$JQ_POLICY_NAMES"
    if [[ $r == apply-identity ]]; then
      expect_json "role tabp-admin-ci-$r に付く managed policy が、統制の policy だけ" "$(aws iam list-attached-role-policies --role-name "tabp-admin-ci-$r")" \
        '.AttachedPolicies | map(.PolicyName) | . == ["tabp-admin-ci-apply-identity-governance"]'
    elif [[ $r == apply ]]; then
      expect_json "role tabp-admin-ci-$r に付く managed policy が、workload の2つの policy だけ" "$(aws iam list-attached-role-policies --role-name "tabp-admin-ci-$r")" \
        '.AttachedPolicies | map(.PolicyName) | sort | . == ["tabp-admin-ci-apply-workload", "tabp-admin-ci-apply-workload-read"]'
    else
      expect_json "role tabp-admin-ci-$r に managed policy が付いていない" "$(aws iam list-attached-role-policies --role-name "tabp-admin-ci-$r")" \
        '.AttachedPolicies == []'
    fi
  done
  local policy
  policy=$(s3 get-bucket-policy "$BUCKET" --query Policy --output text) || policy=""
  expect_json "state Bucket の Bucket Policy の文がすべて Deny で、TLS でない要求を拒む文が在る" "$policy" \
    '(.Statement | all(.Effect == "Deny")) and (.Statement | any(.Condition.Bool["aws:SecureTransport"] == "false"))'
  if [[ -n $BOOTSTRAP_EXECUTOR ]]; then
    expect_json "Bucket Policy の書き込みの例外（DenyObjectWriteByOthers）に、段4 の実行者 ${BOOTSTRAP_EXECUTOR} が在る" "$policy" \
      --arg u ":user/${BOOTSTRAP_EXECUTOR}" "$JQ_EXECUTOR_EXCEPTION"
  else
    expect_json "Bucket Policy に IAM user が無い（段3 で段4 の実行者を例外に入れていない）" "$policy" "$JQ_NO_USER"
  fi
  local days
  days=$(sed -nE 's/^[[:space:]]*state_noncurrent_version_retention_days[[:space:]]*=[[:space:]]*([0-9]+).*/\1/p' "$ROOT/env/admin/identity/main.tf")
  expect_json "lifecycle が規則1つ（版の保持 ${days:-?} 日、delete marker の掃除、未完了の multipart の中止 1 日）" \
    "$(s3 get-bucket-lifecycle-configuration "$BUCKET")" --argjson days "${days:-null}" \
    "$JQ_LIFECYCLE"
  expect_json "state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）" "$(aws kms describe-key --region "$REGION" --key-id "$KEY_ALIAS")" \
    '.KeyMetadata | .KeyState == "Enabled" and .KeySpec == "SYMMETRIC_DEFAULT" and .KeyUsage == "ENCRYPT_DECRYPT" and .MultiRegion == false and .Origin == "AWS_KMS"'
  # 段4 は統制の構成物も作る（ADR-0602 決定1）。資格情報は organization-baseline.md の 3.4 で人が設定する。
  expect_json "組織の FeatureSet が ALL" "$(aws organizations describe-organization)" '.Organization.FeatureSet == "ALL"'
  local u
  for u in break-glass-1 break-glass-2; do
    expect_json "IAM user ${u} がアクセスキーを持たない" "$(aws iam list-access-keys --user-name "$u")" '.AccessKeyMetadata == []'
  done
  expect_json "role tabp-break-glass の信頼条件が MFA を求める" "$(aws iam get-role --role-name tabp-break-glass)" \
    '.Role.AssumeRolePolicyDocument.Statement | length > 0 and all(.Condition.Bool["aws:MultiFactorAuthPresent"] == "true")'
  expect_json "state の鍵の自動 rotation が 365 日" "$(aws kms get-key-rotation-status --region "$REGION" --key-id "$KEY_ALIAS")" \
    '.KeyRotationEnabled == true and .RotationPeriodInDays == 365'
  expect_json "Key Policy が、アカウントへの委任・plan と apply-identity と apply の読み書きする object に限った S3 経由の暗号操作・plan の state の復号・unlock-identity と unlock のそれぞれの lock file の復号の7文だけ" \
    "$(aws kms get-key-policy --region "$REGION" --key-id "$KEY_ALIAS" --policy-name default --query Policy --output text)" \
    --arg root "arn:aws:iam::${ACCOUNT_ID}:root" --arg plan "$(role_arn plan)" --arg ai "$(role_arn apply-identity)" \
    --arg apply "$(role_arn apply)" --arg unlock "$(role_arn unlock-identity)" --arg wunlock "$(role_arn unlock)" --arg via "s3.${REGION}.amazonaws.com" \
    --arg sb "arn:aws:s3:::${BUCKET}" --arg pb "arn:aws:s3:::${PLAN_BUCKET}" --arg ab "arn:aws:s3:::${APPLY_BUCKET}" \
    "$JQ_KEY_POLICY"
}

permissions() {
  local reads=(s3:GetBucketAcl s3:GetBucketCORS s3:GetBucketWebsite s3:GetAccelerateConfiguration s3:GetBucketRequestPayment
    s3:GetBucketLogging s3:GetLifecycleConfiguration s3:GetReplicationConfiguration s3:GetBucketObjectLockConfiguration
    s3:GetBucketTagging s3:GetBucketPolicy s3:GetBucketVersioning s3:GetEncryptionConfiguration
    s3:GetBucketPublicAccessBlock s3:GetBucketOwnershipControls)
  local b="arn:aws:s3:::${BUCKET}" key="arn:aws:s3:::${BUCKET}/identity/terraform.tfstate" pb="arn:aws:s3:::${PLAN_BUCKET}" ab="arn:aws:s3:::${APPLY_BUCKET}"

  echo "# S3 と IAM"
  sim allowed plan "$b" "${reads[@]}"
  sim allowed apply-identity "$b" "${reads[@]}" s3:PutBucketPolicy s3:PutLifecycleConfiguration
  sim allowed apply-identity "$key" s3:PutObject
  sim allowed apply-identity "$(role_arn plan)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy
  sim allowed plan "$pb" "${reads[@]}"
  sim allowed plan "${pb}/pr/1/x" s3:PutObject
  sim allowed apply-identity "$pb" "${reads[@]}" s3:PutLifecycleConfiguration
  sim allowed apply-identity "${pb}/apply/1/x" s3:PutObject
  sim allowed plan "$ab" s3:ListBucket "${reads[@]}"
  sim allowed apply-identity "$ab" s3:CreateBucket s3:ListBucket "${reads[@]}" s3:PutBucketPolicy s3:PutLifecycleConfiguration
  sim implicitDeny apply-identity "$ab" s3:DeleteBucket
  sim implicitDeny plan "$ab" s3:CreateBucket s3:PutBucketPolicy
  sim allowed apply "$b" s3:ListBucket s3:GetEncryptionConfiguration
  sim implicitDeny plan "$key" s3:PutObject
  sim implicitDeny plan "$(role_arn plan)" iam:PutRolePolicy
  sim implicitDeny plan "${pb}/pr/1/x" s3:DeleteObject s3:DeleteObjectVersion
  sim implicitDeny plan "${pb}/x" s3:PutObject
  sim implicitDeny apply-identity "${pb}/pr/1/x" s3:PutObject s3:DeleteObject
  sim implicitDeny apply-identity "${pb}/apply/1/x" s3:DeleteObject s3:DeleteObjectVersion
  sim explicitDeny apply "$(role_arn apply-identity)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy sts:AssumeRole
  sim explicitDeny apply "$b" s3:PutBucketPolicy s3:DeleteBucketPolicy s3:PutLifecycleConfiguration s3:PutBucketTagging s3:TagResource
  sim explicitDeny apply "$key" s3:PutObject s3:DeleteObject
  sim explicitDeny apply "$pb" s3:PutBucketPolicy s3:PutLifecycleConfiguration s3:GetBucketPolicy
  sim explicitDeny apply "${pb}/pr/1/x" s3:PutObject s3:DeleteObjectVersion
  sim allowed apply "${ab}/x" s3:PutObject
  sim explicitDeny apply "${ab}/x" s3:GetObject s3:DeleteObject s3:DeleteObjectVersion s3:PutObjectAcl
  sim explicitDeny apply "$ab" s3:ListBucket s3:GetBucketPolicy s3:PutBucketPolicy s3:PutLifecycleConfiguration s3:DeleteBucket

  echo "# state の鍵"
  local sk other="arn:aws:kms:${REGION}:${ACCOUNT_ID}:key/00000000-0000-0000-0000-000000000000"
  sk=$(state_key_arn) || {
    ng "state の鍵の ARN（${KEY_ALIAS}）を読めない"
    sk="arn:aws:kms:${REGION}:${ACCOUNT_ID}:key/unknown"
  }
  sim allowed apply-identity "$sk" kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags kms:EnableKeyRotation kms:PutKeyPolicy
  sim allowed apply-identity "*" kms:ListAliases
  sim allowed plan "$sk" kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags
  sim allowed plan "*" kms:ListAliases
  sim allowed apply "*" kms:ListAliases
  sim implicitDeny apply-identity "*" kms:CreateKey
  sim implicitDeny apply-identity "$sk" kms:CreateAlias kms:ScheduleKeyDeletion kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:UpdateKeyDescription kms:TagResource kms:Decrypt
  sim implicitDeny apply-identity "$other" kms:DescribeKey kms:PutKeyPolicy
  sim implicitDeny plan "$sk" kms:PutKeyPolicy kms:Decrypt kms:GenerateDataKey
  sim implicitDeny plan "$other" kms:DescribeKey
  sim implicitDeny apply "$sk" kms:Decrypt kms:GenerateDataKey
  sim explicitDeny apply "$sk" kms:CancelKeyDeletion kms:CreateAlias kms:DeleteAlias kms:DescribeKey kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:EnableKeyRotation kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListGrants kms:ListKeyPolicies kms:ListResourceTags kms:PutKeyPolicy kms:RevokeGrant kms:RotateKeyOnDemand kms:ScheduleKeyDeletion kms:TagResource kms:UntagResource kms:UpdateAlias kms:UpdateKeyDescription
  sim explicitDeny apply-identity "*" --context "ContextKeyName=kms:BypassPolicyLockoutSafetyCheck,ContextKeyValues=true,ContextKeyType=boolean" kms:PutKeyPolicy

  echo "# 統制の構成物（deployment-identity/ADR-0201 決定3・5・7）"
  local gp="arn:aws:iam::${ACCOUNT_ID}:policy/tabp-admin-ci-apply-identity-governance"
  local bg="arn:aws:iam::${ACCOUNT_ID}:role/break-glass/tabp-break-glass"
  sim allowed apply-identity "$gp" iam:CreatePolicy iam:CreatePolicyVersion iam:DeletePolicyVersion iam:SetDefaultPolicyVersion iam:GetPolicy iam:GetPolicyVersion iam:ListPolicyVersions
  sim implicitDeny apply-identity "arn:aws:iam::${ACCOUNT_ID}:policy/other" iam:CreatePolicy iam:CreatePolicyVersion
  sim allowed apply-identity "$(role_arn apply-identity)" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=${gp},ContextKeyType=arn" iam:AttachRolePolicy
  sim implicitDeny apply-identity "$(role_arn apply-identity)" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=arn:aws:iam::aws:policy/AdministratorAccess,ContextKeyType=arn" iam:AttachRolePolicy
  sim allowed plan "*" organizations:DescribeOrganization organizations:ListRoots organizations:ListAccounts iam:ListOrganizationsFeatures iam:GetAccountPasswordPolicy
  sim allowed plan "$gp" iam:GetPolicy iam:GetPolicyVersion
  sim implicitDeny plan "*" organizations:CreateOrganization iam:UpdateAccountPasswordPolicy
  sim explicitDeny apply "*" organizations:CreateOrganization organizations:AttachPolicy organizations:LeaveOrganization account:CloseAccount iam:EnableOrganizationsRootSessions iam:UpdateAccountPasswordPolicy sso:CreatePermissionSet sso:AttachManagedPolicyToPermissionSet sso:DeleteAccountAssignment sso:PutPermissionsPolicy sts:AssumeRoot
  sim implicitDeny apply "*" sso:DescribePermissionSet sso:GetPermissionSet sso:ListAccountAssignments
  sim explicitDeny apply "$bg" iam:UpdateAssumeRolePolicy iam:AttachRolePolicy
  sim explicitDeny apply "$gp" iam:CreatePolicyVersion
  if [[ $(aws iam list-attached-role-policies --role-name tabp-admin-ci-apply-identity --query 'AttachedPolicies[].PolicyName' --output text) == tabp-admin-ci-apply-identity-governance ]]; then
    sim allowed apply-identity "*" organizations:CreateOrganization organizations:DescribeOrganization iam:UpdateAccountPasswordPolicy
    sim allowed apply-identity "$bg" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=arn:aws:iam::aws:policy/AdministratorAccess,ContextKeyType=arn" iam:AttachRolePolicy
    sim implicitDeny apply-identity "*" organizations:DeleteOrganization
    sim implicitDeny apply-identity "$bg" iam:DeleteRole
  else
    echo "  統制の policy がまだ付いていないので、作成と設定の権限は確かめない（統制の policy を作って付ける apply の後にもう一度走らせる）"
  fi

  echo "# workload の権限の policy と permissions boundary（deployment-identity/ADR-0201 決定5・13-17）"
  local wp="arn:aws:iam::${ACCOUNT_ID}:policy/tabp-admin-ci-apply-workload"
  local wrp="arn:aws:iam::${ACCOUNT_ID}:policy/tabp-admin-ci-apply-workload-read"
  local bp="arn:aws:iam::${ACCOUNT_ID}:policy/tabp-admin-ci-boundary-ops-notification"
  sim allowed apply-identity "$wp" iam:CreatePolicy iam:CreatePolicyVersion iam:DeletePolicyVersion iam:SetDefaultPolicyVersion iam:GetPolicy iam:GetPolicyVersion iam:ListPolicyVersions
  sim allowed apply-identity "$wrp" iam:CreatePolicy iam:CreatePolicyVersion iam:DeletePolicyVersion iam:SetDefaultPolicyVersion iam:GetPolicy iam:GetPolicyVersion iam:ListPolicyVersions
  sim allowed apply-identity "$bp" iam:CreatePolicy iam:CreatePolicyVersion iam:DeletePolicyVersion iam:SetDefaultPolicyVersion iam:GetPolicy iam:GetPolicyVersion iam:ListPolicyVersions
  sim allowed apply-identity "$(role_arn apply)" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=${wp},ContextKeyType=arn" iam:AttachRolePolicy
  sim allowed apply-identity "$(role_arn apply)" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=${wrp},ContextKeyType=arn" iam:AttachRolePolicy
  sim implicitDeny apply-identity "$(role_arn apply)" --context "ContextKeyName=iam:PolicyARN,ContextKeyValues=${bp},ContextKeyType=arn" iam:AttachRolePolicy
  sim allowed plan "$wp" iam:GetPolicy iam:GetPolicyVersion
  sim allowed plan "$wrp" iam:GetPolicy iam:GetPolicyVersion
  # workload の権限は provider の region と us-east-1 に及び、ほかの region には及ばない（ADR-0208 決定4）。
  sim allowed plan "arn:aws:sns:us-east-1:${ACCOUNT_ID}:tabp-admin-ops-notification-us-east-1" sns:GetTopicAttributes sns:ListTagsForResource
  local other_region=us-west-2
  [[ $REGION == "$other_region" ]] && other_region=eu-west-1
  sim implicitDeny plan "arn:aws:sns:${other_region}:${ACCOUNT_ID}:tabp-admin-ops-notification" sns:GetTopicAttributes
  sim allowed plan "arn:aws:sns:${REGION}:${ACCOUNT_ID}:tabp-admin-ops-notification" sns:GetTopicAttributes sns:ListTagsForResource
  sim implicitDeny plan "arn:aws:sns:${REGION}:${ACCOUNT_ID}:tabp-admin-ops-notification" sns:SetTopicAttributes
  local wk="arn:aws:kms:${REGION}:${ACCOUNT_ID}:key/00000000-0000-0000-0000-000000000001"
  local sym="ContextKeyName=kms:KeySpec,ContextKeyValues=SYMMETRIC_DEFAULT,ContextKeyType=string"
  sim allowed plan "$wk" --context "$sym" --context "ContextKeyName=kms:KeyUsage,ContextKeyValues=ENCRYPT_DECRYPT,ContextKeyType=string" --context "ContextKeyName=kms:KeyOrigin,ContextKeyValues=AWS_KMS,ContextKeyType=string" --context "ContextKeyName=kms:MultiRegion,ContextKeyValues=false,ContextKeyType=boolean" kms:DescribeKey
  sim implicitDeny plan "$wk" --context "ContextKeyName=kms:KeySpec,ContextKeyValues=RSA_2048,ContextKeyType=string" --context "ContextKeyName=kms:KeyUsage,ContextKeyValues=SIGN_VERIFY,ContextKeyType=string" --context "ContextKeyName=kms:KeyOrigin,ContextKeyValues=AWS_KMS,ContextKeyType=string" --context "ContextKeyName=kms:MultiRegion,ContextKeyValues=false,ContextKeyType=boolean" kms:DescribeKey
  sim explicitDeny apply "$wp" iam:CreatePolicyVersion iam:DeletePolicy
  sim explicitDeny apply "$wrp" iam:CreatePolicyVersion iam:DeletePolicy
  sim explicitDeny apply "$bp" iam:CreatePolicyVersion iam:DeletePolicy
  sim explicitDeny apply "arn:aws:iam::${ACCOUNT_ID}:role/tabp-admin-ci-other" iam:CreateRole iam:PassRole
  sim explicitDeny apply "*" iam:DeleteRolePermissionsBoundary
  sim explicitDeny apply "*" --context "ContextKeyName=kms:BypassPolicyLockoutSafetyCheck,ContextKeyValues=true,ContextKeyType=boolean" kms:CreateKey kms:PutKeyPolicy
  local attached
  attached=$(aws iam list-attached-role-policies --role-name tabp-admin-ci-apply --query 'sort(AttachedPolicies[].PolicyName)' --output text)
  if [[ $attached == $'tabp-admin-ci-apply-workload\ttabp-admin-ci-apply-workload-read' ]]; then
    local or="arn:aws:iam::${ACCOUNT_ID}:role/tabp-admin-ops-heartbeat" pbctx="ContextKeyName=iam:PermissionsBoundary,ContextKeyValues=${bp},ContextKeyType=arn"
    sim allowed apply "$or" --context "$pbctx" iam:CreateRole iam:PutRolePolicy iam:DeleteRole
    sim implicitDeny apply "$or" iam:CreateRole iam:PutRolePolicy
    sim implicitDeny apply "arn:aws:iam::${ACCOUNT_ID}:role/tabp-admin-OPS-heartbeat" --context "$pbctx" iam:CreateRole
    sim allowed apply "$or" --context "ContextKeyName=iam:PassedToService,ContextKeyValues=scheduler.amazonaws.com,ContextKeyType=string" iam:PassRole
    sim implicitDeny apply "$or" --context "ContextKeyName=iam:PassedToService,ContextKeyValues=lambda.amazonaws.com,ContextKeyType=string" iam:PassRole
    sim allowed apply "arn:aws:sns:${REGION}:${ACCOUNT_ID}:tabp-admin-ops-notification" sns:CreateTopic sns:SetTopicAttributes
    sim implicitDeny apply "arn:aws:sns:${REGION}:${ACCOUNT_ID}:tabp-admin-ops-notification" sns:DeleteTopic sns:Subscribe
    sim implicitDeny apply "$wk" kms:ScheduleKeyDeletion
  elif [[ -z $attached || $attached == None ]]; then
    echo "  workload の2つの policy がまだ付いていないので、apply の role の workload の権限は確かめない（2つの policy を作って付ける apply の後にもう一度走らせる）"
  else
    ng "apply の role に付く managed policy が workload の2つの policy ではない（${attached}）"
  fi

  echo "# unlock-identity の role"
  local lock="${key}.tflock"
  sim allowed unlock-identity "$b" --context "ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate.tflock,ContextKeyType=string" s3:ListBucket
  sim allowed unlock-identity "$lock" s3:GetObject s3:DeleteObject
  sim implicitDeny unlock-identity "$b" --context "ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate,ContextKeyType=string" s3:ListBucket
  sim implicitDeny unlock-identity "$lock" s3:PutObject s3:DeleteObjectVersion
  sim implicitDeny unlock-identity "$key" s3:GetObject s3:PutObject s3:DeleteObject
  sim implicitDeny unlock-identity "$sk" kms:Decrypt

  echo "# unlock の role"
  local wkey="arn:aws:s3:::${BUCKET}/workload/terraform.tfstate"
  local wlock="${wkey}.tflock"
  sim allowed unlock "$b" --context "ContextKeyName=s3:prefix,ContextKeyValues=workload/terraform.tfstate.tflock,ContextKeyType=string" s3:ListBucket
  sim allowed unlock "$wlock" s3:GetObject s3:DeleteObject
  sim implicitDeny unlock "$b" --context "ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate.tflock,ContextKeyType=string" s3:ListBucket
  sim implicitDeny unlock "$wlock" s3:PutObject s3:DeleteObjectVersion
  sim implicitDeny unlock "$lock" s3:GetObject s3:DeleteObject
  sim implicitDeny unlock "$wkey" s3:GetObject s3:PutObject s3:DeleteObject
  sim implicitDeny unlock "$sk" kms:Decrypt
}

reference_policy() {
  jq -er --arg a "module.state_backend.module.$1.aws_s3_bucket_policy.this" \
    '.resource_changes[] | select(.address == $a) | .change.after.policy' "$ROOT/$REFERENCE_PLAN" |
    LC_ALL=C sed "s/${REFERENCE_ACCOUNT_ID}/${ACCOUNT_ID}/g"
}

# 出力の2つの Bucket の Bucket Policy が書き手を拒まず、書き手の外と削除を拒むこと（state-backend/ADR-0201 決定8）。
output_policy() {
  local pb="arn:aws:s3:::${PLAN_BUCKET}" ab="arn:aws:s3:::${APPLY_BUCKET}" pdoc adoc
  case $POLICY_SOURCE in
  live)
    pdoc=$(s3 get-bucket-policy "$PLAN_BUCKET" --query Policy --output text) || pdoc=""
    adoc=$(s3 get-bucket-policy "$APPLY_BUCKET" --query Policy --output text) || adoc=""
    ;;
  reference)
    pdoc=$(reference_policy plan_output) || pdoc=""
    adoc=$(reference_policy apply_output) || adoc=""
    ;;
  *)
    ng "POLICY_SOURCE が live でも reference でもない（${POLICY_SOURCE}）"
    return 0
    ;;
  esac
  echo "# 文書の出所: ${POLICY_SOURCE}"
  expect_json "plan の出力の Bucket の Bucket Policy の文書を読めた" "$pdoc" '.Statement | length > 0'
  expect_json "apply の出力の Bucket の Bucket Policy の文書を読めた" "$adoc" '.Statement | length > 0'

  echo "# plan の出力の Bucket"
  sim_bucket_policy allowed plan "${pb}/pr/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy allowed plan "${pb}/branch/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy allowed apply-identity "${pb}/apply/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy explicitDeny plan "${pb}/apply/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy explicitDeny apply-identity "${pb}/pr/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy explicitDeny apply "${pb}/pr/1/x" "$pdoc" s3:PutObject
  sim_bucket_policy explicitDeny plan "${pb}/pr/1/x" "$pdoc" s3:DeleteObject s3:DeleteObjectVersion
  sim_bucket_policy explicitDeny apply-identity "${pb}/apply/1/x" "$pdoc" s3:DeleteObject s3:DeleteObjectVersion

  echo "# apply の出力の Bucket"
  sim_bucket_policy allowed apply "${ab}/x" "$adoc" s3:PutObject
  sim_bucket_policy explicitDeny apply-identity "${ab}/x" "$adoc" s3:PutObject
  sim_bucket_policy explicitDeny apply "${ab}/x" "$adoc" s3:DeleteObject s3:DeleteObjectVersion
}

stage5() {
  check_kms_encryption
  expect_json "identity root の state が state の鍵の SSE-KMS で書かれている" \
    "$(s3 head-object "$BUCKET" --key identity/terraform.tfstate)" --arg key "$(state_key_arn)" "$JQ_STATE_OBJECT"
  expect_json "Bucket Policy の例外に IAM user（段4 の実行者）が無い" \
    "$(s3 get-bucket-policy "$BUCKET" --query Policy --output text)" '[.. | strings | select(test(":user/"))] | length == 0'
}

main() {
  local stage=${1:-}
  case $stage in
  stage1 | before-apply | stage4 | permissions | stage5 | output-policy) ;;
  *)
    echo "使い方: $0 <stage1|before-apply|stage4|permissions|stage5|output-policy>" >&2
    return 2
    ;;
  esac
  local cmd
  for cmd in aws jq; do
    command -v "$cmd" >/dev/null || {
      echo "$cmd が PATH にありません" >&2
      return 2
    }
  done
  if [[ ! $ACCOUNT_ID =~ ^[0-9]{12}$ ]]; then
    echo "ACCOUNT_ID が12桁ではありません。make aws-login をやり直してください" >&2
    return 2
  fi
  BUCKET="tabp-admin-tfstate-${ACCOUNT_ID}-${REGION}-an"
  PLAN_BUCKET=${BUCKET/-tfstate-/-tfplan-}
  APPLY_BUCKET=${BUCKET/-tfstate-/-tfapply-}
  echo "# ${stage}（profile: ${AWS_PROFILE}、region: ${REGION}）"
  case $stage in
  stage1) stage1 ;;
  before-apply) before_apply ;;
  stage4) stage4 ;;
  permissions) permissions ;;
  stage5) stage5 ;;
  output-policy) output_policy ;;
  esac
  echo "# 結果: ${checks} 件のうち NG ${fails} 件"
  ((checks > 0 && fails == 0))
}

# 出力をそのまま Pull Request に貼れるように、アカウント ID を伏せる。
ACCOUNT_ID=""
if command -v aws >/dev/null; then
  ACCOUNT_ID=$(aws sts get-caller-identity --region "$REGION" --query Account --output text 2>/dev/null) || ACCOUNT_ID=""
fi
if [[ $ACCOUNT_ID =~ ^[0-9]{12}$ ]]; then
  main "$@" 2>&1 | LC_ALL=C sed "s/${ACCOUNT_ID}/<account>/g"
else
  main "$@"
fi
exit "${PIPESTATUS[0]}"
