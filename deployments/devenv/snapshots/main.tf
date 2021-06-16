module "s3-testbucket" {
  source                = "git@github.com:getoutreach/terraform-modules.git//s3-datastorage?ref=246500c"
  team                  = "cia-dev-tooling"
  data_classification   = "High"
  data_retention_months = "0"
  personal_information  = "No"
  environment           = "Development"
  application           = "dev-environment"
  bento                 = "None"
  tier                  = "Non-essential"
  k8s_cluster           = "None"
  bucket_name           = "outreach-devenv-snapshots"
}

data "aws_iam_policy_document" "automated_snapshot_policy_data" {
  statement {
    actions = [
      "s3:PutObject",
      "s3:GetObject",
    ]
    resources = [
      "arn:aws:s3:::outreach-devenv-snapshots/automated-snapshots/*",
    ]
  }
}

resource "aws_iam_policy" "automated_snapshot_policy" {
  name   = "devenv-automated-snapshot-policy"
  policy = data.aws_iam_policy_document.automated_snapshot_policy_data.json
}

resource "aws_iam_user_policy_attachment" "circleci-attach" {
  user       = "circleci"
  policy_arn = aws_iam_policy.automated_snapshot_policy.arn
}
