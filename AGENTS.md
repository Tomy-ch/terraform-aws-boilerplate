# エージェント向け文書

このリポジトリで作業する **AI コーディングエージェント**（Claude Code / Codex / Copilot / Gemini 等）のためのリポジトリ規約。

このリポジトリは、限定された AWS アーキテクチャとユースケースを、安全かつ再現可能な形で Terraform へエンコードした参照実装である（[0002](docs/adr/0002-architecture-principles.md)）。汎用の Terraform module collection ではなく、自由度より保証可能性を優先する。[README.md](README.md) がこのリポジトリは何かを述べ、本書はその中でどう作業するかを述べる。**アーキテクチャと規約をここに再掲しない** —— どの文書が何を所有するかは *正典となる文書* の表が言い、その割当は [0015](docs/adr/0015-documentation-ownership-and-language.md) 決定2 が決めている。

すべての作業に3つの制約が掛かる。

1. **決定的な検査が在るなら、それがあなたの判断より上位に立つ。** `terraform validate`、TFLint、Trivy、`terraform test`、Policy Test、CI の実行。結論したことではなく、それが何と言ったかを報告し、行や件数を落とすフィルタ越しに報告しない。検査を持たない安全性の主張は、保証として扱わない（[0011](docs/adr/0011-least-privilege-policy.md) 決定19、[0012](docs/adr/0012-testing-policy.md) 決定3）。
2. **アーキテクチャと方針の決定は、人のゲートを残す。** 決定と選択肢を差し出すのであって、選ばない。どこがそれに当たるかは *手を止めてよい場所* が閉じた一覧として持ち、その外では決めて、決定を Pull Request へ記録する。
3. **インフラは AI に依存しない。** `terraform validate` / `terraform test` / `terraform plan` / `terraform apply` と CI の必須検査は、エージェントが1つも居ない状態で通らなければならない。必須検査の経路へ自らを差し込む道具は、ここに置かない。

## 現在の配線状態

**このリポジトリはまだ実装を持たない。** 存在するのは `docs/adr/` と、リポジトリ直下の一部の設定ファイルだけである。次はいずれも未配線とする。

- `modules/` / `examples/` —— 実装とユースケースが無い
- CI（`.github/`）—— [0014](docs/adr/0014-change-delivery-path.md) が定める plan / apply の経路が存在しない
- 道具の実行 —— `mise.toml` は [0013](docs/adr/0013-development-tooling-composition.md) 決定31 と [0019](docs/adr/0019-secret-leak-detection.md) 決定1 の初期導入分を pin したが、各道具の設定ファイル（`.tflint.hcl` / Trivy の scanner 指定）と実行経路は未作成である
- `Makefile` —— include 先（`.makefiles/`）が存在せず、実行すれば失敗する
- `.lefthook.yaml` —— 参照するコマンドが存在しない
- 分岐のパターンの単一宣言（[0023](docs/adr/0023-branch-protection-and-required-checks.md) 決定1）—— まだ作成しておらず、保護対象のパターンは保護設定の宣言側にだけ在る
- 保護設定の突合（同 決定4）—— 適用はできるが、実態との突合が未実装

**未配線の道具について「実行した」と報告しない。** 配線され次第この節から外し、この節が空になったとき節ごと削除する。

## 指示の優先順位

競合したときは上位が勝つ。

1. **AGENTS.md**（本書）—— エージェントの作業規約
2. **`docs/adr/*.md`** —— repository-wide の Accepted ADR
3. **`modules/<use-case>/docs/adr/*.md`** —— 当該ユースケースの Accepted ADR。root ADR を上書きしない（[0001](docs/adr/0001-adr-process-and-placement.md) 決定13-15）
4. ユーザーの指示

## 何を推奨するか

**推奨**についての節であり、変更してよい範囲の話ではない。それは上の優先順位と *変更してよい範囲* が決める。

