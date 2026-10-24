## Trivy による検査
##
## `trivy config` は misconfiguration 専用のサブコマンドで、`fs --scanners misconfig` と違って
## lang-pkgs の空 Result が混ざらない。`.trivyignore.yaml` は自動検出されないため `--ignorefile` の
## 明示が要り、その結果として抑止が他のスキャナへ暗黙に波及しない。
##
## 秘密の検出の権威は gitleaks が持つ。trivy-secret は補助として併用する（ADR-0302 決定2）。
##
## 出力形式は既定の table が人向け。CI は json / sarif を渡して stdout をリダイレクトする。trivy は
## 診断ログを stderr へ出すので stdout は純粋な json / sarif になる。**ただし make 自身がレシピ行を
## stdout へエコーするため、機械可読な出力を取るときは必ず `make -s` で呼ぶこと。** `-s` を落とすと
## JSON の1行目にコマンド文字列が混ざり、jq が構文エラーになる。
TRIVY_FORMAT ?= table

TRIVY_SKIP_FLAGS := --skip-version-check

# 脆弱性スキャンの共通条件。
TRIVY_VULN_FLAGS := --scanners vuln --pkg-types library --severity CRITICAL,HIGH,MEDIUM

# tmp/ は gitignore された作業領域で、tf-gates-test の fixture が置かれる。commit されない構成の
# 誤設定で手元のゲートを落とさない。
.PHONY: trivy-config ## Terraform と Dockerfile の誤設定を検査（CRITICAL,HIGH,MEDIUM でゲート）
trivy-config:
	@$(GO_TOOL) trivy config --severity CRITICAL,HIGH,MEDIUM --ignorefile .trivyignore.yaml --exit-code 1 $(TRIVY_SKIP_FLAGS) --skip-dirs tmp --format $(TRIVY_FORMAT) .

.PHONY: trivy-fs ## 依存の既知脆弱性を検査（報告のみ。ブロック判定は trivy-fs-release が持つ）
trivy-fs:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --ignore-unfixed --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-fs-release ## 修正版のない脆弱性も含めて依存を検査（リリース昇格ゲート）
trivy-fs-release:
	@$(GO_TOOL) trivy fs $(TRIVY_VULN_FLAGS) --exit-code 1 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-license ## 依存のライセンスを列挙（報告のみ）
##
## 禁止ライセンスの方針が無いため、severity で絞らず、ゲートにもしない。
trivy-license:
	@$(GO_TOOL) trivy fs --scanners license --exit-code 0 $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-secret ## 作業ツリーの秘密を検査（gitleaks との重複は意図的）
##
## 上の脆弱性ターゲット群は `--scanners vuln` を明示して secret を落としているため、これは報告の
## 追加ではなく検査そのものの追加である。severity では絞らない —— 秘密に「軽微な漏洩」は無い。
trivy-secret: trivy-secret-canary
	@$(GO_TOOL) trivy fs --scanners secret --exit-code 1 --ignorefile .trivyignore.yaml $(TRIVY_SKIP_FLAGS) --format $(TRIVY_FORMAT) .

.PHONY: trivy-secret-canary ## 埋めた偽の秘密を trivy-secret が検出できることを確かめる
##
## Trivy の結果は所見のあるファイルしか載せず、走査した件数を報告しない。走査が対象を失っても
## 「所見なし」と区別できないので（ADR-0702 決定13）、本走査の前に、実行時に作った偽の秘密を
## 検出させる。秘密の文字列はリポジトリに置かない —— 置けば gitleaks と本走査が拾う。
## 置き場所を tmp/ にするのは、container の経路でもツールランナーから見えるため。
## 出力は stderr へ出す。trivy-secret の stdout は json / sarif として読まれる。
## 検出は終了コードではなく結果の RuleID で見る。`--exit-code 1` の非0は、検出と trivy 自身の異常終了を
## 区別できない。
trivy-secret-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/trivy-secret-canary.XXXXXX); \
	trap 'rm -rf "$$dir"' EXIT; \
	printf 'token = "ghp_%s"\n' "$$(head -c 96 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 36)" > "$$dir/canary.txt"; \
	out=$$($(GO_TOOL) trivy fs --scanners secret --exit-code 0 $(TRIVY_SKIP_FLAGS) --quiet --format json "$$dir") || { \
		echo "❌ trivy-secret-canary: trivy が異常終了しました" >&2; exit 1; }; \
	case "$$out" in \
	*'"RuleID": "github-pat"'*) ;; \
	*) echo "❌ trivy-secret-canary: 埋めた偽の秘密を検出できませんでした" >&2; exit 1 ;; \
	esac; \
	echo "✅ trivy-secret-canary: 埋めた偽の秘密を検出しました" >&2
