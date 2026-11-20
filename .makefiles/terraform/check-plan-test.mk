## check-plan.sh の判定そのものの検査（ADR-0702 決定13・14・16・17）
##
## fixture は毎回 tmp/ に作る。スクリプトの写しを fixture の docs/setup/scripts/ に置き、期待値もそこへ書く。
## 本物の plan の経路は terraform を TERRAFORM で差し替え、git は fixture の中の使い捨ての repository を読ませる
## （外側のリポジトリから切り離すのは GIT_ISOLATE。git-isolation.mk）。

CHECK_PLAN_TEST_DIR = tmp/check-plan-test
# fixture の plan の resource_changes。期待値の「import b.y」「add a.x」に当たり、data source は数えない。
CHECK_PLAN_TEST_CHANGES = [{"address":"a.x","mode":"managed","change":{"actions":["create"],"importing":null}},{"address":"b.y","mode":"managed","change":{"actions":["no-op"],"importing":{"id":"i"}}},{"address":"data.c","mode":"data","change":{"actions":["read"]}}]

.PHONY: check-plan-expected-test ## check-plan.sh が一致・不足・余剰・読めない入力で期待どおりの終了コードを返すかを検査
check-plan-expected-test:
	@set -u; $(GIT_ISOLATE); command -v jq >/dev/null || { echo "❌ jq がホストにありません"; exit 1; }; \
	mkdir -p tmp; root=$$(mktemp -d "$(CHECK_PLAN_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; \
	root=$$(cd "$$root" && pwd); s="$$root/docs/setup/scripts"; failed=""; \
	mkdir -p "$$s" "$$root/env/admin/identity" "$$root/bin"; cp docs/setup/scripts/check-plan.sh "$$s/"; \
	changes='$(CHECK_PLAN_TEST_CHANGES)'; \
	expect() { \
		want="$$1"; name="$$2"; shift 2; \
		out=$$("$$s/check-plan.sh" "$$@" 2>&1); rc=$$?; \
		if [ "$$rc" -ne "$$want" ]; then failed="$$failed $$name"; printf '%s: 終了コード %s（%s を期待）\n%s\n' "$$name" "$$rc" "$$want" "$$out"; fi; \
	}; \
	expected() { printf '%s\n' "$$@" > "$$s/check-plan.expected"; }; \
	test_plan() { printf '{"plan_format_version":"%s","resource_changes":%s,"provider_schemas":{"x":{}}}' "$$1" "$$2"; }; \
	mock() { \
		file="$$root/$$1"; status="$$2"; shift 2; \
		printf '{"type":"test_run","@testfile":"tests/imports.tftest.hcl","@testrun":"check_plan","test_run":{"status":"%s"}}\n' "$$status" > "$$file"; \
		printf '{"type":"test_plan","@testfile":"tests/imports.tftest.hcl","@testrun":"other","test_plan":%s}\n' "$$(test_plan 1.2 '[]')" >> "$$file"; \
		for p in "$$@"; do printf '{"type":"test_plan","@testfile":"tests/imports.tftest.hcl","@testrun":"check_plan","test_plan":%s}\n' "$$p" >> "$$file"; done; \
	}; \
	ok_plan=$$(test_plan 1.2 "$$changes"); \
	mock ok.jsonl pass "$$ok_plan"; \
	expected "# comment" "" "import b.y" "add a.x"; expect 0 "mock:一致" --mock "$$root/ok.jsonl"; \
	expected "import b.y" "add a.x" "add z.z"; expect 1 "mock:不足" --mock "$$root/ok.jsonl"; \
	expected "import b.y"; expect 1 "mock:余剰" --mock "$$root/ok.jsonl"; \
	expected "# comment"; expect 2 "mock:期待値が空" --mock "$$root/ok.jsonl"; \
	expected "import b.y" "add a.x"; \
	mock zero.jsonl pass; expect 2 "mock:plan が0件" --mock "$$root/zero.jsonl"; \
	mock two.jsonl pass "$$ok_plan" "$$ok_plan"; expect 2 "mock:plan が2件" --mock "$$root/two.jsonl"; \
	mock fail.jsonl fail "$$ok_plan"; expect 2 "mock:run が pass でない" --mock "$$root/fail.jsonl"; \
	mock pfv.jsonl pass "$$(test_plan 2.0 "$$changes")"; expect 2 "mock:plan の形の版が違う" --mock "$$root/pfv.jsonl"; \
	printf 'not json\n' > "$$root/broken.jsonl"; expect 2 "mock:JSON でない" --mock "$$root/broken.jsonl"; \
	expect 2 "mock:ファイルが無い" --mock "$$root/missing.jsonl"; \
	expect 2 "mock:--partial と併用" --partial --mock "$$root/ok.jsonl"; \
	{ printf '{"type":"test_run","@testfile":"tests/other.tftest.hcl","@testrun":"check_plan","test_run":{"status":"pass"}}\n'; \
		printf '{"type":"test_plan","@testfile":"tests/other.tftest.hcl","@testrun":"check_plan","test_plan":%s}\n' "$$ok_plan"; } > "$$root/other-file.jsonl"; \
	expect 2 "mock:別のファイルの run" --mock "$$root/other-file.jsonl"; \
	mock twice.jsonl pass "$$ok_plan"; sed -n 1p "$$root/twice.jsonl" >> "$$root/twice.jsonl"; \
	expect 2 "mock:pass が2回" --mock "$$root/twice.jsonl"; \
	mock unknown.jsonl pass "$$(test_plan 1.2 "$$(printf '%s' "$$changes" | sed 's/"create"/"frobnicate"/')")"; \
	expect 1 "mock:知らない actions" --mock "$$root/unknown.jsonl"; \
	mock replace.jsonl pass "$$(test_plan 1.2 "$$(printf '%s' "$$changes" | sed 's/\["create"\]/["delete","create"]/')")"; \
	expected "import b.y" "add a.x" "destroy a.x"; expect 0 "mock:置き換えを add と destroy に数える" --mock "$$root/replace.jsonl"; \
	git -C "$$root" init -q && \
		git -C "$$root" -c user.name=canary -c user.email=canary@example.invalid -c commit.gpgsign=false commit -q --allow-empty -m canary \
		|| { echo "❌ fixture の git repository を作れません"; exit 1; }; \
	printf '#!/bin/sh\ncat "%s/real.json"\n' "$$root" > "$$root/bin/terraform"; chmod +x "$$root/bin/terraform"; export TERRAFORM="$$root/bin/terraform"; \
	: > "$$root/p.tfplan"; real() { printf '{"format_version":"1.2","complete":%s,"errored":%s,"resource_changes":%s}' "$$1" "$$2" "$$3" > "$$root/real.json"; }; \
	expected "import b.y" "add a.x"; \
	real true false "$$changes"; expect 0 "real:一致" "$$root/p.tfplan"; \
	real true true "$$changes"; expect 2 "real:plan がエラー" "$$root/p.tfplan"; \
	real false false "$$changes"; expect 2 "real:plan が完全でない" "$$root/p.tfplan"; \
	printf '{"resource_changes":[]}' > "$$root/real.json"; expect 2 "real:plan の JSON でない" "$$root/p.tfplan"; \
	real true false "$$changes"; expected "import b.y" "add a.x" "add z.z"; expect 0 "real:--partial で apply 済みを済みとする" --partial "$$root/p.tfplan"; \
	expected "import b.y"; expect 1 "real:--partial でも余剰で落ちる" --partial "$$root/p.tfplan"; \
	r="$$root/recipe"; mkdir -p "$$r/env/admin/identity" "$$r/docs/setup/scripts"; : > "$$r/env/admin/identity/main.tf"; \
	cp docs/setup/scripts/check-plan.sh "$$r/docs/setup/scripts/"; printf '%s\n' "import b.y" "add a.x" > "$$r/docs/setup/scripts/check-plan.expected"; \
	mock recipe.jsonl pass "$$ok_plan"; \
	printf '#!/bin/sh\nfor a in "$$@"; do if [ "$$a" = test ]; then [ -e "%s/test-fails" ] && exit 1; cat "%s/recipe.jsonl"; fi; done\nexit 0\n' "$$root" "$$root" > "$$root/bin/tf-recipe"; \
	chmod +x "$$root/bin/tf-recipe"; \
	recipe() { \
		want="$$1"; name="$$2"; pattern="$$3"; \
		out=$$($(MAKE) -s --no-print-directory check-plan-expected TF_BASE="$$r" TF_BIN="$$root/bin/tf-recipe" 2>&1); rc=$$?; \
		if { [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; } || { [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; } || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then \
			failed="$$failed $$name"; printf '%s: 終了コード %s（%s を期待）\n%s\n' "$$name" "$$rc" "$$want" "$$out"; \
		fi; \
	}; \
	recipe pass "recipe:一致" "✅ check-plan.expected は identity root の mock の plan と一致します"; \
	printf '%s\n' "import b.y" > "$$r/docs/setup/scripts/check-plan.expected"; \
	recipe fail "recipe:差分" "一致しません"; \
	printf '%s\n' "import b.y" "add a.x" > "$$r/docs/setup/scripts/check-plan.expected"; \
	mock recipe.jsonl fail "$$ok_plan"; recipe fail "recipe:検証できない" "突き合わせられません"; \
	: > "$$root/recipe.jsonl"; recipe fail "recipe:terraform test の出力が空" "突き合わせられません"; \
	mock recipe.jsonl pass "$$ok_plan"; : > "$$root/test-fails"; recipe fail "recipe:terraform test が落ちる" "terraform test が落ちました"; \
	if [ -n "$$failed" ]; then echo "❌ check-plan-expected-test: 期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ check-plan-expected-test: 全ケースが期待どおりでした"