- **新しいリポジトリが受け取るスナップショットに対して**選択肢を比べる。git log を読まない人にとって一貫して見えるかを基準にする。
- **品質と一貫性は、それを達成する費用に優先する。** 教えている順序と矛盾する採番、ここ以外では守られている規約、改名が手間だという理由だけで生き延びた名前 —— 直すことを推奨してよい。「すでに動いている」は根拠として軽い。
- **推奨には費用を添える** —— 触るファイル、誰にとって何が壊れるか、何を作り直すか。方向を保ったまま範囲だけを断れるようにする。
- **権威を持つのは、公式の定義と、アーキテクチャが導く形だけである。** 何を「公式」と呼ぶかは [0009](docs/adr/0009-official-implementations-as-reference.md) 決定3 が、その優先順位は [0010](docs/adr/0010-default-value-policy.md) 決定3 が持つ。どちらでも言い表せない推奨は、推奨の服を着た好みである。
- **特定の利用者の事情を、残るものへ焼き付けない。** ただしこれは上の規則に従属する —— 公式推奨が既に決めている箇所へ置くつまみは、標準からの逸脱であり、[0010](docs/adr/0010-default-value-policy.md) 決定11 の記録を要するか、消える必要がある。

## 正典となる文書

**規則はここに再掲しない。** 変更が触れる領域を所有する索引を開き、そこが名指しする項目を読む。索引を機能名で検索するだけでは足りない —— 文書は、それが所有する関心事の名前で置かれているのであって、探し物の名前では置かれていない。

| 必要なもの | 読む先 |
| --- | --- |
| 受理されたすべての決定、1行ずつ | [`docs/adr/README.md`](docs/adr/README.md) —— ADR の一覧が存在する唯一の場所 |
| ユースケースの責務境界・supported / unsupported・公開契約 | `modules/<use-case>/README.md`（[0003](docs/adr/0003-use-case-centric-scope.md) 決定2、[0015](docs/adr/0015-documentation-ownership-and-language.md) 決定2）—— 未作成 |
| そのユースケースに固有の設計判断 | `modules/<use-case>/docs/adr/`（[0001](docs/adr/0001-adr-process-and-placement.md) 決定5）—— 未作成 |
| **AWS Provider の schema は version で変わる** | 記憶ではなく Terraform MCP Server → Terraform Registry → Provider Documentation の順に確認する（[0013](docs/adr/0013-development-tooling-composition.md) 決定21）。記憶にある argument 名は、確認するまで仮説である |
| どのテスト層が何を検出するか | [0012](docs/adr/0012-testing-policy.md) 決定4。それを担う道具は [0013](docs/adr/0013-development-tooling-composition.md) 決定4 |
| どの文書が何を所有し、開発の経緯はどこへ行くか | [0015](docs/adr/0015-documentation-ownership-and-language.md) 決定2・3 |

## 作業手順

実装に着手する前に。

1. **触ろうとしているディレクトリを所有する `README.md` を読む。** 無ければ最も近い祖先まで遡る。そこが述べる責務境界と supported / unsupported は、ゲートが検査できる範囲より広い。
2. **上の索引を開き、変更が触れる決定を所有する項目を読む。** 何が決まっているかは `docs/adr/README.md` が、その適用範囲は当該ユースケースの `README.md` が持つ。
3. **既存の実装が既に覆っていないかを確認する。** 同じユースケース内を先に探し、新規作成より既存の編集を選ぶ。重複のみを理由に共通化しないこと（[0008](docs/adr/0008-module-dependency-policy.md) 決定11）と、重複に気づかず二重に書くことは別である。
4. **契約をコードより先に動かす。** 公開 `variable` / `output` の変更は公開契約の変更であり（[0004](docs/adr/0004-public-interface-policy.md)、[0005](docs/adr/0005-internal-topology-as-implementation-detail.md) 決定1）、Contract Test の更新を伴う。内部 resource の address を変える変更は、同じ変更の中で `moved` block を提供する（[0005](docs/adr/0005-internal-topology-as-implementation-detail.md) 決定6）。
5. **変更の種類に対応するテスト層を先に更新する**（[0013](docs/adr/0013-development-tooling-composition.md) 決定29）。どの層かは [0012](docs/adr/0012-testing-policy.md) 決定4 と [0013](docs/adr/0013-development-tooling-composition.md) 決定9 が決める。

