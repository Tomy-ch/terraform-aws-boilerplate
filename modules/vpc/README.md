# vpc

VPC を1つ、VPC 単位の不変条件を必ず持つ形で作る一般 module（ADR-0102 決定12・15）。呼び出し1回が、VPC と、その既定の Security Group と main Route Table の封鎖と、VPC 単位の Flow Logs とその配送 role を作る。subnet・Route Table・経路・Internet Gateway・NAT Gateway は持たない。それらは VPC を持つ usecase（`network-foundation`）の `internal/` に置く。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_vpc` | IPv4 の CIDR は入力から。DNS の解決とホスト名を有効、IPv6 の CIDR を持たない、tenancy は `default`。tag を持たない |
| `aws_default_security_group` | この VPC の既定の Security Group を管理下に置き、ingress と egress の規則を空にする（Security Hub EC2.2） |
| `aws_default_route_table` | この VPC の main Route Table を管理下に置き、経路と経路の伝播を空にする |
| `aws_flow_log` | VPC 単位。`traffic_type = "ALL"`、宛先は CloudWatch Logs の入力の Log Group、集約の間隔 600 秒、`log_format` は下の固定の項目集合（Security Hub EC2.6） |
| `aws_iam_role`・`aws_iam_role_policy`・`aws_iam_role_policies_exclusive`・`aws_iam_role_policy_attachments_exclusive` | Flow Logs の配送 role（`<name>-vpc-flow-logs`）。下の文書だけを持ち、帯域外の policy は排他の resource が剥がす |

| 入力 | 意味 |
| --- | --- |
| `name` | 資源名の前半。小文字の英数字と `-` で 1-50 文字（配送 role の名前が IAM の上限 64 文字に収まる長さ） |
| `cidr` | VPC の IPv4 の CIDR。`/16` から `/28`、RFC 1918 の範囲の中、`172.17.0.0/16` と重ならない（Amazon VPC User Guide の VPC CIDR blocks）。規模は env の値 |
| `flow_logs_log_group_arn` | Flow Logs の宛先の Log Group の ARN。`modules/log-group` の出力 `log_group_arn` を resource 参照のまま渡す。VPC と同じ partition・リージョン・アカウントのものだけを受ける（plan で落ちる） |

| 出力 | 使う側 |
| --- | --- |
| `vpc_id` | subnet・Route Table・Security Group・VPC Endpoint を置く module |
| `vpc_cidr` | subnet の CIDR を切り出す module。Security Group の参照で書けない CIDR の許可（ADR-0301 決定14） |

既定の Security Group と main Route Table の ID は出力しない。どちらも使ってはならないもので、出さなければ module の外から参照する経路が無い。

既定の Security Group と main Route Table は `conftest/hcl/layer/decl.json` で `aws_vpc` を親に取る attachment に分類している。層の検査（LY301）により、`aws_vpc` と同じ module 以外には置けない。`aws_main_route_table_association` も同じ分類で、この module の外から main Route Table を差し替えられない。

Flow Logs の宛先に literal の ARN ではなく `modules/log-group` の出力を渡すのは、resource 参照の依存によって、destroy が Flow Logs、Log Group の順になるからである。

### 配送 role の文書

文書は、テストが mock の下で中身を確かめられるよう `jsonencode` で組む。

trust policy:

| Sid | Principal | Action | Condition |
| --- | --- | --- | --- |
| `AllowVpcFlowLogsOfThisAccount` | `vpc-flow-logs.amazonaws.com` | `sts:AssumeRole` | `aws:SourceAccount` が同じアカウント、`aws:SourceArn` が同じ partition・リージョン・アカウントの `vpc-flow-log/*` |

`aws:SourceArn` を flow log の ID まで絞らないのは、ID が role より後に決まり、role と flow log が循環するからである（ADR-0301 決定11）。Amazon VPC User Guide の IAM role for publishing flow logs は、ID が未知のときに `*` を使うことを認めている。

permission policy:

| Sid | Action | Resource |
| --- | --- | --- |
| `WriteLogEvents` | `logs:CreateLogStream`・`logs:PutLogEvents` | 入力の Log Group の `log-stream:*` |
| `DescribeLogStreams` | `logs:DescribeLogStreams` | 入力の Log Group |
| `DescribeLogGroups` | `logs:DescribeLogGroups` | `*`（resource-level の権限を持たない。Service Authorization Reference） |

`logs:CreateLogGroup` を持たない。Log Group は Terraform が先に作り、サービスが保持日数と鍵の無い Log Group を暗黙に作る経路を残さない（`modules/log-group` の「持たないもの」）。AWS の文書の例からの逸脱であり、その記録は「公式推奨からの逸脱」が持つ。

### `log_format`

AWS の既定（v2）の14項目に、次を足した固定の項目集合である。順序は Amazon VPC User Guide の Flow log records の表の順。

| 項目 | 用途 |
| --- | --- |
| `vpc-id`・`subnet-id`・`instance-id`・`type` | 記録がどこの通信か |
| `tcp-flags` | 接続の試行（SYN）と、接続が閉じた後の遅れたパケットの区別 |
| `pkt-srcaddr`・`pkt-dstaddr` | NAT Gateway など中間の ENI を越えた通信の、元の送信元と宛先 |
| `region`・`az-id` | 記録の場所 |
| `pkt-src-aws-service`・`pkt-dst-aws-service`・`flow-direction`・`traffic-path` | AWS のサービスとの通信と、通信の方向と経路 |
| `reject-reason` | 拒否の理由（VPC Block Public Access による拒否の区別） |
| `interface-type` | NAT Gateway・VPC Endpoint などの ENI の区別 |

ECS の項目（v7）は Flow Logs を作る主体に `ecs:ListClusters` を要し、tag の項目（v11）はアカウントに service-linked role と EventBridge の managed rule を作るので、入れない。ログ量は v2 の既定のおよそ2倍になる。Flow Logs は作成後に設定を変えられないので、項目集合の変更は Flow Logs の置換になる。

## 持たないもの

| 持たないもの | 行き先 |
| --- | --- |
| subnet・Route Table・経路・Route Table の関連付け | VPC を持つ usecase の `internal/`。tier の集合と外部 egress の有無は環境で変わり、入力で resource の有無を変える切り替えになる（ADR-0102 決定15）。経路は Route Table と同じ module に置く（同 決定18） |
| Internet Gateway・NAT Gateway・Elastic IP | 同上。既定は外部 egress を持たない（`docs/v1.0-plan/scope.md` 第9.2節） |
| `aws_main_route_table_association` | main Route Table を差し替えない。Provider は `aws_default_route_table` との併用を禁じている。subnet は custom の Route Table へ明示的に関連付ける |
| Network ACL（既定の NACL の管理を含む） | 既定の NACL を規則なしで管理下に置くと全通信が止まり、規則を書くと AWS の既定と同じになる。subnet 層の遮断が要る tier の NACL は、VPC を持つ usecase の `internal/` |
| IPv6（`aws_vpc_ipv6_cidr_block_association`・`aws_egress_only_internet_gateway` など） | 責務外（ADR-0102 決定5・7）。IPv6 アドレスは既定で公開され、egress の不変条件を IPv6 に広げる検査が要る |
| IPAM からの CIDR の割り当て | 入力が AWS の表現になる。要る usecase の着手時に見直す |
| 追加の IPv4 CIDR | 同上 |
| DNS Firewall・Resolver のクエリログ・VPC Block Public Access の除外 | VPC を親に取る関連付けで、所有者が決まっていない |
| `aws_security_group` と規則 | 守られる側の module（`docs/v1.0-plan/scope.md` 第3.1節） |
| VPC Endpoint | `private-aws-access` |
| Log Group と鍵 | 呼ぶ usecase が `modules/kms` と `modules/log-group` で作り、ARN を渡す。鍵の単位は呼ぶ usecase が決める |
| 別のアカウントの宛先（`deliver_cross_account_role`）と S3・Firehose の宛先 | 責務外（ADR-0301 決定18）。宛先は CloudWatch Logs（`docs/v1.0-plan/scope.md` 第9.3節） |
| tag | 共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない |

## 保証しないもの

- 入力から導いた値と固定の値（VPC の属性、既定の Security Group と main Route Table が空であること、Flow Logs の属性、配送 role の文書、validation と precondition）は `tests/` が mock の plan と apply で見る。Unit Test に属する（ADR-0501 決定9）。規則と経路の空は plan で見るので、宣言から空の list を消すと値が unknown になってテストが落ちる
- IPv6・Internet Gateway・NAT Gateway の resource を持たないことは、宣言にそれらが無いことだけによる。どの検査も止めない。層の検査は未分類の型を止めるが（LY303）、型が分類された後はこの module への追加を止めない
- 既定の Security Group の規則が空であることを見るのは `tests/` だけである。既定の Security Group に `0.0.0.0/0` への egress を足した変異に、Checkov と Trivy は所見を出さなかった。KICS の 9a4ef195 は規則の宣言を咎めるが抑止している。Flow Logs の存在は、`aws_flow_log` を消した変異で Checkov の CKV2_AWS_11 と Trivy の AWS-0178 が落とすことを確かめた。どちらも Policy で見る検査は無く、この module を含む最初の reference の root と同じ変更で足す
- 配送 role の trust policy の Condition を Policy で見る検査は無い。同上
- `logs:CreateLogGroup` と、Resource を Log Group に絞った `DescribeLogStreams` で配送が成り立つことは、どのテストも確かめていない。最初の消費者の apply の後に、Log Group へ Flow Logs の record が届くことで確かめる
- 帯域外で既定の Security Group に規則を足すか、main Route Table に経路を足したとき、次の plan が差分を出すこと。Provider のソース（attribute-as-blocks の空の list）から導いたもので、実際の AWS では確かめていない
- VPC の destroy が、GuardDuty が作る VPC Endpoint で止まらないこと（Provider の `aws_vpc` の文書は、削除に任意の権限が要る場合を挙げる）
- `flow_logs_log_group_arn` に resource 参照を渡すことは、呼ぶ usecase のテストが見る
- Security の4つの道具のうち KICS の4件（既定の Security Group の規則、Network Firewall、IAM Access Analyzer、tag）は抑止している。理由と撤回条件は各ファイルの先頭が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。使う引数と、既定の Security Group の `ingress`・`egress` と main Route Table の `route` の扱い（attribute-as-blocks で、省略は既存の規則と経路の無視、空の list は全削除）は v6.73.0 の Provider の文書とソースで確かめた。`terraform test` を走らせたのは、lock の無いこの module で init が選んだ版だけである
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求である

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-19

- AWS が publisher の `aws-ia/vpc/aws` v4.9.0（Terraform Registry の latest）の `main.tf`・`variables.tf`・`modules/flow_logs/main.tf` を読んだ
- 参照した一次資料: AWS Provider v6.73.0 の `aws_vpc`・`aws_default_security_group`・`aws_default_route_table`・`aws_flow_log`・`aws_iam_role_policies_exclusive`・`aws_iam_role_policy_attachments_exclusive` の文書、Amazon VPC User Guide の VPC CIDR blocks・Default security groups・Subnet route tables・IAM role for publishing flow logs・Flow log records・Flow logs limitations、Service Authorization Reference の Amazon CloudWatch Logs、AWS Security Hub の EC2.2・EC2.6・EC2.15・EC2.172、AWS Well-Architected の SEC05-BP01

採用した知見:

- DNS の解決とホスト名を有効にする（`aws-ia/vpc`。interface 型の VPC Endpoint の private DNS の前提）
- Flow Logs の `traffic_type` を `ALL` にする（`aws-ia/vpc` の既定。EC2.6 の REJECT を含み、許可された通信の事後の調査にも使える）
- 集約の間隔を AWS と Provider の既定の 600 秒にする
- 配送 role の trust policy に `aws:SourceAccount` と `aws:SourceArn` を置く（IAM role for publishing flow logs）
- 既定の Security Group の規則を空にする（EC2.2）。main Route Table を経路の無いまま残し、subnet を custom の Route Table へ明示的に関連付ける（Subnet route tables）

採用しなかった知見と理由:

- `aws-ia/vpc` の Flow Logs の既定の無効: EC2.6 と `docs/v1.0-plan/scope.md` 第9.2節に反する
- `aws-ia/vpc` の配送 role の Condition の無い trust policy と、Resource `*` の permission: 最小権限に反する（ADR-0301 決定8・9・11）
- `aws-ia/vpc` が既定の Security Group と main Route Table を管理しないこと: 上の EC2.2 と Subnet route tables
- `aws-ia/vpc` の public subnet の公開 IP の自動割り当て: EC2.15。この module は subnet を持たない
- `aws-ia/vpc` の tier・NAT・IPv6・Transit Gateway などを入力で切り替える構成: ADR-0102 決定15、ADR-0101 決定9
- VPC Block Public Access（EC2.172）: アカウント・リージョン単位の設定で、VPC を親に取らない。この module の範囲外

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。

| 対象 | 理由 | 由来 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- | --- |
| 配送 role に `logs:CreateLogGroup` を持たせない。Amazon VPC User Guide の IAM role for publishing flow logs は、role が少なくとも持つ権限としてこれを含む5つの Action を挙げる | Log Group が消えたときに、サービスが保持日数と鍵の無い Log Group を作る経路を残さない（ADR-0301 決定2・9、`modules/log-group` の「持たないもの」） | ユースケースの制約ではなく、この module の不変条件 | security は、保持日数と暗号化の不変条件を持たない Log Group の暗黙の作成を塞ぐ。availability は、配送が `logs:CreateLogGroup` を要するなら Flow Logs が届かない（「保証しないもの」）。cost は変わらない | 5つの Action をすべて持ち、`logs:CreateLogGroup` の Resource を入力の Log Group に絞る | 最初の消費者の apply の後に、Flow Logs の record が Log Group へ届かなかったとき |
