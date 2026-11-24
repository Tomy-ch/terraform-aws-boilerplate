mock_provider "aws" {
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

run "chatbot_in_aws" {
  command = plan

  assert {
    condition     = length(aws_iam_service_linked_role.chatbot) == 1
    error_message = "aws の partition で、Chatbot の service-linked role が1つになっていません。"
  }

  assert {
    condition     = aws_iam_service_linked_role.chatbot[0].aws_service_name == "management.chatbot.amazonaws.com"
    error_message = "service-linked role のサービスが management.chatbot.amazonaws.com ではありません。"
  }

  assert {
    condition     = aws_iam_service_linked_role.chatbot[0].custom_suffix == null && aws_iam_service_linked_role.chatbot[0].tags == null
    error_message = "service-linked role に custom_suffix か tag が付いています。AWS が決める名前と、tag の無い形にしてください（deployment-identity/ADR-0002 決定3）。"
  }
}

run "absent_outside_aws" {
  command = plan

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }

  assert {
    condition     = length(aws_iam_service_linked_role.chatbot) == 0
    error_message = "aws でない partition で、Chatbot の service-linked role を作ろうとしています。"
  }
}
