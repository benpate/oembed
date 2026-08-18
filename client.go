package oembed

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/benpate/derp"
	"github.com/benpate/remote"
)

// Client resolves target URLs into validated oEmbed metadata. Construct it
// with NewClient; the zero-config NewClient() works well, using the embedded
// provider registry and safe network defaults.
type Client struct {
	registry        Registry
	maxWidth        int
	maxHeight       int
	allowPrivateIPs bool
	maxBodySize     int64
	userAgent       string
	roundTripper    func(http.RoundTripper) http.RoundTripper
}

// NewClient returns a Client configured by the given options. With no options
// it uses the embedded provider registry, blocks non-public IP addresses, and
// caps response bodies at DefaultMaxBodySize.
func NewClient(options ...ClientOption) Client {

	// Defaults first, so an option can override any of them.
	result := Client{
		registry:    defaultRegistry,
		maxBodySize: DefaultMaxBodySize,
		userAgent:   defaultUserAgent,
	}

	for _, option := range options {
		option(&result)
	}

	return result
}

/******************************************
 * Resolution Pipeline
 ******************************************/

// Fetch resolves a target URL into validated oEmbed metadata: it tries a
// provider registry match first (no page fetch needed), falls back to
// fetching the page and running HTML discovery, calls the resolved endpoint,
// and validates the parsed response.
func (client Client) Fetch(ctx context.Context, targetURL string) (Response, error) {

	const location = "oembed.Client.Fetch"

	// RULE: the target must be a fetchable http(s) URL
	if err := validateHTTPURL(targetURL); err != nil {
		return Response{}, derp.Wrap(err, location, "Invalid target URL", targetURL)
	}

	// Try the provider registry first — no page fetch needed on a hit.
	if endpoint, found := client.registry.Find(targetURL); found {
		return client.fetchEndpoint(ctx, targetURL, endpoint)
	}

	// On a registry miss, fetch the page and run HTML discovery.
	endpoints, err := client.discoverEndpoints(ctx, targetURL)

	if err != nil {
		return Response{}, derp.Wrap(err, location, "Discovering oEmbed endpoint", targetURL)
	}

	// Neither the registry nor discovery produced an endpoint. Callers test
	// for this case with derp.IsNotFound.
	if len(endpoints) == 0 {
		return Response{}, derp.NotFound(location, "No oEmbed endpoint found", targetURL)
	}

	// Call the preferred (first) discovered endpoint.
	return client.fetchEndpoint(ctx, targetURL, endpoints[0])
}

// FetchHTML resolves an oEmbed response for pageURL using a response the
// caller already holds, so no page fetch is needed. It behaves exactly like
// Fetch — provider registry, then the Link header, then the discovery links
// in the HTML — except that the response comes from the caller instead of the
// network. pageURL does double duty: relative discovery references resolve
// against it, and it is the target URL sent to the endpoint.
//
// Pass the response's own header so Link-header discovery works; a nil or
// empty http.Header is fine and simply skips that step.
//
// This is the whole resolution pipeline in one step, for callers (crawlers,
// link-preview services) that fetched the page already.
func (client Client) FetchHTML(ctx context.Context, pageURL string, header http.Header, reader io.Reader) (Response, error) {

	const location = "oembed.Client.FetchHTML"

	// RULE: the page URL must be a usable http(s) URL — the endpoint request
	// carries it, and relative discovery references resolve against it.
	if err := validateHTTPURL(pageURL); err != nil {
		return Response{}, derp.Wrap(err, location, "Invalid page URL", pageURL)
	}

	// Try the provider registry first, exactly as Fetch does. Several major
	// providers (Vimeo, Spotify, TikTok) publish no discovery links at all,
	// so their pages are only resolvable this way.
	if endpoint, found := client.registry.Find(pageURL); found {
		return client.fetchEndpoint(ctx, pageURL, endpoint)
	}

	// RULE: an endpoint advertised in the Link header wins outright, as it
	// does in Fetch.
	if headerEndpoints, err := discoverLinkHeader(header, pageURL); err == nil {
		if len(headerEndpoints) > 0 {
			return client.fetchEndpoint(ctx, pageURL, headerEndpoints[0])
		}
	}

	// On a miss, read the endpoints advertised in the supplied HTML.
	endpoints, err := discover(reader, pageURL)

	// Partial results from a malformed page still count; only a barren error fails.
	if len(endpoints) == 0 {

		if err != nil {
			return Response{}, derp.Wrap(err, location, "Parsing HTML for discovery", pageURL)
		}

		// Callers test for this case with derp.IsNotFound.
		return Response{}, derp.NotFound(location, "No oEmbed endpoint found", pageURL)
	}

	// Call the preferred (first) discovered endpoint.
	return client.fetchEndpoint(ctx, pageURL, endpoints[0])
}

