provider "pmon" {
  # May also be set with PMON_ENDPOINT.
  endpoint = "https://pmon.example.com/mcp"

  # Optional: narrow what this configuration is allowed to attempt. Scopes are a
  # consent ceiling; pmon still evaluates Cedar for the real decision.
  scopes = ["mcp:read", "mcp:policies:write"]

  # Optional: present a token obtained elsewhere instead of logging in, for hosts where
  # no browser can be opened. Prefer PMON_ACCESS_TOKEN over writing it here -- pmon expires
  # these after ten minutes and the provider cannot renew them.
  # access_token = var.pmon_access_token
}
