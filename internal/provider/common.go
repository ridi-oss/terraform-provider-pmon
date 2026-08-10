package provider

import (
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
