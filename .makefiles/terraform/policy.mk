## Policy Test（ADR-0402、ADR-0501 決定4・8）
##
## 入力の作り方は conftest/plan/README.md の「入力」。terraform はホストで、conftest はツールランナーで実行する。
##
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。
##
## conftest verify はテストが0件でも、conftest test は入力が0件でも失敗しない。件数はここで独立に数える。

POLICY_PLAN_DIR ?= conftest/plan
# Rego のテストは宣言そのもの（POLICY_PLAN_DECL）に対して走る。canary が差し替えるのは、適用の段の宣言だけである。
POLICY_PLAN_DECL = conftest/data/policy-plan.json
POLICY_PLAN_DATA ?= $(POLICY_PLAN_DECL)
CONFTEST_BIN ?= conftest
# conftest はツールランナーの中で走り、リポジトリは /app に mount される。パスは相対で渡す。
TF_POLICY_INPUT = $(TF_BASE)/tmp/tf-policy

.PHONY: tf-policy ## Policy Test を実行（Rego のテストと、reference の plan への適用）
tf-policy: tf-init
	@set -eu; $(TF_DATA_DIR_OF); \
	roots=$$(for d in $(TF_BASE)/usecases/*/reference; do if [ -d "$$d" ]; then echo "$$d"; fi; done); \
	if [ -z "$$roots" ]; then echo "❌ Policy の入力にする reference の root が0件です（$(TF_BASE)/usecases/*/reference）"; exit 1; fi; \
	missing=""; for d in $$roots; do if [ ! -f "$$d/tests/policy.tftest.hcl" ]; then missing="$$missing $$d"; fi; done; \
	if [ -n "$$missing" ]; then echo "❌ tests/policy.tftest.hcl を持たない reference の root:$$missing"; exit 1; fi; \
	no_test=""; for f in $$(find $(POLICY_PLAN_DIR) -name '*.rego' -not -name '*_test.rego'); do \
		if grep -q '^deny contains' "$$f"; then \
			t="$${f%.rego}_test.rego"; \
			grep -q '^test_violation_' "$$t" 2>/dev/null && grep -q '^test_pass_' "$$t" || no_test="$$no_test $$f"; \
		fi; \
	done; \
	if [ -n "$$no_test" ]; then echo "❌ 違反する入力と違反しない入力の両方のテストを持たない Policy（ADR-0402 決定8・9）:$$no_test"; exit 1; fi; \
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
		if ! TF_DATA_DIR="$$(data_dir "$$d")" $(TF) -chdir="$$d" test -filter=tests/policy.tftest.hcl -json -verbose > "$$out.jsonl"; then \
			jq -r 'select(.["@level"] == "error") | .["@message"]' "$$out.jsonl" 2>/dev/null || true; failed="$$failed $$d"; continue; \
		fi; \
		n=$$(jq -s '[.[] | select(.type == "test_plan")] | length' "$$out.jsonl"); \
		if [ "$$n" -ne 1 ]; then echo "❌ $$d: test_plan が $$n 件です（1件を期待）"; failed="$$failed $$d"; continue; fi; \
		jq -s '[.[] | select(.type == "test_plan") | .test_plan | del(.provider_schemas)][0]' "$$out.jsonl" > "$$out.json"; \
		inputs="$$inputs $$out.json"; \
	done; \
	$(TF_REPORT_FAILED); \
	result=$$($(GO_TOOL) $(CONFTEST_BIN) test --combine --all-namespaces --no-color --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DATA) $$inputs) || { \
		echo "$$result"; echo "❌ Policy の違反があります"; exit 1; }; \
	echo "$$result"; echo "✅ Policy を reference の root $$(echo $$inputs | wc -w | tr -d ' ') 件に適用しました"

# canary の fixture は provider を持たない構成（terraform_data）で毎回 tmp/ に作り、TF_BASE と
# POLICY_PLAN_DATA で差し替える。文書の表を terraform_data の input に向け、init がネットワークを要さない。
TF_POLICY_CANARY_DIR = tmp/tf-policy-canary

define TF_POLICY_FIXTURE
mkdir -p "$$1/usecases/x/reference/tests"; printf '%s\n' "$$2" > "$$1/usecases/x/reference/main.tf"; \
printf 'run "policy_input" {\n  command = plan\n}\n' > "$$1/usecases/x/reference/tests/policy.tftest.hcl"
endef

.PHONY: tf-policy-canary ## tf-policy が0件・テストの欠落・違反・unknown の文書で落ちることを検査
tf-policy-canary:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(TF_POLICY_CANARY_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	fixture() { $(TF_POLICY_FIXTURE); }; \
	printf '%s\n' '{"policy_plan": {"plan_format_version": "1.2", "documents": {"terraform_data": ["input"]}, "deployment_identity": {"roles": [], "oidc_provider": ""}}}' > "$$root/data.json"; \
	expect() { \
		case_name="$$1"; base="$$2"; pattern="$$3"; shift 3; \
		out=$$($(MAKE) -s --no-print-directory tf-policy TF_BASE="$$base" POLICY_PLAN_DATA="$$root/data.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$rc" -eq 0 ] || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; fi; \
	}; \
	mkdir -p "$$root/empty/modules/a"; printf 'variable "x" {\n  type = string\n}\n' > "$$root/empty/modules/a/main.tf"; \
	expect empty "$$root/empty" "reference の root が0件"; \
	fixture "$$root/no-test" 'resource "terraform_data" "doc" {}'; rm "$$root/no-test/usecases/x/reference/tests/policy.tftest.hcl"; \
	expect no-policy-test "$$root/no-test" "tests/policy.tftest.hcl を持たない"; \
	fixture "$$root/star" 'resource "terraform_data" "doc" { input = jsonencode({ Statement = [{ Effect = "Allow", Action = "*", Resource = "*" }] }) }'; \
	expect star "$$root/star" "[PLAN-IAM-01]"; \
	fixture "$$root/unknown" 'resource "terraform_data" "a" {}'; \
	printf '%s\n' 'resource "terraform_data" "doc" { input = terraform_data.a.id }' >> "$$root/unknown/usecases/x/reference/main.tf"; \
	expect unknown "$$root/unknown" "[PLAN-INPUT-04]"; \
	mkdir -p "$$root/rego"; cp -R $(POLICY_PLAN_DIR)/. "$$root/rego/"; \
	sed '/^test_pass_/,/^}/d' "$$root/rego/iam_action_wildcard_test.rego" > "$$root/rego/t" && mv "$$root/rego/t" "$$root/rego/iam_action_wildcard_test.rego"; \
	expect no-pass-test "$$root/star" "両方のテストを持たない" POLICY_PLAN_DIR="$$root/rego"; \
	if [ -n "$$failed" ]; then echo "❌ tf-policy が落ちなかったケース:$$failed"; exit 1; fi; \
	echo "✅ tf-policy は0件・テストの欠落・違反・unknown の文書で落ちる"
