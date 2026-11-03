data "pmon_workload_bindings" "all" {}

output "service_principals" {
  value = distinct([for b in data.pmon_workload_bindings.all.bindings : b.principal])
}
