## resource の作成の順序の検査（ADR-0501 決定5 がソースの構造の規約として Conftest に割り当てるもの）
##
## 判定は conftest/hcl/resource_order/ の Rego が持つ（規則は conftest/hcl/resource_order/README.md）。
## ここが持つのは、入力の集め方と起動の順だけである。
##
## Conftest の入力は scripts/hcl-input が走査の root の下から作る JSON で、読んだファイルの集合は find の一覧と、
## 読んだ block の数は行頭の数えと突き合わせる（ADR-0702 決定15。段は hcl-input.mk）。構成ファイルの find の一覧も
## --data で渡し、Rego が入力のファイルの集合と突き合わせる。規則の評価の数は tf-local policy-result が
## Rego の deny contains の数と突き合わせる —— 規則の無い名前空間を渡された Conftest は、何も数えずに 0 で終える。

include .makefiles/terraform/hcl-input.mk

RO_BASE ?= .
RO_ROOTS = usecases modules env
RO_DECL ?= conftest/hcl/resource_order/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
RO_POLICY = conftest/hcl/lib conftest/hcl/resource_order
RO_REGO = conftest/hcl/resource_order/order.rego
RO_NS ?= resource_order
RO_POLICY_LIST = $(subst $(space),$(comma),$(strip $(RO_POLICY)))
RO_OUT = tmp/resource-order-check
RO_FIXTURES = conftest/hcl/lib/fixtures conftest/hcl/resource_order/fixtures
RO_HCL_INPUT_CMD = $(call HCL_INPUT_CMD,$(RO_BASE),$(RO_OUT)/hcl)

.PHONY: resource-order-check ## resource の作成の順序の宣言を、HCL のソースの depends_on で検査する
resource-order-check:
	@set -u; failed=""; \
	for t in resource-order-verify resource-order-detect; do \
		$(MAKE) -s --no-print-directory "$$t" || failed="$$failed $$t"; \
	done; \
	if [ -n "$$failed" ]; then echo "❌ resource-order-check が落ちた検査:$$failed"; exit 1; fi

.PHONY: resource-order-verify ## 順序の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
resource-order-verify:
	@set -u; mkdir -p $(RO_OUT); rc=0; \
	$(call HCL_INPUT_FIXTURES,$(RO_OUT),$(RO_FIXTURES)); \
	$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(RO_POLICY),-p $(p)) $(foreach d,$(RO_FIXTURES),--data $(call HCL_FIXTURE_JSON,$(RO_OUT),$(d))) --no-color --report notes > $(RO_OUT)/verify.txt 2>&1 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then cat $(RO_OUT)/verify.txt; fi; \
	$(call RUN_SCRIPT,infra-code-gate,rego-tests -policy $(RO_POLICY_LIST) -report $(RO_OUT)/verify.txt -namespaces $(RO_NS)) || rc=1; \
	exit $$rc

.PHONY: resource-order-detect ## Terraform の構成の depends_on を順序の宣言と照合し、読んだファイルを独立の走査と突き合わせる
resource-order-detect:
	@set -u; mkdir -p $(RO_OUT); rm -f $(RO_OUT)/found.txt $(RO_OUT)/files.json $(RO_OUT)/result.json; \
	roots=""; for d in $(RO_ROOTS); do if [ -d "$(RO_BASE)/$$d" ]; then roots="$$roots $$d"; fi; done; \
	if [ -z "$$roots" ]; then echo "❌ 走査の root が1つもありません（$(RO_ROOTS)）" >&2; exit 2; fi; \
	(cd "$(RO_BASE)" && find $$roots \( -name '*.tf' -o -name '*.tf.json' \) -not -path '*/.terraform/*') > $(RO_OUT)/found.txt \
		|| { echo "❌ 構成ファイルの独立の走査（find）が失敗しました" >&2; exit 1; }; \
	files=$$(sort $(RO_OUT)/found.txt | sed 's/[\\"]/\\&/g; s/.*/"&"/' | paste -sd, -); \
	printf '{"resource_order_files": [%s]}\n' "$$files" > $(RO_OUT)/files.json; \
	mkdir -p $(RO_OUT)/hcl; \
	$(call HCL_INPUT_DETECT,$(RO_BASE),$(RO_OUT)/hcl,$(RO_HCL_INPUT_CMD)); \
	$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(RO_POLICY),-p $(p)) --data $(RO_DECL) --data $(RO_OUT)/files.json --data $(RO_OUT)/hcl/hcl-counts.json \
		--namespace $(RO_NS) --no-color -o json $(RO_OUT)/hcl/hcl-input.json > $(RO_OUT)/result.json || :; \
	$(call RUN_SCRIPT,tf-local,policy-result -namespace $(RO_NS) -rego $(RO_REGO) $(RO_OUT)/result.json)
