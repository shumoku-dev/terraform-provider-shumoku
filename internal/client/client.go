// Package client is a small typed HTTP client for the Shumoku Server API.
//
// It covers the endpoints the Terraform provider manages. Types mirror the
// schemas in the server's OpenAPI document (apps/server/api/openapi.json in the
// shumoku repository); only the fields the provider needs are modelled.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds a single API request.
const DefaultTimeout = 60 * time.Second

// Config configures a Client.
type Config struct {
	// Endpoint is the server's base URL, e.g. https://shumoku.example.com.
	// The client appends /api; an endpoint that already ends in /api is accepted.
	Endpoint string
	// Token is the API token sent as "Authorization: Bearer <token>".
	Token string
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
	// UserAgent is sent with every request.
	UserAgent string
	// HTTPClient overrides the HTTP client (tests). When set, InsecureSkipVerify is ignored.
	HTTPClient *http.Client
}

// Client talks to one Shumoku Server.
type Client struct {
	baseURL   *url.URL
	token     string
	userAgent string
	http      *http.Client
}

// New validates the configuration and returns a Client.
func New(cfg Config) (*Client, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("endpoint must not be empty")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid endpoint %q: scheme must be http or https", endpoint)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid endpoint %q: missing host", endpoint)
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/api") {
		path += "/api"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.InsecureSkipVerify {
			// #nosec G402 -- opt-in via provider configuration.
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		httpClient = &http.Client{Transport: transport, Timeout: DefaultTimeout}
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = "terraform-provider-shumoku"
	}
	return &Client{baseURL: u, token: cfg.Token, userAgent: ua, http: httpClient}, nil
}

// BaseURL returns the API base URL (ending in /api).
func (c *Client) BaseURL() string { return c.baseURL.String() }

// APIError is a non-2xx response from the server.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"requestId"`
	// Legacy is the deprecated "error" alias of Message.
	Legacy string `json:"error"`
	// Body holds the raw body when it was not the server's JSON error shape.
	Body string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Legacy
	}
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	s := fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Code != "" {
		s += " " + e.Code
	}
	s += ": " + msg
	if e.RequestID != "" {
		s += " (requestId " + e.RequestID + ")"
	}
	return s
}

// IsNotFound reports whether err is an HTTP 404 from the API.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsUnauthorized reports whether err is an HTTP 401 from the API.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsForbidden reports whether err is an HTTP 403 from the API.
func IsForbidden(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsConflict reports whether err is an HTTP 409 from the API.
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

func hasStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// do sends a request. body (if non-nil) is JSON-encoded; a 2xx response is
// decoded into out (if non-nil). It returns the HTTP status code.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: "/api" + path}
		if jerr := json.Unmarshal(data, apiErr); jerr != nil || (apiErr.Message == "" && apiErr.Legacy == "") {
			apiErr.Body = truncate(string(data), 512)
		}
		return resp.StatusCode, apiErr
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s %s: decoding response: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func pathEscape(s string) string { return url.PathEscape(s) }
