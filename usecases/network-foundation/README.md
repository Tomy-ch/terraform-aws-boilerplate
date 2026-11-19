# network-foundation

各環境のアカウント（dev・stg・prd・sandbox）に、VPC と subnet と Route Table と VPC Flow Logs を1組ずつ持つユースケース。VPC の中に資源を置く他のユースケースは、このユースケースの出力を入力で受ける（[network-foundation/ADR-0001](docs/adr/0001-one-vpc-per-environment-account.md)）。

## 解決する問題

VPC の中に資源を置くユースケース（VPC Endpoint、ECS、RDS など）は、同じ VPC と subnet を前提にする。VPC の既定の Security Group の規則、main Route Table の経路、Flow Logs は VPC に1つずつで、持ち主が2つあると上書きし合い、持ち主が居ないと AWS の既定のまま残る。このユースケースが VPC とその既定の資源を1つの持ち主として持ち、他のユースケースへは識別子で渡す。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。資源は Terraform が作る
- **外部 egress を持たない。** Internet Gateway・NAT Gateway・egress-only Internet Gateway・VPC peering を持たず、どの Route Table も local の外への経路を持たない（[network-foundation/ADR-0102](docs/adr/0102-routes-owned-by-network-foundation.md)）。保証するのは経路の層だけで、Route 53 Resolver を経由した DNS による外部への到達は塞がない（「保証しないもの」）
- subnet は公開 IP も IPv6 も自動で割り当てない
- 既定の Security Group は規則を持たない。VPC の中の通信の許可は、守られる側の module の Security Group が持つ

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string` | 資源の名前の先頭。英小文字と数字で 42 文字以下 |
| `env` | `string` | `dev` / `stg` / `prd` / `sandbox` |
| `cidr` | `string` | VPC の IPv4 の CIDR。RFC 1918 の範囲の `/16` から `/24` で、ホスト部が 0、172.17.0.0/16 と重ならない |
| `availability_zone_ids` | `list(string)` | subnet を置く AZ の ID（例: `apne1-az1`）。2 個から 4 個、重ねない。位置が subnet の CIDR を決めるので、足すときは末尾に足し、途中を消さない |
| `flow_logs_retention_days` | `number` | Flow Logs を残す日数。365 以上の、CloudWatch Logs が受ける値。下限は `modules/log-group` と同じ（AWS Security Hub の CloudWatch.16 の既定の parameter） |
| `flow_logs_deletion_protection` | `bool` | Flow Logs の Log Group の削除を拒むか。destroy する環境では `false` |

どの入力も既定値を持たない。どれも環境の値か、利用者の要件である。

| 出力 | 型 | 使う側 |
| --- | --- | --- |
| `vpc_id` | `string` | VPC に Security Group や VPC Endpoint を置くユースケース |
| `private_subnet_ids` | `list(string)` | workload を置くユースケース（`private-aws-access` の interface 型の endpoint、ECS、内部の ALB）。`availability_zone_ids` の順 |
| `isolated_subnet_ids` | `list(string)` | データの層のユースケース（RDS、Aurora、ElastiCache、OpenSearch の subnet group）。外部 egress を有効にした環境でも外への経路を持たない。同じ順 |
| `private_route_table_ids` | `list(string)` | gateway 型の VPC Endpoint（S3・DynamoDB）の関連付けを置くユースケース（`private-aws-access`）。同じ順 |
| `flow_logs_log_group_arn` | `string` | ログの所在。VPC Flow Logs の Log Group の ARN（末尾の `:*` を持たない） |

基盤系アラームの通知先は受けない。通知に値する metric を出す resource を持たない（[network-foundation/ADR-0202](docs/adr/0202-no-alarm-topic.md)）。

## 持つもの

| 構成 | 中身 |
| --- | --- |
| `modules/kms` | Flow Logs の Log Group を暗号化する鍵（alias `<project>-<env>-network-flow-logs`） |
| `modules/log-group` | Flow Logs の Log Group（`/<project>/<env>/network-foundation/vpc-flow-logs`） |
| `modules/vpc` | VPC、既定の Security Group と main Route Table の封鎖、Flow Logs（`ALL`）とその配送 role（`<project>-<env>-vpc-flow-logs`） |
| `internal/subnet-tiers` | `private` と `isolated` の tier ごと AZ ごとの subnet と Route Table（経路は空）、subnet ごとの明示の関連付け（[network-foundation/ADR-0101](docs/adr/0101-two-tiers-and-fixed-cidr-slots.md)） |

subnet の CIDR は、VPC を 16 に分けた4つずつの slot から、tier と AZ の位置で切り出す。slot 0 が `private`、slot 1 が `isolated`、slot 2・3 は公開の入口と外部 egress の tier のために空けてある。

## まだ持たないもの

責務外ではなく、消費者の着手で足すもの。

| まだ持たないもの | 足す変更 |
| --- | --- |
| 外部 egress の tier、Internet Gateway、外部 egress の経路 | `controlled-external-egress`（#27）。このユースケースが装置の識別子を入力で受け、自分の Route Table に経路を書く。NAT Gateway・EIP・Network Firewall は `controlled-external-egress` が持つ |
| 公開の入口の tier | `public-api-alb` |

## supported / unsupported

| 区分 | 項目 |
| --- | --- |
| supported | 各環境のアカウントに VPC を1つ、AZ を 2 個から 4 個、IPv4 だけ |
| unsupported | IPv6（`modules/vpc`） |
| unsupported | Transit Gateway・Site-to-Site VPN・Direct Connect、組織の外のアカウントとの接続 |
| unsupported | VPC peering（同じアカウントの中を含む）。環境ごとに VPC は1つで、つなぐ相手の VPC が無い（[network-foundation/ADR-0001](docs/adr/0001-one-vpc-per-environment-account.md)）。アカウントをまたぐ接続は既定で許さない（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定18） |
| unsupported | IPAM からの CIDR の割り当てと、追加の IPv4 CIDR。入力が AWS の表現になる |
| unsupported | AZ が1つの構成と5つ以上の構成 |
| unsupported | tier の自由な追加と subnet の CIDR の指定、custom の Network ACL（[network-foundation/ADR-0101](docs/adr/0101-two-tiers-and-fixed-cidr-slots.md)） |
| unsupported | このユースケースの Route Table へ他のユースケースが直接経路を書く構成（[network-foundation/ADR-0102](docs/adr/0102-routes-owned-by-network-foundation.md)） |

## 所有者

| 資源 | 持ち主 |
| --- | --- |
| VPC、既定の Security Group、main Route Table、Flow Logs と配送 role、Flow Logs の Log Group と鍵 | このユースケース |
| subnet、Route Table、経路、Route Table の関連付け、Internet Gateway | このユースケース |
| NAT Gateway、EIP、Network Firewall、DNS Firewall | `controlled-external-egress`（#27） |
| VPC Endpoint とその Security Group、gateway 型の endpoint の Route Table の関連付け | `private-aws-access`（#26） |
| VPC の中の資源の Security Group と規則 | 守られる側のユースケースの module |

## 依存するユースケースと判断 Issue

- 依存するユースケース: 無い（各環境のアカウントと、そのアカウントの state backend と apply の経路だけ）
- 判断: #94（共有基盤の持ち主）、#86（sandbox の常駐）、#111（`modules/vpc`）

## 検証方法

| 層 | 道具 | 何を見るか |
| --- | --- | --- |
| Unit | `internal/subnet-tiers/tests/`、`tests/composition.tftest.hcl` | CIDR の割り付け、AZ を末尾に足しても既存の CIDR が動かないこと、subnet の属性、Route Table の経路の空、関連付け、名前 |
| Contract | `tests/contract.tftest.hcl`、`tests/validation.tftest.hcl` | 出力の配線と型、入力の許容値 |
| Policy | `conftest/plan/network_foundation.rego`（入力は `reference/tests/policy-input.tftest.hcl` の plan） | 既定の Security Group の規則の空、Flow Logs の `ALL`、配送 role の trust、Route Table の経路と経路の伝播の空、ユースケースの下に宣言の外の型が無いこと、Flow Logs の Log Group がこのユースケースの鍵で暗号化されること |
| Integration / E2E | sandbox の apply の経路（#331）で行う | 下の「保証しないもの」の各項 |

sandbox の destroy の後の期待残存: Flow Logs の鍵（alias `<project>-sandbox-network-flow-logs`、状態は削除待ち、期限は 30 日）。

## 保証しないもの

- 実際の AWS での作成と破棄。この変更の時点で、このユースケースを apply する経路が無い（dev・stg・prd は #330、sandbox は #331）
- `logs:CreateLogGroup` を持たない配送 role で Flow Logs が Log Group に届くこと（`modules/vpc` の逸脱）
- 帯域外で足した経路と既定の Security Group の規則が、次の plan の差分に出ること
- AZ の ID が実際の API で受け付けられること（Provider の `aws_subnet` の文書は、`availability_zone_id` が一部のリージョンで使えないとする）
- destroy が止まらないこと。GuardDuty の Runtime Monitoring が作る VPC Endpoint と、消費者の ENI が VPC の destroy を止めうる
- Route 53 Resolver を経由した DNS による外部への到達を塞ぐこと（#27）
- `flow_logs_retention_days` と `flow_logs_deletion_protection` が Log Group に届くこと、Flow Logs の宛先がこのユースケースの Log Group であること。`terraform test` は子の module の resource を assert から参照できないので、このユースケースのテストは子の module の出力だけを見る。Log Group がこのユースケースの鍵で暗号化されることは Policy（PLAN-NET-05）が見る
- Policy が掛かるのは reference の plan だけである。env の plan に掛けるかは env への配線（#330）で決める
- `availability_zone_ids` の途中の削除と並べ替えで subnet が置換になること。止める検査は無い（#330）

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19

- AWS が publisher の `aws-ia/vpc/aws` v4.9.0（Terraform Registry の latest）の `main.tf`
- AWS Provider v6.73.0 の `aws_route_table`・`aws_route`・`aws_route_table_association`・`aws_subnet` の文書と `internal/service/ec2/vpc_route_table.go`
- AWS Security Hub の EC2 のコントロール（EC2.2・EC2.6・EC2.10・EC2.15・EC2.16・EC2.21・EC2.172）

採用した知見:

- private の Route Table を AZ ごとに持つ（`aws-ia/vpc`）
- subnet を Route Table へ明示的に関連付け、main Route Table を使わない（`aws-ia/vpc`、`modules/vpc`）
- subnet に公開 IP を自動で割り当てない（EC2.15）
- 既定の Security Group の規則を空にし（EC2.2）、全 VPC で Flow Logs を有効にする（EC2.6）

採用しなかった知見と理由:

- `aws-ia/vpc` の `aws_route` の個別の resource: 帯域外の経路が plan に出ない（[network-foundation/ADR-0102](docs/adr/0102-routes-owned-by-network-foundation.md)）
- `aws-ia/vpc` の public subnet の公開 IP の自動の割り当て: EC2.15
- `aws-ia/vpc` の `az_count` と、tier・NAT・TGW・IPv6 を入力で切り替える構成: [network-foundation/ADR-0101](docs/adr/0101-two-tiers-and-fixed-cidr-slots.md)
- EC2.10（EC2 の interface 型の endpoint）: `private-aws-access` が持つ
- EC2.172（VPC Block Public Access）: アカウント・リージョン単位の設定で、VPC を親に取らない。`account-baseline` の候補。このユースケースは Internet Gateway を持たないので、どの設定とも両立する
- EC2.16・EC2.21（Network ACL）: custom の Network ACL を作らない。既定の Network ACL は EC2.21 の対象外

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

このユースケース自身の逸脱は無い。`modules/vpc` の逸脱（配送 role に `logs:CreateLogGroup` を持たせない）を引き継ぐ。記録は [`modules/vpc/README.md`](../../modules/vpc/README.md) の「公式推奨からの逸脱」が持つ。

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。`terraform test` と reference の lock は v6.73.0
- Terraform は `>= 1.7.0, < 2.0.0`
