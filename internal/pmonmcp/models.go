package pmonmcp

// The types here mirror what pmon's MCP tools return. Fields pmon omits are pointers so an
// absent value stays distinguishable from an empty one: a datasource whose catalog has never
// synced reports no catalogSyncedAt at all, which is a meaningfully different state from a
// datasource that synced and found nothing.

// Datasource is a brokered datasource. Tags are read-only here by necessity: the proxy pushes
// them over gRPC at registration from its own PM_DATASOURCE_TAGS, and pmon exposes no tool to
// edit them.
type Datasource struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Engine          string   `json:"engine"`
	Host            string   `json:"host"`
	Port            int64    `json:"port"`
	DBName          string   `json:"dbName"`
	Tags            []string `json:"tags"`
	DefaultSchemas  []string `json:"defaultSchemas"`
	EngineVersion   *string  `json:"engineVersion"`
	CatalogSyncedAt *string  `json:"catalogSyncedAt"`
	LastSeenAt      *string  `json:"lastSeenAt"`
}

// Policy is a Cedar policy. Origin is SYSTEM for shipped policies, which are immutable, and USER
// for everything an operator wrote.
type Policy struct {
	ID        int64   `json:"id"`
	Origin    string  `json:"origin"`
	SystemKey *string `json:"systemKey"`
	Name      string  `json:"name"`
	CedarSrc  string  `json:"cedarSrc"`
	Enabled   bool    `json:"enabled"`
	UpdatedBy *string `json:"updatedBy"`
	UpdatedAt *string `json:"updatedAt"`
}

// IsSystem reports whether the policy is a shipped SYSTEM row that pmon refuses to modify.
func (p Policy) IsSystem() bool { return p.Origin == "SYSTEM" }

// PolicySchema is the Cedar schema policies are validated against.
type PolicySchema struct {
	Schema string `json:"schema"`
}

// ValidationResult is the outcome of validate_policy.
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// Role is an access-control role. Cedar attaches these to a principal as graph parents.
type Role struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// RoleRef is a role as it appears nested inside a group or assignment.
type RoleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Group is an identity group. Source is LOCAL for groups managed here, OIDC for groups
// provisioned from the IdP group claim, and SYSTEM for shipped groups pmon will not let you edit.
// MemberCount is the only membership signal a group listing carries; who the members are has to
// come from the user listing.
type Group struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Source      string    `json:"source"`
	MemberCount int64     `json:"memberCount"`
	Roles       []RoleRef `json:"roles"`
}

// IsSystem reports whether the group is a shipped SYSTEM row that pmon refuses to modify.
func (g Group) IsSystem() bool { return g.Source == "SYSTEM" }

// RoleNames returns the group's role names in the order pmon reported them.
func (g Group) RoleNames() []string {
	names := make([]string, 0, len(g.Roles))
	for _, role := range g.Roles {
		names = append(names, role.Name)
	}
	return names
}

// GroupRef is a group as it appears nested inside a user.
type GroupRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// User is a locally known principal. Groups is the authoritative membership view: a group
// listing reports only how many members it has, not which.
type User struct {
	ID        int64      `json:"id"`
	Principal string     `json:"principal"`
	Email     *string    `json:"email"`
	Source    string     `json:"source"`
	Active    bool       `json:"active"`
	CreatedAt *string    `json:"createdAt"`
	Groups    []GroupRef `json:"groups"`
}

// InGroup reports whether the user belongs to the named group.
func (u User) InGroup(name string) bool {
	for _, group := range u.Groups {
		if group.Name == name {
			return true
		}
	}
	return false
}
