#!/usr/bin/env bash
# 保存した identity root の plan を check-plan.expected と突き合わせる。何も作らず、何も変えない。
# 使い方: docs/setup/scripts/check-plan.sh <plan のファイル>
# 前提: env/admin/identity が init 済み。terraform と jq が PATH に在る。
# 終了コード: 0 は一致、1 は差分あり、2 は plan を読めず検証できない。
# 出力するのは resource address と変更の種類だけで、属性の値は出さない。
set -euo pipefail
# sort と comm の照合順序を揃える。
export LC_ALL=C

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
EXPECTED="$HERE/check-plan.expected"
TERRAFORM=${TERRAFORM:-terraform}
KINDS=(import add change destroy)

# plan の JSON から「<種類> <address>」を1行ずつ出す。種類の数え方は terraform plan の要約行と同じで、
# 置き換えは add と destroy の両方に数える。知らない actions は種類ごと出して差分にする。
JQ_CHANGES=$(
  cat <<'JQ'
if (.format_version | type) != "string" then error("plan の JSON ではありません")
elif .errored == true then error("plan がエラーで終わっています")
elif .complete == false then error("plan が完全ではありません（deferred な変更があります）")
else
  (.resource_changes // [])[] | select(.mode == "managed")
  | .address as $a | .change.actions as $act
  | (if .change.importing != null then "import \($a)" else empty end),
    (if $act == ["no-op"] then empty
     elif $act == ["create"] then "add \($a)"
     elif $act == ["update"] then "change \($a)"
     elif $act == ["delete"] then "destroy \($a)"
     elif $act == ["delete", "create"] or $act == ["create", "delete"] then "add \($a)", "destroy \($a)"
     else "unknown(\($act | join(","))) \($a)"
     end)
end
JQ
)

count() { grep -c "^$1 " <<<"$2" || true; }

summary() {
  local label=$1 lines=$2 kind s=""
  for kind in "${KINDS[@]}"; do s+="${s:+ / }${kind} $(count "$kind" "$lines")"; done
  echo "# ${label}: ${s}"
}

report() {
  local line
  while read -r line; do
    [[ -n $line ]] && echo "NG  $1  $line"
  done <<<"$2"
  return 0
}

main() {
  if (($# != 1)); then
    echo "使い方: $0 <plan のファイル>" >&2
    return 2
  fi
  local plan cmd
  for cmd in "$TERRAFORM" jq; do
    command -v "$cmd" >/dev/null || {
      echo "$cmd が PATH にありません" >&2
      return 2
    }
  done
  [[ -f $1 ]] || {
    echo "plan のファイルがありません: $1" >&2
    return 2
  }
  plan=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")

  local expected
  expected=$(grep -vE '^[[:space:]]*(#|$)' "$EXPECTED" | sort)
  [[ -n $expected ]] || {
    echo "期待値が空です: $EXPECTED" >&2
    return 2
  }

  # terraform の振る舞いを変える TF_ の環境変数を外し、CLI の設定（dev_overrides）を読ませない。
  local unset_tf=() name
  while read -r name; do
    [[ $name == TF_* ]] && unset_tf+=(-u "$name")
  done < <(compgen -e)
  local json actual
  if ! json=$(env ${unset_tf[@]+"${unset_tf[@]}"} TF_CLI_CONFIG_FILE=/dev/null CHECKPOINT_DISABLE=1 \
    "$TERRAFORM" -chdir="$ROOT/env/admin/identity" show -json "$plan"); then
    echo "terraform show が失敗しました。plan を検証できません" >&2
    return 2
  fi
  if ! actual=$(jq -r "$JQ_CHANGES" <<<"$json"); then
    echo "plan の JSON を読めません。plan を検証できません" >&2
    return 2
  fi
  actual=$(sort <<<"$actual")

  summary 期待値 "$expected"
  summary plan "$actual"
  local missing extra
  missing=$(comm -23 <(echo "$expected") <(echo "$actual"))
  extra=$(comm -13 <(echo "$expected") <(echo "$actual"))
  report 不足 "$missing"
  report 余剰 "$extra"
  local n_missing n_extra
  n_missing=$(grep -c . <<<"$missing" || true)
  n_extra=$(grep -c . <<<"$extra" || true)
  echo "# 結果: 不足 ${n_missing} 件、余剰 ${n_extra} 件"
  ((n_missing == 0 && n_extra == 0)) || return 1
}

# 出力をそのまま Pull Request に貼れるように、12桁の数字（アカウント ID）を伏せる。
main "$@" 2>&1 | sed -E 's/[0-9]{12}/<account>/g'
exit "${PIPESTATUS[0]}"
