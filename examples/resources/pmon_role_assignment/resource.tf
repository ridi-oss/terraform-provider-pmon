# Prefer pmon_group_roles. A direct assignment does not show up in the group structure and is
# easy to lose track of when someone changes team.
resource "pmon_role_assignment" "oncall" {
  principal = "alice@example.com"
  role_name = pmon_role.service_alpha.name
}
