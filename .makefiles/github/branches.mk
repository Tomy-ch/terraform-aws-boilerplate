## 分岐のパターンの単一宣言を保護設定へ反映・検証するコマンド群
##
## 保護対象のブランチ、リリース線の形、ブランチ名の接頭辞は scripts/lib/branches だけが持つ
## （ADR-0603 決定1-3）。生成できるのは保護設定の宣言だけで、他の読み手は同じパッケージを
## import するためコンパイラが一致を保証する。
.PHONY: branches-apply ## 分岐のパターンの宣言を .github/settings/branch-protection.json へ反映
.PHONY: branches-check ## 保護設定が宣言通りか検証（書き換えなし・CI/hook用）

branches-apply:
	@$(call RUN_SCRIPT,branches,apply)

branches-check:
	@$(call RUN_SCRIPT,branches,check)
