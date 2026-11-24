package provider

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/shumoku-dev/terraform-provider-shumoku/internal/client"
)

// addAPIError turns a client error into a diagnostic. 401 and 403 get
// actionable auth messages; every API error carries the server's message and requestId.
func addAPIError(diags *diag.Diagnostics, action string, err error) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		diags.AddError("Shumoku API request failed", fmt.Sprintf("Unable to %s: %s", action, err))
		return
	}
	msg := apiErr.Message
	if msg == "" {
		msg = apiErr.Legacy
	}
	if msg == "" {
		msg = apiErr.Body
	}
	detail := fmt.Sprintf("Unable to %s.\n\n%s %s returned HTTP %d", action, apiErr.Method, apiErr.Path, apiErr.StatusCode)
	if apiErr.Code != "" {
		detail += " (" + apiErr.Code + ")"
	}
	if msg != "" {
		detail += ": " + msg
	}
	if apiErr.RequestID != "" {
		detail += "\nRequest ID: " + apiErr.RequestID
	}

	switch apiErr.StatusCode {
	case http.StatusUnauthorized:
		diags.AddError("Shumoku authentication failed",
			detail+"\n\nThe server rejected the API token: it may be invalid, expired or revoked. "+
				"Check the provider's token attribute or the SHUMOKU_TOKEN environment variable.")
	case http.StatusForbidden:
		diags.AddError("Shumoku permission denied",
			detail+"\n\nThe API token is valid but its role does not allow this. Data sources, credentials, "+
				"settings and topology source attachments require the admin role (admin:manage).")
	case http.StatusServiceUnavailable:
		diags.AddError("Shumoku Server unavailable", detail+
			"\n\nIf the message says administrator setup is required, finish the server's initial setup first.")
	default:
		diags.AddError("Shumoku API error", detail)
	}
}

func clientFromResource(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return nil
	}
	return c
}

func clientFromDataSource(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return nil
	}
	return c
}

func strPtr(s string) *string { return &s }
