# The entity types and actions a policy may reference.
data "pmon_policy_schema" "current" {}

output "cedar_schema" {
  value = data.pmon_policy_schema.current.schema
}
