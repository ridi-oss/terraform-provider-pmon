resource "pmon_workload_binding" "settlement_batch" {
  name     = "settlement-batch"
  issuer   = "aws-prod"
  subject  = "arn:aws:iam::111122223333:role/settlement-batch"
  purposes = ["wire"]
  conditions = {
    "principal_tags.kubernetes-namespace"       = ["batch"]
    "principal_tags.kubernetes-service-account" = ["settlement"]
  }
}

resource "pmon_role_assignment" "settlement_batch" {
  principal = pmon_workload_binding.settlement_batch.principal
  role_name = "service:settlement"
}

resource "pmon_workload_binding" "pmon_plan" {
  name       = "tf-pmon-plan"
  issuer     = "hcp-terraform"
  subject    = "organization:acme:project:infra:workspace:pmon:run_phase:plan"
  principal  = "svc:tf-pmon"
  purposes   = ["mcp"]
  mcp_scopes = ["mcp:read"]
}

resource "pmon_workload_binding" "pmon_apply" {
  name       = "tf-pmon-apply"
  issuer     = "hcp-terraform"
  subject    = "organization:acme:project:infra:workspace:pmon:run_phase:apply"
  principal  = "svc:tf-pmon"
  purposes   = ["mcp"]
  mcp_scopes = ["mcp:read", "mcp:policies:write", "mcp:identity:write"]
}
