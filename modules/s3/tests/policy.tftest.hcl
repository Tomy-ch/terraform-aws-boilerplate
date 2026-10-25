mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

# Bucket の ARN は plan では決まらないので、mock の apply で確かめる。
run "deny_insecure_transport" {
  command = apply

  variables {
    name = "tabp-mgmt-tfstate"
  }

  assert {
    condition = jsondecode(aws_s3_bucket_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid       = "DenyInsecureTransport"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:*"
          Resource  = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
          Condition = { Bool = { "aws:SecureTransport" = "false" } }
        },
      ]
    }
    error_message = "Bucket Policy が、TLS でない要求を Bucket とその object のすべてについて拒否する1文だけになっていません。"
  }

  assert {
    condition     = aws_s3_bucket_policy.this.bucket == aws_s3_bucket.this.id
    error_message = "Bucket Policy が、この module の Bucket に付いていません。"
  }
}
