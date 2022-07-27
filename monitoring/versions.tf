terraform {
  # This is a top-level project, so we pin to specific versions.  All child
  # modules should use versions ">=" version specifiers for maximum flexibility.
  required_providers {
    datadog = {
      source  = "datadog/datadog"
      version = ">= 2.20.0"
    }
    vault = {
      source = "hashicorp/vault"
      version = ">= 2.15.0"
    }
  }
  required_version = ">= 0.13"
}
