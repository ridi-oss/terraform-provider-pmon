package pmonmcp

import (
	"context"
	"sync"
)

// Listings is a per-process cache over pmon's list tools.
//
// Nothing pmon exposes reads a single group or a single user, and membership is only visible from
// the user side, so a plan over N membership resources would otherwise pull the whole user list N
// times. Terraform runs a fresh provider process per command, so caching for the process lifetime
// still gives every command a current view; within one command the graph is walked once and a
// second read of the same object would return the same answer anyway.
type Listings struct {
	client *Client

	mu          sync.Mutex
	roles       []Role
	groups      []Group
	users       []User
	datasources []Datasource
}

// NewListings returns a cache over client.
func NewListings(client *Client) *Listings {
	return &Listings{client: client}
}

// Roles returns every role, listing once per process.
func (l *Listings) Roles(ctx context.Context) ([]Role, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.roles != nil {
		return l.roles, nil
	}
	roles, err := Call[[]Role](ctx, l.client, "list_roles", map[string]any{})
	if err != nil {
		return nil, err
	}
	l.roles = roles
	return roles, nil
}

// Groups returns every group, listing once per process.
func (l *Listings) Groups(ctx context.Context) ([]Group, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.groups != nil {
		return l.groups, nil
	}
	groups, err := Call[[]Group](ctx, l.client, "list_groups", map[string]any{})
	if err != nil {
		return nil, err
	}
	l.groups = groups
	return groups, nil
}

// Users returns every locally known user, listing once per process.
func (l *Listings) Users(ctx context.Context) ([]User, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.users != nil {
		return l.users, nil
	}
	users, err := Call[[]User](ctx, l.client, "list_users", map[string]any{})
	if err != nil {
		return nil, err
	}
	l.users = users
	return users, nil
}

// Invalidate drops the cache. A write invalidates it so a read-after-write in the same process
// does not answer from a stale listing.
func (l *Listings) Invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.roles = nil
	l.groups = nil
	l.users = nil
	l.datasources = nil
}

// Group returns the named group, and whether it exists.
func (l *Listings) Group(ctx context.Context, name string) (Group, bool, error) {
	groups, err := l.Groups(ctx)
	if err != nil {
		return Group{}, false, err
	}
	for _, group := range groups {
		if group.Name == name {
			return group, true, nil
		}
	}
	return Group{}, false, nil
}

// User returns the named principal, and whether it exists.
func (l *Listings) User(ctx context.Context, principal string) (User, bool, error) {
	users, err := l.Users(ctx)
	if err != nil {
		return User{}, false, err
	}
	for _, user := range users {
		if user.Principal == principal {
			return user, true, nil
		}
	}
	return User{}, false, nil
}

// Datasources returns every brokered datasource, listing once per process.
func (l *Listings) Datasources(ctx context.Context) ([]Datasource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.datasources != nil {
		return l.datasources, nil
	}
	datasources, err := Call[[]Datasource](ctx, l.client, "list_datasources", map[string]any{})
	if err != nil {
		return nil, err
	}
	l.datasources = datasources
	return datasources, nil
}

// Datasource returns the named datasource, and whether it exists.
func (l *Listings) Datasource(ctx context.Context, name string) (Datasource, bool, error) {
	datasources, err := l.Datasources(ctx)
	if err != nil {
		return Datasource{}, false, err
	}
	for _, datasource := range datasources {
		if datasource.Name == name {
			return datasource, true, nil
		}
	}
	return Datasource{}, false, nil
}

// Role returns the named role, and whether it exists.
func (l *Listings) Role(ctx context.Context, name string) (Role, bool, error) {
	roles, err := l.Roles(ctx)
	if err != nil {
		return Role{}, false, err
	}
	for _, role := range roles {
		if role.Name == name {
			return role, true, nil
		}
	}
	return Role{}, false, nil
}
