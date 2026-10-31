## 秘密の混入の検査（trufflehog）
##
## gitleaks と同じ範囲を、発行元ごとの検出器の集合で走査する。どちらか一方の検出でも通さない
## （ADR-0302 決定2）。
##
## 候補を発行元の API へ送る検証はしない（--no-verification）。検証の成否で合否も分けない
## （ADR-0302 決定2）。
##
## --fail-on-scan-errors を外さない。外すと、範囲の起点が解決できないときも0件の走査として
## exit 0 で通る。--no-ignore-tag は行に置く `trufflehog:ignore` を効かせない（同 決定9）。
##
## 終了コード: 0 = 所見なし、183 = 所見あり、それ以外 = 検査の不全。
##
## 既定の出力は候補の値を平文で出す。CI は TRUFFLEHOG_FORMAT=json で受け、値を運ばない要約だけを
## ログとコメントへ出す（.github/workflows/secret-scan.yaml）。
TRUFFLEHOG_FORMAT ?= text

TRUFFLEHOG_FLAGS := --no-verification --no-update --fail --fail-on-scan-errors --no-ignore-tag \
	--results=verified,unverified,unknown $(if $(filter json,$(TRUFFLEHOG_FORMAT)),--json)

## 抑止は path の正規表現で置く（trufflehog は fingerprint の抑止を持たない）。各エントリの理由は
## ファイルが持つ。履歴にだけ残る検出の抑止は全履歴の走査にだけ足す（ADR-0302 決定9）。
## 抑止のファイルを足すときは、scripts/secret-suppression-lint の対象にも足す。
TRUFFLEHOG_EXCLUDE := .trufflehog-exclude-paths.txt
TRUFFLEHOG_EXCLUDE_HISTORY := .trufflehog-exclude-paths-history.txt

.PHONY: trufflehog-scan ## push 予定の commit 範囲を trufflehog で走査
trufflehog-scan:
	@set -eu; $(SECRET_SCAN_FROM_GUARD); \
	$(MAKE) -s --no-print-directory trufflehog-canary; \
	from=$$(git rev-parse --verify "$(COMMITLINT_FROM)^{commit}"); \
	$(GO_TOOL) trufflehog git file://. --since-commit "$$from" --branch HEAD \
		$(TRUFFLEHOG_FLAGS) --exclude-paths=$(TRUFFLEHOG_EXCLUDE)

.PHONY: trufflehog-history ## 全履歴を trufflehog で走査
trufflehog-history: trufflehog-canary
	@set -eu; mkdir -p tmp; ex=$$(mktemp tmp/trufflehog-exclude.XXXXXX); trap 'rm -f "$$ex"' EXIT; \
	cat $(TRUFFLEHOG_EXCLUDE) $(TRUFFLEHOG_EXCLUDE_HISTORY) > "$$ex"; \
	$(GO_TOOL) trufflehog git file://. $(TRUFFLEHOG_FLAGS) --exclude-paths="$$ex"

## 名指ししたファイルの一覧を filesystem で走査する。filesystem は .gitignore を読まないので、一覧は
## 呼び出し側が作る。--exclude-paths は名指ししたファイルには効かないので、抑止は一覧から外して当てる。
## 一覧は1回の起動へ引数で渡す —— xargs は子の終了コードを 123 へ畳むので、所見（183）と検査の不全を
## 区別できなくなる。trufflehog の終了コードで exit する。
##   $(1) 一覧のファイル（path はリポジトリ直下から）  $(2) 失敗の文言に出す名前  $(3) trufflehog の引数
define TRUFFLEHOG_FILES
sed -e '/^#/d' -e '/^$$/d' $(TRUFFLEHOG_EXCLUDE) > $(1).pat; \
rc=0; grep -v -E -f $(1).pat $(1) > $(1).sel || rc=$$?; \
[ "$$rc" -le 1 ] || { echo "❌ $(2): 抑止の正規表現を解釈できません（$(TRUFFLEHOG_EXCLUDE)）" >&2; exit 2; }; \
test -s $(1).sel || { echo "❌ $(2): 走査対象のファイルが0件です" >&2; exit 2; }; \
$(GO_TOOL) sh -c 'set -f; IFS="$$(printf "\n_")"; IFS="$${IFS%_}"; list=$$1; shift; \
	exec trufflehog filesystem "$$@" $$(cat "$$list")' sh $(1).sel $(3)
endef

