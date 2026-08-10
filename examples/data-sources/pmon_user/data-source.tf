data "pmon_user" "alice" {
  principal = "alice@example.com"
}

output "alice_is_admin" {
  value = contains(data.pmon_user.alice.group_names, "system:admin")
}
