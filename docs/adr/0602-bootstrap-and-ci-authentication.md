# ADR-0602: bootstrapをCLIとimportで立ち上げ、CI認証をOIDCとする

- Status: Accepted
- Date: 2026-10-21
- Scope: repository-wide
- Related: ADR-0102, ADR-0202, ADR-0301, ADR-0501, ADR-0601, ADR-0402, ADR-0603, ADR-0604

## Context

ADR-0601（変更経路をGitに限定し、applyをmerge後のCI/CDで行う）は変更経路を Git に限定し、apply を merge 後の CI/CD に置いた。しかしこの経路そのものが、次の2つを前提にしている。

- Terraform state を保存する backend が存在すること
- CI が AWS を操作する権限を持つこと

いずれも本リポジトリが作る対象であり、作る前には経路が成立しない。state backend については純粋な循環（backend が無ければ state を保存できず、state が無ければ backend を Terraform で作れない）が存在する。

CI の権限については循環の性質が異なる。backend さえあれば、人のローカル権限から Terraform で作成できる。つまり CLI で作らなければならない理由は無い。

一方で、CI の信頼条件は本構成で最も危険な単一のオブジェクトである。GitHub Actions の OIDC claim による限定を誤ると、当該リポジトリ以外からも role を assume できる。この形の誤りは、権限の広さではなく信頼の広さとして現れるため、権限の最小化では防げない。したがって、この構成が**実在する前に検査を通過している**ことが、他のどの構成よりも重要になる。

また、AWS CLI で作成した構成をそのまま Terraform 管理外に置くと、その構成に対して ADR-0601 決定1（Git が Desired State）が成立しない。state backend と CI の権限は、drift 検出の対象から外してよい構成ではない。

### 契機・回復・裏打ちの範囲

信頼条件の形、例外の契機、権限による裏打ちの3点は、素朴な形では他の決定または前提と両立しない。

- **信頼条件を ref と event で限定する形（plan は pull_request、apply は保護ブランチの ref）は、決定15（ワイルドカードの禁止）と両立しない。**
  保護ブランチはリリース線の version を名前に含むため（ADR-0603 決定1）、ref で限定するには
  リリース線ごとに信頼条件を書き換えるか、ワイルドカードを使うしかない。また定期的な plan
  （drift 検出）は `pull_request` の契機を持たないため、plan の role を assume できない。
- **例外を初回の1回限りとする形は、信頼条件の誤りから回復する経路を持たない。** CI role の変更は ADR-0601 の
  経路で行う（決定9 の「以後」）。誤った信頼条件が merge されると、以後どの CI role も assume できず、
  ADR-0601 の経路そのものが失われる。この状態からの回復は初回の契機に当たらない。
- **権限による裏打ちは、すべての主体には及ばない。** bucket policy や key policy による拒否は
  IAM principal に効くが、AWS Organizations の management account の root には SCP が効かず、
  root は bucket policy を削除できる。裏打ちの範囲を決定として書かなければ、保証の外にある主体が見えない。

## Decision

### 対象

1. bootstrap を「CI が Terraform を実行できるようになるまでに必要な構成」と定義し、対象を次の2つに限る。

   - Terraform の state backend
   - GitHub Actions から AWS を操作するための OIDC provider と IAM role

2. bootstrap の構成物も use-case として `usecases/` 配下へ置く（ADR-0102）。bootstrap であることを理由に、型付き契約（ADR-0203）・Generic Escape Hatch の禁止（ADR-0204）・最小権限（ADR-0301）・テスト（ADR-0401）のいずれも免除しない。
3. bootstrap 層であることは、適用の契機が異なることのみを意味する。公開契約とテストの要件は他のユースケースと同一とする。

### 順序

4. 次の順序で立ち上げる。

   1. AWS CLI で state backend の実体を作成する
   2. 同じ構成を表す Terraform を書き、`import` block で取り込む。`terraform plan` が変更なしを示すまで Terraform 側を修正する
   3. OIDC provider と IAM role の use-case を書く
   4. 人のローカル権限で apply する（決定9・10 の例外。回復の契機でも同じ手順を踏む）
   5. 以後、CI が OIDC で role を assume して実行する

5. 段1 が CLI であるのは、state backend が存在しない時点では Terraform が state を保存できないためである。**この理由が成立しない構成物を CLI で作成しない。** OIDC provider と IAM role は段2 の完了後に Terraform で作成できるため、CLI の対象としない。
6. 段2 で `terraform import` コマンドを用いない。`import` block を用い、state を書く前に plan で突合する。config と実物の不一致を、state を書いたあとの plan で発見する経路を採らない。
7. CLI の手順と `import` block を同一の変更で提供する。片方のみを変更しない。
8. 段2 の完了条件を「`terraform plan` が変更なしを示すこと」とする。この条件の充足をもって、CLI が作成した実体と Terraform の宣言が一致したとみなす。

