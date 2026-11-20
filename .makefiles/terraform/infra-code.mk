## インフラ内コードの検出（ADR-0103 決定8-18）
##
## 判定は conftest/hcl/infra_code/ の Rego と scripts/infra-code-gate が持つ（分担は conftest/hcl/infra_code/README.md）。
## ここが持つのは、道具を起動する順だけである。前の段が落ちれば、make はそこで止まり Conftest を起動しない。
##
## Conftest の違反、規則が評価されたか、root の外に置いた構成ファイルは infra-code-gate count が見る。

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
IC_OUT = tmp/infra-code-gate

.PHONY: infra-code-gate ## インフラ内コードを検出し、許可リストに無いものを落とす（ADR-0103）
infra-code-gate: infra-code-verify infra-code-decl-check infra-code-detect

.PHONY: infra-code-verify ## 検出の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
infra-code-verify:
	$(call HCL_VERIFY,$(IC_OUT)/verify,$(IC_POLICY),$(IC_FIXTURES),$(IC_NS))

.PHONY: infra-code-decl-check ## 許可リストの宣言を ADR・ソースの所在・code owner の宣言・保護設定と突き合わせる
infra-code-decl-check:
	@$(call RUN_SCRIPT,infra-code-gate,decl -decl $(IC_DECL))

.PHONY: infra-code-detect ## Terraform の構成からインフラ内コードを検出し、読んだファイルと block を独立の経路と突き合わせる
infra-code-detect:
	@mkdir -p $(IC_OUT)
	@$(call HCL_TALLY,$(IC_BASE),$(IC_OUT),$(IC_ROOTS))
	@$(call HCL_INPUT,$(IC_BASE),$(IC_OUT),$(IC_ROOTS))
	-@$(GO_TOOL) $(CONFTEST_BIN) test $(foreach p,$(IC_POLICY),-p $(p)) --data $(IC_DECL) --data $(IC_OUT)/hcl-counts.json \
		--namespace $(IC_NS) --no-color --output json $(IC_OUT)/hcl-input.json > $(IC_OUT)/combined.json
	@$(call RUN_SCRIPT,infra-code-gate,count -repo "$(IC_BASE)" -combined $(IC_OUT)/combined.json $(IC_ROOTS))
