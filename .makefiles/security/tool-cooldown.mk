## ツールの更新猶予（cooldown）の検査
##
## **このターゲット群だけはホストで実行する。** 検査の入力が version manager 自身だからである
## —— mise.toml の短縮名がどの backend へ解決されるかは、mise に聞くほかない。ツールランナーの
## 提供段には version manager を残していないため（ADR-0503 決定11）、コンテナ経由では成立しない。
##
## CI でも ADR-0503 決定7 のとおり実行環境へ直接用意するので、同じ経路で動く。
##
## GitHub API は未認証だと 60 req/hour（IP 単位）で1回の実行を賄えないため GITHUB_TOKEN を渡す。
## CI は workflow が渡し、手元では gh から借りる。

TOOL_COOLDOWN_ENV = GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null)}"

.PHONY: tool-cooldown-gate ## 宣言の差分で追加/更新したツールが cooldown を満たすか検査（違反で fail）
tool-cooldown-gate:
	@test -n "$(BASE)" || { echo "❌ BASE が要ります: make tool-cooldown-gate BASE=origin/release/vX.Y.0"; exit 1; }
	@$(TOOL_COOLDOWN_ENV) $(call RUN_SCRIPT_HOST,tool-cooldown,gate --base=$(BASE) $(COOLDOWN_REPORT))

.PHONY: tool-cooldown-audit ## 宣言全件の cooldown 状況を棚卸し（警告のみ・ゲートしない）
## バイパスの期限切れだけは失敗させる。**期限は宣言が変わらなくても訪れる**ため、
## このターゲットは定期実行にも載せる（ADR-0702 決定18）。
tool-cooldown-audit:
	@$(TOOL_COOLDOWN_ENV) $(call RUN_SCRIPT_HOST,tool-cooldown,audit $(COOLDOWN_REPORT))

.PHONY: tool-cooldown-outdated ## 上流の新版を調べ、窓を満たす更新先を報告（ゲートしない）
## gate も audit も宣言済みの版の齢しか測らないため、上流に新版が出たことはどちらも見ない。
tool-cooldown-outdated:
	@$(TOOL_COOLDOWN_ENV) $(call RUN_SCRIPT_HOST,tool-cooldown,outdated $(if $(OUT),--summary-out=$(OUT),))
