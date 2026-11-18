output "budget" {
  description = "予算の名前・額・通知の宣言。usecase の Contract Test が配線を確かめるのに使う。"
  value = {
    name          = aws_budgets_budget.this.name
    limit_amount  = aws_budgets_budget.this.limit_amount
    notifications = aws_budgets_budget.this.notification
  }
}
