## Dockerfile と compose の検査

.PHONY: docker-lint ## Dockerfile を hadolint で検査
docker-lint:
	@$(GO_TOOL) hadolint --config .hadolint.yaml docker/*/Dockerfile