手順1と2は省略できない。**読まなかった規則も、その変更を拘束する。**

## 禁止事項をここに列挙しない

機械的に判定できるものはゲートが捕まえる —— `terraform fmt` / `terraform validate`、TFLint、Trivy、Policy Test、Contract Test（[0013](docs/adr/0013-development-tooling-composition.md) 決定4）。残りは、触っているユースケースの `README.md` と ADR が述べる。

**ここに禁止が書かれていないことは、本書についての情報であって、その禁止についての情報ではない。**

**ゲートを持たない規則が3つある。** いずれもコードの形からは判定できない。自分で保つこと。

- **公式実装の確認**（[0009](docs/adr/0009-official-implementations-as-reference.md) 決定2・7）—— 実装着手時に最新の公式実装を確認し、採用した知見と、採用しなかった知見およびその理由を記録する。検査できるのは記録の存在までであり、確認したかどうかは検査できない。
- **要求の3択分類**（[0003](docs/adr/0003-use-case-centric-scope.md) 決定5）—— 契約への一般化 / 新規ユースケースへの分離 / 責務外。判断を保留したまま暫定の設定口を作らない（[0007](docs/adr/0007-no-generic-escape-hatch.md) 決定4）。責務外と判断したなら、その判断を unsupported 一覧へ記録する（同 決定7）。
- **昇格の判断**（[0001](docs/adr/0001-adr-process-and-placement.md) 決定16-18）—— 同じ AWS resource を使っていることは、use-case ADR を root へ昇格させる理由にならない。

## 手を止めてよい場所

**判断を人へ返してよい場所は一覧であって、判断ではない。** 一覧は閉じている。その外では、決めて、何を選び何を選ばなかったかを Pull Request の本文へ書く。

### 停止点

| 停止 | 所有 |
| --- | --- |
| 公式推奨から外れる構成の採用 | [0010](docs/adr/0010-default-value-policy.md) 決定11 —— 外れる対象・理由・影響・代替案・見直し条件を記録する |
| 新しいユースケースの追加、および要求を責務外と判断すること | [0003](docs/adr/0003-use-case-centric-scope.md) 決定5・7 |
| experimental なユースケースの stable への昇格、および削除 | [0003](docs/adr/0003-use-case-centric-scope.md) 決定14 |
| ADR の supersede | [0001](docs/adr/0001-adr-process-and-placement.md) 決定2・3 |
| 検出結果の新規の抑止 | [0013](docs/adr/0013-development-tooling-composition.md) 決定13 —— 理由を書けないものは抑止せず、値そのものを直す |
| amend したうえで既存の Pull Request ブランチへ push すること | *Git 規約* —— そこにある文言をそのまま使う |

### トリップワイヤ

**判断が何を言おうと作業を止める。** 続行そのものが誤りである。

1. **次の一手が外部 Terraform module への依存を必要とする。** [0008](docs/adr/0008-module-dependency-policy.md) は決定5 で例外条項を持たない。fork も vendoring も同じ決定に当たる（同 決定4）。
2. **次の一手が Generic Escape Hatch を作る。** [0007](docs/adr/0007-no-generic-escape-hatch.md) も決定8 で例外条項を持たない。名前が `extra_*` でなくとも、受け取る値の意味が AWS Provider または AWS API の表現に依存していれば該当する（同 決定2）。
3. **次の一手が安全側の設定を弱める。** テストを通すため、開発を容易にするためであっても（[0011](docs/adr/0011-least-privilege-policy.md) 決定2、[0013](docs/adr/0013-development-tooling-composition.md) 決定14）。
4. **次の一手が生成物を編集する。** terraform-docs の生成区間、`.terraform.lock.hcl`、tfautomv の出力（[0015](docs/adr/0015-documentation-ownership-and-language.md) 決定4）。
5. **権威を主張する2つの情報源が食い違っている。** 気づくことが仕事であり、解決することは仕事ではない。
6. **文書が、検査手段を持たない主張を述べようとしている**（[0012](docs/adr/0012-testing-policy.md) 決定3、[0015](docs/adr/0015-documentation-ownership-and-language.md) 決定12）。
7. **次の一手が履歴を書き換えるか、保護ブランチへ触れる** —— force push、rebase、amend してからの push、保護ブランチの checkout。
8. **次の一手が実環境へ直接 apply する**（[0014](docs/adr/0014-change-delivery-path.md) 決定3・5・13）。bootstrap の初回 apply も含む —— [0017](docs/adr/0017-bootstrap-and-ci-authentication.md) 決定9 の例外は人が実行するものであり、エージェントがこのワイヤを越える根拠にならない。

