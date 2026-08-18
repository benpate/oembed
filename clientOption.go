package oembed

import (
	"net/http"
)

// ClientOption configures a Client during NewClient.
type ClientOption func(*Client)

// WithRegistry replaces the embedded provider registry, for callers that
// supply a fresher or custom providers.json.
func WithRegistry(registry Registry) ClientOption {
	return func(client *Client) {
		client.registry = registry
	}
}

// WithMaxWidth sets a default maxwidth parameter sent with every endpoint request.
func WithMaxWidth(maxWidth int) ClientOption {
	return func(client *Client) {
		client.maxWidth = maxWidth
	}
}

// WithMaxHeight sets a default maxheight parameter sent with every endpoint request.
func WithMaxHeight(maxHeight int) ClientOption {
	return func(client *Client) {
		client.maxHeight = maxHeight
	}
}

// WithAllowPrivateIPs controls whether fetches may reach non-public IP
// addresses (loopback, private, link-local). The default is FALSE, which
// guards against SSRF; set TRUE only for tests against loopback servers or
// intentional internal calls.
func WithAllowPrivateIPs(allow bool) ClientOption {
	return func(client *Client) {
		client.allowPrivateIPs = allow
	}
}

// WithMaxBodySize caps how many bytes are read from any response body. Values
// of zero or less restore DefaultMaxBodySize.
func WithMaxBodySize(maxBytes int64) ClientOption {
	return func(client *Client) {

		if maxBytes <= 0 {
			maxBytes = DefaultMaxBodySize
		}

		client.maxBodySize = maxBytes
	}
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(userAgent string) ClientOption {
	return func(client *Client) {
		client.userAgent = userAgent
	}
}

// WithRoundTripper wraps the SSRF-hardened base transport with caller-supplied
// middleware (caching, instrumentation, custom headers). The middleware
// receives the base transport as "next" and must delegate to it; the
// private-IP guard stays underneath.
func WithRoundTripper(wrap func(next http.RoundTripper) http.RoundTripper) ClientOption {
	return func(client *Client) {
		client.roundTripper = wrap
	}
}
