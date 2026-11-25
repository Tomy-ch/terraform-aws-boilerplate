## 運用機構（scripts/）の Go に対する検査

# カバレッジの下限。運用機構はそれ自体がゲートであり、**テストが薄いことは「検査が働いていない」
# ことと見分けがつかない**（ADR-0702 決定16）。したがって下限割れは警告ではなく失敗にする。
COVERAGE_THRESHOLD ?= 95

# テストは、git の位置の変数が壊れた外側を指す環境で走らせる。TestMain で RunGitIsolated を呼ばずに git を
# 起動するパッケージは、そこで落ちる（scripts/README.md の Test Strategy）。
GO_TEST_GIT_POISON := env GIT_DIR=/dev/null/git-isolation-required GIT_INDEX_FILE=/dev/null/git-isolation-required

.PHONY: go-test ## 運用機構のテストを実行
go-test:
	@$(GO_TOOL) $(GO_TEST_GIT_POISON) go -C scripts test ./...

.PHONY: go-test-cover ## カバレッジ付きでテストを実行
go-test-cover:
	@$(GO_TOOL) $(GO_TEST_GIT_POISON) go -C scripts test -race -coverprofile=../coverage.out -covermode=atomic -count=1 ./...

.PHONY: cover-gate ## 総カバレッジが下限以上かを検査
##
## 判定は scripts/cover-gate（テストの当たる Go 側）が持つ。ここが渡すのは下限値だけである。
## awk のパイプラインで書くと、数値でないパーセンテージが 0 へ強制され、壊れたプロファイルが
## 「カバレッジ不足」として報告される —— 道具の失敗が、検査対象の失敗に化ける。
cover-gate:
	@$(call RUN_SCRIPT,cover-gate,-profile coverage.out -threshold $(COVERAGE_THRESHOLD))

.PHONY: test-mapping ## 関数・メソッドとテストが 1:1 で対応していることを検査
##
## 規約の正本は .claude/skills/scaffold-test。何を見るかは scripts/README.md の test-mapping 行。
test-mapping:
	@$(call RUN_SCRIPT,test-mapping,)

.PHONY: go-tidy-check ## go.mod / go.sum が整っていることを検査
go-tidy-check:
	@$(GO_TOOL) sh -c 'go -C scripts mod tidy && git diff --exit-code scripts/go.mod scripts/go.sum'

.PHONY: go-test-fails ## go test のログから失敗だけを抜き出す（LOG= でログ指定、LOG=- で標準入力）
##
## どの行を落とすかは scripts/go-test-fails が持つ。標準入力を読むので、ホストで起動する（今までのレシピと同じ）。
LOG ?= /dev/stdin

go-test-fails:
	@$(call RUN_SCRIPT_HOST,go-test-fails,-log "$(LOG)" -strip "$(CURDIR)/")
