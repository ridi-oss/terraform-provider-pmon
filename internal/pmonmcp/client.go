// Package pmonmcp is a typed client for pmon's MCP administration endpoint.
//
// pmon exposes management as MCP tools rather than REST, and the tools are name-keyed: a role is
// Role::"analyst", not role id 42. That is what makes them a better fit for Terraform state than
// the id-keyed REST surface, and it is why this package speaks MCP directly.
package pmonmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures a Client.
type Options struct {
	// Endpoint is the pmon MCP endpoint.
	Endpoint string
	// OAuth authorizes requests. A nil handler means an unauthenticated session, which pmon
	// accepts only when it is running with its development auth bypass.
	OAuth auth.OAuthHandler
	// Version is reported to the server as the client implementation version.
	Version string
	// HTTPClient overrides the transport's HTTP client. Nil uses a client with a sane timeout.
	HTTPClient *http.Client
}

// Client is a connected MCP session. It is safe for concurrent use: Terraform walks the resource
// graph with parallelism, and each tool call is an independent HTTP request.
type Client struct {
	session  *mcp.ClientSession
	endpoint string
	listings *Listings
}

// Connect opens and initializes an MCP session.
func Connect(ctx context.Context, opts Options) (*Client, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("pmonmcp: endpoint is required")
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	version := opts.Version
	if version == "" {
		version = "dev"
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   opts.Endpoint,
		HTTPClient: httpClient,
		// Terraform only ever makes request-response calls. Without this the client would also
		// hold a standalone SSE stream open for server-initiated notifications it never reads.
		DisableStandaloneSSE: true,
		OAuthHandler:         opts.OAuth,
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "terraform-provider-pmon",
		Version: version,
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to the pmon MCP endpoint %s: %w", opts.Endpoint, err)
	}

	connected := &Client{session: session, endpoint: opts.Endpoint}
	connected.listings = NewListings(connected)
	return connected, nil
}

// Listings returns the process-lifetime cache over this client's list tools.
func (c *Client) Listings() *Listings {
	return c.listings
}

// Endpoint reports the endpoint this client is connected to.
func (c *Client) Endpoint() string { return c.endpoint }

// Close ends the MCP session.
func (c *Client) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}

// Call invokes a tool and decodes its result into T. Go has no generic methods, so this is a
// function taking the client rather than a method on it.
func Call[T any](ctx context.Context, c *Client, tool string, args any) (T, error) {
	var out T

	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return out, fmt.Errorf("calling %s: %w", tool, err)
	}

	payload := resultPayload(result)

	if result.IsError {
		return out, newToolError(tool, payload)
	}

	if len(payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(unwrapResult(payload), &out); err != nil {
		return out, fmt.Errorf("decoding the result of %s: %w", tool, err)
	}
	return out, nil
}

// Do invokes a tool whose result carries nothing worth decoding.
func Do(ctx context.Context, c *Client, tool string, args any) error {
	_, err := Call[json.RawMessage](ctx, c, tool, args)
	return err
}

// resultPayload extracts the tool's JSON. The SDK surfaces it as structured content when the tool
// declares an output schema and as text content otherwise, and pmon's catalog is not uniform, so
// both paths matter.
func resultPayload(result *mcp.CallToolResult) []byte {
	if result.StructuredContent != nil {
		if raw, err := json.Marshal(result.StructuredContent); err == nil {
			return raw
		}
	}

	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return []byte(strings.TrimSpace(text.String()))
}

// unwrapResult peels pmon's {"result": ...} envelope. A payload without one is returned as is,
// so a tool that ever answers bare JSON still decodes rather than silently yielding a zero value.
func unwrapResult(payload []byte) []byte {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return payload
	}
	if inner, ok := envelope["result"]; ok {
		return inner
	}
	return payload
}
