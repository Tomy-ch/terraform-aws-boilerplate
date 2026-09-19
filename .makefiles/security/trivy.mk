## Trivy による検査
##
## `trivy config` は misconfiguration 専用のサブコマンドで、`fs --scanners misconfig` と違って
## lang-pkgs の空 Result が混ざらない。`.trivyignore.yaml` は自動検出されないため `--ignorefile` の
## 明示が要り、その結果として抑止が他のスキャナへ暗黙に波及しない。
##
## 秘密の検出の権威は gitleaks が持つ（ADR-0302 決定1）。trivy-secret はそれと重複するが、意図した
## 重複である —— Trivy は誤検知が少なく、gitleaks の正規表現 / エントロピー網は取りこぼしが少ない。
##
## 出力形式は既定の table が人向け。CI は json / sarif を渡して stdout をリダイレクトする。trivy は
## 診断ログを stderr へ出すので stdout は純粋な json / sarif になる。**ただし make 自身がレシピ行を
## stdout へエコーするため、機械可読な出力を取るときは必ず `make -s` で呼ぶこと。** `-s` を落とすと
## JSON の1行目にコマンド文字列が混ざり、jq が構文エラーになる。
TRIVY_FORMAT ?= table

# 走査から外すもの。我々が書いたものではない取得物と VCS の内部。
TRIVY_SKIP_FLAGS := --skip-version-check

# 脆弱性スキャンの共通条件。`--ignore-unfixed` の有無だけが通常スキャンとリリースゲートの差なので、
# それ以外はここへ集約する。
TRIVY_VULN_FLAGS := --scanners vuln --pkg-types library --severity CRITICAL,HIGH,MEDIUM

.PHONY: trivy-config ## Terraform と Dockerfile の誤設定を検査（CRITICAL,HIGH でゲート）
trivy-config:
	@$(GO_TOOL) trivy config --severity CRITICAL,HIGH --ignorefile .trivyignore.yaml --exit-code 1 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-fs ## 依存の既知脆弱性を検査（報告のみ。ブロック判定は trivy-fs-release が持つ）
trivy-fs:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --ignore-unfixed --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-fs-release ## 修正版のない脆弱性も含めて依存を検査（リリース昇格ゲート）
trivy-fs-release:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --exit-code 1 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-license ## 依存のライセンスを列挙（報告のみ）
##
## 禁止ライセンスの方針が無いため severity でも絞らず、ゲートにもしない。方針が決まるまでは、
## 何が入っているかを見えるようにすることだけがこのターゲットの仕事である。
trivy-license:
	@$(GO_TOOL) trivy fs --scanners license --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-secret ## 作業ツリーの秘密を検査（gitleaks との重複は意図的）
##
## 上の脆弱性ターゲット群は `--scanners vuln` を明示して secret を落としているため、これは報告の
## 追加ではなく検査そのものの追加である。severity では絞らない —— 秘密に「軽微な漏洩」は無い。
trivy-secret:
	@$(GO_TOOL) trivy fs --scanners secret --exit-code 1 --ignorefile .trivyignore.yaml $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .
