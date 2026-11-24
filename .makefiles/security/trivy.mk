## Trivy による検査
##
## 出力形式は既定の table が人向け。CI は json / sarif を渡して stdout をリダイレクトする。trivy は
## 診断ログを stderr へ出すので stdout は純粋な json / sarif になる。**ただし make 自身がレシピ行を
## stdout へエコーするため、機械可読な出力を取るときは必ず `make -s` で呼ぶこと。** `-s` を落とすと
## JSON の1行目にコマンド文字列が混ざり、jq が構文エラーになる。
TRIVY_FORMAT ?= table

TRIVY_SKIP_FLAGS := --skip-version-check

TRIVY_VULN_FLAGS := --scanners vuln --pkg-types library --severity CRITICAL,HIGH,MEDIUM

TRIVY_CONFIG_SEVERITY := CRITICAL,HIGH,MEDIUM

.PHONY: trivy-config ## Terraform と Dockerfile の誤設定を検査（閾値は TRIVY_CONFIG_SEVERITY）
## 引数の実体（閾値の一覧、canary、走査の対象の数え方）は scripts/iac-scan が持つ。
trivy-config:
	@$(call RUN_SCRIPT,iac-scan,trivy-config -base "$(TF_BASE)" -severity $(TRIVY_CONFIG_SEVERITY) -format $(TRIVY_FORMAT) $(TF_ROOTS))

.PHONY: trivy-fs ## 依存の既知脆弱性を検査（報告のみ。ブロック判定は trivy-fs-release が持つ）
trivy-fs:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --ignore-unfixed --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-fs-release ## 修正版のない脆弱性も含めて依存を検査（リリース昇格ゲート）
trivy-fs-release:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --ignorefile .trivyignore.yaml --exit-code 1 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-license ## 依存のライセンスを列挙（報告のみ）
##
## 禁止ライセンスの方針が無いため、severity で絞らず、ゲートにもしない。
trivy-license:
	@$(GO_TOOL) trivy fs --scanners license --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-secret ## 作業ツリーの秘密を検査（gitleaks との重複は意図的）
##
trivy-secret:
	@$(call RUN_SCRIPT,secret-scan,"$$@") secret-scan trivy -format "$(TRIVY_FORMAT)"
