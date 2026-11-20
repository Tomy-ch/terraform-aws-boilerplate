## root の構成と backend 設定の不変条件の検査（ADR-0604 決定2-4・7・8。ADR-0501 決定5 がソースの構造の規約として
## Conftest に割り当てるもの）
##
## 判定は conftest/hcl/root/ の Rego が持つ（規則は conftest/hcl/root/README.md）。
## ここが持つのは、道具を起動する順だけである。前の段が落ちれば、make はそこで止まり Conftest を起動しない。
##
## 読んだものの数は独立の経路の数と突き合わせる（ADR-0702 決定15。段の説明は conftest/hcl/root/README.md）。
## 規則の無い名前空間を渡された Conftest は何も数えずに 0 で終えるので、規則の評価の数は
## tf-local policy-result が Rego の deny contains の数と突き合わせる。

include .makefiles/terraform/hcl-input.mk

RT_BASE ?= .
RT_ROOTS = usecases env
RT_DECL ?= conftest/hcl/root/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
RT_POLICY = conftest/hcl/lib conftest/hcl/root
RT_REGO = conftest/hcl/root/root.rego
RT_NS ?= root
RT_OUT = tmp/root-check
RT_FIXTURES = conftest/hcl/lib/fixtures conftest/hcl/root/fixtures

.PHONY: root-check ## root が呼ぶ usecase の構成と、backend 設定の不変条件を HCL のソースで検査する
root-check: root-verify root-detect

.PHONY: root-verify ## root の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
root-verify:
	$(call HCL_VERIFY,$(RT_OUT)/verify,$(RT_POLICY),$(RT_FIXTURES),$(RT_NS))

.PHONY: root-detect ## Terraform の構成を root の規則で検査し、読んだファイルと module block を独立の走査と突き合わせる
root-detect:
	@mkdir -p $(RT_OUT)
	@$(call HCL_TALLY,$(RT_BASE),$(RT_OUT),$(RT_ROOTS),-files-key root_files -module-counts-key root_module_counts)
	@$(call HCL_INPUT,$(RT_BASE),$(RT_OUT),$(RT_ROOTS))
	-@$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(RT_POLICY),-p $(p)) --data $(RT_DECL) --data $(RT_OUT)/files.json --data $(RT_OUT)/hcl-counts.json \
		--namespace $(RT_NS) --no-color -o json $(RT_OUT)/hcl-input.json > $(RT_OUT)/result.json
	@$(call RUN_SCRIPT,tf-local,policy-result -namespace $(RT_NS) -rego $(RT_REGO) $(RT_OUT)/result.json)