### ADR-0601 決定2-6 の例外

9. 段4 のローカルからの apply を、ADR-0601 決定2-6 の例外とする。外れるのは、Review と Merge の後に apply
   する順序（決定2）、Pull Request の時点で apply しないこと（決定3）、merge 後に CI/CD が apply すること
   （決定4）、人がローカル環境から本番環境へ直接 apply しないこと（決定5）、apply 対象が merge された
   commit と一致すること（決定6）である。例外の範囲を次に限る。

   - 対象: 本 ADR 決定1 が定める bootstrap の構成物のみ
   - 契機: 次のいずれか
     - 当該 AWS アカウントで初めて CI 実行基盤を用意するとき、1回限り。**既に CI 実行基盤を持つ
       別のアカウントの CI から当該アカウントの bootstrap の構成物を作れる場合は、この契機に当たらない。**
       その経路は cross-account access であり、ADR-0301 決定17 に従って明示的なユースケースとして設計する
     - 当該 AWS アカウントのいずれの CI role も assume できなくなった状態から回復するとき
   - 以後: 同じ構成の変更は ADR-0601 の経路で行う

   回復には、決定11 の拒否を解除できる主体の資格情報が要る。その資格情報の発行と管理は、本リポジトリの
   責務外とする（ADR-0301 決定18）。本リポジトリはその手順を提供せず、当該 use-case の unsupported
   一覧へ記載する。

10. 例外の apply は、Pull Request のブランチ上の commit から行い、その Pull Request を apply の後に merge する。
    いずれの契機でも、apply の前は CI が role を assume できず、plan が required check を満たさないため
    （ADR-0603 決定12・14）、merge を先に行えない。次を条件とする。

    - apply の前に、その commit に対するローカルの plan の結果を Pull Request に記録する。plan 以外の
      required check がその commit で成功しており、保護設定が承認者を要求している場合はその承認を得ている
    - 例外を実行した記録（実行者、日時、対象アカウント、適用した commit）を、その Pull Request の本文に残す。
      記録を残さない実行を認めない
    - merge の前に、Pull Request の head に対する CI の plan が変更なしを示すこと。apply の後に commit を
      足した場合も、この条件で判定する。apply した構成と merge される構成の一致を、この plan が担う
11. bootstrap の完了後、state backend への書き込みを CI の role に限定する。bootstrap 実行者の書き込み権限を残さない。ADR-0601 決定5 を、手順ではなく権限として裏打ちする。

    裏打ちの範囲を次に限る。

    - IAM principal に対しては、state backend の resource-based policy（bucket policy、key policy）で拒否する
    - AWS Organizations の member account では、加えて SCP と root 資格情報の中央管理で、root を含めて拒否する
    - **management account の root は本決定の保証の外とする。** SCP が効かず、resource-based policy を
      削除できるためである。この判断を当該 use-case の unsupported 一覧へ記載する

### CI 認証

12. CI から AWS への認証を、OIDC による role の assume とする。長期のアクセスキーを CI の secret として保持しない。
13. plan を行う role と apply を行う role を分ける。plan の role に変更系の権限を与えない。plan の role は
    state の読み取りと復号を持つ。plan に必要だからである。したがって plan の role を assume できる主体は
    state を読める。state に置く秘密の扱いは、当該 use-case の ADR が決定する。

    apply を行う role を、さらに2つに分ける。bootstrap の構成物（決定1）を変更する apply-identity の
    role と、それ以外を変更する apply の role である。apply の role に bootstrap の構成物を変更する権限を
    与えない。apply-identity の role は、本 ADR が apply の role に課す条件（決定14-16）にすべて従い、
    apply の role とは別の environment を持つ。
14. 各 role の信頼条件を、GitHub Actions の OIDC claim のうち **environment** によって限定する。ref と
    event による限定（`ref:` / `pull_request` 形式の `sub`）を用いない。ref の限定は environment の
    deployment branch policy が、event の限定は workflow の起動条件が担う。

    - 信頼条件は `aud` と、`sub` が含むリポジトリと environment 名を、当該リポジトリの固定値との完全一致で
      限定する。加えて、リポジトリと所有者の変更されない識別子（ID）を表す claim でも限定する。用いる
      claim と AWS の条件キーは、実装の前に一次資料で確かめ、当該 use-case の ADR に記録する（ADR-0501 決定21）
    - role ごとに environment を分ける。plan の role の environment は保護ブランチと Pull Request を許し、
      apply の role の environment は保護ブランチだけを許す。deployment branch policy が Pull Request の
      ref をどう扱うかは、実装の前に一次資料で確かめ、当該 use-case の ADR に記録する
    - fork からの Pull Request では、plan の job を role の assume の前に失敗させる。skip にしない
      （ADR-0603 決定17 により skip は成功として数えられる）。この限定は deployment branch policy に頼らず、
      job の条件で行う
    - environment の宣言（名前、deployment branch policy、管理者による保護の迂回の可否）を、保護設定の
      宣言（ADR-0603 決定4）の一部としてリポジトリへ置き、適用と突合の対象とする（同 決定4-7）
    - deployment branch policy の保護ブランチのパターンは、分岐のパターンの単一宣言（ADR-0603 決定1）から
      生成する。environment の宣言にパターンを直接書かない（同 決定3）
    - environment の管理者による保護の迂回を無効とする。これは ADR-0603 決定22 が禁じるブランチの保護規則の
      bypass とは別の設定である
    - apply の role を assume する job の起動条件を、保護ブランチへの push と手動の起動に限る。手動の起動は
      ref や commit を入力に取らず、起動したブランチの最新の commit だけを対象とする。environment は同じ ref
      から起動された event を区別しないためである

