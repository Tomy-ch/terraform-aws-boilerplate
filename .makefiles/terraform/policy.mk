## Policy Test（ADR-0402、ADR-0501 決定4・8）
##
## 入力の作り方は conftest/plan/README.md の「入力」。terraform はホストで、conftest はツールランナーで実行する。
##
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。
##
## conftest verify はテストが0件でも、conftest test は入力が0件でも失敗しない。件数はここで独立に数える。
##
## conftest は名前が deny / warn / violation で始まる規則をすべて評価する（deny_list のような名前も含む）。
## 規則は deny contains の形だけを認め、それ以外を落とす。

POLICY_PLAN_DIR = conftest/plan
# Rego のテストは宣言そのもの（POLICY_PLAN_DECL）に対して走る。canary が差し替えるのは、適用の段の宣言だけである。
POLICY_PLAN_DECL = conftest/plan/decl.json
POLICY_PLAN_DATA = $(POLICY_PLAN_DECL)
CONFTEST_BIN ?= conftest
# conftest はツールランナーの中で走り、リポジトリは /app に mount される。パスは相対で渡す。
TF_POLICY_INPUT = $(TF_BASE)/tmp/tf-policy

.PHONY: tf-policy ## Policy Test を実行（Rego のテストと、reference の plan への適用）
tf-policy: tf-init
	@set -eu; $(TF_DATA_DIR_OF); \
	command -v jq >/dev/null || { echo "❌ jq がホストにありません（tf-policy は入力の抽出と件数の突き合わせに使う）"; exit 1; }; \
	roots=$$(for d in $(TF_BASE)/usecases/*/reference; do if [ -d "$$d" ]; then echo "$$d"; fi; done); \
	if [ -z "$$roots" ]; then echo "❌ Policy の入力にする reference の root が0件です（$(TF_BASE)/usecases/*/reference）"; exit 1; fi; \
	missing=""; for d in $$roots; do if [ ! -f "$$d/tests/policy-input.tftest.hcl" ]; then missing="$$missing $$d"; fi; done; \
	if [ -n "$$missing" ]; then echo "❌ tests/policy-input.tftest.hcl を持たない reference の root:$$missing"; exit 1; fi; \
	sources=$$(find $(POLICY_PLAN_DIR) -name '*.rego' -not -name '*_test.rego'); \
	if [ -z "$$sources" ]; then echo "❌ Rego のテストが0件です（Policy も無い: $(POLICY_PLAN_DIR)）"; exit 1; fi; \
	other=$$(for f in $$sources; do grep -E '^(default +)?(deny|warn|violation)' "$$f" | grep -vqE '^deny contains ' && echo "$$f"; done || true); \
	if [ -n "$$other" ]; then echo "❌ deny contains 以外の形で書かれた規則があります（warn は失敗にならず、下の検査も届かない）: $$other"; exit 1; fi; \
	no_test=""; for f in $$sources; do \
		if grep -q '^deny contains' "$$f"; then \
			t="$${f%.rego}_test.rego"; \
			grep -q '^test_violation_' "$$t" 2>/dev/null && grep -q '^test_pass_' "$$t" || no_test="$$no_test $$f"; \
		fi; \
	done; \
	if [ -n "$$no_test" ]; then echo "❌ 違反する入力と違反しない入力の両方のテストを持たない Policy（ADR-0402 決定8・9）:$$no_test"; exit 1; fi; \
	no_id=$$(for f in $$sources; do awk -v f="$$f" '/^deny contains/ { open = 1; id = 0; start = NR } open && /\[PLAN-[A-Z0-9]*-[0-9]*\]/ { id = 1 } open && (/^}/ || (NR == start && !/\{ *$$/)) { if (!id) print f ":" start; open = 0 }' "$$f"; done); \
	if [ -n "$$no_id" ]; then echo "❌ 違反の ID（[PLAN-<区分>-<番号>]）を持たない deny 規則があります（ID ごとのテストの要求が届かない）: $$no_id"; exit 1; fi; \
	untested=""; for id in $$(grep -ho '\[PLAN-[A-Z0-9]*-[0-9]*\]' $$sources </dev/null | sort -u); do \
		find $(POLICY_PLAN_DIR) -name '*_test.rego' -exec grep -qF -- "\"$$id" {} + || untested="$$untested $$id"; \
	done; \
	if [ -n "$$untested" ]; then echo "❌ テストが一度も名指ししない違反の ID（ADR-0402 決定8・9）:$$untested"; exit 1; fi; \
	want=$$(find $(POLICY_PLAN_DIR) -name '*_test.rego' -exec grep -h '^test_' {} + | wc -l | tr -d ' '); \
	if [ "$$want" -eq 0 ]; then echo "❌ Rego のテストが0件です（$(POLICY_PLAN_DIR)）"; exit 1; fi; \
	verify=$$($(GO_TOOL) $(CONFTEST_BIN) verify --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) -o json) || { \
		echo "$$verify"; echo "❌ Rego のテストが落ちました（$(POLICY_PLAN_DIR)）"; exit 1; }; \
	passed=$$(printf '%s' "$$verify" | jq '[.[].successes] | add // 0'); \
	if [ "$$passed" -ne "$$want" ]; then echo "❌ 通った Rego のテストが $$passed 件で、宣言された $$want 件と一致しません"; exit 1; fi; \
	echo "✅ Rego のテスト $$passed 件"; \
	rm -rf "$(TF_POLICY_INPUT)"; mkdir -p "$(TF_POLICY_INPUT)"; inputs=""; failed=""; \
	for d in $$roots; do \
		name=$$(printf '%s' "$${d#$(TF_BASE)/usecases/}" | sed 's|/reference$$||'); out="$(TF_POLICY_INPUT)/$$name"; \
		if ! TF_DATA_DIR="$$(data_dir "$$d")" $(TF) -chdir="$$d" test -filter=tests/policy-input.tftest.hcl -json -verbose > "$$out.jsonl"; then \
			jq -r 'select(.["@level"] == "error") | .["@message"]' "$$out.jsonl" 2>/dev/null || true; failed="$$failed $$d"; continue; \
		fi; \
		n=$$(jq -s '[.[] | select(.type == "test_plan")] | length' "$$out.jsonl"); \
		if [ "$$n" -ne 1 ]; then echo "❌ $$d: test_plan が $$n 件です（1件を期待）"; failed="$$failed $$d"; continue; fi; \
		jq -s '[.[] | select(.type == "test_plan") | .test_plan | del(.provider_schemas)][0]' "$$out.jsonl" > "$$out.json"; \
		inputs="$$inputs $$out.json"; \
	done; \
	$(TF_REPORT_FAILED); \
	if [ -z "$$inputs" ]; then echo "❌ Policy の入力が0件です（reference の root から test_plan を1件も得られませんでした）"; exit 1; fi; \
	result=$$($(GO_TOOL) $(CONFTEST_BIN) test --combine --all-namespaces --no-color --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DATA) $$inputs) || { \
		echo "$$result"; echo "❌ Policy の違反があります"; exit 1; }; \
	echo "$$result"; echo "✅ Policy を reference の root $$(echo $$inputs | wc -w | tr -d ' ') 件に適用しました"

