# Terraform Provider for proxy-monster

Manages [proxy-monster](https://github.com/ridi-oss/proxy-monster) access control as code:
Cedar policies, roles, groups and their memberships, masking functions, and column classification.

The provider talks to pmon's MCP administration endpoint rather than its REST API. MCP is a
first-class management surface in pmon -- it reuses the same services, transactions, and
validators as REST -- and it addresses everything by name (`Role::"analyst"`,
`Datasource::"example-prod-rw"`) instead of by numeric id, which makes for far more stable Terraform
state. Every write also carries an idempotency key derived from the intended end state, so a
retry after a dropped connection cannot apply twice.

## Resources

| Resource | Manages |
| --- | --- |
| `pmon_policy` | A Cedar policy. Validated during `plan`, not at apply. |
| `pmon_role` | An access-control role. |
| `pmon_group` | A local identity group. |
| `pmon_group_roles` | The complete set of roles bound to a group. Authoritative. |
| `pmon_group_member` | One principal's membership of one group. Non-authoritative. |
| `pmon_role_assignment` | A role granted straight to a principal, bypassing groups. |
| `pmon_mask_fn` | A masking function. |
| `pmon_column_classification` | Tags on one datasource's columns. |

## Data sources

| Data source | Reads |
| --- | --- |
| `pmon_datasources` | Every brokered datasource, with the tags the proxy pushed. |
| `pmon_datasource_liveness` | Whether a proxy is attached to one datasource right now. |
| `pmon_catalog` | Every column in a datasource, with the tags it already carries. |
| `pmon_table_detail` | One table: columns, foreign keys, size. |
| `pmon_column_tags` | Only the classified columns of a datasource. |
| `pmon_policies` / `pmon_policy` | Every Cedar policy, or one by name. |
| `pmon_policy_schema` | The Cedar schema policies are validated against. |
| `pmon_roles` / `pmon_groups` / `pmon_users` | The identity estate. |

`pmon_catalog` is what lets a classification be derived rather than typed out:

```terraform
data "pmon_catalog" "prod" {
  datasource = "example-prod-rw"
  schema     = "example"
}

resource "pmon_column_classification" "prod" {
  datasource = "example-prod-rw"
  columns = [
    for c in data.pmon_catalog.prod.columns : {
      schema = c.schema, table = c.table, column = c.column, tags = ["pii"]
    }
    if anytrue([for p in ["email", "phone", "birthday"] : strcontains(c.column, p)])
  ]
}
```

Filter it. pmon returns the whole catalog in one response and everything that survives the filter
lands in state. `pmon_users` likewise writes every principal and email address into state; prefer
`pmon_groups` when entitlements are all you need.

## Authentication

`terraform plan` opens a browser on first use. pmon is an OAuth 2.1 resource server: the
provider discovers the authorization server, identifies itself with a Client ID Metadata
Document, and runs authorization code + PKCE. The token is cached at `~/.pmon/tf-token.json`
and refreshed without a browser until the refresh token expires.

Set `PMON_NO_BROWSER=1` to fail with a diagnostic instead of opening a browser.

### Using a token you already have

Set `access_token` (or `PMON_ACCESS_TOKEN`) to a pmon token obtained elsewhere and the provider
presents it instead of logging in: no metadata document is fetched, no browser opens, and nothing
is written to the token cache, because the token is not this machine's to keep. It is the way in
while the client metadata document has nowhere public to live, and the only way to run the
provider where a browser cannot be opened.

pmon issues access tokens with a ten-minute lifetime and rotates the refresh token beside them on
every use, so the provider takes the access token alone and never renews it. A run that outlives
one fails with a diagnostic asking for a fresher token; nothing is left half-applied, and the next
run picks up where it stopped. The token must also already carry the scope a write needs, since
there is no authorization request left in which to ask for more.

Scopes are a consent ceiling, never a grant. pmon re-resolves your roles and re-evaluates Cedar
on every single call, so a token can only ever attempt what you are already entitled to. Set the
`scopes` argument to lower that ceiling. pmon asks for `mcp:read` up front and re-challenges with
whatever scope a write turns out to need; the provider caps that challenge at what you listed, so
a write outside the list fails with `insufficient_scope` rather than asking you to consent to it.
That is how a read-only configuration stays read-only.

Because the token names a person, every change lands in pmon's audit trail attributed to the human
who ran it, with `channel=mcp`.

That also makes the provider unsuitable for unattended CI today: refresh tokens are short-lived
and rotate on every use, so two concurrent runs would invalidate each other.

## What this provider does not manage

Datasource registration and datasource tags. The proxy pushes those over gRPC at registration
time from its own `PM_DATASOURCE_TAGS`, so there is no API to set them and no resource here that
could. Read them with `pmon_datasources` and assert on them where a policy depends on a tag:

```terraform
data "pmon_datasources" "all" {}

resource "pmon_policy" "service_fence" {
  name      = "service:fence"
  cedar_src = file("${path.module}/fence.cedar")

  lifecycle {
    precondition {
      condition = alltrue([
        for d in data.pmon_datasources.all.datasources :
        anytrue([for t in d.tags : startswith(t, "service:")])
      ])
      error_message = "A datasource carries no service: tag. Deploy PM_DATASOURCE_TAGS first, or this fence denies it to everyone."
    }
  }
}
```

Shipped `system:` policies, roles, and groups are immutable in pmon and are rejected during
`plan` rather than mid-apply.

## Development

Requires Go >= 1.25.

```shell
make build      # type-check the module
make dev        # build bin/terraform-provider-pmon and bin/dev.tfrc
make test       # unit tests
make lint       # golangci-lint
make generate   # regenerate docs/ from examples/ and the schema
make install    # install into $GOBIN
```

Run `make generate` after any schema change: CI fails if `docs/` is out of date.

To run a local build:

```shell
make dev
export TF_CLI_CONFIG_FILE=$PWD/bin/dev.tfrc
```

### Logging in

`bin/pmon-login` runs the same login the provider runs, from a terminal:

```shell
export PMON_ENDPOINT=https://pmon.example.com/mcp
bin/pmon-login
```

Prefer it to letting the first `terraform plan` log in. The provider runs as a Terraform plugin,
and Terraform captures a plugin's stderr into its own log, so the authorization URL never reaches
the terminal. That goes unnoticed while the browser opens by itself, and turns into a five-minute
stall the moment it does not. `pmon-login` prints the URL where you can see it, and the plan after
it finds a cached token and needs no browser.

`bin/dev.tfrc` is a `dev_overrides` block mapping `ridi-oss/pmon` to `bin/`, and both files are
gitignored. Overriding the CLI config file beats editing `~/.terraformrc`: the override lasts only
as long as the shell that exported it, and it still works where a config manager owns
`~/.terraformrc` as a read-only symlink.

With `dev_overrides` in place, skip `terraform init` -- Terraform uses the local binary and writes
no lock file. Terraform prints a warning on every command to say so, which is expected.

Acceptance tests create real infrastructure and are gated behind `TF_ACC`:

```shell
make testacc
```

Point them at a local control plane (`PM_AUTH_DEBUG=true`), never a production pmon.

## License

Apache-2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
