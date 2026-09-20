## Go モジュールの供給網 cooldown
# -----ホスト上で実行するコマンド群-----
.PHONY: go-cooldown-gate ## go.mod の差分で追加/更新した direct モジュールが cooldown を満たすか検査(違反でfail)
.PHONY: go-cooldown-audit ## go.mod 全件の cooldown 状況を棚卸し(警告のみ・ゲートしない)

# 報告の形。CI では ::warning:: 注釈と step summary を出す（GITHUB_ACTIONS は Actions が立てる）。
# **この組み立てを workflow 側へ書かない** —— 検査の定義が2箇所に分かれると、片方だけが更新できる
# 状態が生まれる（ADR-0702 決定10-11）。
COOLDOWN_REPORT = $(if $(GITHUB_ACTIONS),--github,) $(if $(SUMMARY_OUT),--summary-out=$(SUMMARY_OUT),)

# -----ホスト上で実行するコマンド群-----
# gate が検知器ではなく防御の本体である理由、direct だけを落とす理由は
# .github/workflows/README.md の Go Cooldown 節。CI はワークフローが base ref から解決して渡す。
go-cooldown-gate:
	@test -n "$(BASE)" || { echo "❌ BASE が要ります: make go-cooldown-gate BASE=origin/release/vX.Y.0"; exit 1; }
	@$(call RUN_SCRIPT,go-cooldown,gate --base=$(BASE) $(COOLDOWN_REPORT))

# 棚卸しのみ。バイパスの期限切れだけ失敗させる（期限は go.mod が変わらなくても訪れる）。
go-cooldown-audit:
	@$(call RUN_SCRIPT,go-cooldown,audit $(COOLDOWN_REPORT))
