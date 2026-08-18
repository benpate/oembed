package oembed

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover(t *testing.T) {

	discoverEndpoints := func(t *testing.T, input string, baseURL string) []Endpoint {
		t.Helper()
		endpoints, err := discover(strings.NewReader(input), baseURL)
		require.NoError(t, err)
		return endpoints
	}

	t.Run("typical head", func(t *testing.T) {

		input := `<!DOCTYPE html><html><head>
			<title>A Page</title>
			<link rel="alternate" type="application/json+oembed" href="https://example.com/oembed?url=x&format=json" title="JSON">
			<link rel="alternate" type="text/xml+oembed" href="https://example.com/oembed?url=x&format=xml" title="XML">
		</head><body></body></html>`

		endpoints := discoverEndpoints(t, input, "https://example.com/page")
		require.Len(t, endpoints, 2)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
		assert.Equal(t, "https://example.com/oembed?url=x&format=json", endpoints[0].URL)
		assert.Equal(t, FormatXML, endpoints[1].Format)
	})

	t.Run("json sorts before xml regardless of document order", func(t *testing.T) {

		input := `<head>
			<link rel="alternate" type="text/xml+oembed" href="/xml1">
			<link rel="alternate" type="application/json+oembed" href="/json1">
			<link rel="alternate" type="text/xml+oembed" href="/xml2">
			<link rel="alternate" type="application/json+oembed" href="/json2">
		</head>`

		endpoints := discoverEndpoints(t, input, "https://example.com/page")
		require.Len(t, endpoints, 4)

		// JSON first; document order preserved within each format
		assert.Equal(t, "https://example.com/json1", endpoints[0].URL)
		assert.Equal(t, "https://example.com/json2", endpoints[1].URL)
		assert.Equal(t, "https://example.com/xml1", endpoints[2].URL)
		assert.Equal(t, "https://example.com/xml2", endpoints[3].URL)
	})

	t.Run("relative href resolves against base", func(t *testing.T) {

		input := `<head><link rel="alternate" type="application/json+oembed" href="oembed.json"></head>`

		endpoints := discoverEndpoints(t, input, "https://example.com/articles/1")
		require.Len(t, endpoints, 1)
		assert.Equal(t, "https://example.com/articles/oembed.json", endpoints[0].URL)
	})

	t.Run("rel alternative accepted", func(t *testing.T) {

		input := `<head><link rel="alternative" type="application/json+oembed" href="/oembed"></head>`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("rel list and case handled", func(t *testing.T) {

		input := `<head><LINK REL="noopener Alternate" TYPE="Application/JSON+oEmbed" HREF="/oembed"></head>`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("media type parameters ignored", func(t *testing.T) {

		input := `<head><link rel="alternate" type="application/json+oembed; charset=utf-8" href="/oembed"></head>`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("self-closing link", func(t *testing.T) {

		input := `<head><link rel="alternate" type="application/json+oembed" href="/oembed" /></head>`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("irrelevant links ignored", func(t *testing.T) {

		input := `<head>
			<link rel="stylesheet" href="/style.css">
			<link rel="alternate" type="application/rss+xml" href="/feed">
			<link rel="icon" href="/favicon.ico">
		</head>`

		assert.Empty(t, discoverEndpoints(t, input, "https://example.com/"))
	})

	t.Run("missing href ignored", func(t *testing.T) {

		input := `<head><link rel="alternate" type="application/json+oembed"></head>`
		assert.Empty(t, discoverEndpoints(t, input, "https://example.com/"))
	})

	t.Run("non-http schemes dropped", func(t *testing.T) {

		input := `<head>
			<link rel="alternate" type="application/json+oembed" href="javascript:alert(1)">
			<link rel="alternate" type="application/json+oembed" href="data:application/json,{}">
			<link rel="alternate" type="application/json+oembed" href="ftp://example.com/oembed">
		</head>`

		assert.Empty(t, discoverEndpoints(t, input, "https://example.com/"))
	})

	t.Run("stops at closing head", func(t *testing.T) {

		input := `<head></head>
			<link rel="alternate" type="application/json+oembed" href="/late">`

		assert.Empty(t, discoverEndpoints(t, input, "https://example.com/"))
	})

	t.Run("stops at body", func(t *testing.T) {

		// Misnested: no </head>, link sits inside <body>
		input := `<head><title>x</title>
			<body><link rel="alternate" type="application/json+oembed" href="/in-body">`

		assert.Empty(t, discoverEndpoints(t, input, "https://example.com/"))
	})

	t.Run("headless fragment still parses", func(t *testing.T) {

		input := `<link rel="alternate" type="application/json+oembed" href="/oembed">`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("truncated html returns what was found", func(t *testing.T) {

		input := `<head><link rel="alternate" type="application/json+oembed" href="/oembed"><link rel="alt`
		assert.Len(t, discoverEndpoints(t, input, "https://example.com/"), 1)
	})

	t.Run("empty input", func(t *testing.T) {
		assert.Empty(t, discoverEndpoints(t, "", "https://example.com/"))
	})

	t.Run("hostile non-html bytes", func(t *testing.T) {
		assert.Empty(t, discoverEndpoints(t, "\x00\xff\xfe<<<>>>", "https://example.com/"))
	})

	t.Run("invalid base url", func(t *testing.T) {
		_, err := discover(strings.NewReader("<head></head>"), "http://[::1")
		assert.Error(t, err)
	})

	t.Run("empty base drops relative hrefs", func(t *testing.T) {

		input := `<head>
			<link rel="alternate" type="application/json+oembed" href="/relative">
			<link rel="alternate" type="application/json+oembed" href="https://example.com/absolute">
		</head>`

		endpoints := discoverEndpoints(t, input, "")
		require.Len(t, endpoints, 1)
		assert.Equal(t, "https://example.com/absolute", endpoints[0].URL)
	})
}

func TestFormatFromLinkType(t *testing.T) {

	test := func(input string, expectedFormat string, expectedMatch bool) {
		t.Run(input, func(t *testing.T) {
			format, isOEmbedType := formatFromLinkType(input)
			require.Equal(t, expectedMatch, isOEmbedType)
			assert.Equal(t, expectedFormat, format)
		})
	}

	test("application/json+oembed", FormatJSON, true)
	test("text/xml+oembed", FormatXML, true)
	test("  Application/JSON+oEmbed ; charset=utf-8", FormatJSON, true)
	test("application/json", "", false)
	test("text/html", "", false)
	test("", "", false)
}

// TestDiscover_LivePages runs the extractor against real page heads recorded
// from live sites (testdata/pages/), pinning discovery against the HTML that
// providers actually serve.
func TestDiscover_LivePages(t *testing.T) {

	discoverEndpoints := func(t *testing.T, filename string, baseURL string) []Endpoint {
		t.Helper()

		file, err := os.Open("testdata/pages/" + filename)
		require.NoError(t, err)
		t.Cleanup(func() { _ = file.Close() })

		endpoints, err := discover(file, baseURL)
		require.NoError(t, err)
		return endpoints
	}

	t.Run("x.com status", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "x.html", "https://x.com/jack/status/20")

		require.Len(t, endpoints, 1)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
		assert.Contains(t, endpoints[0].URL, "publish.x.com/oembed")
	})

	t.Run("mastodon status", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "mastodon.html", "https://mastodon.social/@Gargron/100254678717223630")

		require.Len(t, endpoints, 1)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
		assert.Contains(t, endpoints[0].URL, "mastodon.social/api/oembed")
	})

	t.Run("flickr photo", func(t *testing.T) {

		// Flickr advertises both formats; JSON must sort first
		endpoints := discoverEndpoints(t, "flickr.html", "https://www.flickr.com/photos/bees/2341623661")

		require.Len(t, endpoints, 2)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
		assert.Equal(t, FormatXML, endpoints[1].Format)
		assert.Contains(t, endpoints[0].URL, "flickr.com/services/oembed")
	})
}

// BenchmarkDiscover measures the streaming extractor against a real recorded
// page head (Mastodon, ~47KB).
func BenchmarkDiscover(b *testing.B) {

	page, err := os.ReadFile("testdata/pages/mastodon.html")
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := discover(bytes.NewReader(page), "https://mastodon.social/@Gargron/100254678717223630"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDiscover_SmallHead measures the extractor on a minimal document,
// isolating fixed overhead from page-size costs.
func BenchmarkDiscover_SmallHead(b *testing.B) {

	const page = `<html><head><link rel="alternate" type="application/json+oembed" href="/oembed"></head><body></body></html>`

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := discover(strings.NewReader(page), "https://example.com/page"); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzDiscover(f *testing.F) {

	// Seed with the interesting unit-test shapes
	f.Add(`<head><link rel="alternate" type="application/json+oembed" href="/oembed"></head>`)
	f.Add(`<head><link rel="alternate" type="text/xml+oembed" href="https://example.com/x"></head>`)
	f.Add(`<link rel="alt`)
	f.Add(`<head><body><link>`)
	f.Add("\x00\xff<<<<")
	f.Add(`<head><link rel="alternate" type="application/json+oembed" href="javascript:alert(1)"></head>`)

	f.Fuzz(func(t *testing.T, input string) {

		endpoints, _ := discover(strings.NewReader(input), "https://example.com/page")

		// Property: never panics, and every returned endpoint is http(s) with a format
		for _, endpoint := range endpoints {

			if err := validateHTTPURL(endpoint.URL); err != nil {
				t.Fatalf("discovered non-http endpoint %q from %q", endpoint.URL, input)
			}

			if endpoint.Format != FormatJSON && endpoint.Format != FormatXML {
				t.Fatalf("discovered endpoint with invalid format %q", endpoint.Format)
			}
		}
	})
}

func TestDiscoverLinkHeader(t *testing.T) {

	discoverEndpoints := func(t *testing.T, baseURL string, values ...string) []Endpoint {
		t.Helper()

		header := http.Header{}
		for _, value := range values {
			header.Add("Link", value)
		}

		endpoints, err := discoverLinkHeader(header, baseURL)
		require.NoError(t, err)
		return endpoints
	}

	t.Run("single json link", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`<https://example.com/oembed?url=x>; rel="alternate"; type="application/json+oembed"`)

		require.Len(t, endpoints, 1)
		assert.Equal(t, "https://example.com/oembed?url=x", endpoints[0].URL)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
	})

	t.Run("json sorts before xml across headers", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</xml>; rel="alternate"; type="text/xml+oembed"`,
			`</json>; rel="alternate"; type="application/json+oembed"`)

		require.Len(t, endpoints, 2)
		assert.Equal(t, "https://example.com/json", endpoints[0].URL)
		assert.Equal(t, "https://example.com/xml", endpoints[1].URL)
	})

	t.Run("comma-separated link values in one header", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</json>; rel="alternate"; type="application/json+oembed", </xml>; rel="alternate"; type="text/xml+oembed"`)

		assert.Len(t, endpoints, 2)
	})

	t.Run("commas inside quoted parameters do not split", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed>; title="Hello, World"; rel="alternate"; type="application/json+oembed"`)

		assert.Len(t, endpoints, 1)
	})

	t.Run("commas inside the uri reference do not split", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed?list=a,b,c>; rel="alternate"; type="application/json+oembed"`)

		require.Len(t, endpoints, 1)
		assert.Equal(t, "https://example.com/oembed?list=a,b,c", endpoints[0].URL)
	})

	t.Run("unquoted parameters accepted", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed>; rel=alternate; type=application/json+oembed`)

		assert.Len(t, endpoints, 1)
	})

	t.Run("parameter names and rel are case-insensitive", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed>; REL="Alternate"; TYPE="Application/JSON+oEmbed"`)

		assert.Len(t, endpoints, 1)
	})

	t.Run("rel list with extra tokens", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed>; rel="noopener alternate"; type="application/json+oembed"`)

		assert.Len(t, endpoints, 1)
	})

	t.Run("relative reference resolves against base", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/articles/1",
			`<oembed.json>; rel="alternate"; type="application/json+oembed"`)

		require.Len(t, endpoints, 1)
		assert.Equal(t, "https://example.com/articles/oembed.json", endpoints[0].URL)
	})

	t.Run("first rel and type win per RFC 8288", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</oembed>; rel="alternate"; type="application/json+oembed"; rel="stylesheet"; type="text/css"`)

		require.Len(t, endpoints, 1)
		assert.Equal(t, FormatJSON, endpoints[0].Format)
	})

	t.Run("irrelevant links ignored", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`</style.css>; rel="stylesheet"`,
			`</feed>; rel="alternate"; type="application/rss+xml"`,
			`</next>; rel="next"; type="application/json+oembed"`)

		assert.Empty(t, endpoints)
	})

	t.Run("non-http schemes dropped", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`<javascript:alert(1)>; rel="alternate"; type="application/json+oembed"`,
			`<data:application/json,{}>; rel="alternate"; type="application/json+oembed"`)

		assert.Empty(t, endpoints)
	})

	t.Run("malformed values are skipped", func(t *testing.T) {

		endpoints := discoverEndpoints(t, "https://example.com/page",
			`no-brackets; rel="alternate"; type="application/json+oembed"`,
			`<unterminated; rel="alternate"; type="application/json+oembed"`,
			``)

		assert.Empty(t, endpoints)
	})

	t.Run("nil header", func(t *testing.T) {

		endpoints, err := discoverLinkHeader(nil, "https://example.com/page")
		require.NoError(t, err)
		assert.Empty(t, endpoints)
	})

	t.Run("empty header", func(t *testing.T) {
		assert.Empty(t, discoverEndpoints(t, "https://example.com/page"))
	})

	t.Run("invalid base url", func(t *testing.T) {

		header := http.Header{}
		header.Set("Link", `</oembed>; rel="alternate"; type="application/json+oembed"`)

		_, err := discoverLinkHeader(header, "http://[::1")
		assert.Error(t, err)
	})
}

