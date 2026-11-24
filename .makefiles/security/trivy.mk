## Trivy による検査
##
## `trivy config` は misconfiguration 専用のサブコマンドで、`fs --scanners misconfig` と違って
## lang-pkgs の空 Result が混ざらない。`.trivyignore.yaml` は自動検出されないため `--ignorefile` の
## 明示が要り、その結果として抑止が他のスキャナへ暗黙に波及しない。
##
## 出力形式は既定の table が人向け。CI は json / sarif を渡して stdout をリダイレクトする。trivy は
## 診断ログを stderr へ出すので stdout は純粋な json / sarif になる。**ただし make 自身がレシピ行を
## stdout へエコーするため、機械可読な出力を取るときは必ず `make -s` で呼ぶこと。** `-s` を落とすと
## JSON の1行目にコマンド文字列が混ざり、jq が構文エラーになる。
TRIVY_FORMAT ?= table

TRIVY_SKIP_FLAGS := --skip-version-check

TRIVY_VULN_FLAGS := --scanners vuln --pkg-types library --severity CRITICAL,HIGH,MEDIUM

# trivy-config のゲートの条件。trivy-config-canary も同じ条件で走る。
# tmp/ は gitignore された作業領域で、tf-validate と tf-policy の canary の fixture が置かれる。commit されない構成の
# 誤設定で手元のゲートを落とさない。canary は fixture を走査の起点として渡すので、これに掛からない。
TRIVY_CONFIG_FLAGS := --severity CRITICAL,HIGH,MEDIUM --ignorefile .trivyignore.yaml --exit-code 1 $(TRIVY_SKIP_FLAGS) --skip-dirs tmp

.PHONY: trivy-config ## Terraform と Dockerfile の誤設定を検査（閾値は TRIVY_CONFIG_FLAGS）
trivy-config: trivy-config-canary
	@$(GO_TOOL) trivy config $(TRIVY_CONFIG_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-config-canary ## trivy-config の閾値が MEDIUM の誤設定を落とすことを検査
## 閾値が緩められると、MEDIUM の誤設定は件数0の「所見なし」として通る。本走査の前に、MEDIUM の
## 所見だけを持つ Dockerfile を実行時に作り、本走査と同じ条件で落ちることを確かめる。置き場所と
## 出力先は trivy-secret-canary と同じ理由による。非0の終了は検出と trivy の異常終了を区別できない
## ので、所見の ID と合わせて見る。
trivy-config-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/trivy-config-canary.XXXXXX); \
	trap 'rm -rf "$$dir"' EXIT; \
	printf 'FROM alpine:latest\nUSER nobody\nHEALTHCHECK CMD true\n' > "$$dir/Dockerfile"; \
	rc=0; out=$$($(GO_TOOL) trivy config $(TRIVY_CONFIG_FLAGS) --quiet --format json "$$dir") || rc=$$?; \
	case "$$rc:$$out" in \
	0:*) echo "❌ trivy-config-canary: MEDIUM の誤設定（DS-0001）を持つ構成でゲートが落ちませんでした" >&2; exit 1 ;; \
	*'"ID": "DS-0001"'*) ;; \
	*) echo "❌ trivy-config-canary: trivy が DS-0001 を報告せずに終了しました（終了コード $${rc}。異常終了か、DS-0001 の ID・重大度の変更）" >&2; exit 1 ;; \
	esac; \
	echo "✅ trivy-config-canary: MEDIUM の誤設定を閾値の内側で検出しました" >&2

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
