## 判定は scripts/bootstrap-check が持ち、ここは terraform test と bootstrap-check を順に起動するだけである。
## 実 AWS との突合は段2 の手順が持つ。
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。
##
## terraform test の終了コードは、落ちても次の行へ進めるようにファイルへ書き、bootstrap-check が読む。
## 落ちたときの error の文と終了コードの扱いは bootstrap-check が持つ。

## bootstrap-check へ渡すファイルはホストの絶対パスなので、RUN_SCRIPT_HOST で起動する
## （コンテナの中では /app になり、同じパスが無い）。

CHECK_PLAN_ROOT = $(TF_BASE)/env/admin/identity
CHECK_PLAN_OUT = $(abspath $(TF_BASE))/tmp/check-plan
# tf-init が root ごとに分ける置き場の名前（gates.mk の TF_DATA_DIR_OF と同じ写し）の、CHECK_PLAN_ROOT の分。
CHECK_PLAN_DATA = $(TF_GATES_DATA)/$(subst /,__,$(subst _,_u,$(patsubst $(TF_BASE)/%,%,$(CHECK_PLAN_ROOT))))

.PHONY: check-plan-expected ## bootstrap の plan の期待値が identity root の mock の plan と一致するかを検査
check-plan-expected: tf-init
	@mkdir -p "$(CHECK_PLAN_OUT)"
	@TF_DATA_DIR="$(CHECK_PLAN_DATA)" $(TF) -chdir="$(CHECK_PLAN_ROOT)" test -filter=tests/imports.tftest.hcl -json -verbose > "$(CHECK_PLAN_OUT)/test.jsonl"; echo $$? > "$(CHECK_PLAN_OUT)/test.exit"
	@$(call RUN_SCRIPT_HOST,bootstrap-check,plan -mock -test-exit-file "$(CHECK_PLAN_OUT)/test.exit" "$(CHECK_PLAN_OUT)/test.jsonl")
