package oembed

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/benpate/derp"
	"github.com/benpate/rosetta/lenient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistry returns a single-provider Registry that routes the given
// scheme pattern to an endpoint URL — pointing registry hits at a test server.
func testRegistry(schemePattern string, endpointURL string, formats ...string) Registry {

	return NewRegistry([]Provider{{
		Name: "Test Provider",
		URL:  "https://example.com/",
		Endpoints: []ProviderEndpoint{{
			Schemes: []string{schemePattern},
			URL:     endpointURL,
			Formats: formats,
		}},
	}})
}

// validVideoJSON returns a spec-valid video response document.
func validVideoJSON() []byte {

	value := NewResponse(TypeVideo)
	value.Title = "Test Video"
	value.HTML = `<iframe src="https://example.com/embed/1"></iframe>`
	value.Width = 640
	value.Height = 360

	result, _ := json.Marshal(value)
	return result
}

func TestClient_Fetch_RegistryHit(t *testing.T) {

	// Endpoint server asserts the composed request and serves a valid response
	var receivedQuery map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		receivedQuery = map[string]string{
			"url":       r.URL.Query().Get("url"),
			"maxwidth":  r.URL.Query().Get("maxwidth"),
			"maxheight": r.URL.Query().Get("maxheight"),
			"format":    r.URL.Query().Get("format"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/video/*", server.URL+"/oembed", "json")),
		WithMaxWidth(300),
		WithMaxHeight(200),
		WithAllowPrivateIPs(true),
	)

	result, err := client.Fetch(context.Background(), "https://example.com/video/1")
	require.NoError(t, err)

	// The response parsed and validated
	assert.Equal(t, TypeVideo, result.Type)
	assert.Equal(t, "Test Video", result.Title)
	assert.Equal(t, lenient.Int64(640), result.Width)

	// The request carried the spec parameters, URL-encoded
	assert.Equal(t, "https://example.com/video/1", receivedQuery["url"])
	assert.Equal(t, "300", receivedQuery["maxwidth"])
	assert.Equal(t, "200", receivedQuery["maxheight"])
	assert.Equal(t, "json", receivedQuery["format"])
}

func TestClient_Fetch_DiscoveryFallback(t *testing.T) {

	// One server plays both the content page and the oEmbed endpoint
	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Relative href: resolution against the page URL is part of the test
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/json+oembed" href="/oembed">
		</head><body>content</body></html>`))
	})

	var receivedTargetURL string

	mux.HandleFunc("/oembed", func(w http.ResponseWriter, r *http.Request) {
		receivedTargetURL = r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	})

	// The default registry has no entry for a loopback URL, so discovery runs
	client := NewClient(WithAllowPrivateIPs(true))

	result, err := client.Fetch(context.Background(), server.URL+"/page")
	require.NoError(t, err)

	assert.Equal(t, TypeVideo, result.Type)
	assert.Equal(t, server.URL+"/page", receivedTargetURL, "endpoint receives the target URL")
}

func TestClient_Fetch_DiscoveryPreservesBakedURL(t *testing.T) {

	// Discovered endpoints often bake in the provider's canonical url parameter
	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<head><link rel="alternate" type="application/json+oembed"
			href="/oembed?url=https%3A%2F%2Fcanonical.example%2F1"></head>`))
	})

	var receivedTargetURL string

	mux.HandleFunc("/oembed", func(w http.ResponseWriter, r *http.Request) {
		receivedTargetURL = r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	})

	client := NewClient(WithAllowPrivateIPs(true))

	_, err := client.Fetch(context.Background(), server.URL+"/page")
	require.NoError(t, err)

	assert.Equal(t, "https://canonical.example/1", receivedTargetURL, "baked-in url parameter wins")
}

func TestClient_Fetch_XMLEndpoint(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		assert.Equal(t, "xml", r.URL.Query().Get("format"))

		response := NewResponse(TypePhoto)
		response.URL = "https://example.com/photo.jpg"
		response.Width = 1024
		response.Height = 683

		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>`))

		data, err := xml.Marshal(response)
		require.NoError(t, err)
		_, _ = w.Write(data)
	}))

	t.Cleanup(server.Close)

	// An XML-only provider forces the XML path end-to-end
	client := NewClient(
		WithRegistry(testRegistry("https://example.com/photo/*", server.URL+"/oembed", "xml")),
		WithAllowPrivateIPs(true),
	)

	result, err := client.Fetch(context.Background(), "https://example.com/photo/1")
	require.NoError(t, err)

	assert.Equal(t, TypePhoto, result.Type)
	assert.Equal(t, lenient.Int64(1024), result.Width)
	assert.Equal(t, "https://example.com/photo.jpg", result.URL)
}

func TestClient_Fetch_ErrorMapping(t *testing.T) {

	test := func(name string, statusCode int, verify func(*testing.T, error)) {
		t.Run(name, func(t *testing.T) {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "provider says no", statusCode)
			}))

			t.Cleanup(server.Close)

			client := NewClient(
				WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
				WithAllowPrivateIPs(true),
			)

			_, err := client.Fetch(context.Background(), "https://example.com/video/1")
			require.Error(t, err)
			verify(t, err)
		})
	}

	// The spec's error semantics survive wrapping
	test("404 not found", http.StatusNotFound, func(t *testing.T, err error) {
		assert.True(t, derp.IsNotFound(err), "expected 404, got %d", derp.ErrorCode(err))
	})

	test("401 private resource", http.StatusUnauthorized, func(t *testing.T, err error) {
		assert.True(t, derp.IsUnauthorized(err), "expected 401, got %d", derp.ErrorCode(err))
	})

	test("501 format unimplemented", http.StatusNotImplemented, func(t *testing.T, err error) {
		assert.True(t, derp.IsNotImplemented(err), "expected 501, got %d", derp.ErrorCode(err))
	})

	test("500 wrapped with status", http.StatusInternalServerError, func(t *testing.T, err error) {
		assert.Equal(t, http.StatusInternalServerError, derp.ErrorCode(err))
	})
}

func TestClient_FetchHTML(t *testing.T) {

	// One endpoint server stands in for the provider throughout
	var receivedTargetURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTargetURL = r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	// An empty registry forces the discovery path unless a test says otherwise
	emptyRegistryClient := NewClient(WithRegistry(NewRegistry(nil)), WithAllowPrivateIPs(true))

	t.Run("discovers and fetches in one step", func(t *testing.T) {

		page := `<html><head>
			<link rel="alternate" type="application/json+oembed" href="` + server.URL + `/oembed">
		</head><body>content</body></html>`

		result, err := emptyRegistryClient.FetchHTML(context.Background(), "https://example.com/page", nil, strings.NewReader(page))
		require.NoError(t, err)

		assert.Equal(t, TypeVideo, result.Type)
		assert.Equal(t, "Test Video", result.Title)
		assert.Equal(t, "https://example.com/page", receivedTargetURL, "endpoint receives the page URL")
	})

	t.Run("relative hrefs resolve against the page URL", func(t *testing.T) {

		// The href is relative, so only the pageURL can complete it
		page := `<head><link rel="alternate" type="application/json+oembed" href="/oembed"></head>`

		_, err := emptyRegistryClient.FetchHTML(context.Background(), server.URL+"/articles/1", nil, strings.NewReader(page))
		require.NoError(t, err)
	})

	t.Run("registry hit ignores the HTML", func(t *testing.T) {

		// Providers like Vimeo publish no discovery links; the registry is the
		// only way to resolve them, so a hit must win over an empty page.
		client := NewClient(
			WithRegistry(testRegistry("https://example.com/video/*", server.URL+"/oembed", "json")),
			WithAllowPrivateIPs(true),
		)

		result, err := client.FetchHTML(context.Background(), "https://example.com/video/1", nil, strings.NewReader("<html><head></head></html>"))
		require.NoError(t, err)

		assert.Equal(t, TypeVideo, result.Type)
	})

	t.Run("no endpoint in the html is not-found", func(t *testing.T) {

		page := `<html><head><title>No oEmbed here</title></head><body></body></html>`

		_, err := emptyRegistryClient.FetchHTML(context.Background(), "https://example.com/page", nil, strings.NewReader(page))

		require.Error(t, err)
		assert.True(t, derp.IsNotFound(err))
	})

	t.Run("empty body is not-found", func(t *testing.T) {

		_, err := emptyRegistryClient.FetchHTML(context.Background(), "https://example.com/page", nil, strings.NewReader(""))

		require.Error(t, err)
		assert.True(t, derp.IsNotFound(err))
	})

	t.Run("invalid page url is rejected before reading", func(t *testing.T) {

		page := `<head><link rel="alternate" type="application/json+oembed" href="` + server.URL + `/oembed"></head>`

		_, err := emptyRegistryClient.FetchHTML(context.Background(), "javascript:alert(1)", nil, strings.NewReader(page))
		assert.Error(t, err)
	})

	t.Run("matches the two-step form", func(t *testing.T) {

		page := `<head><link rel="alternate" type="application/json+oembed" href="` + server.URL + `/oembed"></head>`
		const pageURL = "https://example.com/page"

		// The one-step form must equal Discover + FetchEndpoint exactly
		endpoints, err := discover(strings.NewReader(page), pageURL)
		require.NoError(t, err)
		require.NotEmpty(t, endpoints)

		twoStep, err := emptyRegistryClient.fetchEndpoint(context.Background(), pageURL, endpoints[0])
		require.NoError(t, err)

		oneStep, err := emptyRegistryClient.FetchHTML(context.Background(), pageURL, nil, strings.NewReader(page))
		require.NoError(t, err)

		assert.Equal(t, twoStep, oneStep)
	})
}

func TestClient_Fetch_NoEndpointFound(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>No oEmbed here</title></head><body></body></html>`))
	}))

	t.Cleanup(server.Close)

	client := NewClient(WithAllowPrivateIPs(true))

	_, err := client.Fetch(context.Background(), server.URL+"/page")
	require.Error(t, err)

	// The no-endpoint case is testable by kind
	assert.True(t, derp.IsNotFound(err))
}

func TestClient_Fetch_ContentTypeMismatch(t *testing.T) {

	// Valid JSON labeled text/html must be refused, not sniffed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contradicts")

	// A misbehaving provider is a 502, not a 500 -- the fault is upstream.
	assert.True(t, derp.IsBadGateway(err))
	assert.False(t, derp.IsInternalServerError(err))
}

func TestClient_Fetch_UnparseableResponse(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type": "video"`))
	}))

	t.Cleanup(server.Close)

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	assert.Error(t, err)
}

func TestClient_Fetch_InvalidResponseRejected(t *testing.T) {

	// A parseable but spec-invalid response (photo without url) fails Validate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type": "photo", "version": "1.0", "width": 100, "height": 100}`))
	}))

	t.Cleanup(server.Close)

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	require.Error(t, err)
	assert.True(t, derp.IsValidationError(err), "expected validation error, got %d", derp.ErrorCode(err))
}

func TestClient_Fetch_SSRFGuardBlocksLoopbackByDefault(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the SSRF guard should have blocked this request")
	}))

	t.Cleanup(server.Close)

	// RULE: without WithAllowPrivateIPs(true), loopback fetches must fail
	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	assert.Error(t, err)
}

func TestClient_Fetch_MaxBodySize(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	// A response larger than the cap is refused
	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
		WithMaxBodySize(10),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	assert.Error(t, err)
}

func TestClient_Fetch_RejectsNonHTTPTargets(t *testing.T) {

	client := NewClient()

	test := func(name string, targetURL string) {
		t.Run(name, func(t *testing.T) {
			_, err := client.Fetch(context.Background(), targetURL)
			assert.Error(t, err)
		})
	}

	test("ftp scheme", "ftp://example.com/file")
	test("javascript scheme", "javascript:alert(1)")
	test("file scheme", "file:///etc/passwd")
	test("no host", "https:///path")
	test("relative url", "/just/a/path")
	test("empty url", "")
	test("unparseable url", "http://[::1")
}

func TestClient_FetchEndpoint_RejectsBadEndpoints(t *testing.T) {

	client := NewClient()

	t.Run("non-http endpoint URL", func(t *testing.T) {
		endpoint := Endpoint{URL: "ftp://example.com/oembed", Format: FormatJSON}
		_, err := client.fetchEndpoint(context.Background(), "https://example.com/1", endpoint)
		assert.Error(t, err)
	})

	t.Run("unknown format", func(t *testing.T) {
		endpoint := Endpoint{URL: "https://example.com/oembed", Format: "yaml"}
		_, err := client.fetchEndpoint(context.Background(), "https://example.com/1", endpoint)
		assert.Error(t, err)
	})
}

func TestClient_Fetch_CanceledContext(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
	)

	// A pre-canceled context fails the fetch before any bytes move
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Fetch(ctx, "https://example.com/video/1")
	assert.Error(t, err)
}

// countingRoundTripper wraps a transport and counts the requests through it.
type countingRoundTripper struct {
	next  http.RoundTripper // transport the calls are delegated to
	count int               // number of requests seen
}

// RoundTrip delegates to the wrapped transport, counting the call.
func (c *countingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	c.count++
	return c.next.RoundTrip(request)
}

func TestClient_UserAgentAndRoundTripper(t *testing.T) {

	var receivedUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	}))

	t.Cleanup(server.Close)

	// The middleware wraps the guarded base transport
	counter := &countingRoundTripper{}

	client := NewClient(
		WithRegistry(testRegistry("https://example.com/*", server.URL+"/oembed")),
		WithAllowPrivateIPs(true),
		WithUserAgent("test-agent/1.0"),
		WithRoundTripper(func(next http.RoundTripper) http.RoundTripper {
			counter.next = next
			return counter
		}),
	)

	_, err := client.Fetch(context.Background(), "https://example.com/video/1")
	require.NoError(t, err)

	assert.Equal(t, "test-agent/1.0", receivedUserAgent)
	assert.Equal(t, 1, counter.count, "the request should pass through the middleware")
}

func TestBuildRequestURL(t *testing.T) {

	t.Run("merges with an existing query string", func(t *testing.T) {

		endpoint := Endpoint{URL: "https://hearthis.at/oembed/?format=json", Format: FormatJSON}

		result, err := buildRequestURL(endpoint, FormatJSON, "https://hearthis.at/track/1", 0, 0)
		require.NoError(t, err)

		assert.Contains(t, result, "format=json")
		assert.Contains(t, result, "url=https%3A%2F%2Fhearthis.at%2Ftrack%2F1")
		assert.Equal(t, 1, strings.Count(result, "?"), "query strings must merge, not stack")
	})

	t.Run("url parameter is encoded", func(t *testing.T) {

		endpoint := Endpoint{URL: "https://example.com/oembed", Format: FormatJSON}

		result, err := buildRequestURL(endpoint, FormatJSON, "https://example.com/a?b=c&d=e", 0, 0)
		require.NoError(t, err)

		assert.Contains(t, result, "url=https%3A%2F%2Fexample.com%2Fa%3Fb%3Dc%26d%3De")
	})

	t.Run("omits parameters without values", func(t *testing.T) {

		endpoint := Endpoint{URL: "https://example.com/oembed", Format: FormatJSON}

		result, err := buildRequestURL(endpoint, FormatJSON, "https://example.com/1", 0, 0)
		require.NoError(t, err)

		assert.NotContains(t, result, "maxwidth")
		assert.NotContains(t, result, "maxheight")
		assert.NotContains(t, result, "format=")
	})
}

func FuzzBuildRequestURL(f *testing.F) {

	// Seed with real endpoint shapes and hostile URLs
	f.Add("https://www.youtube.com/oembed", "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	f.Add("https://hearthis.at/oembed/?format=json", "https://hearthis.at/track/1")
	f.Add("https://example.com/oembed", "https://example.com/a?b=c&d=e#frag")
	f.Add("javascript:alert(1)", "https://example.com/1")
	f.Add("https://example.com/%zz", "%%%")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, endpointURL string, targetURL string) {

		endpoint := Endpoint{URL: endpointURL, Format: FormatJSON, AddFormatParameter: true}

		result, err := buildRequestURL(endpoint, FormatJSON, targetURL, 300, 200)

		// Property: never panics; errors are fine (hostile URLs are expected)
		if err != nil {
			return
		}

		// Property: a successful build is a parseable https/http URL carrying
		// exactly one query string
		parsed, parseErr := url.Parse(result)

		if parseErr != nil {
			t.Fatalf("built URL %q does not parse: %v", result, parseErr)
		}

		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			t.Fatalf("built URL %q has scheme %q", result, parsed.Scheme)
		}

		// Fragments may legally contain "?", so only the wire portion counts
		if beforeFragment, _, _ := strings.Cut(result, "#"); strings.Count(beforeFragment, "?") > 1 {
			t.Fatalf("built URL %q has stacked query strings", result)
		}
	})
}

func TestNormalizeContentType(t *testing.T) {

	assert.Equal(t, "application/json", normalizeContentType("application/json"))
	assert.Equal(t, "application/json", normalizeContentType("Application/JSON; charset=utf-8"))
	assert.Equal(t, "text/xml", normalizeContentType("  text/xml ; q=1"))
	assert.Equal(t, "", normalizeContentType(""))
}

func TestContentTypeMatchesFormat(t *testing.T) {

	test := func(contentType string, format string, expected bool) {
		t.Run(contentType+" as "+format, func(t *testing.T) {
			assert.Equal(t, expected, contentTypeMatchesFormat(contentType, format))
		})
	}

	test("application/json", FormatJSON, true)
	test("application/json+oembed", FormatJSON, true)
	test("text/json", FormatJSON, true)
	test("application/xml", FormatXML, true)
	test("text/xml", FormatXML, true)
	test("text/xml+oembed", FormatXML, true)

	test("text/html", FormatJSON, false)
	test("text/html", FormatXML, false)
	test("application/json", FormatXML, false)
	test("text/xml", FormatJSON, false)
	test("", FormatJSON, false)
	test("application/json", "yaml", false)
}

// TestClient_LinkHeaderDiscovery covers the Link header short-circuit: a
// header-advertised endpoint wins outright, ahead of the HTML body, in both
// Fetch and FetchHTML.
func TestClient_LinkHeaderDiscovery(t *testing.T) {

	// The endpoint server records which endpoint path answered
	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/from-header", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validVideoJSON())
	})

	mux.HandleFunc("/from-body", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		response := NewResponse(TypeVideo)
		response.Title = "From Body"
		response.HTML = `<iframe src="https://example.com/embed/1"></iframe>`
		response.Width = 640
		response.Height = 360

		body, _ := json.Marshal(response)
		_, _ = w.Write(body)
	})

	// This page advertises BOTH: a Link header and a conflicting <link> tag
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Link", `</from-header>; rel="alternate"; type="application/json+oembed"`)
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/json+oembed" href="/from-body">
		</head><body></body></html>`))
	})

	// This page has only the HTML tag, so the body is the only answer
	mux.HandleFunc("/body-only", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/json+oembed" href="/from-body">
		</head><body></body></html>`))
	})

	client := NewClient(WithRegistry(NewRegistry(nil)), WithAllowPrivateIPs(true))

	t.Run("Fetch prefers the Link header over the body", func(t *testing.T) {

		result, err := client.Fetch(context.Background(), server.URL+"/page")
		require.NoError(t, err)

		assert.Equal(t, "Test Video", result.Title, "the header endpoint should have answered")
	})

	t.Run("Fetch falls back to the body when no Link header", func(t *testing.T) {

		result, err := client.Fetch(context.Background(), server.URL+"/body-only")
		require.NoError(t, err)

		assert.Equal(t, "From Body", result.Title)
	})

	t.Run("FetchHTML prefers the Link header over the body", func(t *testing.T) {

		header := http.Header{}
		header.Set("Link", `<`+server.URL+`/from-header>; rel="alternate"; type="application/json+oembed"`)

		page := `<head><link rel="alternate" type="application/json+oembed" href="` + server.URL + `/from-body"></head>`

		result, err := client.FetchHTML(context.Background(), server.URL+"/page", header, strings.NewReader(page))
		require.NoError(t, err)

		assert.Equal(t, "Test Video", result.Title)
	})

	t.Run("FetchHTML with nil header reads the body", func(t *testing.T) {

		page := `<head><link rel="alternate" type="application/json+oembed" href="` + server.URL + `/from-body"></head>`

		result, err := client.FetchHTML(context.Background(), server.URL+"/page", nil, strings.NewReader(page))
		require.NoError(t, err)

		assert.Equal(t, "From Body", result.Title)
	})

	t.Run("FetchHTML with an irrelevant header reads the body", func(t *testing.T) {

		header := http.Header{}
		header.Set("Link", `</style.css>; rel="stylesheet"`)

		page := `<head><link rel="alternate" type="application/json+oembed" href="` + server.URL + `/from-body"></head>`

		result, err := client.FetchHTML(context.Background(), server.URL+"/page", header, strings.NewReader(page))
		require.NoError(t, err)

		assert.Equal(t, "From Body", result.Title)
	})

	t.Run("registry still outranks the Link header", func(t *testing.T) {

		registryClient := NewClient(
			WithRegistry(testRegistry("https://example.com/video/*", server.URL+"/from-body", "json")),
			WithAllowPrivateIPs(true),
		)

		header := http.Header{}
		header.Set("Link", `<`+server.URL+`/from-header>; rel="alternate"; type="application/json+oembed"`)

		result, err := registryClient.FetchHTML(context.Background(), "https://example.com/video/1", header, strings.NewReader(""))
		require.NoError(t, err)

		assert.Equal(t, "From Body", result.Title, "the registry endpoint should have answered")
	})
}
