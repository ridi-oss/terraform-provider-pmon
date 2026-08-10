# Authoritative: pmon replaces the group's roles outright, so a role missing here is removed.
resource "pmon_group_roles" "alpha_service" {
  group_name = pmon_group.alpha_service.name
  role_names = [pmon_role.service_alpha.name]
}

# Works on an IdP-provisioned group too, which is how a team gets its entitlements without
# its membership being managed here.
resource "pmon_group_roles" "developers" {
  group_name = "developers"
  role_names = [
    "system:development-viewer",
    "system:development-updater",
  ]
}
