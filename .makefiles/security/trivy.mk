## Trivy による検査
##
## `trivy config` は misconfiguration 専用のサブコマンドで、secret scanner を含まない。
## `trivy fs` 側は vuln に限定する。秘密の検出の権威は gitleaks が持ち、同じ検査を二重に持たない。

.PHONY: trivy-config ## Terraform と Dockerfile の誤設定を検査
trivy-config:
	@$(GO_TOOL) trivy config --ignorefile .trivyignore.yaml --exit-code 1 .

.PHONY: trivy-fs ## 依存の既知脆弱性を検査（報告のみ。Pull Request を落とさない）
trivy-fs:
	@$(GO_TOOL) trivy fs --scanners vuln --exit-code 0 .
