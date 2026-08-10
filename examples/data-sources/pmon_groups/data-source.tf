data "pmon_groups" "all" {}

# Groups with no roles and no members grant nothing and are usually leftovers.
output "empty_groups" {
  value = [
    for g in data.pmon_groups.all.groups :
    g.name if length(g.role_names) == 0 && g.member_count == 0
  ]
}

# Which groups hand out production read access.
output "production_viewers" {
  value = [
    for g in data.pmon_groups.all.groups :
    g.name if contains(g.role_names, "system:production-viewer")
  ]
}
