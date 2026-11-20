## conftest/hcl の検査が共有する、scripts/hcl-input の起動（ADR-0501 決定5）
##
## 入力の JSON の作り方と、それを独立の経路の数と突き合わせるための data（find の一覧と行頭の数え）を作る段、
## Rego のテストの fixture（txtar）を JSON にする段を、ここだけが持つ。どのファイルを走査するか（root）と、
## Conftest をどう起動するかは各検査の make が持つ。

# トップレベルの block の行頭。terraform fmt の木では、トップレベルの block は行頭に在り、入れ子は字下げされる。
# ヒアドキュメントやブロックコメントの中の行頭の一致は数え過ぎになり、検査の入力の規則で落ちる（通す向きには壊れない）。
HCL_BLOCK_LINE = ^[A-Za-z_][A-Za-z0-9_-]*([[:space:]]+("[^"]*"|[A-Za-z_][A-Za-z0-9_-]*))*[[:space:]]*\{

# hcl-input の起動。$(1) は走査の base、$(2) は出力の置き場。root は sh -c の位置引数で渡す（RUN_SCRIPT の $(2) は
# 単一引用符の内側に入り、レシピのシェル変数が展開されない）。
HCL_INPUT_CMD = $(call RUN_SCRIPT,hcl-input,-base "$(1)" -listed "$(2)/found.txt" -out "$(2)/hcl-input.json" "$$@") hcl-input $$roots

# HCL_INPUT_DETECT は、呼び出し側のシェル変数 roots（空白区切り）の下を find で列挙し、$(3) で hcl-input を起動し、
# $(2)/hcl-input.json（Conftest の入力）と $(2)/hcl-counts.json（--data）を書く。$(1) は走査の base。
# hcl-input が失敗するか JSON を書かなければ、Conftest を起動させずに落ちる。
define HCL_INPUT_DETECT
rm -f "$(2)/found.txt" "$(2)/hcl-input.json" "$(2)/hcl-input.json.atomicwrite.tmp" "$(2)/hcl-counts.json"; \
(cd "$(1)" && find $$roots -type f -not -path '*/.terraform/*') > "$(2)/found.txt" \
	|| { echo "❌ ファイルの独立の走査（find）が失敗しました" >&2; exit 1; }; \
$(3) || { echo "❌ hcl-input が失敗したので、Conftest を起動しません" >&2; exit 1; }; \
[ -f "$(2)/hcl-input.json" ] || { echo "❌ hcl-input が成功を返しましたが、$(2)/hcl-input.json を書いていません" >&2; exit 1; }; \
parsed=""; counts=""; \
while IFS= read -r p; do \
	case "$$p" in *.tf|*.tftest.hcl) ;; *) continue ;; esac; \
	n=$$(grep -cE '$(HCL_BLOCK_LINE)' "$(1)/$$p"); rc=$$?; \
	if [ "$$rc" -gt 1 ]; then echo "❌ $$p のトップレベルの block を数えられません（grep）" >&2; exit 1; fi; \
	q=$$(printf '%s' "$$p" | sed 's/[\\"]/\\&/g'); \
	parsed="$$parsed$${parsed:+,}\"$$q\""; counts="$$counts$${counts:+,}\"$$q\": $$n"; \
done < "$(2)/found.txt"; \
printf '{"hcl_parsed_files": [%s], "hcl_block_counts": {%s}}\n' "$$parsed" "$$counts" > "$(2)/hcl-counts.json"
endef

# HCL_FIXTURE_JSON は、fixture のディレクトリ $(2)（<木>/fixtures）から作る JSON の置き場を返す。
HCL_FIXTURE_JSON = $(1)/fixtures-$(notdir $(patsubst %/fixtures,%,$(2))).json

# fixture の JSON を作る起動。第1引数が txtar のディレクトリ、第2引数が出力のパス。
HCL_FIXTURES_CMD = $(call RUN_SCRIPT,hcl-input,fixtures -dir "$$1" -out "$$2") hcl-input

# HCL_INPUT_FIXTURES は、$(2) の各ディレクトリの txtar を JSON にして $(1) に置き、txtar の数と JSON のケースの数を
# 突き合わせる。古い JSON は先に消し、書かれなかった JSON は落とす。ケースが欠けたまま Rego のテストが空振りで
# 通ることを塞ぐ。
define HCL_INPUT_FIXTURES
$(foreach d,$(2),$(call HCL_INPUT_FIXTURE,$(d),$(call HCL_FIXTURE_JSON,$(1),$(d)))) :
endef

# HCL_INPUT_FIXTURE は、ディレクトリ $(1) の txtar を JSON $(2) にする（HCL_INPUT_FIXTURES の1つ分）。
define HCL_INPUT_FIXTURE
rm -f "$(2)" "$(2).atomicwrite.tmp"; \
$(HCL_FIXTURES_CMD) "$(1)" "$(2)" || { echo "❌ $(1) の fixture を JSON にできません" >&2; exit 1; }; \
[ -f "$(2)" ] || { echo "❌ hcl-input fixtures が成功を返しましたが、$(2) を書いていません" >&2; exit 1; }; \
want=$$(find "$(1)" -maxdepth 1 -type f -name '*.txtar' | wc -l | tr -d ' '); \
got=$$(grep -cE '^    "[^"]+": \{$$' "$(2)"); \
if [ "$$want" != "$$got" ]; then echo "❌ $(1) の txtar は $$want 件ですが、$(2) のケースは $$got 件です" >&2; exit 1; fi;
endef
