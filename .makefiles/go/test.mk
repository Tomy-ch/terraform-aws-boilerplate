## 運用機構（scripts/）の Go に対する検査

.PHONY: go-test ## 運用機構のテストを実行
go-test:
	@$(GO_TOOL) go -C scripts test ./...

.PHONY: go-test-cover ## カバレッジ付きでテストを実行
go-test-cover:
	@$(GO_TOOL) go -C scripts test -coverprofile=../coverage.out ./...

.PHONY: go-tidy-check ## go.mod / go.sum が整っていることを検査
go-tidy-check:
	@$(GO_TOOL) sh -c 'go -C scripts mod tidy && git diff --exit-code scripts/go.mod scripts/go.sum'
