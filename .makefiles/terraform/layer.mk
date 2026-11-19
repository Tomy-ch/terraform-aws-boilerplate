## 層の規則の検査（ADR-0102 決定14・17・18。ADR-0501 決定5 がソースの構造の規約として Conftest に割り当てるもの）
##
## 判定は conftest/hcl/layer/ の Rego が持つ（規則は conftest/hcl/layer/README.md）。
## ここが持つのは、入力の集め方と起動の順だけである。
##
## Conftest は走査の root を自分で歩く。find がそれとは別の経路で構成ファイルを数え、その一覧を --data で渡して
## Rego が読んだファイルの集合と突き合わせる（ADR-0702 決定15）。規則の評価の数は tf-local policy-result が
## Rego の deny contains の数と突き合わせる —— 規則の無い名前空間を渡された Conftest は、何も数えずに 0 で終える。
##
## 検査(3) の参照は scripts/hcl-input が HCL の構文の木から作る JSON（data.hcl_input）を読む（ADR-0501 決定5）。
## hcl-input が失敗したら Conftest を起動しない —— 欠けた入力の上の Rego の結果は、読み手を迷わせる。

LY_BASE ?= .
LY_ROOTS = usecases modules env
LY_DECL ?= conftest/hcl/layer/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
LY_POLICY = conftest/hcl/lib conftest/hcl/layer
LY_REGO = conftest/hcl/layer/layer.rego
LY_NS ?= layer
LY_POLICY_LIST = $(subst $(space),$(comma),$(strip $(LY_POLICY)))
LY_OUT = tmp/layer-check
# root は sh -c の位置引数で渡す。RUN_SCRIPT の $(2) は単一引用符の内側に入り、レシピのシェル変数が展開されない。
LY_HCL_INPUT_CMD = $(call RUN_SCRIPT,hcl-input,-base "$(LY_BASE)" -listed "$(LY_OUT)/found.txt" -out "$(LY_OUT)/hcl-input.json" "$$@") hcl-input $$roots

.PHONY: layer-check ## 層の規則（usecase 直下の block、module の source の向き、attachment 型の親の同居）を HCL のソースで検査する
layer-check:
	@set -u; failed=""; \
	for t in layer-verify layer-detect; do \
		$(MAKE) -s --no-print-directory "$$t" || failed="$$failed $$t"; \
	done; \
	if [ -n "$$failed" ]; then echo "❌ layer-check が落ちた検査:$$failed"; exit 1; fi

.PHONY: layer-verify ## 層の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
layer-verify:
	@set -u; mkdir -p $(LY_OUT); rc=0; \
	$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(LY_POLICY),-p $(p)) --no-color --report notes > $(LY_OUT)/verify.txt 2>&1 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then cat $(LY_OUT)/verify.txt; fi; \
	$(call RUN_SCRIPT,infra-code-gate,rego-tests -policy $(LY_POLICY_LIST) -report $(LY_OUT)/verify.txt -namespaces $(LY_NS)) || rc=1; \
	exit $$rc

.PHONY: layer-detect ## Terraform の構成を層の規則で検査し、読んだファイルを独立の走査と突き合わせる
layer-detect:
	@set -u; mkdir -p $(LY_OUT); rm -f $(LY_OUT)/found.txt $(LY_OUT)/files.json $(LY_OUT)/hcl-input.json $(LY_OUT)/hcl-input.json.atomicwrite.tmp $(LY_OUT)/result.json; \
	roots=""; for d in $(LY_ROOTS); do if [ -d "$(LY_BASE)/$$d" ]; then roots="$$roots $$d"; fi; done; \
	if [ -z "$$roots" ]; then echo "❌ 走査の root が1つもありません（$(LY_ROOTS)）" >&2; exit 2; fi; \
	(cd "$(LY_BASE)" && find $$roots \( -name '*.tf' -o -name '*.tf.json' \) -not -path '*/.terraform/*') > $(LY_OUT)/found.txt \
		|| { echo "❌ 構成ファイルの独立の走査（find）が失敗しました" >&2; exit 1; }; \
	files=$$(sort $(LY_OUT)/found.txt | sed 's/[\\"]/\\&/g; s/.*/"&"/' | paste -sd, -); \
	printf '{"layer_files": [%s]}\n' "$$files" > $(LY_OUT)/files.json; \
	$(LY_HCL_INPUT_CMD) || { echo "❌ hcl-input が失敗したので、Conftest を起動しません" >&2; exit 1; }; \
	[ -f $(LY_OUT)/hcl-input.json ] || { echo "❌ hcl-input が成功を返しましたが、$(LY_OUT)/hcl-input.json を書いていません" >&2; exit 1; }; \
	$(GO_TOOL) sh -c 'set -u; root=$$PWD; cd "$(LY_BASE)"; \
		exec $(CONFTEST_BIN) test --combine $(foreach p,$(LY_POLICY),-p "$$root/$(p)") --data "$$root/$(LY_DECL)" --data "$$root/$(LY_OUT)/files.json" \
		--data "$$root/$(LY_OUT)/hcl-input.json" \
		--namespace $(LY_NS) --ignore "$(IC_IGNORE)" --no-color -o json "$$@"' sh $$roots > $(LY_OUT)/result.json || :; \
	$(call RUN_SCRIPT,tf-local,policy-result -namespace $(LY_NS) -rego $(LY_REGO) $(LY_OUT)/result.json)
