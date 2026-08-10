# An IdP-provisioned group already exists, so look it up instead of trying to create it.
data "pmon_group" "developers" {
  name = "developers"
}

resource "pmon_group_roles" "developers" {
  group_name = data.pmon_group.developers.name
  role_names = ["system:development-viewer"]
}

output "developer_count" {
  value = data.pmon_group.developers.member_count
}
