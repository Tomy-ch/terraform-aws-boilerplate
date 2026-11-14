## 運用機構（scripts/）の Go に対する検査

# カバレッジの下限。**テストが薄いことは「検査が働いていない」ことと見分けがつかない**
# （ADR-0702 決定16）ので、割れば警告ではなく失敗にする。
COVERAGE_THRESHOLD ?= 95

.PHONY: go-test ## 運用機構のテストを実行
go-test:
	@$(GO_TOOL) env GIT_DIR=$(GIT_POISON) GIT_INDEX_FILE=$(GIT_POISON) go -C scripts test ./...

.PHONY: go-test-cover ## カバレッジ付きでテストを実行
go-test-cover:
	@$(GO_TOOL) env GIT_DIR=$(GIT_POISON) GIT_INDEX_FILE=$(GIT_POISON) go -C scripts test -race -coverprofile=../coverage.out -covermode=atomic -count=1 ./...

.PHONY: cover-gate ## 総カバレッジが下限以上かを検査
##
## 判定は scripts/cover-gate（テストの当たる Go 側）が持つ。ここが渡すのは下限値だけである。
## awk で書かない理由は scripts/README.md の cover-gate 行。
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
## カバレッジ行は3つの形で出る。
##   1. `ok  <pkg> <時間>  coverage: X% ...`             … 通過。ok で落ちる
##   2. `coverage: X% of statements`                      … 失敗パッケージぶん。単独行
##   3. `<TAB><pkg><TAB><TAB>coverage: 0.0% of statements` … テストの無いパッケージ
## 落ちたときに残るのは 2 と 3 なので、行頭の ok / ? だけを見ていると最大の行が素通りする。
##
## 1つ目の sed は `gh run view --log` の行接頭辞を剥がす。剥がさないと行頭アンカーが外れる。
## 時刻手前の `[^0-9]*` は BOM を吸うため。
##
## grep の -a は必須。NUL が1バイト混ざると grep はバイナリとみなし、何も出さずに終わる。
## 失敗行が無いときの報せは stderr へ出す —— 呼び出し側が `> file` で受けて空判定できるように。
LOG ?= /dev/stdin

go-test-fails:
	@out="$$(cat $(LOG) \
		| sed -E -e 's|^[^	]*	[^	]*	[^0-9]*[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ||' \
		| sed -e 's|$(CURDIR)/||g' \
		| grep -avE '^(ok|\?)[[:space:]]' \
		| grep -avE 'coverage: [0-9.]+% of statements')"; \
	if [ -z "$$out" ]; then echo "✅ 失敗行はありません（$(LOG)）" >&2; else printf '%s\n' "$$out"; fi
