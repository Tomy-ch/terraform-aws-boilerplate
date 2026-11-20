## `.mk` にテストのターゲットが無いことの検査（ADR-0702 決定25）
##
## make は自分が読んだ規則の一覧（`make -p`）を書き出すだけで、判定は mk-test-lint が持つ。
## 一覧は `.mk` を読んで取り出したターゲットと突き合わせる独立の経路である（決定15）。
## `-q` の終了コードは「更新が要る」の 1 を返しうるので、そのまま渡して判定を任せる。

MK_TEST_LINT_DB = tmp/mk-test-lint.db
MK_TEST_BYPASS = .github/mk-test-bypass.toml

.PHONY: mk-test-lint ## .mk にテストのターゲットが無いことを検査（移行中の宣言を除く）
mk-test-lint:
	@mkdir -p tmp; rc=0; LC_ALL=C $(MAKE) --no-print-directory -pRrq -f Makefile help > $(MK_TEST_LINT_DB) 2>/dev/null || rc=$$?; \
	$(call RUN_SCRIPT,mk-test-lint,-makefile Makefile -dir .makefiles -db $(MK_TEST_LINT_DB) -db-exit '"$$rc"' -bypass $(MK_TEST_BYPASS))
