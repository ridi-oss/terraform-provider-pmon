data "pmon_policies" "all" {}

# A disabled policy is inert but still listed, which is easy to miss when reading the console.
output "disabled_policies" {
  value = [for p in data.pmon_policies.all.policies : p.name if !p.enabled]
}

# Shipped presets cannot be managed as resources; these are the ones you authored.
output "authored_policies" {
  value = [for p in data.pmon_policies.all.policies : p.name if p.origin == "USER"]
}
