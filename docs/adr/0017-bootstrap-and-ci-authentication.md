# ADR-0017: bootstrapをCLIとimportで立ち上げ、CI認証をOIDCとする

- Status: Accepted
- Date: 2026-09-16
- Scope: repository-wide
- Related: ADR-0003, ADR-0005, ADR-0011, ADR-0013, ADR-0014, ADR-0016

## Context

ADR-0014 は変更経路を Git に限定し、apply を merge 後の CI/CD に置いた。しかしこの経路そのものが、次の2つを前提にしている。

- Terraform state を保存する backend が存在すること
- CI が AWS を操作する権限を持つこと

いずれも本リポジトリが作る対象であり、作る前には経路が成立しない。state backend については純粋な循環（backend が無ければ state を保存できず、state が無ければ backend を Terraform で作れない）が存在する。

CI の権限については循環の性質が異なる。backend さえあれば、人のローカル権限から Terraform で作成できる。つまり CLI で作らなければならない理由は無い。

一方で、CI の信頼条件は本構成で最も危険な単一のオブジェクトである。GitHub Actions の OIDC claim による限定を誤ると、当該リポジトリ以外からも role を assume できる。この形の誤りは、権限の広さではなく信頼の広さとして現れるため、権限の最小化では防げない。したがって、この構成が**実在する前に検査を通過している**ことが、他のどの構成よりも重要になる。

また、AWS CLI で作成した構成をそのまま Terraform 管理外に置くと、その構成に対して ADR-0014 決定1（Git が Desired State）が成立しない。state backend と CI の権限は、drift 検出の対象から外してよい構成ではない。

## Decision

### 対象

1. bootstrap を「CI が Terraform を実行できるようになるまでに必要な構成」と定義し、対象を次の2つに限る。

   - Terraform の state backend
   - GitHub Actions から AWS を操作するための OIDC provider と IAM role

2. bootstrap の構成物も use-case として `modules/` 配下へ置く（ADR-0003）。bootstrap であることを理由に、型付き契約（ADR-0006）・Generic Escape Hatch の禁止（ADR-0007）・最小権限（ADR-0011）・テスト（ADR-0012）のいずれも免除しない。
3. bootstrap 層であることは、適用の契機が異なることのみを意味する。公開契約とテストの要件は他のユースケースと同一とする。

### 順序

4. 次の順序で立ち上げる。

   1. AWS CLI で state backend の実体を作成する
   2. 同じ構成を表す Terraform を書き、`import` block で取り込む。`terraform plan` が変更なしを示すまで Terraform 側を修正する
   3. OIDC provider と IAM role の use-case を書く
   4. 人のローカル権限で1回だけ apply する
   5. 以後、CI が OIDC で role を assume して実行する

5. 段1 が CLI であるのは、state backend が存在しない時点では Terraform が state を保存できないためである。**この理由が成立しない構成物を CLI で作成しない。** OIDC provider と IAM role は段2 の完了後に Terraform で作成できるため、CLI の対象としない。
6. 段2 で `terraform import` コマンドを用いない。`import` block を用い、state を書く前に plan で突合する。config と実物の不一致を、state を書いたあとの plan で発見する経路を採らない。
7. CLI の手順と `import` block を同一の変更で提供する。片方のみを変更しない。
8. 段2 の完了条件を「`terraform plan` が変更なしを示すこと」とする。この条件の充足をもって、CLI が作成した実体と Terraform の宣言が一致したとみなす。

### ADR-0014 決定5 の例外

9. 段4 のローカルからの apply を、ADR-0014 決定5（人がローカル環境から本番環境へ直接 apply しない）の例外とする。例外の範囲を次に限る。

   - 対象: 本 ADR 決定1 が定める bootstrap の構成物のみ
   - 契機: 当該 AWS アカウントで初めて CI 実行基盤を用意するとき、1回限り
   - 以後: 同じ構成の変更は ADR-0014 の経路で行う

10. 例外を実行した記録（実行者、日時、対象アカウント、適用した commit）を残す。記録を残さない実行を認めない。
11. bootstrap の完了後、state backend への書き込みを CI の role に限定する。bootstrap 実行者の書き込み権限を残さない。ADR-0014 決定5 を、手順ではなく権限として裏打ちする。

### CI 認証

