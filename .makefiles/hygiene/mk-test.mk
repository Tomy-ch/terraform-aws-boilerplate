## `.mk` のレシピが道具を順に起動するだけであることの検査（ADR-0702 決定25）
##
## mk-test-lint が make の読んだ規則の一覧（`make -pRrq`）を自分で取り、`.mk` を読んだ結果と突き合わせる
## （決定15）。host の make を起動するので、提供段のコンテナを経由しない。

MK_TEST_BYPASS = .github/mk-test-bypass.toml
MK_RECIPE_SCOPE = .github/mk-recipe-scope.toml

.PHONY: mk-test-lint ## .mk のレシピの分岐・繰り返しとテストのターゲットを検査（移行中は範囲の宣言と除外の宣言に従う）
mk-test-lint:
	@$(call RUN_SCRIPT_HOST,mk-test-lint,-makefile Makefile -dir .makefiles -bypass $(MK_TEST_BYPASS) -scope $(MK_RECIPE_SCOPE))
