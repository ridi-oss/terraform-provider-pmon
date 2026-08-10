# Filter it. pmon returns the whole catalog in one response, and everything that survives the
# filter lands in Terraform state.
data "pmon_catalog" "prod" {
  datasource = "example-prod-rw"
  schema     = "example"
}

# Deriving the classification beats spelling out several hundred column names by hand.
locals {
  pii_patterns = ["email", "phone", "birthday", "password"]

  pii_columns = [
    for c in data.pmon_catalog.prod.columns : {
      schema = c.schema
      table  = c.table
      column = c.column
      tags   = ["pii"]
    }
    if anytrue([for p in local.pii_patterns : strcontains(c.column, p)])
  ]
}

# Columns that look like PII but are not tagged yet.
output "unclassified_pii_candidates" {
  value = [
    for c in data.pmon_catalog.prod.columns : "${c.table}.${c.column}"
    if length(c.tags) == 0 && anytrue([for p in local.pii_patterns : strcontains(c.column, p)])
  ]
}
