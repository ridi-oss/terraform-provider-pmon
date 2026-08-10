package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &policySchemaDataSource{}
	_ datasource.DataSourceWithConfigure = &policySchemaDataSource{}
)

// NewPolicySchemaDataSource returns the data source exposing pmon's Cedar schema.
func NewPolicySchemaDataSource() datasource.DataSource { return &policySchemaDataSource{} }

type policySchemaDataSource struct {
	client *pmonmcp.Client
}

type policySchemaModel struct {
	Schema types.String `tfsdk:"schema"`
}

func (d *policySchemaDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_schema"
}

func (d *policySchemaDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Cedar schema every policy is validated against. It names the entity " +
			"types and actions a policy may reference, and is the reference for writing `cedar_src`.",
		Attributes: map[string]schema.Attribute{
			"schema": schema.StringAttribute{
				MarkdownDescription: "The Cedar schema source.",
				Computed:            true,
			},
		},
	}
}

func (d *policySchemaDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *policySchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	result, err := pmonmcp.Call[pmonmcp.PolicySchema](ctx, d.client, "get_policy_schema", map[string]any{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the pmon Cedar schema", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &policySchemaModel{
		Schema: types.StringValue(result.Schema),
	})...)
}
