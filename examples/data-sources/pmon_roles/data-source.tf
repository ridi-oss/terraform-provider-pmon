data "pmon_roles" "all" {}

output "role_names" {
  value = [for r in data.pmon_roles.all.roles : r.name]
}
