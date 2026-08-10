data "pmon_table_detail" "users" {
  datasource = "example-prod-rw"
  schema     = "example"
  table      = "users"
}

# A key into a table holding PII usually deserves the same treatment as the column it points at.
output "keys_into_this_table" {
  value = [
    for fk in data.pmon_table_detail.users.referenced_by :
    "${fk.source_table}.${join(",", fk.source_columns)}"
  ]
}

# Column comments are often the best hint about what a column actually holds.
output "commented_columns" {
  value = {
    for c in data.pmon_table_detail.users.columns :
    c.name => c.comment if c.comment != ""
  }
}
