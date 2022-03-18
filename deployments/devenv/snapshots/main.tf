module "s3-testbucket" {
  source               = "git@github.com:getoutreach/terraform-modules.git//s3-datastorage?ref=a50123ccb22a5b7bc778c185c32784b4f4d7eec9"
  team                 = "fnd:dtss"
  data_classification  = "high"
  personal_information = "no"
  environment          = "development"
  application          = "dev-environment"
  bento                = "none"
  tier                 = "non-essential"
  k8s_cluster          = "none"
  bucket_name          = "outreach-devenv-snapshots"
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