// fetchEndpoint calls a resolved oEmbed endpoint for the given target URL,
// parses the response by the endpoint's format, and validates the result.
// It is the shared tail of both Fetch and FetchHTML, whichever way the
// endpoint was resolved.
func (client Client) fetchEndpoint(ctx context.Context, targetURL string, endpoint Endpoint) (Response, error) {

	const location = "oembed.Client.fetchEndpoint"

	// Default the response format to JSON when the endpoint doesn't say.
	format := endpoint.Format

	if format == "" {
		format = FormatJSON
	}

	if format != FormatJSON && format != FormatXML {
		return Response{}, derp.BadRequest(location, "Unrecognized endpoint format", format)
	}

	// Compose the endpoint request URL.
	requestURL, err := buildRequestURL(endpoint, format, targetURL, client.maxWidth, client.maxHeight)

	if err != nil {
		return Response{}, derp.Wrap(err, location, "Building request URL", endpoint.URL)
	}

	// Call the endpoint (SSRF-guarded, size-capped), keeping the raw body so
	// content-type discipline stays in our hands.
	var body []byte

	transaction := client.transaction(ctx, requestURL).
		Accept(acceptHeaderForFormat(format)).
		Result(&body)

	if err := transaction.Send(); err != nil {
		return Response{}, mapProviderError(err, targetURL)
	}

	// RULE: the response Content-Type must agree with the requested format —
	// refuse to guess when they contradict (no content sniffing).
	if contentType := normalizeContentType(transaction.ResponseContentType()); !contentTypeMatchesFormat(contentType, format) {
		return Response{}, derp.BadGateway(location, "Response Content-Type contradicts the requested format", contentType, format)
	}

	// Parse the response body by the endpoint's declared format.
	result, err := parseResponseBody(body, format)

	if err != nil {
		return Response{}, derp.Wrap(err, location, "Parsing oEmbed response", endpoint.URL)
	}

	// RULE: the response must be usable — but per Postel's law the receive-side
	// check is forgiving; strict spec validation (Validate) is for documents we author.
	if err := result.validateReceived(); err != nil {
		return Response{}, derp.Wrap(err, location, "Unusable oEmbed response", endpoint.URL)
	}

	// Ta-da!
	return result, nil
}

// discoverEndpoints fetches the target page and extracts the oEmbed endpoints
// advertised in its HTML head.
func (client Client) discoverEndpoints(ctx context.Context, targetURL string) ([]Endpoint, error) {

	const location = "oembed.Client.discoverEndpoints"

	// Fetch the page (SSRF-guarded, size-capped).
	var body []byte

	transaction := client.transaction(ctx, targetURL).
		Accept(remote.ContentTypeHTML).
		Result(&body)

	if err := transaction.Send(); err != nil {
		return nil, derp.Wrap(err, location, "Fetching page for discovery", targetURL)
	}

	// Resolve relative hrefs against the page's final URL (after redirects).
	baseURL := targetURL

	if response := transaction.Response(); response != nil {
		if response.Request != nil {
			if response.Request.URL != nil {
				baseURL = response.Request.URL.String()
			}
		}
	}

	// RULE: an endpoint advertised in the Link header wins outright. It is the
	// explicit answer, and a resource with no HTML head has no other way to
	// advertise one.
	if response := transaction.Response(); response != nil {

		if endpoints, err := discoverLinkHeader(response.Header, baseURL); err == nil {
			if len(endpoints) > 0 {
				return endpoints, nil
			}
		}
	}

	// Otherwise, extract the advertised endpoints from the page head.
	endpoints, err := discover(bytes.NewReader(body), baseURL)

	// Partial results from a malformed page still count; only a barren error fails.
	if len(endpoints) > 0 {
		return endpoints, nil
	}

	if err != nil {
		return nil, derp.Wrap(err, location, "Parsing page for discovery", targetURL)
	}

	return endpoints, nil
}

// transaction assembles the SSRF-guarded, size-capped GET request every
// client fetch goes through.
func (client Client) transaction(ctx context.Context, requestURL string) *remote.Transaction {

	transaction := remote.Get(requestURL).
		WithContext(ctx).
		UserAgent(client.userAgent).
		AllowPrivateIPs(client.allowPrivateIPs).
		MaxResponseSize(client.maxBodySize)

	if client.roundTripper != nil {
		transaction.WithRoundTripper(client.roundTripper)
	}

	return transaction
}

