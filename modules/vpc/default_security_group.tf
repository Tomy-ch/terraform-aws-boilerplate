# kics-scan disable=9a4ef195-74b9-4c58-b8ed-2b2fe4353a75,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# 9a4ef195（既定 Security Group の規則）: KICS は ingress / egress の宣言の有無を見て、値が空の list であることを見ない。規則が0件であることは tests/attributes.tftest.hcl が plan で見る。撤回: KICS が空の list を規則なしと読むようになったとき、または Provider が省略を空の集合として管理するようになったとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

resource "aws_default_security_group" "this" {
  vpc_id = aws_vpc.this.id
  # 省略すると Provider は既存の規則を無視し、帯域外で足された規則が plan に出ない。空を明示して規則の集合を管理する。
  ingress = []
  egress  = []
}
