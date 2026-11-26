## 運用機構（scripts/）の Go の整形

.PHONY: go-fmt ## Go を整形
go-fmt:
	@$(GO_TOOL) go -C scripts fmt ./...

## canary と本走査の順と判定は scripts/gofmt-gate が持つ。
.PHONY: go-fmt-check ## 整形済みであることを検査
go-fmt-check:
	@$(call RUN_SCRIPT,gofmt-gate,check -work tmp/gofmt-gate -dir scripts)
