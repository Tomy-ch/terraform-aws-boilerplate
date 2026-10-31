## インフラ内コードの検出（ADR-0103 決定8-18）
##
## 判定は conftest/hcl/ の Rego と scripts/infra-code-gate が持つ（分担は conftest/README.md）。
## ここが持つのは、入力の集め方と起動の順だけである。
##
## Conftest は走査の root を自分で歩く。infra-code-gate count がそれとは別の経路で歩き、読んだファイルの
## 集合を突き合わせる（ADR-0702 決定15）。root の外に置いた構成ファイルは、count が追跡ファイルから拾う。
##
## infra_code_input と infra_code は別に起動する（理由は conftest/README.md「インフラ内コードの検出」）。

IC_BASE ?= .
IC_ROOTS = usecases modules env
IC_DATA ?= conftest/data
IC_INPUT_NS ?= infra_code_input
IC_NS ?= infra_code
# $(call) の引数の中では , が区切りになるので、変数に入れて渡す。
IC_NAMESPACES = $(IC_INPUT_NS),$(IC_NS)
# decl へ渡す引数。ゲート自身の検査が、宣言の読み先を差し替えるためにある。
IC_DECL_ARGS ?=
IC_OUT = tmp/infra-code-gate
CONFTEST_BIN ?= conftest
# init が落とした写しと lock は構成ではない。--ignore はファイルにだけ効き、ディレクトリの中は歩く。
IC_IGNORE = (^|/)\.terraform(/|$$)|\.terraform\.lock\.hcl$$

# Conftest を IC_BASE で起動する。Rego と宣言は呼び出し元の root から渡す。
define IC_CONFTEST
$(GO_TOOL) sh -c 'set -u; root=$$PWD; cd "$(IC_BASE)"; roots=""; \
	for d in $(IC_ROOTS); do if [ -d "$$d" ]; then roots="$$roots $$d"; fi; done; \
	if [ -z "$$roots" ]; then echo "❌ 走査の root が1つもありません（$(IC_ROOTS)）" >&2; exit 2; fi; \
	exec $(CONFTEST_BIN) test -p "$$root/conftest/hcl" --data "$$root/$(IC_DATA)" --ignore "$(IC_IGNORE)" --no-color $(1) $$roots'
endef

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
	$(GO_TOOL) $(CONFTEST_BIN) verify -p conftest/hcl --no-color --report notes > $(IC_OUT)/verify.txt 2>&1 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then cat $(IC_OUT)/verify.txt; fi; \
	$(call RUN_SCRIPT,infra-code-gate,rego-tests -report $(IC_OUT)/verify.txt -namespaces $(IC_NAMESPACES)) || rc=1; \
	exit $$rc

.PHONY: infra-code-decl-check ## 許可リストの宣言を ADR・ソースの所在・code owner の宣言・保護設定と突き合わせる
infra-code-decl-check:
	@$(call RUN_SCRIPT,infra-code-gate,decl $(IC_DECL_ARGS))

.PHONY: infra-code-detect ## Terraform の構成からインフラ内コードを検出し、読んだファイルの件数を突き合わせる
infra-code-detect:
	@set -u; mkdir -p $(IC_OUT); failed=""; \
	$(call IC_CONFTEST,--namespace $(IC_INPUT_NS) --output json) > $(IC_OUT)/input.json || { \
		failed="$$failed 入力の検査"; \
		$(call IC_CONFTEST,--namespace $(IC_INPUT_NS)) || true; \
	}; \
	$(call IC_CONFTEST,--combine --namespace $(IC_NS) --output json) > $(IC_OUT)/combined.json || { \
		failed="$$failed 検出"; \
		$(call IC_CONFTEST,--combine --namespace $(IC_NS)) || true; \
	}; \
	$(call RUN_SCRIPT,infra-code-gate,count -repo "$(IC_BASE)" -results $(IC_OUT)/input.json -combined $(IC_OUT)/combined.json $(IC_ROOTS)) || failed="$$failed 件数の突き合わせ"; \
	if [ -n "$$failed" ]; then echo "❌ infra-code-detect が落ちた段:$$failed"; exit 1; fi
