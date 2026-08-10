package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &rolesDataSource{}
	_ datasource.DataSourceWithConfigure = &rolesDataSource{}
)

// NewRolesDataSource returns the data source listing every access-control role.
func NewRolesDataSource() datasource.DataSource { return &rolesDataSource{} }

type rolesDataSource struct {
	client *pmonmcp.Client
}

type roleEntryModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type rolesModel struct {
	Roles []roleEntryModel `tfsdk:"roles"`
}

func (d *rolesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every access-control role, including the shipped `system:` ones that " +
			"cannot be managed as resources. A role grants nothing by itself; policies decide what " +
			"holding it means.",
		Attributes: map[string]schema.Attribute{
			"roles": schema.ListNestedAttribute{
				MarkdownDescription: "The roles, as pmon reports them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							MarkdownDescription: "pmon's internal id.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Role name, as Cedar refers to it: `Role::\"<name>\"`.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "What holding the role means.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *rolesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *rolesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	found, err := d.client.Listings().Roles(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon roles", err.Error())
		return
	}

	state := rolesModel{Roles: make([]roleEntryModel, 0, len(found))}
	for _, role := range found {
		state.Roles = append(state.Roles, roleEntryModel{
			ID:          types.Int64Value(role.ID),
			Name:        types.StringValue(role.Name),
			Description: stringOrNull(role.Description),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
