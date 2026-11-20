## インフラ内コードの検出（ADR-0103 決定8-18）
##
## 判定は conftest/hcl/infra_code/ の Rego と scripts/infra-code-gate が持つ（分担は conftest/hcl/infra_code/README.md）。
## ここが持つのは、入力の集め方と起動の順だけである。
##
## Conftest の入力は scripts/hcl-input が走査の root の下から作る JSON で、読んだファイルの集合は find の一覧と、
## 読んだ block の数は行頭の数えと突き合わせる（ADR-0702 決定15。段は hcl-input.mk）。root の外に置いた構成
## ファイルと、規則が評価されたかは infra-code-gate count が見る。

include .makefiles/terraform/hcl-input.mk

IC_BASE ?= .
IC_ROOTS = usecases modules env
# 宣言のパスはここだけが持ち、Conftest の --data と infra-code-gate decl の -decl へ同じものを渡す。
# --data にディレクトリを渡すと、そこに在る他のファイルも深くマージされて検出の入力に混ざる。
IC_DECL ?= conftest/hcl/infra_code/decl.json
# 共有部分と検出の木を別々に -p へ渡す。conftest/hcl の下に別の検査の木が足されても、その規則を読まない。
IC_POLICY = conftest/hcl/lib conftest/hcl/infra_code
IC_NS ?= infra_code
IC_FIXTURES = conftest/hcl/lib/fixtures conftest/hcl/infra_code/fixtures
IC_POLICY_LIST = $(subst $(space),$(comma),$(strip $(IC_POLICY)))
comma := ,
space := $(subst ,, )
IC_OUT = tmp/infra-code-gate
CONFTEST_BIN ?= conftest
# init が落とした写しと lock は構成ではない。--ignore はファイルにだけ効き、ディレクトリの中は歩く。層の検査が使う。
IC_IGNORE = (^|/)\.terraform(/|$$)|\.terraform\.lock\.hcl$$
IC_HCL_INPUT_CMD = $(call HCL_INPUT_CMD,$(IC_BASE),$(IC_OUT))

.PHONY: infra-code-gate ## インフラ内コードを検出し、許可リストに無いものを落とす（ADR-0103）
infra-code-gate:
	@set -u; failed=""; \
	for t in infra-code-verify infra-code-decl-check infra-code-detect; do \
		$(MAKE) -s --no-print-directory "$$t" || failed="$$failed $$t"; \
	done; \
	if [ -n "$$failed" ]; then echo "❌ infra-code-gate が落ちた検査:$$failed"; exit 1; fi

.PHONY: infra-code-verify ## 検出の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
infra-code-verify:
	@set -u; mkdir -p $(IC_OUT); rc=0; \
	$(call HCL_INPUT_FIXTURES,$(IC_OUT),$(IC_FIXTURES)); \
	$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(IC_POLICY),-p $(p)) $(foreach d,$(IC_FIXTURES),--data $(call HCL_FIXTURE_JSON,$(IC_OUT),$(d))) --no-color --report notes > $(IC_OUT)/verify.txt 2>&1 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then cat $(IC_OUT)/verify.txt; fi; \
	$(call RUN_SCRIPT,infra-code-gate,rego-tests -policy $(IC_POLICY_LIST) -report $(IC_OUT)/verify.txt -namespaces $(IC_NS)) || rc=1; \
	exit $$rc

.PHONY: infra-code-decl-check ## 許可リストの宣言を ADR・ソースの所在・code owner の宣言・保護設定と突き合わせる
infra-code-decl-check:
	@$(call RUN_SCRIPT,infra-code-gate,decl -decl $(IC_DECL))

.PHONY: infra-code-detect ## Terraform の構成からインフラ内コードを検出し、読んだファイルと block を独立の経路と突き合わせる
infra-code-detect:
	@set -u; mkdir -p $(IC_OUT); failed=""; rm -f $(IC_OUT)/combined.json; \
	roots=""; for d in $(IC_ROOTS); do if [ -d "$(IC_BASE)/$$d" ]; then roots="$$roots $$d"; fi; done; \
	if [ -z "$$roots" ]; then echo "❌ 走査の root が1つもありません（$(IC_ROOTS)）" >&2; exit 2; fi; \
	$(call HCL_INPUT_DETECT,$(IC_BASE),$(IC_OUT),$(IC_HCL_INPUT_CMD)); \
	$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(IC_POLICY),-p $(p)) --data $(IC_DECL) --data $(IC_OUT)/hcl-counts.json \
		--namespace $(IC_NS) --no-color --output json $(IC_OUT)/hcl-input.json > $(IC_OUT)/combined.json || { \
		failed="$$failed 検出"; \
		$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(IC_POLICY),-p $(p)) --data $(IC_DECL) --data $(IC_OUT)/hcl-counts.json \
			--namespace $(IC_NS) --no-color $(IC_OUT)/hcl-input.json || true; \
	}; \
	$(call RUN_SCRIPT,infra-code-gate,count -repo "$(IC_BASE)" -combined $(IC_OUT)/combined.json $(IC_ROOTS)) || failed="$$failed 件数の突き合わせ"; \
	if [ -n "$$failed" ]; then echo "❌ infra-code-detect が落ちた段:$$failed"; exit 1; fi
