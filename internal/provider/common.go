package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

// clientFromProviderData unwraps the client the provider's Configure stored. The framework calls
// Configure with nil provider data during early graph walks, which is not an error: it means the
// provider is not configured yet and the caller should do nothing.
func clientFromProviderData(providerData any) (*pmonmcp.Client, diag.Diagnostics) {
	var diags diag.Diagnostics

	if providerData == nil {
		return nil, diags
	}

	client, ok := providerData.(*pmonmcp.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			"The pmon provider passed an unexpected type to a resource or data source. This is a bug in the provider.",
		)
		return nil, diags
	}
	return client, diags
}

// notFoundDetail explains a singular lookup that matched nothing. A plural data source answers
// with an empty list, but a reference to one specific object that is not there is a configuration
// mistake, and the plan should stop where it can still be corrected.
func notFoundDetail(kind, name, listDataSource string) string {
	return "pmon has no " + kind + " named " + name + ". Use the " + listDataSource +
		" data source to see what exists."
}

// errNoDatasource reads as not found to isNotFound, so clearing tags of a removed datasource is a no-op.
var errNoDatasource = errors.New("no such datasource")

// datasourceCatalog returns the one catalog pmon accepts on column calls for this datasource.
func datasourceCatalog(ctx context.Context, client *pmonmcp.Client, name string) (string, error) {
	datasource, exists, err := client.Listings().Datasource(ctx, name)
	if err != nil {
		return "", fmt.Errorf("listing datasources: %w", err)
	}
	return catalogOf(name, datasource, exists)
}

func catalogOf(name string, datasource pmonmcp.Datasource, exists bool) (string, error) {
	if !exists {
		return "", fmt.Errorf("%w: %s", errNoDatasource, notFoundDetail("datasource", name, "pmon_datasources"))
	}
	if datasource.CurrentCatalog == nil || strings.TrimSpace(*datasource.CurrentCatalog) == "" {
		return "", fmt.Errorf("datasource %s has not pushed a catalog yet, so pmon cannot place its columns", name)
	}
	return *datasource.CurrentCatalog, nil
}
