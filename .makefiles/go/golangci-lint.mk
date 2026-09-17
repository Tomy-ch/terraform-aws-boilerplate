## 運用機構（scripts/）の Go の Lint

.PHONY: go-lint ## Go を golangci-lint で検査
go-lint:
	@$(GO_TOOL) sh -c 'cd scripts && golangci-lint run --config ../.golangci.yaml ./...'
