terraform {
  backend "s3" {
    bucket               = "outreach-terraform"
    dynamodb_table       = "terraform_statelock"
    workspace_key_prefix = "terraform_workspaces"
    #####
    # Ensure this key is unique per project
    #####
    key    = "monitoring/devenv/main.tfstate"
    region = "us-west-2"
  }
}

provider "vault" {
  address = "https://vault.outreach.cloud"
}

data "vault_generic_secret" "datadog" {
  path = "deploy/datadog/alerting"
}

provider "datadog" {
  api_key = data.vault_generic_secret.datadog.data["api_key"]
  app_key = data.vault_generic_secret.datadog.data["app_key"]
}
