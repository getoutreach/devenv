terraform {
  required_version = "~> 0.14"
  backend "s3" {
    bucket               = "outreach-terraform"
    dynamodb_table       = "terraform_statelock"
    workspace_key_prefix = "terraform_workspaces"
    #####
    # Ensure this key is unique per project
    #####
    key    = "devenv/snapshots/tfstate"
    region = "us-west-2"
  }
}

provider "aws" {
  region = "us-west-2"
}