15. 信頼条件にワイルドカードを用いない。この禁止を、当該 use-case の Policy Test の invariant として実装する（ADR-0402 決定6）。
16. apply の role が持つ権限の与え方（Action の列挙か permissions boundary か）は、当該 use-case の ADR が決定する。ADR-0301 決定8（Action wildcard の禁止）はこの role にも適用されるため、`*` による解決を採らない。

### 本 ADR で決定しない事項

17. state のロック方式は本 ADR の対象外とし、ADR-0604 が決める。ADR-0601 決定16 が要求するのは排他制御が成立することのみである。
18. state の分割単位は本 ADR の対象外とし、ADR-0604 が決める。環境の表現方法は ADR-0102 決定11-12 が決める。

## 検討した代替案

### 案A: bootstrap の構成物を Terraform 管理外に置く

手順は最も単純で、循環も発生しない。ただし state backend と CI の権限に対して ADR-0601 決定1 が成立せず、drift 検出の対象外になる。最も権限の強い構成が、唯一検査されない構成になる。

### 案B: OIDC provider と IAM role も CLI で作成し、import する

bootstrap の手段が CLI に統一される。ただし信頼条件の JSON が、実在する前に一度も検査を通らない。CLI が流し込む JSON を Conftest で検査することは可能だが、その JSON の構造は plan の出力とは異なるため、同一の invariant に対して Policy を2本持つことになる。ADR-0402 決定3 が Trivy との間で避けている検査の重複を、Policy の内側に作ることになる。

### 案C: 長期のアクセスキーを CI の secret として保持する

配線が単純で、OIDC provider の作成が不要になる。ただし失効期限を持たない資格情報がリポジトリの設定に存在し続ける。漏洩時の影響範囲が、契機や ref によらず全操作に及ぶ。

### 案D: 信頼条件を ref と event で限定する

GitHub 側に environment の宣言を持たずに済む。ただし保護ブランチの名前がリリース線の version を
含むため、決定15 のもとではリリース線を切るたびに信頼条件の変更が要る。定期的な plan は
`pull_request` の契機を持たず、plan の role を assume するには第3の形の信頼条件が要る。
event を区別できないことは採用案と同じであり、どちらも workflow の起動条件で補う（決定14）。

### 案E: 回復をローカル apply の例外に含めない

例外が1回限りに保たれる。ただし誤った信頼条件が merge された時点で、ADR-0601 の経路に戻る手段が
ローカル apply の例外の範囲内に存在しなくなる。回復は記録の義務（決定10）を負わない操作として、範囲の外で行われる。

### 評価

| 評価軸 | 案A: 管理外 | 案B: 全て CLI + import | 案C: 長期キー | 案D: ref で限定 | 案E: 回復を例外にしない | 採用案 |
| --- | --- | --- | --- | --- | --- | --- |
| Security | 低（最強の構成が未検査） | 中（信頼条件が未検査で実在する） | 低（無期限の資格情報） | 中（誤った信頼条件を入れる機会がリリース線ごとに生じる） | 低（ADR-0601 の経路を失う） | 高 |
| Testability | 低 | 中（Policy が二重になる） | 中 | 中（期待値がリリース線ごとに変わる） | 中 | 高 |
| Contract Clarity | 低 | 中 | 中 | 低（決定15 と両立しない） | 中 | 高 |
| Scope Control | 中 | 中 | 高 | 中 | 高 | 中（GitHub 側の宣言が増える） |
| Reproducibility | 低（手順のみ） | 中 | 中 | 中 | 低（範囲外の操作） | 高（plan による突合） |
| 手順の単純さ | 高 | 中 | 高 | 中 | 高 | 低（意図的） |

## 意図的に捨てるもの

