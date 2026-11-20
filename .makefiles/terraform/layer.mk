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
# LY306 が読む schema は layer-schema が作る（conftest/hcl/layer/README.md の Provider の schema）。
# fixture は LY_SCHEMA_STUB に固定の JSON を渡す。
LY_SCHEMA_STUB =
LY_SCHEMA_DIR = $(LY_OUT)/schema
LY_AWS_PROVIDER = registry.terraform.io/hashicorp/aws
# lock の版の抽出と検証、取り直しの判定、抽出と和は scripts/provider-schema が持つ。ここは init と schema の取得の順序だけを持つ。
# lock は git が追跡する一覧を位置引数で、find の独立の走査を -listed で渡す（ADR-0702 決定15。LY_HCL_INPUT_CMD と同じ理由）。
LY_SCHEMA_PREPARE_CMD = $(call RUN_SCRIPT,provider-schema,prepare -provider $(LY_AWS_PROVIDER) -out "$(LY_SCHEMA_DIR)" -listed "$(LY_OUT)/locks.found" "$$@") provider-schema $$locks
# root は sh -c の位置引数で渡す。RUN_SCRIPT の $(2) は単一引用符の内側に入り、レシピのシェル変数が展開されない。
LY_HCL_INPUT_CMD = $(call RUN_SCRIPT,hcl-input,-base "$(LY_BASE)" -listed "$(LY_OUT)/found.txt" -out "$(LY_OUT)/hcl-input.json" "$$@") hcl-input $$roots

.PHONY: layer-check ## 層の規則（usecase 直下の block、module の source の向き、attachment 型の親の同居と共存）を HCL のソースで検査する
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

.PHONY: layer-schema ## 層の検査に渡す AWS Provider の schema の一覧（lock した版ごとの和）を作る
layer-schema:
	@set -eu; mkdir -p $(LY_OUT); : > $(LY_OUT)/schema.json; \
	if [ -n "$(LY_SCHEMA_STUB)" ]; then cp "$(LY_SCHEMA_STUB)" $(LY_OUT)/schema.json; exit 0; fi; \
	roots=""; for d in $(LY_ROOTS); do if [ -d "$(LY_BASE)/$$d" ]; then roots="$$roots $$d"; fi; done; \
	[ -n "$$roots" ] || { echo "❌ 走査の root が1つもありません（$(LY_ROOTS)）" >&2; exit 2; }; \
	git -C "$(LY_BASE)" ls-files -- $(foreach r,$(LY_ROOTS),'$(r)/*.terraform.lock.hcl') > $(LY_OUT)/locks.git; \
	(cd "$(LY_BASE)" && find $$roots -name .terraform.lock.hcl -not -path '*/.terraform/*') > $(LY_OUT)/locks.raw; \
	sed 's|^|$(LY_BASE)/|' $(LY_OUT)/locks.raw > $(LY_OUT)/locks.found; \
	locks=$$(sed 's|^|$(LY_BASE)/|' $(LY_OUT)/locks.git); \
	mkdir -p "$$TF_PLUGIN_CACHE_DIR"; \
	dirs=$$($(LY_SCHEMA_PREPARE_CMD)); \
	for d in $$dirs; do \
		$(TF) -chdir="$$d" init -backend=false -lockfile=readonly -input=false -no-color > "$$d/init.log" 2>&1 \
			|| { cat "$$d/init.log" >&2; echo "❌ $$d の init が失敗しました" >&2; exit 1; }; \
		$(TF) -chdir="$$d" providers schema -json > "$$d/raw.tmp" || { echo "❌ $$d の schema を取れませんでした" >&2; exit 1; }; \
		mv "$$d/raw.tmp" "$$d/raw.json"; \
	done; \
	$(call RUN_SCRIPT,provider-schema,merge -provider $(LY_AWS_PROVIDER) -dir "$(LY_SCHEMA_DIR)" -out $(LY_OUT)/schema.json)

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
	$(MAKE) -s --no-print-directory layer-schema || { echo "❌ layer-schema が失敗したので、Conftest を起動しません" >&2; exit 1; }; \
	[ -s $(LY_OUT)/schema.json ] || { echo "❌ layer-schema が成功を返しましたが、$(LY_OUT)/schema.json を書いていません" >&2; exit 1; }; \
	$(GO_TOOL) sh -c 'set -u; root=$$PWD; cd "$(LY_BASE)"; \
		exec $(CONFTEST_BIN) test --combine $(foreach p,$(LY_POLICY),-p "$$root/$(p)") --data "$$root/$(LY_DECL)" --data "$$root/$(LY_OUT)/files.json" \
		--data "$$root/$(LY_OUT)/hcl-input.json" --data "$$root/$(LY_OUT)/schema.json" \
		--namespace $(LY_NS) --ignore "$(IC_IGNORE)" --no-color -o json "$$@"' sh $$roots > $(LY_OUT)/result.json || :; \
	$(call RUN_SCRIPT,tf-local,policy-result -namespace $(LY_NS) -rego $(LY_REGO) $(LY_OUT)/result.json)