.PHONY: trufflehog-worktree ## commit 前の作業ツリーを trufflehog で走査
trufflehog-worktree: trufflehog-canary
	@set -eu; mkdir -p tmp; list=$$(mktemp tmp/trufflehog-worktree.XXXXXX); trap 'rm -f "$$list" "$$list".*' EXIT; \
	git -c core.quotepath=false ls-files -co --exclude-standard | while IFS= read -r f; do [ -e "$$f" ] && printf '%s\n' "$$f"; done > "$$list"; \
	$(call TRUFFLEHOG_FILES,"$$list",trufflehog-worktree,$(TRUFFLEHOG_FLAGS))

## 検出器が、本走査と同じ引数・範囲指定・抑止で、検証なしに働くことを確かめる。
## 抑止は全履歴の走査が使う和集合を渡す —— 全 path に当たる行が混ざると、ここが落ちる。値はリポジトリに置かず実行時に
## 作る（trivy-secret-canary と同じ理由）。
## 同じファイルを trufflehog-worktree と同じ TRUFFLEHOG_FILES に通す。
## AWS の検出器は ID と secret のエントロピーに閾値を持つので、乱数で作る。
.PHONY: trufflehog-canary ## trufflehog が偽の資格情報を git と filesystem の両経路で検証なしに検出することを検査
trufflehog-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/trufflehog-canary.XXXXXX); \
	trap 'rm -rf "$$dir" "$$dir".*' EXIT; \
	cat $(TRUFFLEHOG_EXCLUDE) $(TRUFFLEHOG_EXCLUDE_HISTORY) > "$$dir.ex"; \
	id=$$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Z2-7' | head -c 16); \
	secret=$$(head -c 512 /dev/urandom | base64 | LC_ALL=C tr -dc 'A-Za-z0-9/+' | head -c 40); \
	printf '[default]\naws_access_key_id = AKIA%s\naws_secret_access_key = %s\n' "$$id" "$$secret" > "$$dir/credentials"; \
	$(CANARY_GIT) -C "$$dir" init -q; \
	commit() { $(CANARY_GIT) -C "$$dir" -c user.name=canary -c user.email=canary@example.invalid -c commit.gpgsign=false "$$@"; }; \
	commit commit -q --allow-empty -m base; \
	from=$$($(CANARY_GIT) -C "$$dir" rev-parse HEAD); \
	commit add credentials; commit commit -q -m canary; \
	rc=0; out=$$($(GO_TOOL) trufflehog git "file://$$dir" --since-commit "$$from" --branch HEAD \
		$(filter-out --json,$(TRUFFLEHOG_FLAGS)) --json \
		--exclude-paths="$$dir.ex" 2>"$$dir.err") || rc=$$?; \
	$(call TRUFFLEHOG_CANARY_JUDGE,git); \
	printf '%s\n' "$$dir/credentials" > "$$dir.list"; \
	rc=0; out=$$( ( $(call TRUFFLEHOG_FILES,"$$dir.list",trufflehog-canary,$(filter-out --json,$(TRUFFLEHOG_FLAGS)) --json) ) \
		2>"$$dir.err") || rc=$$?; \
	$(call TRUFFLEHOG_CANARY_JUDGE,filesystem); \
	echo "✅ trufflehog-canary: 埋めた偽の AWS キーを git と filesystem の両経路で検証なしに検出しました" >&2

## $(1) 経路の名前。$$rc / $$out / $$dir.err を読む。
define TRUFFLEHOG_CANARY_JUDGE
case "$$rc:$$out" in \
183:*'"DetectorName":"AWS"'*) ;; \
0:*) echo "❌ trufflehog-canary（$(1)）: 埋めた偽の AWS キーを検出できませんでした（検出器・閾値・--results の変更を疑う）" >&2; exit 1 ;; \
*) echo "❌ trufflehog-canary（$(1)）: trufflehog が異常終了したか、AWS 以外の所見だけを返しました (rc=$$rc)" >&2; \
	tail -n 5 "$$dir.err" >&2; exit 1 ;; \
esac
endef