- bootstrap を単一の手段で完結させること
- 人のローカルからの実行を一度も行わないこと
- CI の資格情報を設定画面のみで完結させること
- ローカル apply の例外が、アカウントごとに1回で終わること
- 信頼条件を AWS 側の宣言だけで読めること。契機の限定は GitHub の environment の宣言と合わせて読む

## 保証範囲

- 保証すること: bootstrap の構成物が Terraform の管理下にあり、以後の変更が ADR-0601 の経路に乗ること。CI が長期の資格情報を保持しないこと。apply の role が bootstrap の構成物を変更できないこと。CI role を assume できるのが当該リポジトリの、宣言された environment の job に限られること。その environment の deployment branch policy が宣言と一致すること（突合の検査が通っている限り）
- 保証しないこと: 段1 の CLI 実行そのものの再現性。これは段2 の plan による突合で事後的に確認される。同一リポジトリへ Pull Request を開ける主体が、workflow を書き換えて plan の role を assume すること（その role は変更系を持たないが、state を読める。決定13）。management account の root による state backend への書き込みと resource-based policy の削除（決定11）。member account での SCP と root 資格情報の中央管理による拒否が、それを持つ use-case の外で機能すること（決定11）。回復の契機に用いる人の資格情報の発行と管理（決定9）

## 検証方法

- Static Analysis: CI の workflow が長期のアクセスキーを参照していないこと（決定12）
- Static Analysis: plan を行う job と apply を行う job が、異なる role を assume すること（決定13）
- Static Analysis: role を assume する job が `environment` を宣言していること（決定14）
- Static Analysis: plan を行う job と apply を行う job が、異なる `environment` を宣言すること（決定14）
- Static Analysis: apply-identity の role を assume する job が、apply の role を assume する job と異なる role と `environment` を宣言すること（決定13-14）
- Policy Test: apply の role の有効な権限（IAM policy と、課す場合は permissions boundary を合わせたもの）が、bootstrap の構成物（決定1）の resource（OIDC provider、plan / apply / apply-identity の role、state backend）に対する変更系の Action を許さないこと（決定13）
- Static Analysis: apply の role を assume する job の起動条件が、保護ブランチへの push と手動の起動に限られ、手動の起動が ref や commit を入力に取らないこと（決定14）
- Static Analysis: plan の role を assume する job が、fork からの Pull Request で assume の前に失敗する条件を持つこと（決定14）
- Policy Test: 信頼条件にワイルドカードが存在しないこと（決定15、ADR-0402）
- Policy Test: 信頼条件の `sub` が environment 形式であり、ref 形式と event 形式を含まないこと（決定14）
- Policy Test: 信頼条件が `aud`、リポジトリ、environment 名、リポジトリと所有者の ID を完全一致で限定していること。environment 名が environment の宣言に存在すること（決定14）
- Policy Test: state backend の bucket policy と key policy が、CI の role 以外の IAM principal による書き込みを拒否すること（決定11）
- 突合: environment の宣言と GitHub 上の実態が一致すること、deployment branch policy のパターンが分岐のパターンの単一宣言から生成されたものと一致すること、apply の environment が保護ブランチのパターンだけを許すこと（決定14、ADR-0603 決定4-7）
- Contract Test: bootstrap の use-case の公開 variable / output の schema
- 手順: 段2 の完了条件（決定8）は、それ自体が検証である。plan が変更なしを示さない限り段3 へ進まない

決定9 の契機の判定と、決定10 の apply 前の条件と記録の存在は、機械判定できない。例外の commit を含む Pull Request のレビューで扱う。決定10 の merge 前の条件は、required check の plan が担う（ADR-0603 決定12）。決定11 のうち member account の SCP と root 資格情報の中央管理は、本 ADR では検査しない。

## 影響

- bootstrap は2つの use-case として実装され、他のユースケースと同じテスト要件を負う。
- ADR-0601 決定2-6 に、範囲と契機が限定された例外が1つ存在することになる。その契機は初回と回復の2つである。
- 別アカウントの CI から bootstrap の構成物を作れるアカウントでは、初回の例外を使わない。そのための cross-account の経路は、明示的なユースケースとして設計される（ADR-0301 決定17）。
- 保護設定の宣言に environment が加わり、ADR-0603 の適用と突合の対象が広がる。
- apply の role の権限設計（決定16）が、当該 use-case の最大の設計課題になる。ADR-0301 決定8 のもとでは wildcard による解決が採れない。

## 見直し条件

- AWS アカウントを新規に用意する頻度が上がり、段1-4 の手順が運用上の障害になった場合
- Terraform が state backend 自体の作成を扱える機構を提供した場合
- GitHub Actions 以外の CI を採用する場合（決定14 の claim の形が変わる）
- GitHub が environment 形式の `sub` を廃止または変更した場合
- 回復の契機（決定9）による例外の実行が観測された場合。信頼条件の誤りを merge 前に止める検査の不足を示す
