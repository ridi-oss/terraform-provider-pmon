package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &groupDataSource{}
	_ datasource.DataSourceWithConfigure = &groupDataSource{}
)

// NewGroupDataSource returns the data source reading one group by name.
func NewGroupDataSource() datasource.DataSource { return &groupDataSource{} }

type groupDataSource struct {
	client *pmonmcp.Client
}

type groupDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	ID          types.Int64  `tfsdk:"id"`
	Description types.String `tfsdk:"description"`
	Source      types.String `tfsdk:"source"`
	MemberCount types.Int64  `tfsdk:"member_count"`
	RoleNames   []string     `tfsdk:"role_names"`
}

func (d *groupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One identity group, by name.\n\n" +
			"Reading an `OIDC` group here is the usual way to attach `pmon_group_roles` to a team the " +
			"IdP populates: the group exists already, so it is looked up rather than created.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Group name.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the group is for.",
				Computed:            true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "`LOCAL`, `OIDC` for one provisioned from the IdP group claim, or " +
					"`SYSTEM` for a shipped group.",
				Computed: true,
			},
			"member_count": schema.Int64Attribute{
				MarkdownDescription: "How many members the group has. Which principals those are is only " +
					"visible through `pmon_users`.",
				Computed: true,
			},
			"role_names": schema.ListAttribute{
				MarkdownDescription: "Roles the group carries, sorted.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (d *groupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state groupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	group, found, err := d.client.Listings().Group(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon groups", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"No such pmon group",
			notFoundDetail("group", name, "pmon_groups"),
		)
		return
	}

	state.ID = types.Int64Value(group.ID)
	state.Description = stringOrNull(group.Description)
	state.Source = types.StringValue(group.Source)
	state.MemberCount = types.Int64Value(group.MemberCount)
	state.RoleNames = sortedCopy(group.RoleNames())

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
