data "pmon_datasource_liveness" "prod" {
  datasource = "example-prod-rw"
}

# A datasource stays registered after its proxy goes away, so gate changes that only make sense
# against something actually serving traffic.
resource "pmon_column_classification" "prod" {
  datasource = "example-prod-rw"
  columns    = local.pii_columns

  lifecycle {
    precondition {
      condition     = data.pmon_datasource_liveness.prod.attached
      error_message = "No proxy is attached to example-prod-rw, so its catalog may be stale."
    }
  }
}
