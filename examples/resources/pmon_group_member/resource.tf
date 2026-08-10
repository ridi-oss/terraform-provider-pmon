# Adds exactly this membership and leaves the group's other members alone.
resource "pmon_group_member" "alice" {
  group_name = pmon_group.alpha_service.name
  principal  = "alice@example.com"
}
