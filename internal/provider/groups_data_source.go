package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &groupsDataSource{}
	_ datasource.DataSourceWithConfigure = &groupsDataSource{}
)

// NewGroupsDataSource returns the data source listing every identity group.
func NewGroupsDataSource() datasource.DataSource { return &groupsDataSource{} }

type groupsDataSource struct {
	client *pmonmcp.Client
}

type groupEntryModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Source      types.String `tfsdk:"source"`
	MemberCount types.Int64  `tfsdk:"member_count"`
	RoleNames   []string     `tfsdk:"role_names"`
}

type groupsModel struct {
	Groups []groupEntryModel `tfsdk:"groups"`
}

func (d *groupsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_groups"
}

func (d *groupsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every identity group with the roles it carries.\n\n" +
			"This is the fastest way to see who is entitled to what, since almost all access flows " +
			"through groups rather than direct assignments. `member_count` is all pmon reports here; " +
			"use `pmon_users` to see which principals those are.",
		Attributes: map[string]schema.Attribute{
			"groups": schema.ListAttribute{
				MarkdownDescription: "The groups, as pmon reports them. Each object contains " +
					"`id` (pmon's internal id), `name`, `description` (nullable), " +
					"`source` (`LOCAL`, `OIDC`, or `SYSTEM`), `member_count`, and " +
					"`role_names` (a sorted list of strings).",
				Computed: true,
				ElementType: types.ObjectType{AttrTypes: map[string]attr.Type{
					"id":           types.Int64Type,
					"name":         types.StringType,
					"description":  types.StringType,
					"source":       types.StringType,
					"member_count": types.Int64Type,
					"role_names":   types.ListType{ElemType: types.StringType},
				}},
			},
		},
	}
}

func (d *groupsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *groupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	found, err := d.client.Listings().Groups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon groups", err.Error())
		return
	}

	state := groupsModel{Groups: make([]groupEntryModel, 0, len(found))}
	for _, group := range found {
		state.Groups = append(state.Groups, groupEntryModel{
			ID:          types.Int64Value(group.ID),
			Name:        types.StringValue(group.Name),
			Description: stringOrNull(group.Description),
			Source:      types.StringValue(group.Source),
			MemberCount: types.Int64Value(group.MemberCount),
			RoleNames:   sortedCopy(group.RoleNames()),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
