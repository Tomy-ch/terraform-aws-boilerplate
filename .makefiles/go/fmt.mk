## 運用機構（scripts/）の Go の整形

.PHONY: go-fmt ## Go を整形
go-fmt:
	@$(GO_TOOL) go -C scripts fmt ./...

.PHONY: go-fmt-check ## 整形済みであることを検査
go-fmt-check:
	@$(GO_TOOL) sh -c 'test -z "$$(gofmt -l scripts)" || { gofmt -l scripts; exit 1; }'
