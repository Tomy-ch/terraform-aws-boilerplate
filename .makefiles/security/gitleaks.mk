## 秘密の混入の検査（gitleaks）
##
## 走査対象は commit と履歴であり、作業ツリーのファイルではない。同じ範囲を trufflehog も走査する
## （trufflehog.mk 先頭、ADR-0302 決定2）。
##
## 混入を検出した場合の第一手は当該資格情報の**失効**である（ADR-0302 決定11）。
##
## 行に置く `gitleaks:allow` は効かせない（ADR-0302 決定9）。抑止は .gitleaks.toml と
## .gitleaksignore に理由とともに置く。
GITLEAKS_FLAGS := --redact --no-banner --ignore-gitleaks-allow

## 範囲の起点の番兵。trufflehog-scan も使う。
##
## gitleaks も trufflehog も、起点が解決できないと範囲を0件として扱い、所見なしで通る。範囲が
## 0件であること自体は正当（push するものが無い）なので、この番兵は起点の解決だけを見る。
## gitleaks の走査数は GITLEAKS_COUNTED が別に検める。
## 違反（1）と区別するため exit 2 にする（commitlint-range と同じ）。
define SECRET_SCAN_FROM_GUARD
test -n "$(COMMITLINT_FROM)" || { \
	echo "❌ 走査範囲の起点が空です。GITHUB_BASE_REF を渡すか COMMITLINT_FROM を指定してください" >&2; exit 2; }; \
git rev-parse --verify --quiet "$(COMMITLINT_FROM)^{commit}" >/dev/null || { \
	echo "❌ 走査範囲の起点 $(COMMITLINT_FROM) が解決できません。浅い取得か、ref の取り違えです" >&2; exit 2; }
endef

## canary が一時のリポジトリを操作するときの git。git の hook の中では GIT_DIR / GIT_INDEX_FILE などが
## 外側のリポジトリを指しており、`git -C <一時の場所>` でもそちらへ commit・add してしまう。hook も
## 外側の設定から走るので外す。
CANARY_GIT := env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_OBJECT_DIRECTORY \
	-u GIT_ALTERNATE_OBJECT_DIRECTORIES -u GIT_COMMON_DIR -u GIT_PREFIX git -c core.hooksPath=/dev/null

## gitleaks は git を読めなくても「0 commits scanned」で exit 0 を返す。走査した commit の数を
## 出力から取り、$(2) の条件で検める（ADR-0702 決定15）。数を読めない出力も通さない。
##   $(1) gitleaks git の追加の引数
##   $(2) 走査した数 $$n に課す test の式
define GITLEAKS_COUNTED
mkdir -p tmp; log=$$(mktemp tmp/gitleaks.XXXXXX); trap 'rm -f "$$log"' EXIT; \
rc=0; $(GO_TOOL) gitleaks git $(GITLEAKS_FLAGS) $(1) >"$$log" 2>&1 || rc=$$?; cat "$$log" >&2; \
$(call GITLEAKS_COUNT_CHECK,"$$log",$(2)); \
exit $$rc
endef

define GITLEAKS_COUNT_CHECK
n=$$(grep -o '[0-9][0-9]* commits scanned' $(1) | head -n 1 | cut -d ' ' -f 1); \
test -n "$$n" || { echo "❌ gitleaks: 走査した commit の数を出力から読めません" >&2; exit 2; }; \
test $(2) || { echo "❌ gitleaks: 走査した commit の数が条件（$(2)）を満たしません。git を読めていない可能性があります" >&2; exit 2; }
endef

.PHONY: secret-scan ## push 予定の commit 範囲に秘密が無いことを検査
secret-scan:
	@set -eu; $(SECRET_SCAN_FROM_GUARD); \
	want=$$(git rev-list --count --no-merges "$(COMMITLINT_FROM)..HEAD"); \
	$(call GITLEAKS_COUNTED,--log-opts="$(COMMITLINT_FROM)..HEAD","$$n" -eq "$$want")

## 全履歴の数は ref の数え方が git rev-list と揃わないので、0件でないことだけを見る。
.PHONY: secret-scan-history ## 全履歴を走査（検出ルールの更新で過去が対象になるため定期実行する）
secret-scan-history:
	@set -eu; $(call GITLEAKS_COUNTED,,"$$n" -gt 0)
