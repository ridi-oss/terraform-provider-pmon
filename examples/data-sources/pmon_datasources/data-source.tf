data "pmon_datasources" "all" {}

# Datasource tags come from the proxy's PM_DATASOURCE_TAGS and cannot be set here, so a policy
# that keys on one should assert the tag exists before it is applied.
output "untagged_production" {
  value = [
    for d in data.pmon_datasources.all.datasources :
    d.name if contains(d.tags, "system:production") && !anytrue([
      for t in d.tags : startswith(t, "service:")
    ])
  ]
}
