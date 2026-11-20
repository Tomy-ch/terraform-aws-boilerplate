# ADR-0301: 公開 DNS のクエリログを us-east-1 の、このユースケースの鍵で暗号化した Log Group へ送る

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, dns-zone/ADR-0303

## Context

Security Hub の Route53.2 は、公開の zone に DNS のクエリログを求める。Route 53 は公開の zone のクエリログを us-east-1 の CloudWatch Logs の Log Group にしか送らず、送る権限をアカウント単位の resource policy で受ける（Route 53 Developer Guide「Public DNS query logging」）。アカウント単位の resource policy はリージョンに10個までの共有の資源で、親を持たない。AWS の例は Resource を `log-group:/aws/route53/*` とする。

## Decision

1. 公開の zone のクエリログを有効にする（Route53.2）。
2. Log Group は `/aws/route53/<zone の名前>`、us-east-1、保持 365 日以上、このユースケースの鍵で暗号化する（`modules/log-group` と `modules/kms` に `region = "us-east-1"`）。保持日数と削除の保護は入力で受ける。
3. resource policy は us-east-1 のアカウント単位の policy（`<project>-<env>-dns-query-logs`）を1つ持つ。文は1つで、Principal は `route53.amazonaws.com`、Action は `logs:CreateLogStream` と `logs:PutLogEvents`、Resource はこのユースケースの Log Group の ARN と `:*` だけとし、`aws:SourceAccount` で要求元を限り、`aws:SourceArn` を置かない。zone の ARN を参照すると、zone を作る plan で文書が決まらず、Policy が文書を読めない（`PLAN-INPUT-04`）。`arn:aws:route53:::hostedzone/*` の pattern はアカウントを持たず、`aws:SourceAccount` より狭くならない（repository-wide/ADR-0301 (least-privilege-policy) 決定11）。同じアカウントの別の zone を区別しないことは「意図的に捨てるもの」に置く。
4. query log の作成は resource policy の書き込みの後にする（`internal/query-log` の出力の `depends_on`）。

## 検討した代替案

### 案A: クエリログを持たない

Route53.2 からの逸脱になる。DNSSEC の運用（署名の前後の可用性の確認）にも使えない。

### 案B: Log Group 単位の resource policy（`resource_arn`）

リージョンの10個の枠を使わない。Route 53 がこの形を受けるかを AWS の文書で確認できない。

### 案C: AWS の例のとおり Resource を `log-group:/aws/route53/*` にする

同じアカウントの別の Log Group にも書き込みを許す（repository-wide/ADR-0301 (least-privilege-policy) 決定1）。

### 案D: `modules/vended-log-group` を使う

delivery destination を使わない経路で、resource policy の形も違う。

## 意図的に捨てるもの

- 同じアカウントの別の公開の zone からの書き込みを区別すること（Resource はこの Log Group だけに限る）。
- us-east-1 の resource policy の枠の1つ（sandbox の複製を並行して作ると複製ごとに1つ）。
- resolver の cache が答えた問い合わせの記録。

## 保証範囲

- Log Group の名前、resource policy の文書は `internal/query-log/tests/` が見る。region・鍵・保持日数の下限・resource policy の形は PLAN-DNS-02 が見る。
- Route 53 が、鍵で暗号化した Log Group と、絞った resource policy と `aws:SourceAccount` の条件を受けることは確かめていない（#331）。

## 検証方法

- `internal/query-log/tests/attributes.tftest.hcl`、`conftest/plan/dns_zone.rego`（PLAN-DNS-02）

## 影響

- CI の apply の role に us-east-1 の `logs:` と `kms:` の権限が要る（#330）。

## 見直し条件

- Route 53 が Log Group 単位の resource policy を受けることを確かめたとき（案B へ移す）。
- us-east-1 の枠が足りなくなったとき。
