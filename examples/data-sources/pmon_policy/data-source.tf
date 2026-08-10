# Shipped system: policies are immutable, so they can be read but not managed.
data "pmon_policy" "production_select" {
  name = "system:production-select"
}

output "production_select_is_enabled" {
  value = data.pmon_policy.production_select.enabled
}