### それ以外

決めて、**決定を Pull Request の本文へ書く** —— 何を選び、何を選ばなかったか。記録された決定は読者が覆せる。黙って下された決定は、再導出しない限り見つからない。

## 変更してよい範囲

既定では次の範囲に限る。それ以外はユーザーの明示的な指示を要する。

### 変更してよい

- `modules/` 配下
- `examples/` 配下
- `docs/` 配下（Accepted ADR の本文を除く）

### 指示なしに触らない

- リポジトリ直下の設定ファイル
- `.github/`（workflow / 設定 / issue・Pull Request テンプレート）
- `LICENSE`
- Accepted ADR の本文

### エージェント設定ファイルの保護

エージェントの設定は、そのエージェント自身であっても指示なしに触らない —— `.claude/` / `.agents/` / `.cursor/` / `.github/copilot-instructions.md` / `.gemini/` / `GEMINI.md` 等。`AGENTS.md` はすべてのエージェントが共有する保護された文書である。

### v1.0.0 までの暫定

> **暫定の節である。v1.0.0 で削除する。**

v1.0.0 より前では次を解除する。

- `AGENTS.md`、リポジトリ直下の設定ファイル、`.github/` を、変更ごとの承認なしに編集してよい
- `LICENSE` は解除しない

**ADR の運用は解除しない。** [0001](docs/adr/0001-adr-process-and-placement.md) 決定2 の immutable と supersede による変更、決定4 の番号の非再利用は、v1.0.0 より前でも適用する。Accepted ADR の本文を上書きしない。誤字と参照リンクの修正はこの限りではない（同 決定2）。

## 道具の導入

`install` の面すべてを対象とする —— パッケージマネージャ、toolchain マネージャ、IDE / エージェント連携、プラグイン、拡張。

- **自分の判断で install しない。** 道具を*使いたい*ことは、道具を*入れる*指示ではない。利用できない道具は、報告すべき所見である。エージェント連携はとくに、プロジェクト規模の指示ファイル（`AGENTS.md`、`.cursor/`、`.gemini/`、git hook）を書き込む —— 「install」が、作業している規約そのものへの編集になる。
- **先に既存の配線を確認する。** 道具の version は単一の manifest で pin される（[0013](docs/adr/0013-development-tooling-composition.md) 決定19）。何を確認したかを述べてから、無いと結論する。
- **依存の追加はより厳しい問いである。** 外部 Terraform module は [0008](docs/adr/0008-module-dependency-policy.md) が禁止しており、停止点ではなくトリップワイヤである。

## 使うコマンド

**道具の割当は [0013](docs/adr/0013-development-tooling-composition.md) 決定4 が持つ。** ここに書くのは、そこから導かれない事項だけである。

- **道具はそのまま実行する。** version manager のサブコマンドで包まない（[0013](docs/adr/0013-development-tooling-composition.md) 決定20）。bare で解決しないなら、直すのは `PATH` であって wrapper ではない。
- **Terraform コードを変更したら、最低限 `terraform fmt` / `terraform validate` / TFLint / `terraform test` を実行する**（[0013](docs/adr/0013-development-tooling-composition.md) 決定28）。security / policy の道具が配線されていれば、それらも実行する。
- **実 AWS の優先順位は mock → plan → 実 provider → 実 AWS**（[0013](docs/adr/0013-development-tooling-composition.md) 決定30）。Unit Test で確認できることのために resource を作らない。作る場合は、破棄の失敗を検出できる構成で行う（[0012](docs/adr/0012-testing-policy.md) 決定14-16）。
- **CI の実行結果は、失敗したステップのログから読む。** 実行全体のログを引くと、失敗と無関係な出力が大量に混じる。全体を引くときは、何で絞り込めなかったかを述べる。**ゲートの判定を、行や件数を落とすフィルタ越しに報告しない。**
- **`terraform console` で評価する。** 複雑な expression を推測だけで実装しない（[0013](docs/adr/0013-development-tooling-composition.md) 決定27）。

