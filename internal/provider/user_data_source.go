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
	_ datasource.DataSource              = &userDataSource{}
	_ datasource.DataSourceWithConfigure = &userDataSource{}
)

// NewUserDataSource returns the data source reading one principal.
func NewUserDataSource() datasource.DataSource { return &userDataSource{} }

type userDataSource struct {
	client *pmonmcp.Client
}

type userDataSourceModel struct {
	Principal  types.String `tfsdk:"principal"`
	ID         types.Int64  `tfsdk:"id"`
	Email      types.String `tfsdk:"email"`
	Source     types.String `tfsdk:"source"`
	Active     types.Bool   `tfsdk:"active"`
	CreatedAt  types.String `tfsdk:"created_at"`
	GroupNames []string     `tfsdk:"group_names"`
}

func (d *userDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One principal and the groups it belongs to.\n\n" +
			"Cheaper than `pmon_users` for checking one person, and it puts one principal in state " +
			"rather than the whole directory.\n\n" +
			"pmon provisions a principal from the IdP on first login, so a colleague who has never " +
			"signed in is not here yet and this fails rather than silently resolving to nothing.",
		Attributes: map[string]schema.Attribute{
			"principal": schema.StringAttribute{
				MarkdownDescription: "Principal as pmon knows it, usually an email address.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
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
				MarkdownDescription: "Whether the principal may authenticate.",
				Computed:            true,
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
	}
}

func (d *userDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state userDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	principal := state.Principal.ValueString()
	user, found, err := d.client.Listings().User(ctx, principal)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon users", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"No such pmon principal",
			"pmon does not know a principal named "+principal+". It provisions one from the IdP on "+
				"first login, so somebody who has never signed in is not listed yet. Use the pmon_users "+
				"data source to see who is.",
		)
		return
	}

	groups := make([]string, 0, len(user.Groups))
	for _, group := range user.Groups {
		groups = append(groups, group.Name)
	}
	sort.Strings(groups)

	state.ID = types.Int64Value(user.ID)
	state.Email = stringOrNull(user.Email)
	state.Source = types.StringValue(user.Source)
	state.Active = types.BoolValue(user.Active)
	state.CreatedAt = stringOrNull(user.CreatedAt)
	state.GroupNames = groups

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
