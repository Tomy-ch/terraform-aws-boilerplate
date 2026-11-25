## 層の規則の検査（ADR-0102 決定14・17・18。ADR-0501 決定5 がソースの構造の規約として Conftest に割り当てるもの）
##
## 判定は conftest/hcl/layer/ の Rego が持つ（規則は conftest/hcl/layer/README.md）。
## ここが持つのは、道具を起動する順だけである。前の段が落ちれば、make はそこで止まり Conftest を起動しない。
##
## 規則の評価の数は tf-local policy-result が Rego の deny contains の数と突き合わせる —— 規則の無い名前空間を
## 渡された Conftest は、何も数えずに 0 で終える。

include .makefiles/terraform/hcl-input.mk

LY_BASE = .
LY_ROOTS = usecases modules env
LY_DECL = conftest/hcl/layer/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
LY_POLICY = conftest/hcl/lib conftest/hcl/layer
LY_REGO = conftest/hcl/layer/layer.rego
LY_NS = layer
LY_OUT = tmp/layer-check
LY_FIXTURES = conftest/hcl/lib/fixtures conftest/hcl/layer/fixtures
# LY306 が読む schema は layer-schema が作る（conftest/hcl/layer/README.md の Provider の schema）。
LY_SCHEMA_DIR = $(LY_OUT)/schema
LY_AWS_PROVIDER = registry.terraform.io/hashicorp/aws

.PHONY: layer-check ## 層の規則（usecase 直下の block、module の source の向き、attachment 型の親の同居と共存）を HCL のソースで検査する
layer-check: layer-verify layer-detect

.PHONY: layer-verify ## 層の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
layer-verify:
	$(call HCL_VERIFY,$(LY_OUT)/verify,$(LY_POLICY),$(LY_FIXTURES),$(LY_NS))

.PHONY: layer-inputs ## 層の検査の入力を、HCL の構文の木とは別の経路で数える
layer-inputs:
	@mkdir -p $(LY_OUT)
	@$(call HCL_TALLY,$(LY_BASE),$(LY_OUT),$(LY_ROOTS),-files-key layer_files)

.PHONY: layer-schema ## 層の検査に渡す AWS Provider の schema の一覧（lock した版ごとの和）を作る
layer-schema: layer-inputs
	@$(call RUN_SCRIPT,provider-schema,prepare -provider $(LY_AWS_PROVIDER) -out "$(LY_SCHEMA_DIR)" -base "$(LY_BASE)" -listed "$(LY_OUT)/found.txt" $(LY_ROOTS))
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call RUN_SCRIPT_HOST,provider-schema,fetch -dir "$(LY_SCHEMA_DIR)")
	@$(call RUN_SCRIPT,provider-schema,merge -provider $(LY_AWS_PROVIDER) -dir "$(LY_SCHEMA_DIR)" -out $(LY_OUT)/schema.json)

.PHONY: layer-detect ## Terraform の構成を層の規則で検査し、読んだファイルを独立の走査と突き合わせる
layer-detect: layer-schema
	@$(call HCL_INPUT,$(LY_BASE),$(LY_OUT),$(LY_ROOTS))
	-@$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(LY_POLICY),-p $(p)) --data $(LY_DECL) --data $(LY_OUT)/files.json --data $(LY_OUT)/hcl-counts.json \
		--data $(LY_OUT)/schema.json --namespace $(LY_NS) --no-color -o json $(LY_OUT)/hcl-input.json > $(LY_OUT)/result.json
	@$(call RUN_SCRIPT,tf-local,policy-result -namespace $(LY_NS) -rego $(LY_REGO) $(LY_OUT)/result.json)
