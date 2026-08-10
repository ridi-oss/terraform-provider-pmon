# Shipped system: roles cannot be managed as resources, so refer to one rather than repeating
# the string wherever a policy or a group binding needs it.
data "pmon_role" "production_viewer" {
  name = "system:production-viewer"
}

resource "pmon_group_roles" "analysts" {
  group_name = pmon_group.analysts.name
  role_names = [data.pmon_role.production_viewer.name]
}
