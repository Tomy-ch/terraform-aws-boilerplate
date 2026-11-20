## 判定はスクリプト（docs/setup/scripts/check-plan.sh）が持ち、ここは terraform test を起動して出力を渡すだけである。実 AWS との突合は段2 の手順が持つ。
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。

CHECK_PLAN_ROOT = $(TF_BASE)/env/admin/identity
CHECK_PLAN_SCRIPT = $(TF_BASE)/docs/setup/scripts/check-plan.sh
CHECK_PLAN_OUT = $(abspath $(TF_BASE))/tmp/check-plan

.PHONY: check-plan-expected ## bootstrap の plan の期待値が identity root の mock の plan と一致するかを検査
check-plan-expected: tf-init
	@set -eu; $(TF_DATA_DIR_OF); \
	command -v jq >/dev/null || { echo "❌ jq がホストにありません（check-plan.sh が plan の読み取りに使う）"; exit 1; }; \
	mkdir -p "$(CHECK_PLAN_OUT)"; out="$(CHECK_PLAN_OUT)/test.jsonl"; \
	if ! TF_DATA_DIR="$$(data_dir "$(CHECK_PLAN_ROOT)")" $(TF) -chdir="$(CHECK_PLAN_ROOT)" test -filter=tests/imports.tftest.hcl -json -verbose > "$$out"; then \
		jq -r 'select(.["@level"] == "error") | .["@message"]' "$$out" 2>/dev/null || true; \
		echo "❌ $(CHECK_PLAN_ROOT) の terraform test が落ちました"; exit 1; \
	fi; \
	rc=0; "$(CHECK_PLAN_SCRIPT)" --mock "$$out" || rc=$$?; \
	if [ "$$rc" -eq 1 ]; then echo "❌ check-plan.expected が identity root の plan と一致しません（NG の行を期待値へ写す。docs/setup/bootstrap.md の「この文書の保守」）"; exit 1; fi; \
	if [ "$$rc" -ne 0 ]; then echo "❌ check-plan.expected と identity root の plan を突き合わせられません（理由は上の行）"; exit 1; fi; \
	echo "✅ check-plan.expected は identity root の mock の plan と一致します"
