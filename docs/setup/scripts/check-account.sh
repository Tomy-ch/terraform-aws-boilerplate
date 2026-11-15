#!/usr/bin/env bash
# アカウントの準備（1-0 から 2-2）の結果を、一時 IAM user の資格情報で読んで確かめる。何も作らず、何も変えない。
# 使い方: docs/setup/scripts/check-account.sh
# 前提: make aws-login 済み。aws と jq が PATH に在る。
# 終了コード: 0 はすべて ok、1 は NG あり、2 は確かめられない。
set -euo pipefail

export AWS_PROFILE=${AWS_PROFILE:-tabp-bootstrap}
fails=0
checks=0

ok() {
  checks=$((checks + 1))
  printf 'ok  %s\n' "$1"
}

ng() {
  checks=$((checks + 1))
  fails=$((fails + 1))
  printf 'NG  %s\n' "$1"
}

# aws の呼び出し（第2引数以降）の JSON が jq の式（環境変数 Q）を満たせば ok。呼び出しの失敗も NG。
expect() {
  local desc=$1 json
  shift
  if json=$(aws "$@" --output json 2>/dev/null) && jq -e "$Q" >/dev/null 2>&1 <<<"$json"; then
    ok "$desc"
  else
    ng "$desc"
  fi
}

main() {
  if (($# != 0)); then
    echo "使い方: $0" >&2
    return 2
  fi
  local cmd
  for cmd in aws jq; do
    command -v "$cmd" >/dev/null || {
      echo "$cmd が PATH にありません" >&2
      return 2
    }
  done
  local arn user
  arn=$(aws sts get-caller-identity --query Arn --output text 2>/dev/null) || arn=""
  if [[ ! $arn =~ ^arn:aws:iam::[0-9]{12}:user/([A-Za-z0-9+=,.@_-]+)$ ]]; then
    echo "一時 IAM user の資格情報ではありません。make aws-login をやり直してください" >&2
    return 2
  fi
  user=${BASH_REMATCH[1]}
  echo "# アカウントの準備（profile: ${AWS_PROFILE}、user: ${user}）"

  Q='.accountPlanType == "PAID"' expect "1-0 アカウントが Paid plan" freetier get-account-plan-state
  Q='.SummaryMap.AccountMFAEnabled == 1' expect "1-1 root に MFA が在る（2台以上かはコンソールで見る）" iam get-account-summary
  Q='[.AttachedPolicies[].PolicyName] | sort == ["AdministratorAccess", "SignInLocalDevelopmentAccess"]' \
    expect "1-3 一時 IAM user のポリシーが AdministratorAccess と SignInLocalDevelopmentAccess だけ" \
    iam list-attached-user-policies --user-name "$user"
  Q='.User.Path == "/"' expect "1-3 一時 IAM user の path が /" iam get-user --user-name "$user"
  Q='.MFADevices | length >= 1' expect "1-3 一時 IAM user に MFA が在る" iam list-mfa-devices --user-name "$user"
  Q='.AccessKeyMetadata | length == 0' expect "1-3 一時 IAM user にアクセスキーが無い" iam list-access-keys --user-name "$user"
  Q='.SummaryMap.AccountAccessKeysPresent == 0' expect "1-4 root にアクセスキーが無い" iam get-account-summary
  Q='.ContactInformation | (.FullName // "") != "" and (.PhoneNumber // "") != ""' \
    expect "2-1 連絡先に氏名と電話番号が在る" account get-contact-information
  local type
  for type in BILLING OPERATIONS SECURITY; do
    Q='.AlternateContact.AlternateContactType != null' \
      expect "2-1 代替連絡先 ${type} が在る" account get-alternate-contact --alternate-contact-type "$type"
  done
  Q='.AnomalyMonitors | length >= 1' expect "2-2 Cost Anomaly Detection の monitor が在る" ce get-anomaly-monitors
  Q='.AnomalySubscriptions | length >= 1' expect "2-2 Cost Anomaly Detection の通知が在る" ce get-anomaly-subscriptions

  echo "# 結果: ${checks} 件のうち NG ${fails} 件"
  ((checks > 0 && fails == 0)) || return 1
}

# 出力をそのまま Pull Request に貼れるように、12桁の数字（アカウント ID）を伏せる。
main "$@" 2>&1 | LC_ALL=C sed -E 's/[0-9]{12}/<account>/g'
exit "${PIPESTATUS[0]}"
