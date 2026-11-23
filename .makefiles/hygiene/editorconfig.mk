## .editorconfig の宣言とファイルの一致の検査
##
## 引数を渡さない editorconfig-checker は、git の一覧（追跡ファイルと、ignore されていない未追跡のファイル）を
## すべて見る。.editorconfig-checker.json は空に保つ —— 除外を足すと、検査対象が黙って縮む。canary・設定の確認・
## 本走査の順と判定は scripts/editorconfig-gate が持つ。
##
## 抑止は範囲を開く印と閉じる印で囲んだ範囲に限り、開く印に理由と撤回条件を書く
## （ADR-0501 決定13）。ファイルや行の単位で外さない。書き方は scripts/ の *_test.go にある実例に倣う。
## **散文で印に触れるときは綴りを書かない。** editorconfig-checker は印を行の部分文字列として読むので、
## 引用しただけでそのファイルの残りが検査の外に出る。editorconfig-directive-lint がそれを落とす。

.PHONY: editorconfig-check ## ファイルが .editorconfig の宣言に一致することを検査
editorconfig-check: editorconfig-directive-lint
	@$(call RUN_SCRIPT,editorconfig-gate,check -work tmp/editorconfig-gate -editorconfig .editorconfig -checker-config .editorconfig-checker.json)

.PHONY: editorconfig-directive-lint ## editorconfig-checker の無効化の印が、理由つきの閉じた範囲だけであることを検査
editorconfig-directive-lint:
	@$(call RUN_SCRIPT,editorconfig-directive-lint,)
