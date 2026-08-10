provider "pmon" {
  # May also be set with PMON_ENDPOINT.
  endpoint = "https://pmon.example.com/mcp"

  # Optional: narrow what this configuration is allowed to attempt. Scopes are a
  # consent ceiling; pmon still evaluates Cedar for the real decision.
  scopes = ["mcp:read", "mcp:policies:write"]
}
