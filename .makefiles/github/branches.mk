## 分岐のパターンの単一宣言を生成先へ反映・検証するコマンド群
##
## 保護対象のブランチ、リリース線の形、ブランチ名の接頭辞は .github/branches.toml だけが持つ
## （ADR-0603 決定1-3）。生成できる先（保護設定の宣言、workflow の push 側の起動条件）は
## apply が書き、check がずれで落ちる。生成できない側（Go のツール群）は実行時に読む。
.PHONY: branches-apply ## .github/branches.toml を保護設定と workflow の起動条件へ反映
.PHONY: branches-check ## 生成先が宣言通りか検証（書き換えなし・CI/hook用）
.PHONY: branches-list ## 集合の要素を1行ずつ出力（SET= で集合名を指定）
.PHONY: branches-get ## 単一の値を出力（KEY= でキーを指定）

branches-apply:
	@$(call RUN_SCRIPT,branches,apply)

branches-check:
	@$(call RUN_SCRIPT,branches,check)

branches-list:
	@test -n "$(SET)" || { echo "❌ SET が要ります: make branches-list SET=protected"; exit 1; }
	@$(call RUN_SCRIPT,branches,list $(SET))

branches-get:
	@test -n "$(KEY)" || { echo "❌ KEY が要ります: make branches-get KEY=default.branch"; exit 1; }
	@$(call RUN_SCRIPT,branches,get $(KEY))
