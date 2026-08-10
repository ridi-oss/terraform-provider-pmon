package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &usersDataSource{}
	_ datasource.DataSourceWithConfigure = &usersDataSource{}
)

// NewUsersDataSource returns the data source listing every locally known principal.
func NewUsersDataSource() datasource.DataSource { return &usersDataSource{} }

type usersDataSource struct {
	client *pmonmcp.Client
}

type userEntryModel struct {
	ID         types.Int64  `tfsdk:"id"`
	Principal  types.String `tfsdk:"principal"`
	Email      types.String `tfsdk:"email"`
	Source     types.String `tfsdk:"source"`
	Active     types.Bool   `tfsdk:"active"`
	CreatedAt  types.String `tfsdk:"created_at"`
	GroupNames []string     `tfsdk:"group_names"`
}

type usersModel struct {
	Users []userEntryModel `tfsdk:"users"`
}

func (d *usersDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

func (d *usersDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every locally known principal and the groups it belongs to.\n\n" +
			"Group membership is only visible from this side: a group listing reports how many members " +
			"it has, never which.\n\n" +
			"**This writes every principal and email address into Terraform state.** Treat the state " +
			"file accordingly, and prefer `pmon_groups` when you only need entitlements.",
		Attributes: map[string]schema.Attribute{
			"users": schema.ListNestedAttribute{
				MarkdownDescription: "The users, as pmon reports them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							MarkdownDescription: "pmon's internal id.",
							Computed:            true,
						},
						"principal": schema.StringAttribute{
							MarkdownDescription: "Principal as pmon knows it, usually an email address.",
							Computed:            true,
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "Email address.",
							Computed:            true,
						},
						"source": schema.StringAttribute{
							MarkdownDescription: "`OIDC` for a principal provisioned at login, `LOCAL` otherwise.",
							Computed:            true,
						},
						"active": schema.BoolAttribute{
							MarkdownDescription: "Whether the principal may authenticate. A deprovisioned " +
								"user stays listed with this false.",
							Computed: true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "When pmon first saw the principal.",
							Computed:            true,
						},
						"group_names": schema.ListAttribute{
							MarkdownDescription: "Groups the principal belongs to, sorted.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *usersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	found, err := d.client.Listings().Users(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon users", err.Error())
		return
	}

	state := usersModel{Users: make([]userEntryModel, 0, len(found))}
	for _, user := range found {
		groups := make([]string, 0, len(user.Groups))
		for _, group := range user.Groups {
			groups = append(groups, group.Name)
		}
		sort.Strings(groups)

		state.Users = append(state.Users, userEntryModel{
			ID:         types.Int64Value(user.ID),
			Principal:  types.StringValue(user.Principal),
			Email:      stringOrNull(user.Email),
			Source:     types.StringValue(user.Source),
			Active:     types.BoolValue(user.Active),
			CreatedAt:  stringOrNull(user.CreatedAt),
			GroupNames: groups,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
