# Terraform Provider for proxy-monster

Manages [proxy-monster](https://github.com/ridi-oss/proxy-monster) access control as code:
Cedar policies, roles, groups and their memberships, masking functions, column classification,
and workload bindings.

The provider talks to pmon's MCP administration endpoint rather than its REST API. MCP is a
first-class management surface in pmon -- it reuses the same services, transactions, and
validators as REST -- and it addresses everything by name (`Role::"analyst"`,
`Datasource::"example-prod-rw"`) instead of by numeric id, which makes for far more stable Terraform
state. Every write carries a fresh idempotency key, because pmon keeps
recorded keys and would replay an old outcome for a repeated one: a destroy after a recreate must
delete again, not replay the first delete. A create whose response was lost fails on the next
apply with "already exists"; import the object to adopt it.

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
| `pmon_workload_binding` | Which workload OIDC tokens act as which service principal. |

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
| `pmon_workload_bindings` | Every workload binding. |

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

### Hosting the client metadata document

pmon has no dynamic client registration. It identifies a client by fetching the document at the
`client_id` URL, on every authorization request, and that document has to name the same URL back.
Running this provider therefore means publishing one small JSON file somewhere pmon can reach.

`client-metadata.json` in this repository is that document, and the provider defaults to the URL
it names. **That URL is a 404 until the project moves to its final home**, so today you have to
point the provider at a copy you host:

```shell
export PMON_CLIENT_METADATA_URL=https://your-host.example.com/client-metadata.json
```

or, equivalently, in the provider block:

```terraform
provider "pmon" {
  client_metadata_url = "https://your-host.example.com/client-metadata.json"
}
```

To publish a copy: take `client-metadata.json`, set `client_id` to the exact URL it will be served
from, and serve it so every one of these holds. pmon enforces all of them, and rejects the client
outright when one fails.

| Requirement | What it rules out |
| --- | --- |
| `https://`, with a path, no query, no fragment, no `.` or `..` segments | plain HTTP, bare hostnames |
| `Content-Type: application/json` or `application/…+json` | GitHub raw and gist raw, which serve `text/plain` |
| A direct `200`, no redirect | `gist.github.com/…/raw/…`, which 301s to another host |
| `client_id` byte-identical to the URL fetched | a copy moved without editing the field |
| 5 KiB or smaller, answered inside 5 seconds | anything slow; pmon re-fetches on every authorization and caches nothing |
| Resolves to a public address | Tailscale (`100.64.0.0/10`) and every other private range |

GitHub Pages satisfies all of it and introduces no party you are not already trusting with the
source. A repository with the file at its root, Pages serving that branch, gives you
`https://<owner>.github.io/<repo>/client-metadata.json`.

`.github/workflows/pages.yml` deploys this repository's own copy, and is manual-only: it would
otherwise publish a document naming a `client_id` that does not match wherever this fork lives.

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

A personal token is still the wrong credential for unattended CI: refresh tokens are short-lived
and rotate on every use, so two concurrent runs would invalidate each other. Unattended runs
authenticate as a workload instead.

### Running in HCP Terraform

An HCP Terraform run authenticates as a service principal, with no person and no stored secret.
HCP Terraform signs a short-lived OIDC token for each plan and apply, the provider exchanges it at
pmon's `/api/workload/exchange` for an MCP access token, and pmon maps it to a principal through a
workload binding.

1. On pmon, trust HCP Terraform as an issuer: `PM_CI_OIDC_ISSUERS` gets an entry such as
   `hcp-terraform=https://app.terraform.io` (pmon recognizes the host; for a Terraform Enterprise
   host, also set `PM_WORKLOAD_ISSUER_KINDS=hcp-terraform=hcp-terraform`).
2. In the workspace, set the variable `TFC_WORKLOAD_IDENTITY_AUDIENCE_PMON` to pmon's audience,
   which is `PM_CI_OIDC_AUDIENCE` or, by default, pmon's public origin
   (`https://pmon.example.com`). HCP Terraform then injects `TFC_WORKLOAD_IDENTITY_TOKEN_PMON` into
   each run, and the provider picks it up. The untagged `TFC_WORKLOAD_IDENTITY_AUDIENCE` and
   `TFC_WORKLOAD_IDENTITY_TOKEN` work too; `PMON_WORKLOAD_IDENTITY_TAG` picks another tag.
3. Bind each run phase. The token's `sub` is
   `organization:<org>:project:<project>:workspace:<workspace>:run_phase:<plan|apply>`, so a plan
   binding can carry read scopes only while the apply binding carries the write scopes, both
   acting as the same principal:

```terraform
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

resource "pmon_role_assignment" "tf_pmon" {
  principal = "svc:tf-pmon"
  role_name = "pmon-admin"
}
```

Create the bindings once from a workstation (the provider logs you in with a browser there), then
let the workspace manage itself. The scopes are still only a ceiling: pmon resolves the
principal's roles and evaluates Cedar on every call, and the audit trail names `svc:tf-pmon`.

Each token exchanges once, and the access token lasts the shorter of the binding's `ttl_seconds`
and pmon's MCP access token lifetime, with no refresh. Provider processes running at the same
time share the exchanged token through a file in the temporary directory, readable only by its
owner (mode 0600) and keyed by the token. When the provider process exits it revokes the access
token at pmon's `/oauth/revoke` and deletes that file, so no usable credential outlives it; a
cache file found expired is deleted too. HCP Terraform issues a fresh token for the plan and the
apply phase, so each phase exchanges its own. A single command that walks the graph twice with one
token, such as a local `terraform apply` without a saved plan, cannot exchange it a second time:
fetch a fresh token for `terraform plan -out` and for `terraform apply` of that plan. Any other CI
works the same way with `PMON_WORKLOAD_IDENTITY_TOKEN` set to a token its platform issued, such as
a GitHub Actions OIDC token bound by its `repo:<owner>/<repo>:environment:<env>` subject.

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
