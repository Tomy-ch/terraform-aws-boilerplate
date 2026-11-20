#!/usr/bin/env bash
# 保存した identity root の plan を check-plan.expected と突き合わせる。何も作らず、何も変えない。
# 使い方: docs/setup/scripts/check-plan.sh [--partial] <plan のファイル>
#         docs/setup/scripts/check-plan.sh --mock <terraform test -json -verbose の出力>
#   --partial は段4 のやり直し用。期待値のうち plan に無いもの（apply 済み）を「済み」として扱い、
#   余剰（変更・破棄・期待値に無い追加）が無いことだけを確かめる。
#   --mock は make check-plan-expected 用。env/admin/identity/tests/imports.tftest.hcl の run "check_plan" が
#   mock の provider で作った plan を、同じ判定に通す。期待値と HCL の一致だけを見て、実 AWS は見ない。
# 前提: env/admin/identity が init 済み。terraform・jq・git が PATH に在る（--mock は jq だけ）。
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
# 本物の plan と mock の plan は、前提の検査だけが違い、行を出す本体を共有する。
JQ_LINES=$(
  cat <<'JQ'
def lines:
  (.resource_changes // [])[] | select(.mode == "managed")
  | .address as $a | .change.actions as $act
  | (if .change.importing != null then "import \($a)" else empty end),
    (if $act == ["no-op"] then empty
      elif $act == ["create"] then "add \($a)"
      elif $act == ["update"] then "change \($a)"
      elif $act == ["delete"] then "destroy \($a)"
      elif $act == ["delete", "create"] or $act == ["create", "delete"] then "add \($a)", "destroy \($a)"
      else "unknown(\($act | join(","))) \($a)"
      end);
JQ
)

JQ_CHANGES=$(
  cat <<'JQ'
if (.format_version | type) != "string" then error("plan の JSON ではありません")
elif .errored == true then error("plan がエラーで終わっています")
elif .complete == false then error("plan が完全ではありません（deferred な変更があります）")
else lines
end
JQ
)

# terraform test の出力は complete・errored を持たない。run が pass で終わったことと、plan の形の版で前提を見る。
JQ_MOCK_CHANGES=$(
  cat <<'JQ'
if (.plan_format_version | type) != "string" or (.plan_format_version | test("^1\\.") | not) then error("plan の形の版が想定と違います")
else lines
end
JQ
)

# 判定に使う run。mock と override は同じファイルのほかの run と共有する。
MOCK_FILE=tests/imports.tftest.hcl
MOCK_RUN=check_plan

# jq が失敗したときの理由。jq のエラー本文は入力の断片を含みうるので出さない。
JQ_REASON=$(
  cat <<'JQ'
if (.format_version | type) != "string" then "plan の JSON ではありません"
elif .errored == true then "plan がエラーで終わっています"
elif .complete == false then "plan が完全ではありません（deferred な変更があります）"
else "plan の JSON の形が想定と違います"
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

# Pull Request に貼る出力を、検証した時点の commit に結び付ける。
commit_line() {
  local sha dirty
  sha=$(git -C "$ROOT" rev-parse HEAD) || return 1
  dirty=$(git -C "$ROOT" status --porcelain) || return 1
  echo "# commit: ${sha}${dirty:+（未コミットの変更あり）}"
}

# terraform test -json -verbose の出力から、run "check_plan" の plan を1つだけ取り出して「<種類> <address>」にする。
mock_changes() {
  local out=$1 passed plans n
  passed=$(jq -r --arg f "$MOCK_FILE" --arg r "$MOCK_RUN" \
    'select(.type == "test_run" and .["@testfile"] == $f and .["@testrun"] == $r and .test_run.status == "pass") | "pass"' \
    "$out" 2>/dev/null) || {
    echo "terraform test の出力を読めません。plan を検証できません" >&2
    return 2
  }
  [[ $passed == pass ]] || {
    echo "${MOCK_FILE} の run \"${MOCK_RUN}\" が pass で終わっていません。plan を検証できません" >&2
    return 2
  }
  plans=$(jq -c --arg f "$MOCK_FILE" --arg r "$MOCK_RUN" \
    'select(.type == "test_plan" and .["@testfile"] == $f and .["@testrun"] == $r) | .test_plan | del(.provider_schemas)' \
    "$out" 2>/dev/null) || {
    echo "terraform test の出力を読めません。plan を検証できません" >&2
    return 2
  }
  n=$(grep -c . <<<"$plans" || true)
  ((n == 1)) || {
    echo "${MOCK_FILE} の run \"${MOCK_RUN}\" の plan が ${n} 件です（1件を期待）。plan を検証できません" >&2
    return 2
  }
  jq -r "$JQ_LINES $JQ_MOCK_CHANGES" 2>/dev/null <<<"$plans" || {
    echo "plan の形の版が想定と違うか、plan の JSON の形が想定と違います。plan を検証できません" >&2
    return 2
  }
}

main() {
  local partial="" mock=""
  while (($# > 0)); do
    case $1 in
      --partial) partial=1 ;;
      --mock) mock=1 ;;
      *) break ;;
    esac
    shift
  done
  if (($# != 1)) || [[ -n $partial && -n $mock ]]; then
    echo "使い方: $0 [--partial] <plan のファイル> | $0 --mock <terraform test -json -verbose の出力>" >&2
    return 2
  fi
  local plan cmd needed=("$TERRAFORM" jq git)
  [[ -n $mock ]] && needed=(jq)
  for cmd in "${needed[@]}"; do
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
  [[ -r $EXPECTED ]] || {
    echo "期待値を読めません: $EXPECTED" >&2
    return 2
  }
  expected=$({ grep -vE '^[[:space:]]*(#|$)' "$EXPECTED" || true; } | sort)
  [[ -n $expected ]] || {
    echo "期待値が空です: $EXPECTED" >&2
    return 2
  }

  local actual
  if [[ -n $mock ]]; then
    actual=$(mock_changes "$plan") || return 2
    actual=$(sort <<<"$actual")
    compare "mock の plan" "$expected" "$actual" ""
    return
  fi

  # terraform の振る舞いを変える TF_ の環境変数を外し、CLI の設定（dev_overrides）を読ませない。
  local unset_tf=() name
  while read -r name; do
    [[ $name == TF_* ]] && unset_tf+=(-u "$name")
  done < <(compgen -e)
  local json
  if ! json=$(env ${unset_tf[@]+"${unset_tf[@]}"} TF_CLI_CONFIG_FILE=/dev/null CHECKPOINT_DISABLE=1 \
    "$TERRAFORM" -chdir="$ROOT/env/admin/identity" show -json "$plan"); then
    echo "terraform show が失敗しました。plan を検証できません" >&2
    return 2
  fi
  if ! actual=$(jq -r "$JQ_LINES $JQ_CHANGES" 2>/dev/null <<<"$json"); then
    local reason
    reason=$(jq -r "$JQ_REASON" 2>/dev/null <<<"$json") || reason="plan の JSON ではありません"
    echo "${reason}。plan を検証できません" >&2
    return 2
  fi
  actual=$(sort <<<"$actual")

  commit_line || {
    echo "git の状態を読めません。plan を検証できません" >&2
    return 2
  }
  compare plan "$expected" "$actual" "$partial"
}

compare() {
  local label=$1 expected=$2 actual=$3 partial=$4
  summary 期待値 "$expected"
  summary "$label" "$actual"
  local missing extra
  missing=$(comm -23 <(echo "$expected") <(echo "$actual"))
  extra=$(comm -13 <(echo "$expected") <(echo "$actual"))
  local n_missing n_extra
  n_missing=$(grep -c . <<<"$missing" || true)
  n_extra=$(grep -c . <<<"$extra" || true)
  report 余剰 "$extra"
  if [[ -n $partial ]]; then
    local line
    while read -r line; do
      [[ -n $line ]] && echo "済み  $line"
    done <<<"$missing"
    echo "# 結果（--partial）: 済み ${n_missing} 件、余剰 ${n_extra} 件"
    ((n_extra == 0)) || return 1
    return 0
  fi
  report 不足 "$missing"
  echo "# 結果: 不足 ${n_missing} 件、余剰 ${n_extra} 件"
  ((n_missing == 0 && n_extra == 0)) || return 1
}

# 出力をそのまま Pull Request に貼れるように、12桁の数字（アカウント ID）を伏せる。
main "$@" 2>&1 | sed -E 's/[0-9]{12}/<account>/g'
exit "${PIPESTATUS[0]}"