// TestDiscoverLinkHeader_CanonicalWireForm pins the exact Link header a
// conforming provider emits — both formats, JSON first, page URL escaped into
// the "url" parameter. This literal replaces the generate-and-parse round trip
// that existed while this package still had a provider half; keep it in step
// with what real providers send.
func TestDiscoverLinkHeader_CanonicalWireForm(t *testing.T) {

	const pageURL = "https://example.com/@user"

	header := http.Header{}
	header.Set("Link",
		`<https://example.com/.oembed?url=https%3A%2F%2Fexample.com%2F%40user&format=json>; rel="alternate"; type="application/json+oembed", `+
			`<https://example.com/.oembed?url=https%3A%2F%2Fexample.com%2F%40user&format=xml>; rel="alternate"; type="text/xml+oembed"`)

	endpoints, err := discoverLinkHeader(header, pageURL)
	require.NoError(t, err)

	require.Len(t, endpoints, 2)

	assert.Equal(t, FormatJSON, endpoints[0].Format)
	assert.Equal(t, "https://example.com/.oembed?url=https%3A%2F%2Fexample.com%2F%40user&format=json", endpoints[0].URL)

	assert.Equal(t, FormatXML, endpoints[1].Format)
	assert.Equal(t, "https://example.com/.oembed?url=https%3A%2F%2Fexample.com%2F%40user&format=xml", endpoints[1].URL)

	// The page URL survives the escaping round trip intact
	parsed, err := url.Parse(endpoints[0].URL)
	require.NoError(t, err)
	assert.Equal(t, pageURL, parsed.Query().Get("url"))
}