# canary の fixture は provider を持たない構成（terraform_data）で毎回 tmp/ に作り、TF_BASE と
# POLICY_PLAN_DATA で差し替える。文書の表を terraform_data の input に向け、init がネットワークを要さない。
TF_POLICY_CANARY_DIR = tmp/tf-policy-canary

define TF_POLICY_FIXTURE
mkdir -p "$$1/usecases/x/reference/tests"; printf '%s\n' "$$2" > "$$1/usecases/x/reference/main.tf"; \
printf 'run "policy_input" {\n  command = plan\n}\n' > "$$1/usecases/x/reference/tests/policy-input.tftest.hcl"
endef

.PHONY: tf-policy-canary ## tf-policy が0件・テストの欠落・件数のずれ・違反・unknown の文書で落ち、違反の無い入力で通ることを検査
tf-policy-canary:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(TF_POLICY_CANARY_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	fixture() { $(TF_POLICY_FIXTURE); }; \
	printf '%s\n' '{"policy_plan": {"plan_format_version": "1.2", "documents": {"terraform_data": ["input"]}, "required_documents": ["terraform_data"], "deployment_identity": {"roles": ["module.x.aws_iam_role.this"], "oidc_provider": "module.x.aws_iam_openid_connect_provider.this"}, "stateful_protection": {"protected_types": ["aws_s3_bucket"], "allow": {}}}}' > "$$root/data.json"; \
	expect() { \
		want="$$1"; case_name="$$2"; base="$$3"; pattern="$$4"; shift 4; \
		out=$$($(MAKE) -s --no-print-directory tf-policy TF_BASE="$$base" POLICY_PLAN_DATA="$$root/data.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && { [ "$$rc" -ne 0 ] || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; }; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && { [ "$$rc" -eq 0 ] || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; }; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	mkdir -p "$$root/rego"; cp -R $(POLICY_PLAN_DIR)/. "$$root/rego/"; rm "$$root/rego"/deployment_identity_trust*.rego; \
	fixture "$$root/ok" 'resource "terraform_data" "doc" { input = jsonencode({ Statement = [{ Effect = "Allow", Action = "s3:GetObject", Resource = "*" }] }) }'; \
	expect pass ok "$$root/ok" "Policy を reference の root 1 件に適用しました" POLICY_PLAN_DIR="$$root/rego"; \
	mkdir -p "$$root/empty/modules/a"; printf 'variable "x" {\n  type = string\n}\n' > "$$root/empty/modules/a/main.tf"; \
	expect fail empty "$$root/empty" "reference の root が0件"; \
	fixture "$$root/no-test" 'resource "terraform_data" "doc" {}'; rm "$$root/no-test/usecases/x/reference/tests/policy-input.tftest.hcl"; \
	expect fail no-policy-test "$$root/no-test" "tests/policy-input.tftest.hcl を持たない"; \
	fixture "$$root/star" 'resource "terraform_data" "doc" { input = jsonencode({ Statement = [{ Effect = "Allow", Action = "*", Resource = "*" }] }) }'; \
	expect fail star "$$root/star" "[PLAN-IAM-01]" POLICY_PLAN_DIR="$$root/rego"; \
	fixture "$$root/unknown" 'resource "terraform_data" "a" {}'; \
	printf '%s\n' 'resource "terraform_data" "doc" { input = terraform_data.a.id }' >> "$$root/unknown/usecases/x/reference/main.tf"; \
	expect fail unknown "$$root/unknown" "[PLAN-INPUT-04]" POLICY_PLAN_DIR="$$root/rego"; \
	cp -R "$$root/ok" "$$root/two-runs"; printf 'run "again" {\n  command = plan\n}\n' >> "$$root/two-runs/usecases/x/reference/tests/policy-input.tftest.hcl"; \
	expect fail two-runs "$$root/two-runs" "test_plan が 2 件" POLICY_PLAN_DIR="$$root/rego"; \
	cp -R "$$root/ok" "$$root/zero-runs"; printf '# run を持たない\n' > "$$root/zero-runs/usecases/x/reference/tests/policy-input.tftest.hcl"; \
	expect fail zero-runs "$$root/zero-runs" "test_plan が 0 件" POLICY_PLAN_DIR="$$root/rego"; \
	cp -R "$$root/ok" "$$root/broken-tf"; printf 'output "x" {\n  value = var.missing\n}\n' >> "$$root/broken-tf/usecases/x/reference/main.tf"; \
	expect fail broken-tf "$$root/broken-tf" "tf-policy が落ちたディレクトリ" POLICY_PLAN_DIR="$$root/rego"; \
	mkdir -p "$$root/no-rego"; \
	expect fail no-rego "$$root/ok" "Rego のテストが0件" POLICY_PLAN_DIR="$$root/no-rego"; \
	cp -R "$$root/rego" "$$root/no-pass"; sed '/^test_pass_/,/^}/d' "$$root/no-pass/iam_action_wildcard_test.rego" > "$$root/t" && mv "$$root/t" "$$root/no-pass/iam_action_wildcard_test.rego"; \
	expect fail no-pass-test "$$root/ok" "両方のテストを持たない" POLICY_PLAN_DIR="$$root/no-pass"; \
	cp -R "$$root/rego" "$$root/no-violation"; sed '/^test_violation_/,/^}/d' "$$root/no-violation/iam_action_wildcard_test.rego" > "$$root/t" && mv "$$root/t" "$$root/no-violation/iam_action_wildcard_test.rego"; \
	expect fail no-violation-test "$$root/ok" "両方のテストを持たない" POLICY_PLAN_DIR="$$root/no-violation"; \
	cp -R "$$root/rego" "$$root/failing-test"; printf '\ntest_pass_fails if false\n' >> "$$root/failing-test/iam_action_wildcard_test.rego"; \
	expect fail failing-rego-test "$$root/ok" "Rego のテストが落ちました" POLICY_PLAN_DIR="$$root/failing-test"; \
	cp -R "$$root/rego" "$$root/deny-if"; printf 'package plan.extra\n\ndeny if false\n' > "$$root/deny-if/extra.rego"; \
	expect fail deny-if "$$root/ok" "deny contains 以外の形" POLICY_PLAN_DIR="$$root/deny-if"; \
	cp -R "$$root/rego" "$$root/no-id"; printf '\ndeny contains "unnamed" if false\n' >> "$$root/no-id/iam_action_wildcard.rego"; \
	expect fail deny-without-id "$$root/ok" "ID（[PLAN-<区分>-<番号>]）を持たない" POLICY_PLAN_DIR="$$root/no-id"; \
	cp -R "$$root/rego" "$$root/helper"; printf '\ndeny_list := {"x"}\n' >> "$$root/helper/iam_action_wildcard.rego"; \
	expect fail deny-prefixed-helper "$$root/ok" "deny contains 以外の形" POLICY_PLAN_DIR="$$root/helper"; \
	cp -R "$$root/rego" "$$root/untested-id"; printf '\ndeny contains "[PLAN-IAM-99]" if false\n' >> "$$root/untested-id/iam_action_wildcard.rego"; \
	expect fail untested-id "$$root/ok" "[PLAN-IAM-99]" POLICY_PLAN_DIR="$$root/untested-id"; \
	cp -R "$$root/rego" "$$root/warn-rule"; printf '\nwarn contains "w" if false\n' >> "$$root/warn-rule/iam_action_wildcard.rego"; \
	expect fail warn-rule "$$root/ok" "deny contains 以外の形" POLICY_PLAN_DIR="$$root/warn-rule"; \
	cp -R "$$root/rego" "$$root/count"; printf '\nnot_a_test := `\ntest_counted_but_not_run\n`\n' >> "$$root/count/iam_action_wildcard_test.rego"; \
	expect fail count-mismatch "$$root/ok" "宣言された" POLICY_PLAN_DIR="$$root/count"; \
	if [ -n "$$failed" ]; then echo "❌ tf-policy の canary が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ tf-policy は違反の無い入力で通り、0件・テストの欠落・件数のずれ・違反・unknown の文書で落ちる"
