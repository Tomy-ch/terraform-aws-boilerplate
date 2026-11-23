## conftest/hcl の検査が共有する道具の起動（ADR-0501 決定5）
##
## ここが持つのは起動の順序だけである。Conftest の入力の JSON は scripts/hcl-input が、それと突き合わせる独立の
## 経路の数え（find の一覧・行頭の数え・fixture の件数。ADR-0702 決定15）は scripts/hcl-tally が、Rego のテストの
## 合否は infra-code-gate rego-tests が持つ。どの root を走査し、Conftest をどう起動するかは各検査の make が持つ。

comma := ,
space := $(subst ,, )
CONFTEST_BIN ?= conftest

# HCL_TALLY は、$(1) の下の root $(3) を数え、$(2) に found.txt・hcl-counts.json（と、$(4) のフラグに従い
# files.json）を書く。
HCL_TALLY = $(call RUN_SCRIPT,hcl-tally,inputs -base "$(1)" -out "$(2)" $(4) $(3))

# HCL_INPUT は、$(1) の下の root $(3) から Conftest の入力 $(2)/hcl-input.json を作る。読んだファイルの集合は
# HCL_TALLY の found.txt と突き合わせる。
HCL_INPUT = $(call RUN_SCRIPT,hcl-input,-base "$(1)" -listed "$(2)/found.txt" -out "$(2)/hcl-input.json" $(3))

# HCL_VERIFY は、Rego のテストを走らせる。$(1) は出力の置き場、$(2) は Rego の置き場、$(3) は fixture の txtar の
# 置き場、$(4) は検出が評価する名前空間。Conftest の終了コードは捨て、合否は rego-tests が報告から決める。
define HCL_VERIFY
@mkdir -p $(1)
@$(call RUN_SCRIPT,hcl-input,fixtures -out $(1)/fixtures.json $(3))
@$(call RUN_SCRIPT,hcl-tally,fixtures -json $(1)/fixtures.json $(3))
-@$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(2),-p $(p)) --data $(1)/fixtures.json --no-color --report notes > $(1)/verify.txt 2>&1
@$(call RUN_SCRIPT,infra-code-gate,rego-tests -policy $(subst $(space),$(comma),$(strip $(2))) -report $(1)/verify.txt -namespaces $(4))
endef
