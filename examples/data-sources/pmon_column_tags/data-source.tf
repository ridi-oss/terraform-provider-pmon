data "pmon_column_tags" "finance" {
  datasource = "example-finance-rw"
}

# An empty result on a production datasource means the shipped presets' `unless resource in
# Tag::"pii"` exclusion never fires, so every column is readable in cleartext.
output "finance_is_unclassified" {
  value = length(data.pmon_column_tags.finance.columns) == 0
}
