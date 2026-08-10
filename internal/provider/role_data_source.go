package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &roleDataSource{}
	_ datasource.DataSourceWithConfigure = &roleDataSource{}
)

// NewRoleDataSource returns the data source reading one role by name.
func NewRoleDataSource() datasource.DataSource { return &roleDataSource{} }

type roleDataSource struct {
	client *pmonmcp.Client
}

type roleDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	ID          types.Int64  `tfsdk:"id"`
	Description types.String `tfsdk:"description"`
	IsSystem    types.Bool   `tfsdk:"is_system"`
}

func (d *roleDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One access-control role, by name.\n\n" +
			"Use it to refer to a shipped `system:` role, which cannot be managed as a resource, " +
			"without hard-coding the string in several places.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Role name.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What holding the role means.",
				Computed:            true,
			},
			"is_system": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a shipped role, which pmon will not let you modify.",
				Computed:            true,
			},
		},
	}
}

func (d *roleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state roleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	role, found, err := d.client.Listings().Role(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon roles", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"No such pmon role",
			notFoundDetail("role", name, "pmon_roles"),
		)
		return
	}

	state.ID = types.Int64Value(role.ID)
	state.Description = stringOrNull(role.Description)
	state.IsSystem = types.BoolValue(isReservedName(role.Name))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
