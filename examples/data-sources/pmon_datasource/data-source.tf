data "pmon_datasource" "prod" {
  name = "example-prod-rw"
}

# Tags come from the proxy's PM_DATASOURCE_TAGS and cannot be set here, so a policy that keys on
# one should assert it is present before being applied.
resource "pmon_policy" "production_fence" {
  name      = "service:fence"
  cedar_src = file("${path.module}/fence.cedar")

  lifecycle {
    precondition {
      condition     = contains(data.pmon_datasource.prod.tags, "system:production")
      error_message = "example-prod-rw is not tagged system:production, so the production presets do not apply to it."
    }
  }
}
