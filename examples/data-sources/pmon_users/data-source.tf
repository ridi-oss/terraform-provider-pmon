# Writes every principal and email into state. Prefer pmon_groups when entitlements are enough.
data "pmon_users" "all" {}

output "admins" {
  value = [
    for u in data.pmon_users.all.users :
    u.principal if contains(u.group_names, "system:admin")
  ]
}

output "deactivated" {
  value = [for u in data.pmon_users.all.users : u.principal if !u.active]
}
