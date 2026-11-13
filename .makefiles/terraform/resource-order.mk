## resource の作成の順序の検査（ADR-0501 決定5 がソースの構造の規約として Conftest に割り当てるもの）
##
## 判定は conftest/hcl/resource_order/ の Rego が持つ（規則は conftest/hcl/resource_order/README.md）。
## ここが持つのは、入力の集め方と起動の順だけである。
##
## Conftest は走査の root を自分で歩く。find がそれとは別の経路で構成ファイルを数え、その一覧を --data で渡して
## Rego が読んだファイルの集合と突き合わせる（ADR-0702 決定15）。規則の評価の数は tf-local policy-result が
## Rego の deny contains の数と突き合わせる —— 規則の無い名前空間を渡された Conftest は、何も数えずに 0 で終える。

RO_BASE ?= .
RO_ROOTS = usecases modules env
RO_DECL ?= conftest/hcl/resource_order/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
RO_POLICY = conftest/hcl/lib conftest/hcl/resource_order
RO_REGO = conftest/hcl/resource_order/order.rego
RO_NS ?= resource_order
RO_POLICY_LIST = $(subst $(space),$(comma),$(strip $(RO_POLICY)))
RO_OUT = tmp/resource-order-check

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
	$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(RO_POLICY),-p $(p)) --no-color --report notes > $(RO_OUT)/verify.txt 2>&1 || rc=$$?; \
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
	$(GO_TOOL) sh -c 'set -u; root=$$PWD; cd "$(RO_BASE)"; \
		exec $(CONFTEST_BIN) test --combine $(foreach p,$(RO_POLICY),-p "$$root/$(p)") --data "$$root/$(RO_DECL)" --data "$$root/$(RO_OUT)/files.json" \
		--namespace $(RO_NS) --ignore "$(IC_IGNORE)" --no-color -o json "$$@"' sh $$roots > $(RO_OUT)/result.json || :; \
	$(call RUN_SCRIPT,tf-local,policy-result -namespace $(RO_NS) -rego $(RO_REGO) $(RO_OUT)/result.json)
