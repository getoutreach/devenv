variable P1_notify {
  type = list(string)
  default = []
}
variable P2_notify {
  type = list(string)
  default = []
}
variable additional_dd_tags {
  type = list(string)
  default = []
}

variable cpu_high_threshold {
  type = number
  default = 80
}

# window in minutes
variable cpu_high_window { 
  type = number
  default = 30
}

# Number of restarts per 30m to be considered a P1 incident.
variable pod_restart_threshold {
  type = number
  default = 3
}

variable "alert_on_panics" {
  type        = bool
  default     = true
  description = "Enables/Disables the panics monitor defined based on the logs"
}

locals {
  ddTags = concat(["devenv", "team:fnd-dtss"], var.additional_dd_tags)
}

# splitting the interval 15 mins to 3 windows (moving rollup by 5mins) and if each of them contains restart -> alert
resource "datadog_monitor" "pod_restarts" {
  type = "query alert"
  name = "Devenv Pod Restarts > 0 last 15m"
  query = "min(last_15m):moving_rollup(diff(sum:kubernetes_state.container.restarts{kube_container_name:devenv,!env:development} by {kube_namespace}), 300, 'sum') > 0"
  tags = local.ddTags
  message = <<EOF
  If we ever have a pod restart, we want to know.
  Note: This monitor will auto-resolve after 15 minutes of no restarts.
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/pod-restarts.md"
  Notify: ${join(" ", var.P2_notify)}
  EOF
  require_full_window = false
}

resource "datadog_monitor" "pod_restarts_high" {
  type = "query alert"
  name = "Devenv Pod Restarts > ${var.pod_restart_threshold} last 30m"
  query = "max(last_30m):diff(sum:kubernetes_state.container.restarts{kube_container_name:devenv,!env:development} by {kube_namespace}) > ${var.pod_restart_threshold}"
  tags = local.ddTags
  message = <<EOF
  Several pods are being restarted.
  Note: This monitor will auto-resolve after 30 minutes of no restarts.
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/pod-restarts.md"
  Notify: ${join(" ", var.P1_notify)}
  EOF
  require_full_window = false
}

# default to 0 if the pod was running on high CPU and then stopped/killed
resource "datadog_monitor" "pod_cpu_high" {
  type = "query alert"
  name = "Devenv Pod CPU > ${var.cpu_high_threshold}% of request last ${var.cpu_high_window}m"
  query = "avg(last_${var.cpu_high_window}m):100 * (default_zero(avg:kubernetes.cpu.usage.total{app:devenv,!env:development} by {kube_namespace,pod_name}) / 1000000000) / avg:kubernetes.cpu.requests{app:devenv,!env:development} by {kube_namespace} >= ${var.cpu_high_threshold}"
  tags = local.ddTags
  message = <<EOF
  One of the service's pods has been using over ${var.cpu_high_threshold}% of its requested CPU on average for the last ${var.cpu_high_window} minutes.  This almost certainly means that the service needs more CPU to function properly and is being throttled in its current form.
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/pod-cpu.md"
  Notify: ${join(" ", var.P2_notify)}
  EOF
  require_full_window = false
}

resource "datadog_monitor" "pod_memory_rss_high" {
  type = "query alert"
  name = "Devenv Pod Memory.rss > 80% of limit last 30m"
  query = "avg(last_30m):moving_rollup(default_zero(100 * avg:kubernetes.memory.rss{app:devenv,!env:development} by {kube_namespace,pod_name} / avg:kubernetes.memory.limits{app:devenv,!env:development} by {kube_namespace}), 60, 'max') >= 80"
  tags = local.ddTags
  message = <<EOF
  One of the service's pods has been using over 80% of its limit memory on average for the last 30 minutes.  This almost certainly means that the service needs more memory to function properly and is being throttled in its current form due to GC patterns and/or will be OOMKilled if consumption increases.
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/pod-memory.md"
  Notify: ${join(" ", var.P2_notify)}
  EOF
  require_full_window = false
}

resource "datadog_monitor" "pod_memory_working_set_high" {
  type = "query alert"
  name = "Devenv Pod Memory.working_set > 80% of limit last 30m"
  query = "avg(last_30m):moving_rollup(default_zero(100 * avg:kubernetes.memory.working_set{app:devenv,!env:development} by {kube_namespace,pod_name} / avg:kubernetes.memory.limits{app:devenv,!env:development} by {kube_namespace}), 60, 'max') >= 80"
  tags = local.ddTags
  message = <<EOF
  One of the service's pods has been using over 80% of its limit memory on average for the last 30 minutes.  This almost certainly means that the service needs more memory to function properly and is being throttled in its current form due to GC patterns and/or will be OOMKilled if consumption increases.
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/pod-memory.md"
  Notify: ${join(" ", var.P2_notify)}
  EOF
  require_full_window = false
}

variable available_pods_low_count {
  type    = number
  default = 2
}

resource "datadog_monitor" "available_pods_low" {
  type = "query alert"
  name = "Devenv Available Pods Low"
  query = "max(last_10m):avg:kubernetes_state.deployment.replicas_available{deployment:devenv,env:production} by {kube_namespace} < ${var.available_pods_low_count}"
  tags = local.ddTags
  message = <<EOF
  The Devenv replica count should be at least ${var.available_pods_low_count}, which is also the PDB.  If it's lower, that's below the PodDisruptionBudget and we're likely headed toward a total outage of Devenv.
  Note: This P1 alert only includes production
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/available-pods-low.md"
  Notify: ${join(" ", var.P1_notify)}
  EOF
}

resource "datadog_monitor" "panics" {
  type    = "log alert"
  name    = "Devenv Service panics"
  query   = "logs(\"panic status:error service:devenv -env:development\").index(\"*\").rollup(\"count\").by(\"kube_namespace\").last(\"5m\") > 0"
  tags    = local.ddTags
  message = <<EOF
  Log based monitor of runtime error panics.
  Note: This P1 alert only includes production
  Runbook: "https://github.com/getoutreach/devenv/blob/main/documentation/runbooks/service-panics.md"
  Notify: ${join(" ", var.P1_notify)}
  EOF
}

resource "datadog_downtime" "panics_silence" {
  scope      = ["*"]
  monitor_id = datadog_monitor.panics.id
  count      = var.alert_on_panics ? 0 : 1
}

///Block(tfCustomDatadog)

///EndBlock(tfCustomDatadog)
