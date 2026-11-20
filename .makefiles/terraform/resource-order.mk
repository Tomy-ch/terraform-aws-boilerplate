## resource の作成の順序の検査（ADR-0501 決定5 がソースの構造の規約として Conftest に割り当てるもの）
##
## 判定は conftest/hcl/resource_order/ の Rego が持つ（規則は conftest/hcl/resource_order/README.md）。
## ここが持つのは、道具を起動する順だけである。前の段が落ちれば、make はそこで止まり Conftest を起動しない。
##
## 規則の評価の数は tf-local policy-result が Rego の deny contains の数と突き合わせる —— 規則の無い名前空間を
## 渡された Conftest は、何も数えずに 0 で終える。

include .makefiles/terraform/hcl-input.mk

RO_BASE ?= .
RO_ROOTS = usecases modules env
RO_DECL ?= conftest/hcl/resource_order/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
RO_POLICY = conftest/hcl/lib conftest/hcl/resource_order
RO_REGO = conftest/hcl/resource_order/order.rego
RO_NS ?= resource_order
RO_OUT = tmp/resource-order-check
RO_FIXTURES = conftest/hcl/lib/fixtures conftest/hcl/resource_order/fixtures

.PHONY: resource-order-check ## resource の作成の順序の宣言を、HCL のソースの depends_on で検査する
resource-order-check: resource-order-verify resource-order-detect

.PHONY: resource-order-verify ## 順序の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
resource-order-verify:
	$(call HCL_VERIFY,$(RO_OUT)/verify,$(RO_POLICY),$(RO_FIXTURES),$(RO_NS))

.PHONY: resource-order-detect ## Terraform の構成の depends_on を順序の宣言と照合し、読んだファイルを独立の走査と突き合わせる
resource-order-detect:
	@mkdir -p $(RO_OUT)
	@$(call HCL_TALLY,$(RO_BASE),$(RO_OUT),$(RO_ROOTS),-files-key resource_order_files)
	@$(call HCL_INPUT,$(RO_BASE),$(RO_OUT),$(RO_ROOTS))
	-@$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(RO_POLICY),-p $(p)) --data $(RO_DECL) --data $(RO_OUT)/files.json --data $(RO_OUT)/hcl-counts.json \
		--namespace $(RO_NS) --no-color -o json $(RO_OUT)/hcl-input.json > $(RO_OUT)/result.json
	@$(call RUN_SCRIPT,tf-local,policy-result -namespace $(RO_NS) -rego $(RO_REGO) $(RO_OUT)/result.json)