## Git 規約

[0014](docs/adr/0014-change-delivery-path.md) が変更の経路を、[0023](docs/adr/0023-branch-protection-and-required-checks.md) が分岐のパターンと保護設定を持つ。ここに書くのは操作の規約だけである。

- **force push、rebase、amend、保護ブランチの checkout を行わない。** 修正は**新しい commit** として積む。同じ禁止はサーバ側の宣言としても置かれるが（[0023](docs/adr/0023-branch-protection-and-required-checks.md) 決定26）、片方が在ることを理由にもう片方を省かない。
- amend したうえで既存の Pull Request ブランチへ push する場合、**push の前に確認する**。文言はこれを使う: 「変更はローカルにコミット済みです。これらの変更をプルリクエストにプッシュしますか？」
- **commit message に課す検査は `commitlint.config.js` が持ち、そこに「なぜその3つだけか」も書いてある。** ここへ再掲しない。**形式を細かく規定しない** —— 守られなくても何も壊れない規定は置かない。
- **ブランチ名と保護対象のパターンは単一の宣言が持つ**（[0023](docs/adr/0023-branch-protection-and-required-checks.md) 決定1-3）。同じパターンを2箇所へ書かない。宣言はまだ作成していない（*現在の配線状態*）。
- **Pull Request の title と body は日本語**（[0015](docs/adr/0015-documentation-ownership-and-language.md) 決定9）。body には *手を止めてよい場所* の「それ以外」で下した決定を書く。
- **merge の可否は、実環境への適用の可否と同義である**（[0014](docs/adr/0014-change-delivery-path.md) 決定10）。検証が失敗している Pull Request を merge しない。

## 言語

**割当は [0015](docs/adr/0015-documentation-ownership-and-language.md) 決定6-11 が持つ。** 内部の処理 —— コードの解析、設計の検討、道具の呼び出し、途中の思考 —— は英語でよい。**書き戻すものは日本語である** —— ユーザーへの応答、コードコメント、`variable` / `output` の `description`、commit message、Pull Request、ドキュメント。ユーザーが明示的に別の言語を指示した場合は、その指示が立っている限りそちらに従う。

## 応答の規律

**書き戻すものについての規律であり、してよいことを緩めない。** 簡潔さは、本書が要求する確認を省く理由にならない。

- **答えを先に。** 結果を述べ、自明でないところにだけ理由を添える。前置き、要求の言い換え、末尾の要約を置かない。
- **読んでいない事実を断定しない。** argument 名、flag、version、path、resource type、commit の SHA —— 先にコードか文書を開く。「確認していない」は答えになるが、もっともらしい捏造は答えにならない。
- **求められた範囲と、それを塞いでいるものを報告する。** 隣接して気づいたことは1行か issue にし、頼まれていない節にしない。
- **決定的な検査は、それ自身が報告したとおりに報告する。**
- **生成物に装飾的な Unicode を置かない。** コード、設定、commit message はハイフンと直線引用符を使う。人が読む散文は通常の約物でよい。

## 保護された文書

次は人の判断を経てから変更する。提案を差し出し、承認を得てから編集する。

- `AGENTS.md`（本書）
- Accepted ADR の本文（`docs/adr/0001-*.md` 以降で Status が Accepted のもの）
- `LICENSE`

> v1.0.0 までは `AGENTS.md` の承認要件を解除する —— *v1.0.0 までの暫定* を見よ。ADR の immutable 運用は解除しない。
