# This file is managed by stencil.  Changes outside of `Block`s will be
# clobbered next time it is run.

# This file defines a very generic infrastructure-level dashboard.  It provides
# visibility into service attributes that are generic across services, like k8s
# metrics or counts of gRPC calls.
#
# Most services will need more dashboards that include more service-specific
# information.  You can define them in the Datadog UI or a separate `.tf` file
# in this directory according to your preference.

# Pro tip: For best results, visit
# https://app.datadoghq.com/metric/summary?metric=devenv.grpc_request_handled
# and set the unit metadata to "seconds".  This will make the y axes on some of
# these charts much more readable.

locals {
  dashboard_parameters = [
    {
      name    = "bento"
      prefix  = "bento"
      default = "*"
    },
    {
      name    = "env"
      prefix  = "env"
      default = "*"
    },
    ///Block(customDashboardParams)

    ///EndBlock(customDashboardParams)
  ]
}

# Header section: Here we include descriptions, links and other info.

locals {
  standard_links = [
    "[GitHub](https://github.com/getoutreach/devenv)",
    "[Engdocs](https://engdocs.outreach.cloud/github.com/getoutreach/devenv)",
    "[Logs](https://app.datadoghq.com/logs?cols=service%2C%40bento%2Csource&from_ts=1601507372082&index=main&live=true&messageDisplay=expanded-md&stream_sort=desc&to_ts=1601508272082&query=service%3Adevenv)",
    "[Honeycomb](https://ui.honeycomb.io/outreach-a0/datasets/outreach?query=%7B%22breakdowns%22%3A%5B%22service_name%22%2C%22name%22%2C%22deployment.bento%22%5D%2C%22calculations%22%3A%5B%7B%22op%22%3A%22COUNT%22%7D%2C%7B%22column%22%3A%22duration_ms%22%2C%22op%22%3A%22P90%22%7D%2C%7B%22column%22%3A%22duration_ms%22%2C%22op%22%3A%22HEATMAP%22%7D%5D%2C%22filters%22%3A%5B%7B%22column%22%3A%22service_name%22%2C%22op%22%3A%22exists%22%2C%22join_column%22%3A%22%22%7D%2C%7B%22column%22%3A%22service_name%22%2C%22op%22%3A%22%3D%22%2C%22value%22%3A%22devenv%22%2C%22join_column%22%3A%22%22%7D%5D%2C%22orders%22%3A%5B%7B%22op%22%3A%22COUNT%22%2C%22order%22%3A%22descending%22%7D%5D%2C%22limit%22%3A1000%2C%22time_range%22%3A604800%7D)",
    "[Owning GitHub team](https://github.com/orgs/getoutreach/teams/fnd-dtss)",
    "[Deployment Slack](https://slack.com/app_redirect?channel=dev-tooling-notifications)",
    "[Concourse CI/CD](https://concourse.outreach.cloud/teams/devs/pipelines/devenv)",
  ]
  custom_links = [
///Block(customLinks)

///EndBlock(customLinks)
  ]

  formatted_links = join("\n", formatlist("- %s", concat(local.standard_links, local.custom_links)))
  links_note_content = join("", ["See also:\n\n", local.formatted_links])
}

module "links_note" {
  source = "git@github.com:getoutreach/monitoring-terraform.git//modules/dd-chart/generic/note"
  content = local.links_note_content
}

module "description_note" {
  source = "git@github.com:getoutreach/monitoring-terraform.git//modules/dd-chart/generic/note"
  content = <<-EOF
    # Devenv

    This is the terraform-managed dashboard for the devenv service.
  EOF
}

locals {
  header_section = {
    name = "Devenv Service Info"
    charts = [
      module.description_note.rendered,
      module.links_note.rendered,
    ]
  }
}

# Deployment section: k8s-level service information.

module "deployment" {
  source     = "git@github.com:getoutreach/monitoring-terraform.git//modules/dd-sections/generic_deployment"
  deployment = "devenv"
}

# You can define additional sections here if needed.

///Block(sectionDefinitions)
# If you would like to instantiate additional dashboard section templates, you
# can do so here.  For example, if you wanted to include standard charts for
# some hypothetical "pongrequest" gRPC call, you could do:
#
# module "grpc_pongrequest" {
#   source     = "git@github.com:getoutreach/monitoring-terraform.git//modules/dd-sections/bootstrap_grpc"
#   deployment = "devenv"
#   call       = "api.devenv.pongrequest"
# }
#
# Don't forget to instantiate the new section by referencing it in the
# dashboard definition below.
///EndBlock(sectionDefinitions)

# Here we render the dashboard.

module "dashboard" {
  source = "git@github.com:getoutreach/monitoring-terraform.git//modules/dd-dashboards"
  name = "Terraform: Devenv"
  description = "Managed by terraform in github.com/getoutreach/devenv"

  parameters = local.dashboard_parameters
  sections = [
    local.header_section,
    module.deployment.rendered,
///Block(sectionReferences)

///EndBlock(sectionReferences)
  ]
}