func FuzzDiscoverLinkHeader(f *testing.F) {

	f.Add(`<https://example.com/oembed>; rel="alternate"; type="application/json+oembed"`)
	f.Add(`</a>; rel=alternate; type=text/xml+oembed, </b>; rel="alternate"; type="application/json+oembed"`)
	f.Add(`<javascript:alert(1)>; rel="alternate"; type="application/json+oembed"`)
	f.Add(`<`)
	f.Add(`";,;<<>>`)
	f.Add(`</a>; title="\"quoted, comma\""; rel="alternate"; type="application/json+oembed"`)
	f.Add("")

	f.Fuzz(func(t *testing.T, value string) {

		header := http.Header{}
		header.Set("Link", value)

		endpoints, err := discoverLinkHeader(header, "https://example.com/page")

		if err != nil {
			t.Fatalf("valid base URL should never error: %v", err)
		}

		// Property: never panics, and every endpoint is http(s) with a known format
		for _, endpoint := range endpoints {

			if err := validateHTTPURL(endpoint.URL); err != nil {
				t.Fatalf("discovered non-http endpoint %q from %q", endpoint.URL, value)
			}

			if endpoint.Format != FormatJSON && endpoint.Format != FormatXML {
				t.Fatalf("discovered endpoint with invalid format %q", endpoint.Format)
			}
		}
	})
}
