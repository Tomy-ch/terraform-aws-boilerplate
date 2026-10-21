# ADR-0604: state を apply する role ごとに分け、env ごとに identity と workload の2つの root とする

- Status: Accepted
- Date: 2026-10-22
- Scope: repository-wide
- Related: ADR-0102, ADR-0205, ADR-0301, ADR-0402, ADR-0601, ADR-0602

## Context

Terraform は、1つの root の中でだけ resource と module の出力をつなぐ。root をまたぐ受け渡しは remote state か SSM Parameter になり、依存の順序も Terraform ではなく apply の手順が持つ。ADR-0102 決定16 は、usecase の間の受け渡しを「env が送り側の output を受け側の `variable` へ渡す」形を主とし、root をまたぐ形をその代わりとして置く。

一方、1回の apply は CI が assume した1つの role で走る（ADR-0602 決定12-14）。各アカウントの role は plan / apply / apply-identity の3つであり、`state-backend` と `deployment-identity` は apply-identity が apply する。両者を他の usecase と同じ root に置くと、他の usecase の apply が、IAM と state Bucket を書き換えられる role で走る。

state を細かく分ける利点は、誤った apply と state の破損の範囲を狭めることである。ただし role が3つで固定である以上、IAM の上での範囲は分割しても狭まらない。plan の role は state をすべて読むので（ADR-0602 決定13）、state に入る秘密値の読み手も狭まらない。分割の費用は、root をまたぐ受け渡しと、apply の順序と、途中で失敗したときの組合せである。

アカウントと state Bucket は 1対1 である。各アカウントは自分の state Bucket を持ち、管理アカウントの Bucket には管理アカウント自身の state だけを置く。

## Decision

### root の単位

1. 1つの env（1つのアカウント）の root を、identity root と workload root の2つとする。これ以外の root を env に置かない。
2. identity root は `state-backend` と `deployment-identity` だけを呼び、apply-identity の role で apply する。
3. workload root は、identity root が呼ぶもの以外のすべての usecase を呼び、apply の role で apply する。
4. workload root は `env/<env>/` の直下の `*.tf` とし、identity root は `env/<env>/identity/` の直下の `*.tf` とする。

### 受け渡し

5. 同じ workload root の中の usecase の間の受け渡しは、送り側の output を受け側の `variable` へ渡す形で行う（ADR-0102 決定16）。
6. identity root と workload root の間、およびアカウントの間の受け渡しは、root をまたぐ受け渡しとして ADR-0102 決定16 に従う。

### state

7. 各 root の state は、そのアカウントの state Bucket に、root ごとに固定した1つの key で置く。identity root は `identity/terraform.tfstate`、workload root は `workload/terraform.tfstate` とする。CLI の workspace を使わない。
8. 各 apply の role は、自分が apply する root の key（lock file を含む）だけに書き込める。もう一方の key への書き込みは、state Bucket の bucket policy で拒否する。

### stateful な resource の保護

9. workload root の plan に、stateful な resource（データベース、object を持つ Bucket 等）の削除または置換が含まれる場合、Policy Test で失敗させる。対象とする resource の型は、その Policy Test の宣言が持つ（ADR-0402）。意図した削除は、その Policy Test が定める手順で明示する。

## 検討した代替案

### 案A: ライフサイクル単位で分ける

workload root を foundation / network / data / app などに分ける。data 系を app と同じ apply から外せる。ただし層をまたぐ受け渡しが remote state か SSM になり、ADR-0102 決定16 の主な形が層の境界で使えなくなる。usecase を足すたびに、どの層に入れるかの判断が要る。data 系の保護は決定9 でも取れる。

### 案B: usecase ごとに root を分ける

誤った apply と state の破損の範囲が最も狭い。ただし usecase の間の受け渡しがすべて root をまたぎ、apply の順序を usecase の依存関係から組み立てる必要がある。env が usecase を組み合わせるという ADR-0102 決定12 の形が、Terraform から apply の手順へ移る。

### 案C: 機構の単位で分ける

DNS / CDN / Server などの root に分ける。1つの usecase は複数の機構を持つので、env が usecase だけを呼ぶ限り成立しない（ADR-0102 決定11）。成立させるには usecase を機構の単位で作り直すことになり、ADR-0102 が採らなかったサービス単位の形になる。証明書と alias record のように、機構の間で循環する受け渡しも生じる。

### 案D: env を1つの root にする

受け渡しも順序も最も単純になる。ただし `state-backend` と `deployment-identity` を、apply の role で apply することになる。apply の role に IAM と state Bucket を書き換える権限を与えるか、apply-identity の role で workload を apply するかのどちらかになり、ADR-0602 決定13 が role を分けた意味が消える。

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

- 保証すること: apply の role が identity root の state を書き換えないこと。workload root の中の usecase の受け渡しが、Terraform の依存の解決のもとで行われること。stateful な resource の削除と置換が、意図の明示なしに apply へ進まないこと
- 保証しないこと: workload root の中での、誤った apply の影響の局所化。workload root が大きくなったときの plan の時間とロックの待ち

## 検証方法

- Static Analysis: `env/<env>/identity/` が `state-backend` と `deployment-identity` 以外の usecase を呼ばないこと、`env/<env>/` の直下がその2つを呼ばないこと、`env/<env>/` の下に他の root が無いこと（決定1-4）
- Static Analysis: 各 root の backend の key が決定7 の値であること
- Policy Test: state Bucket の bucket policy が、各 apply の role について、もう一方の key への書き込みを拒否すること（決定8）
- Policy Test: workload root の plan が、対象の型の resource の削除または置換を含まないこと（決定9）

## 影響

- `state-backend` の usecase は、2つの key と、key ごとの書き込みの拒否を bucket policy に持つ。
- workload root の plan と apply は、env の workload 全体を対象にする。変更が1つの usecase に閉じていても、env の workload 全体をロックする。
- stateful な resource を意図して削除するには、決定9 の Policy Test が定める明示の手順を通る。

## 見直し条件

- workload root の plan の時間やロックの待ちが、変更の経路を妨げるようになった場合
- 決定9 で防げない、stateful な resource の事故が起きた場合
- apply の role を usecase ごとや層ごとに分けると決めた場合
