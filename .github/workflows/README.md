# GitHub Actions Workflows

CI の workflow 定義。Pull Request を止めるゲート、供給網のクールダウン、秘密と誤設定の走査、
そしてリリース昇格の境界で効くゲートに分かれる。

道具の割当は [ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定4 が、
検査の定義は `.makefiles/` が持つ。ここが持つのは**起動の条件と、GitHub Actions 固有の制約**だけである。

## 起動の方針

| 群 | いつ走るか | 何をするか |
| --- | --- | --- |
| Pull Request のゲート | すべての Pull Request | lint / テスト / 宣言と実体のずれで merge を止める |
| 供給網のクールダウン | Pull Request と週次 | 公開直後のバージョンの採用を窓で止め、バイパスの期限を回収する |
| 走査 | Pull Request と週次 | 秘密・誤設定・依存の脆弱性・workflow 自身の穴を報告する |
| リリース昇格のゲート | deploy ブランチ宛の Pull Request | 修正版の無い脆弱性も含めて落とす |

## 必須検査はすべての Pull Request で報告される

[`branch-protection.json`](../settings/branch-protection.json) が名指しする context は、merge の前に
**報告されなければならない。** GitHub は報告の無い check を「該当しない」とは読まず、待ち続ける ——
その Pull Request は恒久的に merge 可能にならない。

したがって、必須 context を報告する workflow は `pull_request` の起動を**一切絞らない。**
`paths:` も `branches:` も、そこでは「run が起きるかどうか」を決めてしまい、起きなかった run は
何も報告しない。

起動条件は代わりに job の `if:` に置く。GitHub はそこでは逆の結論を出す ——
**`if:` で skip された job は `skipped` を報告し、required check はそれを成功として数える。**
path で絞る workflow は [`dorny/paths-filter`](https://github.com/dorny/paths-filter) を使う
`changes` job で条件を計算し、報告する job をその出力でゲートする。リリース昇格のゲートは
`github.base_ref` を `if:` で直接読む —— 条件がブランチ名であり、ファイル一覧を要さない。

ゲートは **fail open** で書く。

```yaml
    needs: changes
    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }}
```

`relevant` はフィルタが判定に到達しなかった場所では空になる（pull_request 以外のイベントで skip
された、あるいは失敗した）。空は `false` ではないので、仕事は走る。逆に書くと静かに壊れる ——
skip が誰も稼いでいない緑を必須 context へ渡し、**フィルタが壊れた瞬間こそがそれの起きる瞬間**である。

`push` 側の起動は自分の `paths:` を保つ。そちらは context を報告しないので、絞っても何も塞がない。

`make required-check-lint` が、すべての必須 context がちょうど1つの job から宣言されていることと、
その workflow の `pull_request` にフィルタが無いことを見る。**後半がこの行き止まりを閉じている。**
そして条件が1箇所にしか書かれていないからこそ検査できる。フィルタとその否定へ割ると、不変条件は
2つの一覧の一致へ移り、それを突き合わせられるものはここに無い。

## 分岐のパターン

保護対象のブランチ、リリース線の形、ブランチ名の接頭辞は
[`scripts/lib/branches`](../../scripts/lib/branches/branches.go) **だけ**が持つ
（[ADR-0603](../../docs/adr/0603-branch-protection-and-required-checks.md) 決定1-3）。
そこから生成するのは [`branch-protection.json`](../settings/branch-protection.json) だけで、
`make branches-check` がずれで落ちる。

**`pull_request` 側は絞らない。** 必須 context を報告する側であり、除外された Pull Request では
run が起きず、報告の無い check を GitHub は待ち続ける（「必須検査はすべての Pull Request で
報告される」）。`branches:` を置いてよいのは `push` 側だけである。

**その `push` 側の `branches:` は生成しない。** そちらは context を報告せず、絞っても merge を
塞がない —— 塞がないものを生成対象にすると、生成器が YAML の書式追随という終わりのない仕事を
抱え、その取りこぼしが「検査が素通りする」形で出る。ここは手で書く。

## 結果コメント

Pull Request のコメントは、**検査が報告すべきことを持っているときにだけ作られる。** 通った検査が
1つずつコメントを残すと、誰も訊いていないことが並び、言うことのある1件が埋まる。

判定は `upsert-pr-comment` の `status` が運ぶ。コメントの作成を抑えるのは文字列 `success` だけで、
それ以外の値 —— 打ち切られた job が残す空文字を含めて —— はコメントを作る。

**沈黙は修正の報告にならない。** `success` が抑えるのは*作成*であって更新ではない。前の push で
落ちた検査は、自分の赤いコメントを緑の結果でその場に上書きする。読み手は、失敗が報告された場所で
それが解決したことを知る —— 不在に気づくことによってではなく。

**判定は肯定側から導く。** 手前のステップが `status` を出すならそれを渡し、綺麗な状態が件数や
フラグで表されるなら、その値そのものを検査する（`steps.<id>.outputs.count == '0' && 'success' || 'findings'`）。
所見の側を検査してはならない。差が出るのは**生成側のステップが走らなかったとき**である ——
出力は空になり、それは綺麗な値ではないので、検査は黙らずに報告する。逆に書くと、途中で終わった
run がすべて合格に見える。

## ジョブの打ち切り

job は判定に到達しないまま止まりうる —— timeout、cancel、ランナーの障害。そのとき Pull Request に
何が見えるかは、走っていた道具の性質ではなく、job とコメントステップの宣言の仕方から決まる。
**そしてここでの既定はすべて間違った側である。** `make actions-cutoff-lint` が下の規則を課す。

**`upsert-pr-comment` を呼ぶステップは、cancel の後でも到達可能でなければならない。**
Actions は、状態検査関数を含まない `if:` へ暗黙の `success() &&` を前置する。打ち切られた job は
コメントステップを skip し、Pull Request には何の痕跡も残らない —— 一方 `Fail if …` は通常
`always()` を持つので check は赤くなる。**理由の読めない赤は、どちらの半分よりも悪い。**
条件には `always()` か `cancelled()` が要る。`failure()` は資格を持たない —— cancel された job で
false になる。

**body ファイルの不在は、ステップの失敗ではなく打ち切りとして報告する。** 早くに打ち切られた job は
ファイルを書くステップまで到達しないので、不在はまさにコメントが生き延びるべき場合の通常の形である。
`upsert-pr-comment` は打ち切りの通知を出し、呼び出し側の見出しを差し替える —— job が走らなかったと
述べる本文を、呼び出し側が設定したどの題も説明できない。

**不在は検査の半分でしかないので、呼び出し側は打ち切り用の見出しも渡す。** 多くの検査ステップは
出力を `tee` で body ファイルへ流し、`title` を終了コードから後で設定する。検査の途中で打ち切られた
job は**書きかけのファイル**を残し、action はそれを完成したものと区別できない。呼び出し側が
`${{ steps.<id>.outputs.title || '## ⚠️ <check>: CUT OFF (no result produced)' }}` でもう半分を運ぶ。
GitHub の式の罠に注意 —— `cond && '' || X` は常に `X` になる。空文字が falsy なので、見出しは
truthy 側へ置く。

**すべての job が `timeout-minutes` を持つ。** 無ければ GitHub の既定 360 分まで走り、1つのハングが
ランナーを6時間押さえる。値は実測の最大 × 3 を 5 分単位へ切り上げ、下限 10 分。完了した run が
まだ無い job は 15 分。

| job | 分 | 式から外れる理由 |
| --- | --- | --- |
| `go-test.yaml:go-test` | 20 | 実測 約5分 |
| `notify.yaml:notify`、`tool-outdated-report.yaml:report` | 15 | 実測できる完了 run が無い |
| `secret-scan.yaml` | 15 | 実測は Pull Request のみ。週次は履歴全体を走査し、完了 run が無い |

限界に触れ始めた job は実測を追い越している。数字を小突かず、測り直して式を当て直す。
再利用可能 workflow を呼ぶ job は `timeout-minutes` を持てない（キーが無効）ので、検査は除外し、
上限は呼ばれる側の job が持つ。

3つの規則が1つの検査に同居しているのは、これが3つの方針ではないからである —— 上限の無い job が
打ち切りを生み、2つのコメント規則がそれを読めるものにする。どれか1つだけ直しても Pull Request は
何も良くならない。

## 通知の起動条件

週次のスケジュールを持つ走査は、job が `failure` または `cancelled` で終わったとき `notify.yaml` を
呼ぶ。Pull Request の失敗は作者に見えている。**スケジュールの失敗は誰にも見えない** —— それが
通知の存在理由である。`cancelled` を含めるのは、timeout やランナー障害で殺された job が `failure`
ではなくそちらを報告するため。

報告のみの走査は所見があっても job が緑のままなので、failure モードは決して発火しない。それらは
検出モードで `notify.yaml` を呼び、実行者・ref・commit と所見そのものを名指しする。どちらのモードも
webhook の secret が未設定なら配信を省いて run を緑のままにする —— 作られたばかりのリポジトリが、
送れない通知で落とされることはない。

どの起動条件で検出通知を出すかは、正しい受け手が誰かから決まる。脆弱性の走査ではスケジュール実行
だけである。Pull Request では所見は既に、それを持ち込んだ作者宛のコメントに在る。**週次の所見は、
止まっているコードに対して新しく公開された勧告であり、放っておくと誰にも届かない。**

| workflow | 発火条件 | 起動 |
| --- | --- | --- |
| `trivy-fs.yaml` | 修正版のある CRITICAL / HIGH / MEDIUM | schedule |
| `tool-outdated-report.yaml` | 宣言より新しい版が上流に在る | schedule |

秘密の走査（gitleaks / Trivy secret）と `zizmor`（high）は所見で job を落とすので、failure モードが
既に届けている。`trivy-license` は誰もまだ問題だと合意していないライセンスを報告するため、
意図的に繋いでいない。

## Go のクールダウン

Go には `min-release-age` に相当するものが無い。`go get` に「新しすぎる版を拒む」と言わせる手段は
存在しない。これが道具とゲートの関係を反転させる —— pnpm は解決時に拒むので resolver がゲートだが、
ここでは**検査がゲートそのもの**であり、報告だけに留めると窓はどこにも存在しなくなる。

`go-cooldown.yaml` は Pull Request でゲートし、対象は変更が追加・更新した require だけである。
`go.mod` に既に在るものは据え置かれ、窓はこれから先に効く —— 継承した状態にすべてのブランチを
人質に取らせない。落とすのは**直接**の require だけ。間接の版は MVS が選び、直接依存自身の下限より
上に置かれうる。そこを下げることは Pull Request にできないので、落とせば手当のできない赤になる。

窓は **7日**（`scripts/go-cooldown/main.go` の `defaultWindowDays`）。Go module は install script を
持たない —— `go mod download` は何も実行しない —— ので、公開直後の版が install 時に機械を取る類は
存在しない。窓が買うのは、悪意あるコードがビルドされ出荷されるまでの時間である。

緊急の迂回は [`go-cooldown-bypass.toml`](../go-cooldown-bypass.toml) が持ち、全エントリが期限を持つ
（[ADR-0702](../../docs/adr/0702-repository-operations-substrate.md) 決定18）。期限は `go.mod` が
変わらなくても訪れる。**だからスケジュール実行が要る** —— Pull Request の起動だけでは、期限が切れる
瞬間を誰も見ない。

## ランナーの封じ込め

このディレクトリのすべての job は `step-security/harden-runner` を `egress-policy: block` で、
自分の `allowed-endpoints` とともに開始する。外向きの接続を一覧に照らし、それ以外を拒む ——
侵害された action や道具のダウンロードが、その job に用の無い宛先へ持ち出せない。

このステップは**すべての job にインラインで置かれる。これは好みではなく制約である。**
ローカルの composite action（`uses: ./.github/actions/*`）はリポジトリを checkout して初めて解決され、
harden-runner は checkout の**前**に走らなければならない —— checkout 自体が外向きの呼び出しであり、
それを守ることが目的だからである。括り出すと、塞ぐために置いた窓が開く。

**固定されていないのは一覧の出所の方である。** [`.github/egress.toml`](../egress.toml) が SSOT で、
`make egress-apply` が各 job へ書き込み、`make egress-check` がインラインのブロックとのずれで落ちる。

**job は宛先ではなく能力のクラスを宣言する。** job が何に到達するかは、その job が*何をするか*から
決まり、job 自身の素性からは決まらない —— 実行は `make` から docker、コンテナ内の `mise` へ降りるので、
必要な宛先は YAML から見えない。

| クラス | 宛先 | 当たるもの |
| --- | --- | --- |
| `base` | harden-runner 自身のエージェント、GitHub API / web / codeload、`objects` / `raw` / `release-assets.githubusercontent.com`、`*.actions.githubusercontent.com` | **すべての job に暗黙に適用される。** `classes` に書かない |
| `mise` | mise 自身の配布元と、`mise.toml` が解決するすべての backend —— aqua / GitHub releases、Go の toolchain と module proxy、npm レジストリ、`astral.sh` と PyPI、および Sigstore（mise が artifact attestation を検証する） | 道具を導入するすべての job |
| `terraform` | Terraform Registry と provider の配布元 | Terraform を実行する job（[ADR-0205](../../docs/adr/0205-module-dependency-policy.md) により外部 module は引かない） |

1つの job に固有のものは、その job の `extra` へ置く —— 走査のデータ源、通知先の `hooks.slack.com`。
2つ目の job の `extra` に現れた宛先は、クラスへ移す。

**クラスは意図的に粗い。** 細かく割れば allowlist は締まるが、分類の判断が増える。**そして間違った
クラスに置かれた job こそ、この仕組みが取り除こうとしている失敗である。** 必要より少し広いクラスも、
その外側はすべて拒む。

宛先を足すには `.github/egress.toml` を編集し（能力から導かれるならクラスへ、そうでなければ job の
`extra` へ）、`make egress-apply` を実行して生成されたブロックを commit する。インラインのブロックを
手で編集しない —— `make egress-check` が拒む。

塞がれた宛先は harden-runner の run summary に拒否された接続として現れる。job のログからは理由の
見えない失敗に当たったとき読むのはそこで、直し方はクラスか `extra` を広げることであって、
`audit` へ落とすことではない。

## 共有 composite action

再利用する composite action は [`.github/actions/`](../actions/) に置く。

| action | 役割 |
| --- | --- |
| `setup-mise` | digest を検証してから pin された mise を導入し、呼び出し側が名指しした道具を入れる |
| `upsert-pr-comment` | マーカーによる PR コメントの upsert（既存を検出 → 更新 / 作成）。`status: success` は既存を更新するが新規は作らない |
| `notify-detail` | 走査の Markdown 要約を所見の行だけへ削る。検出通知が、PR コメント向けの散文を運ばずに所見を名指しできる |

### mise の導入

mise を要する workflow は [`setup-mise`](../actions/setup-mise/action.yaml) を使う。action は pin された
mise のバイナリをリトライ付きでファイルへ落とし、版と digest でキャッシュし、実行の前に毎回 SHA256 を
検証する。**復元されたというだけでキャッシュを信用しない** —— 不一致は捨てて取り直し、2度目の
不一致で job を落とす。

道具の版は [`mise.toml`](../../mise.toml) に残る。呼び出し側が渡すのは、必要な道具の指定だけである。
`make actions-mise-pin-lint` が、action の版・digest・キャッシュキーの三点の不一致を拒む。mise 自身を
更新するときは、release の `SHASUMS256.txt` から checksum を取り、三点を同時に更新する。

### キャッシュの安全性

キャッシュはブランチ単位である —— run が復元するのは自分の ref のものと既定ブランチのものだけ ——
ので、Pull Request の run が、後の `release/*` の push が復元するキャッシュを書くことはできない。
だから通常の CI ではキャッシュを有効にしたままにする。

汚染が可能になるのは、**信用できない PR のコードが信用された scope で実行され、その間にキャッシュが
保存されるとき**である。`pull_request_target` と `workflow_run` は base ref の scope で走るので、そこで
PR の head を checkout する workflow は、特権のある run が読む場所にキャッシュを残す。この2つを
組み合わせない。信用できないコードを扱う workflow はキャッシュを切る。

### PR コメントのフェンス

**攻撃者が内容を左右できるテキストを囲むフェンスは、そのテキストから長さを決める。固定しない。**
`upsert-pr-comment` は本文中で最長のバッククォート連続より1つ長いフェンスを計算するが、それは
`details-summary` を渡した経路でのみである。その入力が無ければ本文は素通しになる —— 呼び出し側の
いくつかは、描画されることを意図した Markdown（見出し・表・自前の `<details>`）を書くからである。

素通しの経路で本文の一部を自分でフェンスする呼び出し側は、そのフェンスを自分で所有する。固定の
3連バッククォートは、ソース行を再現する本文なら何でも閉じられる —— Pull Request の作者が書いた
ファイルを引用する linter は、3連バッククォートをブロックの内側へ落とし、以降が bot の名前で
生きた Markdown として描画される。

**同じ規則がインラインのコードスパンにも掛かる。**長さ1のフェンスに過ぎないためである。path が
効く場合で、保持できないバイトは NUL と `/` だけなので、バッククォート・`@`・リンク記法はすべて
使える。呼び出し側は、リポジトリ由来の path をスパンにも裸の Markdown にも置かず、一覧そのものから
取った長さで一覧全体をフェンスする。

呼び出し側は本文を action の `max-length` 未満に保つ必要もある。**これはフェンスの付与より前に
適用される** —— そこで切られた本文は閉じフェンスを失う。

`make pr-comment-fence-lint` は機械的に判定できる部分を見るが、本文が攻撃者由来かどうかは判定できない。
**規則の方が linter より広い。**

## 補足

- **PR へコメントする job に secret を渡さない。** secret のマスクが覆うのは、ランナーが job の出力を
  ログへ取り込む経路だけである。ステップが `tee` でファイルへ書いたバイトはそこを通らず、
  `upsert-pr-comment` は本文をまさにそのファイルから読む。ログでマスクされて見えた値が、公開された
  コメントへ生のまま着地する。`make pr-comment-secret-lint` が、この action を使う job へ
  `GITHUB_TOKEN` 以外が渡されたときに落ちる。検査は直接の `secrets` 参照だけを読むので、
  `needs.<job>.outputs` 経由なら迂回できる —— **保つのは linter ではなく規則である。**
- **`upsert-pr-comment` は自分のコメントを、bot の作者と先頭のマーカーで突き合わせる。** 公開リポジトリ
  では誰でもマーカーを持つコメントを投稿でき、ここの workflow はすべて同じ bot でコメントするので、
  マーカーも作者も単独ではコメントを識別しない。action が書いた本文は必ずマーカーで始まり、
  仕込まれたものはそうならない。`github-token` は bot として投稿するトークンでなければならない ——
  PAT はユーザとして投稿し、その コメントを action は決して見つけられず、毎回新しいコメントを残す。
- **`run:` の本文へ補間された式はコードであり、それを見るのは zizmor だけである。** `${{ }}` は
  シェルが何かを解釈する前に置換されるので、引用符の無い `github.event.*` はコマンドを終わらせて
  攻撃者のものを始める。shellcheck 系のゲートはこれに構造的に盲目である（理由は
  [`scripts/README.md`](../../scripts/README.md) の `actions-shellcheck` の行）。式を `env:` へ束ね、
  シェルでは `"$VAR"` として読む —— 値はそこへデータとして届く。
- zizmor の audit への例外は `.github/zizmor.yml` に置く。`ignore` はファイル単位であり、同じ audit に
  当たる**新しい** workflow は落ちる。エントリは恒久の allowlist として残さず、元の所見が直った時点で
  消す（[ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定13）。
- **GitHub は 60 日 commit の無いスケジュール workflow を自動で無効にし、それを黙って行う。**
  生かし続けることはこのテンプレートの範囲外である —— keepalive の job は用意しない。静かになった
  リポジトリは、Actions タブから再有効化することになると思っておくこと。
- テンプレートから作られたリポジトリは、すべての workflow が `disabled_fork` 状態で始まり、何も
  走らない。`make enable-workflows` が列挙して有効化する。冪等で、再実行してよい。
- `trivy-fs.yaml` は check を落とさない。所見は修正版の有無にかかわらず code scanning と PR コメントへ
  書かれ、止める判定はリリース昇格のゲートが持つ。**昇格が既知の脆弱性を黙って出荷することも、
  通常の Pull Request が自分の持ち込んでいない脆弱性に人質を取られることも、どちらも起こさない。**
