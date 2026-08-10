# A forbid always beats a permit in Cedar, so this layers over the shipped system: presets
# rather than replacing them. Anyone without the matching service role is denied, and a
# datasource carrying no recognised service tag matches no pair and is denied outright.
resource "pmon_policy" "service_fence" {
  name = "service:fence"

  cedar_src = <<-EOT
    forbid (
      principal,
      action in [
        Action::"datasource.connect",
        Action::"sql.select", Action::"sql.insert", Action::"sql.update",
        Action::"sql.delete", Action::"sql.ddl",
        Action::"sql.unanalyzable", Action::"sql.unmaskable",
        Action::"result.read.unmasked", Action::"result.read.masked"
      ],
      resource
    )
    unless {
      (resource in Tag::"service:alpha" && principal in Role::"service:alpha") ||
      (resource in Tag::"service:beta" && principal in Role::"service:beta")
    };
  EOT
}
