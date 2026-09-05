// Package dataiku is a small client for the Dataiku DSS public REST API.
//
// The API is rooted at <host>/public/api/ and authenticates with an API key
// sent as the username of an HTTP Basic credential with an empty password,
// which is what the official Dataiku clients do.
package dataiku

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiPrefix        = "/public/api"
	defaultUserAgent = "terraform-provider-dataiku"
)

// Client talks to a single DSS instance.
type Client struct {
	baseURL   *url.URL
	apiKey    string
	userAgent string
	http      *http.Client
}

// Config holds the options needed to build a Client.
type Config struct {
	// Host is the base URL of the DSS instance, e.g. https://dss.example.com.
	// A /public/api suffix is optional and stripped if present.
	Host string
	// APIKey is a DSS personal or global API key.
	APIKey string
	// Insecure disables TLS certificate verification.
	Insecure bool
	// Timeout bounds each individual request. Defaults to 60s.
	Timeout time.Duration
	// UserAgent is appended to the default user agent string.
	UserAgent string
}

// NewClient validates cfg and returns a ready-to-use Client.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("host must not be empty")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("api_key must not be empty")
	}

	host := strings.TrimSuffix(strings.TrimSpace(cfg.Host), "/")
	// Accept a host that already points at the API root.
	host = strings.TrimSuffix(host, apiPrefix)

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("host %q is not a valid URL: %w", cfg.Host, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("host %q must use the http or https scheme", cfg.Host)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("host %q is missing a hostname", cfg.Host)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	ua := defaultUserAgent
	if cfg.UserAgent != "" {
		ua = cfg.UserAgent + " " + defaultUserAgent
	}

	return &Client{
		baseURL:   u,
		apiKey:    cfg.APIKey,
		userAgent: ua,
		http:      &http.Client{Timeout: timeout, Transport: transport},
	}, nil
}

// Host returns the instance URL the client is bound to.
func (c *Client) Host() string { return c.baseURL.String() }

// do performs an API call. body, when non-nil, is JSON-encoded into the
// request. out, when non-nil, receives the JSON-decoded response.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	endpoint := *c.baseURL
	// path arrives with its dynamic segments already percent-escaped, so it is
	// the raw form. Assigning it to Path alone would escape the percent signs a
	// second time, so set both and let URL.String use RawPath verbatim.
	rawPath := strings.TrimSuffix(endpoint.Path, "/") + apiPrefix + path
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return fmt.Errorf("building request for %s %s: %w", method, path, err)
	}
	endpoint.Path = decodedPath
	endpoint.RawPath = rawPath
	if len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("encoding request body for %s %s: %w", method, path, marshalErr)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return fmt.Errorf("building request for %s %s: %w", method, path, err)
	}
	// DSS expects the API key as the basic-auth username with an empty password.
	req.SetBasicAuth(c.apiKey, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s %s: %w", method, path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(method, path, resp)
	}

	if out == nil {
		return nil
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response of %s %s: %w", method, path, err)
	}
	// Some DSS endpoints answer 200 with an empty body.
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding response of %s %s: %w (body: %s)", method, path, err, truncate(string(raw), 512))
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) post(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.do(ctx, http.MethodPost, path, query, body, out)
}

func (c *Client) put(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.do(ctx, http.MethodPut, path, query, body, out)
}

func (c *Client) delete(ctx context.Context, path string, query url.Values) error {
	return c.do(ctx, http.MethodDelete, path, query, nil, nil)
}

// CheckConnectivity issues a cheap authenticated call so that provider
// configuration fails fast on a bad host or key.
func (c *Client) CheckConnectivity(ctx context.Context) error {
	var out map[string]any
	return c.get(ctx, "/auth/info", nil, &out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