## 秘密の走査の番兵そのものの検査。範囲走査の起点が空・解決不能のとき、走査 commit 数が合わないとき、
## canary が偽キーを報告しないとき、filesystem 走査の対象が0件のとき、それぞれの番兵で落ちることを
## 確かめる。道具は起動しない。
## 子の make は失敗の理由を問わず 2 を返すので、どの番兵で落ちたかは文言で見る。
.PHONY: secret-gates-test ## 秘密の範囲走査が、起点を解決できないとき通らないことを検査
secret-gates-test:
	@set -u; failed=""; \
	for t in secret-scan trufflehog-scan; do \
		for from in origin/does-not-exist ''; do \
			if [ -n "$$from" ]; then want='が解決できません'; else want='走査範囲の起点が空です'; fi; \
			out=$$($(MAKE) -s --no-print-directory "$$t" COMMITLINT_FROM="$$from" 2>&1); rc=$$?; \
			case "$$rc:$$out" in \
			0:*) failed="$$failed $$t(from='$$from',rc=0)"; echo "$$out" >&2 ;; \
			*"$$want"*) ;; \
			*) failed="$$failed $$t(from='$$from',rc=$$rc)"; echo "$$out" >&2 ;; \
			esac; \
		done; \
	done; \
	mkdir -p tmp; d=$$(mktemp -d tmp/secret-gates-test.XXXXXX); trap 'rm -rf "$$d"' EXIT; \
	printf 'INF 3 commits scanned.\n' > "$$d/three"; printf 'INF 0 commits scanned.\n' > "$$d/zero"; \
	printf 'ERR error="stderr is not empty"\n' > "$$d/none"; \
	printf 'INF 2 commits scanned.\n' > "$$d/two"; printf 'INF 4 commits scanned.\n' > "$$d/four"; \
	for c in three:3:0 two:3:2 four:3:2 zero:3:2 none:3:2 zero:0:0; do \
		f=$${c%%:*}; r=$${c#*:}; want=$${r%%:*}; exp=$${r#*:}; \
		rc=0; ( $(call GITLEAKS_COUNT_CHECK,"$$d/$$f","$$n" -eq "$$want") ) 2>/dev/null || rc=$$?; \
		[ "$$rc" = "$$exp" ] || failed="$$failed gitleaks-count($$f,want=$$want,rc=$$rc)"; \
	done; \
	rc=0; ( $(call GITLEAKS_COUNT_CHECK,"$$d/zero","$$n" -gt 0) ) 2>/dev/null || rc=$$?; \
	[ "$$rc" = 2 ] || failed="$$failed gitleaks-count(history-zero,rc=$$rc)"; \
	printf '[{"RuleID": "aws-access-token"}]\n' > "$$d/hit"; printf '[]\n' > "$$d/miss"; : > "$$d/log"; \
	for c in 1:hit:0 1:miss:1 1:absent:1 0:hit:1 2:hit:1; do \
		r=$${c%%:*}; rest=$${c#*:}; f=$${rest%%:*}; exp=$${rest#*:}; \
		rc=0; ( $(call GITLEAKS_CANARY_JUDGE,$$r,"$$d/$$f",test,"$$d/log") ) 2>/dev/null || rc=$$?; \
		[ "$$rc" = "$$exp" ] || failed="$$failed gitleaks-canary-judge($$c,rc=$$rc)"; \
	done; \
	: > "$$d/j.err"; \
	for c in 183/AWS/0 183/Slack/1 0/none/1 1/none/1; do \
		r=$${c%%/*}; rest=$${c#*/}; det=$${rest%%/*}; exp=$${rest#*/}; \
		case "$$det" in none) o='' ;; *) o="{\"DetectorName\":\"$$det\"}" ;; esac; \
		got=0; ( rc=$$r; out=$$o; dir=$$d/j; $(call TRUFFLEHOG_CANARY_JUDGE,test) ) >/dev/null 2>&1 || got=$$?; \
		[ "$$got" = "$$exp" ] || failed="$$failed trufflehog-canary-judge($$c,rc=$$got)"; \
	done; \
	: > "$$d/empty"; got=0; ( $(call TRUFFLEHOG_FILES,"$$d/empty",test,) ) >/dev/null 2>&1 || got=$$?; \
	[ "$$got" = 2 ] || failed="$$failed trufflehog-files(empty,rc=$$got)"; \
	if [ -n "$$failed" ]; then echo "❌ secret-gates-test:$$failed" >&2; exit 1; fi; \
	echo "✅ secret-gates-test: 起点が空・解決不能の範囲走査、走査した commit の数が合わない gitleaks、偽キーを報告しない canary、走査対象が0件の filesystem 走査は、それぞれの番兵で落ちました" >&2