/******************************************
 * Request Building
 ******************************************/

// buildRequestURL composes the endpoint call per oEmbed 1.0 §2.2, merging
// parameters into any query string the endpoint URL already carries (common
// for discovered endpoints) and always URL-encoding values.
func buildRequestURL(endpoint Endpoint, format string, targetURL string, maxWidth int, maxHeight int) (string, error) {

	const location = "oembed.buildRequestURL"

	// RULE: only http(s) endpoints may be fetched
	if err := validateHTTPURL(endpoint.URL); err != nil {
		return "", derp.Wrap(err, location, "Invalid endpoint URL", endpoint.URL)
	}

	parsed, err := url.Parse(endpoint.URL)

	if err != nil {
		return "", derp.Wrap(err, location, "Parsing endpoint URL", endpoint.URL)
	}

	query := parsed.Query()

	// The "url" parameter is required — but a discovered endpoint may already
	// carry the provider's (possibly canonicalized) value, which wins.
	if query.Get("url") == "" {
		query.Set("url", targetURL)
	}

	// Pass maxwidth/maxheight when the client has defaults for them.
	if maxWidth > 0 {
		query.Set("maxwidth", strconv.Itoa(maxWidth))
	}

	if maxHeight > 0 {
		query.Set("maxheight", strconv.Itoa(maxHeight))
	}

	// Pass "format" only when the endpoint says the parameter is understood.
	if endpoint.AddFormatParameter {
		query.Set("format", format)
	}

	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

// validateHTTPURL confirms that a URL is absolute, has a host, and uses the
// http or https scheme — the only schemes this client will fetch.
func validateHTTPURL(rawURL string) error {

	const location = "oembed.validateHTTPURL"

	parsed, err := url.Parse(rawURL)

	if err != nil {
		return derp.Wrap(err, location, "Unparseable URL", rawURL)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return derp.BadRequest(location, "URL scheme must be http or https", rawURL)
	}

	if parsed.Host == "" {
		return derp.BadRequest(location, "URL must include a host", rawURL)
	}

	return nil
}

/******************************************
 * Response Handling
 ******************************************/

// parseResponseBody unmarshals an endpoint response by its declared format.
func parseResponseBody(body []byte, format string) (Response, error) {

	const location = "oembed.parseResponseBody"

	result := Response{}

	switch format {

	case FormatJSON:

		if err := json.Unmarshal(body, &result); err != nil {
			return Response{}, derp.Wrap(err, location, "Parsing JSON response")
		}

	case FormatXML:

		if err := xml.Unmarshal(body, &result); err != nil {
			return Response{}, derp.Wrap(err, location, "Parsing XML response")
		}
	}

	return result, nil
}

// acceptHeaderForFormat returns the Accept header value for a response format.
func acceptHeaderForFormat(format string) string {

	if format == FormatXML {
		return remote.ContentTypeXML
	}

	return remote.ContentTypeJSON
}

// normalizeContentType lowercases a Content-Type header and strips any
// parameters (such as "; charset=utf-8").
func normalizeContentType(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// contentTypeMatchesFormat reports whether a normalized media type is an
// acceptable label for the requested oEmbed format.
func contentTypeMatchesFormat(contentType string, format string) bool {

	switch format {

	case FormatJSON:

		switch contentType {
		case remote.ContentTypeJSON, ContentTypeJSONOEmbed, "text/json":
			return true
		}

	case FormatXML:

		switch contentType {
		case remote.ContentTypeXML, ContentTypeXMLOEmbed, "text/xml":
			return true
		}
	}

	return false
}

// mapProviderError translates an endpoint failure into the spec's error
// semantics: 404 means the provider has no oEmbed for the URL, 401 means the
// resource is private, 501 means the format is unsupported. The original
// error (and its status code) stays in the chain.
func mapProviderError(err error, targetURL string) error {

	const location = "oembed.mapProviderError"

	switch derp.ErrorCode(err) {

	case http.StatusNotFound:
		return derp.Wrap(err, location, "Provider has no oEmbed response for this URL", targetURL)

	case http.StatusUnauthorized:
		return derp.Wrap(err, location, "The requested resource is private", targetURL)

	case http.StatusNotImplemented:
		return derp.Wrap(err, location, "Provider does not support the requested format", targetURL)
	}

	return derp.Wrap(err, location, "Error calling oEmbed endpoint", targetURL)
}
