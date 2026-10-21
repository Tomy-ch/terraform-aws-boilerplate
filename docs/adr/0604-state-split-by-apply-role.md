# ADR-0604: state を apply する role ごとに分け、env ごとに identity と workload の2つの root とする

- Status: Accepted
- Date: 2026-10-22
- Scope: repository-wide
- Related: ADR-0102, ADR-0205, ADR-0301, ADR-0401, ADR-0402, ADR-0601, ADR-0602

## Context

Terraform は、1つの root の中でだけ resource と module の出力をつなぐ。root をまたぐ受け渡しは remote state か SSM Parameter になり、依存の順序も Terraform ではなく apply の手順が持つ。ADR-0102 決定16 は、usecase の間の受け渡しを「env が送り側の output を受け側の `variable` へ渡す」形を主とし、root をまたぐ形をその代わりとして置く。

一方、1回の apply は CI が assume した1つの role で走る（ADR-0602 決定12）。ADR-0602 決定13 は、各アカウントの role を plan / apply / apply-identity の3つに分け、bootstrap の構成物（同 決定1）を apply-identity だけに変更させる。bootstrap の構成物を他の usecase と同じ root に置くと、他の usecase の apply が、IAM と state backend を書き換えられる role で走る。

state を細かく分ける利点は、誤った apply と state の破損の範囲を狭めることである。ただし role が3つで固定である以上、IAM の上での範囲は分割しても狭まらない。plan の role は state をすべて読むので（ADR-0602 決定13）、state に入る秘密値の読み手も狭まらない。分割の費用は、root をまたぐ受け渡しと、apply の順序と、途中で失敗したときの組合せである。

各アカウントは自分の state Bucket を持つ。管理アカウントの Bucket には、管理アカウントの state を置く。

## Decision

### env とアカウント

1. 本 ADR は、1つの env が1つのアカウントに対応する構成を対象とする。1つのアカウントに複数の env を置く構成の root と key は、その構成をサポートするかを決めるときに、あわせて決める。

### root の単位

2. 1つの env の root を、identity root と workload root の2つとする。これ以外の root を env に置かない。
3. identity root は、bootstrap の構成物（ADR-0602 決定1）を持つ usecase だけを呼び、apply-identity の role で apply する。
4. workload root は、identity root が呼ぶもの以外のすべての usecase を呼び、apply の role で apply する。identity root と workload root の間の受け渡しは、root をまたぐ受け渡し（ADR-0102 決定16）である。
5. workload root は `env/<env>/` の直下の `*.tf` とし、identity root は `env/<env>/identity/` の直下の `*.tf` とする。provider の依存の lock（`.terraform.lock.hcl`）は root ごとに持つ（ADR-0205 決定9）。
6. 管理アカウントの identity root は、メンバーアカウントを立ち上げる間に限り、そのメンバーアカウントの bootstrap の構成物も呼ぶ。その state は管理アカウントの Bucket に置く。立ち上げの後、メンバーアカウントの identity root が `import` block で取り込み、管理アカウントの identity root は `removed` block（`destroy = false`）で手放す。取り込みは、state を書く前に plan で突合する（ADR-0602 決定6・8）。管理アカウントの identity root の `removed` block は、メンバーアカウントの identity root の plan が変更なしを示した後に apply する。この経路は ADR-0602 決定9 の cross-account access であり、ADR-0301 決定17 に従う。

### state と lock

7. 各 root の state は、そのアカウントの state Bucket に、root ごとに固定した1つの key で置く。identity root は `identity/terraform.tfstate`、workload root は `workload/terraform.tfstate` とする。CLI の workspace を使わない。
8. lock は、S3 backend が state の key の隣に置く lock file（`<key>.tflock`）で取る。plan の role は state にも lock file にも書き込まない（ADR-0602 決定13）。したがって plan は lock を取らずに実行する。apply は lock を取る（ADR-0601 決定16）。

### 書き込みの分離

9. 各 apply の role は、自分が apply する root の key と lock file だけに書き込める。もう一方の root の key と lock file への書き込みは、state Bucket の bucket policy で拒否する。拒否は role の ARN で書く。
10. apply の role は、state Bucket と、state の暗号化に使う鍵の設定を変更する有効な権限を持たない。対象は bucket policy、版管理、暗号化、lifecycle、object の版の削除、鍵の key policy である。これらを変更できるのは apply-identity の role だけとする。

### stateful な resource の保護

11. identity root と workload root の plan に、保護の対象として宣言した型の resource の削除または置換が含まれる場合、Policy Test で失敗させる。対象の型の宣言は、その Policy Test が持つ（ADR-0402）。state Bucket の型を必ず含める。この Policy Test は、Pull Request の plan と、apply の job が apply する plan の両方に掛ける。apply の job は、失敗したら apply しない。
12. 意図した削除と置換は、対象の address と理由を、許可の宣言に足すことで明示する。許可の宣言は root ごとに持つ。許可の宣言のうち、その root の plan のどの削除と置換にも当たらない項目は、Policy Test で失敗させる。

## 検討した代替案

### 案A: ライフサイクル単位で分ける

