## root の構成と backend 設定の不変条件の検査（ADR-0604 決定2-4・7・8。ADR-0501 決定5 がソースの構造の規約として
## Conftest に割り当てるもの）
##
## 判定は conftest/hcl/root/ の Rego が持つ（規則は conftest/hcl/root/README.md）。
##
## 読んだものの数は独立の経路の数と突き合わせる（ADR-0702 決定15。段の説明は conftest/hcl/root/README.md）。
## 規則の無い名前空間を渡された Conftest は何も数えずに 0 で終えるので、規則の評価の数は
## tf-local policy-result が Rego の deny contains の数と突き合わせる。

RT_BASE ?= .
RT_ROOTS = usecases env
RT_DECL ?= conftest/hcl/root/decl.json
# conftest/hcl の下の別の検査の木の規則を読まない（conftest/README.md）。
RT_POLICY = conftest/hcl/lib conftest/hcl/root
RT_REGO = conftest/hcl/root/root.rego
RT_NS ?= root
RT_POLICY_LIST = $(subst $(space),$(comma),$(strip $(RT_POLICY)))
RT_OUT = tmp/root-check
# module block の行頭。ラベルは引用符で囲むか識別子で書ける。
RT_MODULE_LINE = ^[[:space:]]*module[[:space:]]+("|[A-Za-z_])

.PHONY: root-check ## root が呼ぶ usecase の構成と、backend 設定の不変条件を HCL のソースで検査する
root-check:
	@set -u; failed=""; \
	for t in root-verify root-detect; do \
		$(MAKE) -s --no-print-directory "$$t" || failed="$$failed $$t"; \
	done; \
	if [ -n "$$failed" ]; then echo "❌ root-check が落ちた検査:$$failed"; exit 1; fi

.PHONY: root-verify ## root の検査の Rego のテストを走らせ、各 deny に違反と非違反のテストがあることを検査
root-verify:
	@set -u; mkdir -p $(RT_OUT); rc=0; \
	$(GO_TOOL) $(CONFTEST_BIN) verify $(foreach p,$(RT_POLICY),-p $(p)) --no-color --report notes > $(RT_OUT)/verify.txt 2>&1 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then cat $(RT_OUT)/verify.txt; fi; \
	$(call RUN_SCRIPT,infra-code-gate,rego-tests -policy $(RT_POLICY_LIST) -report $(RT_OUT)/verify.txt -namespaces $(RT_NS)) || rc=1; \
	exit $$rc

.PHONY: root-detect ## Terraform の構成を root の規則で検査し、読んだファイルと module block を独立の走査と突き合わせる
root-detect:
	@set -u; mkdir -p $(RT_OUT); rm -f $(RT_OUT)/found.txt $(RT_OUT)/files.json $(RT_OUT)/result.json; \
	roots=""; for d in $(RT_ROOTS); do if [ -d "$(RT_BASE)/$$d" ]; then roots="$$roots $$d"; fi; done; \
	if [ -z "$$roots" ]; then echo "❌ 走査の root が1つもありません（$(RT_ROOTS)）" >&2; exit 2; fi; \
	(cd "$(RT_BASE)" && find $$roots \( -name '*.tf' -o -name '*.tf.json' \) -not -path '*/.terraform/*') > $(RT_OUT)/found.txt \
		|| { echo "❌ 構成ファイルの独立の走査（find）が失敗しました" >&2; exit 1; }; \
	files=$$(sort $(RT_OUT)/found.txt | sed 's/[\\"]/\\&/g; s/.*/"&"/' | paste -sd, -); \
	counts=""; while IFS= read -r p; do \
		case "$$p" in *.tf) ;; *) continue ;; esac; \
		n=$$(grep -cE '$(RT_MODULE_LINE)' "$(RT_BASE)/$$p"); rc=$$?; \
		if [ "$$rc" -gt 1 ]; then echo "❌ $$p の module block を数えられません（grep）" >&2; exit 1; fi; \
		q=$$(printf '%s' "$$p" | sed 's/[\\"]/\\&/g'); counts="$$counts$${counts:+,}\"$$q\": $$n"; \
	done < $(RT_OUT)/found.txt; \
	printf '{"root_files": [%s], "root_module_counts": {%s}}\n' "$$files" "$$counts" > $(RT_OUT)/files.json; \
	$(GO_TOOL) sh -c 'set -u; root=$$PWD; cd "$(RT_BASE)"; \
		exec $(CONFTEST_BIN) test --combine $(foreach p,$(RT_POLICY),-p "$$root/$(p)") --data "$$root/$(RT_DECL)" --data "$$root/$(RT_OUT)/files.json" \
		--namespace $(RT_NS) --ignore "$(IC_IGNORE)" --no-color -o json "$$@"' sh $$roots > $(RT_OUT)/result.json || :; \
	$(call RUN_SCRIPT,tf-local,policy-result -namespace $(RT_NS) -rego $(RT_REGO) $(RT_OUT)/result.json)