12. CI から AWS への認証を、OIDC による role の assume とする。長期のアクセスキーを CI の secret として保持しない。
13. plan を行う role と apply を行う role を分ける。plan の role に変更系の権限を与えない。
14. 各 role の信頼条件を、GitHub Actions の OIDC claim によって契機まで限定する。plan は pull_request、apply は保護ブランチの ref または environment に限定する。
15. 信頼条件にワイルドカードを用いない。この禁止を、当該 use-case の Policy Test の invariant として実装する（ADR-0016 決定6）。
16. apply の role が持つ権限の与え方（Action の列挙か permissions boundary か）は、当該 use-case の ADR が決定する。ADR-0011 決定8（Action wildcard の禁止）はこの role にも適用されるため、`*` による解決を採らない。

### 本 ADR で決定しない事項

17. state のロック方式（別テーブルによるロックか、backend が提供する lock file か）は、採用する Terraform version の確定後に、当該 use-case の ADR で決定する。ADR-0014 決定16 が要求するのは排他制御が成立することのみである。
18. state の分割単位と環境の表現方法は、ADR-0014 決定15 のとおり引き続き未決とし、本 ADR の対象外とする。

## 検討した代替案

### 案A: bootstrap の構成物を Terraform 管理外に置く

手順は最も単純で、循環も発生しない。ただし state backend と CI の権限に対して ADR-0014 決定1 が成立せず、drift 検出の対象外になる。最も権限の強い構成が、唯一検査されない構成になる。

### 案B: OIDC provider と IAM role も CLI で作成し、import する

bootstrap の手段が CLI に統一される。ただし信頼条件の JSON が、実在する前に一度も検査を通らない。CLI が流し込む JSON を Conftest で検査することは可能だが、その JSON の構造は plan の出力とは異なるため、同一の invariant に対して Policy を2本持つことになる。ADR-0016 決定3（1つの invariant に権威は1つ）と整合しない。

### 案C: 長期のアクセスキーを CI の secret として保持する

配線が単純で、OIDC provider の作成が不要になる。ただし失効期限を持たない資格情報がリポジトリの設定に存在し続ける。漏洩時の影響範囲が、契機や ref によらず全操作に及ぶ。

### 評価

| 評価軸 | 案A: 管理外 | 案B: 全て CLI + import | 案C: 長期キー | 採用案: backend のみ CLI |
| --- | --- | --- | --- | --- |
| Security | 低（最強の構成が未検査） | 中（信頼条件が未検査で実在する） | 低（無期限の資格情報） | 高 |
| Testability | 低 | 中（Policy が二重になる） | 中 | 高 |
| Contract Clarity | 低 | 中 | 中 | 高 |
| Scope Control | 中 | 中 | 高 | 高 |
| Reproducibility | 低（手順のみ） | 中 | 中 | 高（plan による突合） |
| 手順の単純さ | 高 | 中 | 高 | 低（意図的） |

## 意図的に捨てるもの

- bootstrap を単一の手段で完結させること
- 人のローカルからの実行を一度も行わないこと
- CI の資格情報を設定画面のみで完結させること

## 保証範囲

- 保証すること: bootstrap の構成物が Terraform の管理下にあり、以後の変更が ADR-0014 の経路に乗ること。CI が長期の資格情報を保持しないこと
- 保証しないこと: 段1 の CLI 実行そのものの再現性。これは段2 の plan による突合で事後的に確認される

## 検証方法

- Static Analysis: CI の workflow が長期のアクセスキーを参照していないこと（決定12）
- Static Analysis: plan を行う job と apply を行う job が、異なる role を assume すること（決定13）
- Policy Test: 信頼条件にワイルドカードが存在しないこと（決定15、ADR-0016）
- Contract Test: bootstrap の use-case の公開 variable / output の schema
- 手順: 段2 の完了条件（決定8）は、それ自体が検証である。plan が変更なしを示さない限り段3 へ進まない

## 影響

- bootstrap は2つの use-case として実装され、他のユースケースと同じテスト要件を負う。
- ADR-0014 決定5 に、範囲と契機が限定された例外が1つ存在することになる。
- apply の role の権限設計（決定16）が、当該 use-case の最大の設計課題になる。ADR-0011 決定8 のもとでは wildcard による解決が採れない。

## 見直し条件

- AWS アカウントを新規に用意する頻度が上がり、段1-4 の手順が運用上の障害になった場合
- Terraform が state backend 自体の作成を扱える機構を提供した場合
- GitHub Actions 以外の CI を採用する場合（決定14 の claim の形が変わる）