workload root を foundation / network / data / app などに分ける。data 系を app と同じ apply から外せる。ただし層をまたぐ受け渡しが remote state か SSM になり、ADR-0102 決定16 の主な形が層の境界で使えなくなる。usecase を足すたびに、どの層に入れるかの判断が要る。data 系の保護は決定11-12 でも取れる。

### 案B: usecase ごとに root を分ける

誤った apply と state の破損の範囲が最も狭い。ただし usecase の間の受け渡しがすべて root をまたぎ、apply の順序を usecase の依存関係から組み立てる必要がある。env が usecase を組み合わせるという ADR-0102 決定12 の形が、Terraform から apply の手順へ移る。

### 案C: 機構の単位で分ける

DNS / CDN / Server などの root に分ける。1つの usecase は複数の機構を持つので、env が usecase だけを呼ぶ限り成立しない（ADR-0102 決定11）。成立させるには usecase を機構の単位で作り直すことになり、ADR-0102 が採らなかったサービス単位の形になる。証明書と alias record のように、機構の間で循環する受け渡しも生じる。

### 案D: env を1つの root にする

受け渡しも順序も最も単純になる。ただし bootstrap の構成物を、apply の role で apply することになる。apply の role に IAM と state backend を書き換える権限を与えるか、apply-identity の role で workload を apply するかのどちらかになり、ADR-0602 決定13 が role を分けた意味が消える。

### 評価

| 評価軸 | 案A: ライフサイクル | 案B: usecase | 案C: 機構 | 案D: env で1つ | 採用案: identity と workload |
| --- | --- | --- | --- | --- | --- |
| Security | 中（role は増えない） | 中（role は増えない） | 中 | 低（role の分離が消える） | 高 |
| Testability | 中（層をまたぐ受け渡しの検証が要る） | 低（順序の組合せが多い） | 低 | 高 | 高 |
| Contract Clarity | 中 | 低（受け渡しがすべて root をまたぐ） | 低（usecase が分割される） | 高 | 高 |
| Maintainability | 中（層の割り当てが要る） | 低 | 低 | 高 | 高 |
| Scope Control | 中 | 中 | 低 | 高 | 高 |

評価軸の定義は ADR-0101 を参照する。

## 意図的に捨てるもの

- 誤った apply と state の破損を、env の workload より狭い範囲に閉じること
- workload の中の変更を、並行して apply すること

## 保証範囲

- 保証すること:
  - IAM principal である apply の role が、identity root の state と lock file を書き換えず、state Bucket と state の鍵の設定を変更しないこと
  - 保護の対象として宣言した型の resource の削除と置換が、許可の宣言なしに、plan を経る apply へ進まないこと
- 保証しないこと:
  - 管理アカウントの root による state Bucket への書き込みと、bucket policy の削除（ADR-0602 決定11）
  - workload root の中での、誤った apply の影響の局所化
  - workload root が大きくなったときの plan の時間と、apply のロックの待ち
  - 宣言していない型の resource の削除と置換。plan を経ない操作（`terraform destroy` 等）による削除

## 検証方法

- Static Analysis: 次の3つを検査する（決定2-5）
  - `env/<env>/identity/` が bootstrap の構成物を持つ usecase 以外を呼ばないこと
  - `env/<env>/` の直下がそれらの usecase を呼ばないこと
  - `env/<env>/` の下に他の root が無いこと
- Static Analysis: 各 root の backend の key が決定7 の値であり、lock file を使う設定であること。plan の job が lock を取らず、apply の job が取ること（決定8）
- Static Analysis: apply の job が、apply する plan に決定11-12 の Policy Test を掛け、その成功を apply の条件にしていること（決定11）
- Policy Test: state Bucket の bucket policy が、各 apply の role の ARN について、もう一方の root の key と lock file への書き込みを拒否すること（決定9）
- Policy Test: apply の role の権限（IAM policy と、課す場合は permissions boundary を合わせた有効な権限）が、決定10 の変更を state Bucket と state の鍵に対して許さないこと（決定10）
- Policy Test: identity root と workload root の plan が、許可の宣言に無い、対象の型の resource の削除または置換を含まないこと。各 root の許可の宣言に、その root の plan のどれにも当たらない項目が無いこと（決定11-12）。違反する入力と違反しない入力の両方で検証する（ADR-0402）

## 影響

- state Bucket は、2つの key と lock file ごとの書き込みの拒否を、bucket policy に持つ。
- workload root の plan と apply は、env の workload 全体を対象にする。変更が1つの usecase に閉じていても、env の workload 全体をロックする。
- plan は lock を取らないので、apply と並行して走ると、apply の途中の state を読むことがある。apply は merge された commit で lock を取って実行するので（ADR-0601 決定6・16）、lock を取らない plan の結果は apply の内容を決めない。
- stateful な resource を意図して削除するには、許可の宣言に address と理由を足す変更を通す。削除が済んだ後の変更で、その項目を取り除く。

## 見直し条件

- workload root の plan の時間や apply のロックの待ちが、変更の経路を妨げるようになった場合
- 決定11-12 で防げない、stateful な resource の事故が起きた場合
- apply の role を usecase ごとや層ごとに分けると決めた場合
- 1つのアカウントに複数の env を置く構成を、サポートの対象にすると決めた場合
