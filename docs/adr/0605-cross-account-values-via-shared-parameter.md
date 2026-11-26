# ADR-0605: アカウントをまたぐ識別子は、組織の情報を AWS から読み、アカウントの ID の一覧を組織へ共有した parameter で配る

- Status: Accepted
- Date: 2026-11-27
- Scope: repository-wide
- Related: ADR-0102, ADR-0301, ADR-0401, ADR-0402, ADR-0602, ADR-0604

## Context

ADR-0102 決定16 は、root をまたぐ識別子の受け渡しを remote state か SSM Parameter で行い、どちらを使うかと受け側の権限を ADR-0301 に委ねる。ADR-0604 は state を apply する role ごとに分け、state の Bucket と鍵は、そのアカウントの CI の role だけが読める。

メンバーアカウントの root は、自分のアカウントの外の識別子を要る。

- 組織の ID と管理アカウントの ID。例: 組織の trail の配信を受ける Bucket Policy と Key Policy の条件
- ほかのメンバーアカウントの ID。例: Config の配信元のアカウント、log-archive のアカウントの Bucket の名前と鍵の alias の ARN

アカウントの ID の一覧の正本は、管理アカウントの identity root の `organization-baseline` の出力である。アカウントの ID はリポジトリに書かない（bootstrap の手順は、記録の中のアカウントの ID を伏せる）。

次の事実がある。

- Organizations の DescribeOrganization は、メンバーアカウントからも呼べる。組織の ID・ARN と管理アカウントの ID を返す（AWS Organizations API Reference）。アカウントの一覧（ListAccounts）は、管理アカウントか委任管理者からしか呼べない
- Systems Manager の advanced tier の parameter は、AWS RAM で組織か OU へ共有できる。共有の宛先は読み取りだけの permission を受け、値を変えられない。RAM の組織との共有を有効にしてからでないと、組織を宛先にできない（Systems Manager User Guide「Working with shared parameters」）

## Decision

1. メンバーアカウントの root は、組織の ID・ARN と管理アカウントの ID を、自分のアカウントで data source（DescribeOrganization）から読む。値を写さない。
2. メンバーアカウントの root がほかのアカウントの ID を要るときは、管理アカウントの identity root が持つ parameter を、その ARN で data source から読む。parameter は次の形に限る。
   - `organization-baseline` が持ち、値はメンバーアカウントの env から ID への JSON の object とする。正本の出力から組み、人が値を書かない
   - advanced tier の String とする。値は秘密ではないので、鍵の共有を伴う SecureString にしない
   - AWS RAM で組織を宛先に共有する。共有は組織の外の principal を許さない（`allow_external_principals = false`）。permission は読み取りだけの既定に任せる
3. メンバーアカウントの CI の role に、管理アカウントの state の読み取りを与えない（ADR-0301 決定18、ADR-0604）。
4. RAM の組織との共有の有効化は Terraform に持たせず、人の手順にする。組織の信頼アクセスをサービスの側で有効にする扱い（ADR-0601 の `aws_service_access_principals` の行）と同じにする。手順の入口は、確認の入口と対で置く（ADR-0702）。
5. 受け側の CI の plan と apply の role には、共有の parameter の ARN に限った `ssm:GetParameter` だけを与える。parameter の名前は送り側の公開契約が固定し、受け側は管理アカウントの ID（決定1）とその名前から ARN を組む。

## 検討した代替案

### 案A: GitHub の environment の secret を `TF_VAR_*` で渡す

人が値を environment ごとに写し、CI の workflow が渡す。アカウントが増えるたびに人の手順が増え、写した値が正本と一致することを plan で確かめられない。手元の plan にも同じ値が要る。

### 案B: 管理アカウントの state を `terraform_remote_state` で読む

受け側の CI の role に、管理アカウントの state の Bucket と鍵の読み取りが要る。state の全体（統制の構成物の属性を含む）を、別のアカウントの role が読めることになる（ADR-0301 決定18、ADR-0604）。

### 案C: env の `.tf` にアカウントの ID を書く

アカウントの ID を伏せる扱いに反する。正本と一致することも確かめられない。

### 案D: 共有の parameter に、組織の ID と管理アカウントの ID も載せる

経路は1つになるが、AWS が受け側のアカウントで返す値（決定1）の写しになる。写しは送り側の apply の後でしか直らない。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 中（secret の写し） | 低（state の全体を読める） | 低（ID がリポジトリに残る） | 高 | 高 |
| Testability | 低（一致を確かめられない） | 中 | 低 | 高 | 高（正本の出力から組む値を Unit Test が固定する） |
| Contract Clarity | 低 | 低（state の内部に依存する） | 中 | 中（同じ値の経路が2つ） | 高 |
| Maintainability | 低（アカウントごとに人の手順） | 中 | 低 | 高 | 高 |
| Scope Control | 中 | 低 | 中 | 高 | 高 |

評価軸の定義は repository-wide/ADR-0101 を参照する。

## 意図的に捨てるもの

- 管理アカウントの apply の前に、メンバーアカウントが新しいアカウントの ID を読めること。parameter は管理アカウントの apply が書く。
- RAM の組織との共有を Terraform で持つこと。
- 1つの parameter を advanced tier に置く費用（parameter ごとに月額がかかり、受け側の API の呼び出しも課金される）。

## 保証範囲

- 保証すること
  - 共有が組織の外の principal を許さないこと
  - parameter の値が正本の出力から組まれること
- 保証しないこと
  - RAM の組織との共有が有効であること。人の手順と、その確認の入口が見る
  - 受け側が読んだ値と、管理アカウントの state の現在の値が一致すること。parameter は管理アカウントの apply が書くので、その apply の前の値を読むことがある

## 検証方法

- Policy Test: `plan.ram_resource_share`（`PLAN-RAM-01`）が、入力のすべての共有の `allow_external_principals` が plan で `false` に決まることを見る
- Unit Test: `organization-baseline` の `internal/account-ids` が、parameter の名前・型・tier・値と、共有と宛先を固定する。`deployment-identity` の Unit Test が、管理アカウントの CI の role の parameter と共有の権限を固定する
- 人の手順: RAM の組織との共有の有効化と確認（`docs/setup/organization-baseline.md` 3.10）

## 影響

- 管理アカウントの CI の plan と apply-identity の role に、parameter と共有の読み書きが入る。
- 新しく使う管理アカウントでは、段4 の apply が共有の宛先の作成で止まる。StackSets の信頼アクセスと同じく、人の手順の後にやり直す。
- メンバーアカウントの root は、自分の env の配線で、決定1 と決定2 の data source を読む。

## 見直し条件

- アカウントの ID のほかに、アカウントをまたいで配る識別子が要るとき（同じ経路に載せるか、parameter を分けるか）
- RAM の組織との共有を Terraform で持てる権限の形を、統制の構成物として採るとき
- 受け側の数が増え、advanced tier の費用が無視できなくなったとき
