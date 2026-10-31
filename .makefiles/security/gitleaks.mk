## 秘密の混入の検査（gitleaks）
##
## 走査対象は commit と履歴であり、作業ツリーのファイルではない。
##
## 行に置く `gitleaks:allow` は効かせない（ADR-0302 決定9）。抑止は .gitleaks.toml と
## .gitleaksignore に理由とともに置く。
##
## 設定と抑止は明示で渡す。省くと gitleaks は環境変数 GITLEAKS_CONFIG と走査先の .gitleaks.toml を
## 探すので、本走査の設定が環境で黙って差し替わり、一時のリポジトリを走査する canary は抑止を読まない。
GITLEAKS_FLAGS := --redact --no-banner --ignore-gitleaks-allow --config .gitleaks.toml --gitleaks-ignore-path .

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
	$(MAKE) -s --no-print-directory gitleaks-canary; \
	want=$$(git rev-list --count --no-merges "$(COMMITLINT_FROM)..HEAD"); \
	$(call GITLEAKS_COUNTED,--log-opts="$(COMMITLINT_FROM)..HEAD","$$n" -eq "$$want")

## 全履歴の数は ref の数え方が git rev-list と揃わないので、0件でないことだけを見る。
.PHONY: secret-scan-history ## 全履歴を走査（検出ルールの更新で過去が対象になるため定期実行する）
secret-scan-history: gitleaks-canary
	@set -eu; $(call GITLEAKS_COUNTED,,"$$n" -gt 0)

## 検出器が、本走査と同じ引数・設定・抑止で働くことを確かめる。値はリポジトリに置かず
## 実行時に作る（trufflehog-canary と同じ組み方）。
.PHONY: gitleaks-canary ## gitleaks が commit された偽の資格情報を範囲と全履歴の両方で検出することを検査
gitleaks-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/gitleaks-canary.XXXXXX); trap 'rm -rf "$$dir" "$$dir".*' EXIT; \
	id=$$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Z2-7' | head -c 16); \
	secret=$$(head -c 512 /dev/urandom | base64 | LC_ALL=C tr -dc 'A-Za-z0-9/+' | head -c 40); \
	$(CANARY_GIT) -C "$$dir" init -q; \
	commit() { $(CANARY_GIT) -C "$$dir" -c user.name=canary -c user.email=canary@example.invalid -c commit.gpgsign=false "$$@"; }; \
	printf 'canary\n' > "$$dir/README"; commit add README; commit commit -q -m base; \
	from=$$($(CANARY_GIT) -C "$$dir" rev-parse HEAD); \
	printf '[default]\naws_access_key_id = AKIA%s\naws_secret_access_key = %s\n' "$$id" "$$secret" > "$$dir/credentials"; \
	commit add credentials; commit commit -q -m canary; \
	rc=0; ( $(call GITLEAKS_COUNTED,--log-opts="$$from..HEAD" --report-format json --report-path "$$dir.range.json" "$$dir","$$n" -eq 1) ) \
		2>"$$dir.range.log" || rc=$$?; \
	$(call GITLEAKS_CANARY_JUDGE,$$rc,"$$dir.range.json",範囲,"$$dir.range.log"); \
	rc=0; ( $(call GITLEAKS_COUNTED,--report-format json --report-path "$$dir.history.json" "$$dir","$$n" -eq 2) ) \
		2>"$$dir.history.log" || rc=$$?; \
	$(call GITLEAKS_CANARY_JUDGE,$$rc,"$$dir.history.json",全履歴,"$$dir.history.log"); \
	echo "✅ gitleaks-canary: 埋めた偽の AWS キーを範囲と全履歴の両方で検出しました" >&2

## canary の判定。gitleaks は検出でも異常終了でも 1 を返すので、終了コードと報告の RuleID の両方で見る。
##   $(1) 終了コード  $(2) JSON の報告  $(3) 経路の名前  $(4) 失敗時に出すログ
define GITLEAKS_CANARY_JUDGE
case "$(1)" in \
0) echo "❌ gitleaks-canary（$(3)）: 埋めた偽の AWS キーを検出できませんでした（抑止・検出規則・--log-opts の変更を疑う）" >&2; \
	cat $(4) >&2 2>/dev/null || :; exit 1 ;; \
1) grep -q '"RuleID": *"aws-access-token"' $(2) 2>/dev/null || { \
	echo "❌ gitleaks-canary（$(3)）: 終了コード 1 ですが、偽の AWS キーの所見がありません。gitleaks の異常終了を疑う" >&2; \
	cat $(4) >&2 2>/dev/null || :; exit 1; } ;; \
*) echo "❌ gitleaks-canary（$(3)）: gitleaks が異常終了したか、走査した commit の数が合いません (rc=$(1))" >&2; \
	cat $(4) >&2 2>/dev/null || :; exit 1 ;; \
esac
endef

## 秘密の走査の抑止（.gitleaksignore・.gitleaks.toml・.trufflehog-exclude-paths*.txt・.trivyignore.yaml の
## secrets 節）の単位と理由の検査（ADR-0302 決定9・10）。対象のファイルはツールが固定で持つ。
.PHONY: secret-suppression-lint ## 秘密の走査の抑止が、理由つきの fingerprint・path 単位であることを検査
secret-suppression-lint:
	@set -eu; mkdir -p tmp; trap 'rm -f tmp/secret-suppression-lint.tracked tmp/secret-suppression-lint.commits' EXIT; \
	git -c core.quotepath=false ls-files > tmp/secret-suppression-lint.tracked; \
	git rev-list --all > tmp/secret-suppression-lint.commits; \
	$(call RUN_SCRIPT,secret-suppression-lint,-tracked tmp/secret-suppression-lint.tracked -commits tmp/secret-suppression-lint.commits)
