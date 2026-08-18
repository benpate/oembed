package oembed

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRegistry(t *testing.T) {

	// The embedded snapshot must parse (defaultRegistry treats this as unreachable)
	var providers []Provider
	require.NoError(t, json.Unmarshal(providersJSON, &providers))
	require.NotEmpty(t, providers)

	// The registry compiles a matcher for every scheme pattern
	registry := DefaultRegistry()
	assert.GreaterOrEqual(t, registry.Size(), 800, "embedded registry lost most of its matchers")

	// The lazy singleton hands back the same parsed registry
	assert.Equal(t, registry.Size(), DefaultRegistry().Size())
}

func TestRegistry_Find(t *testing.T) {

	registry := DefaultRegistry()

	found := func(name string, targetURL string, expectedEndpointURL string) {
		t.Run(name, func(t *testing.T) {
			endpoint, matched := registry.Find(targetURL)
			require.True(t, matched, "expected a match for %s", targetURL)
			assert.Equal(t, expectedEndpointURL, endpoint.URL)
		})
	}

	missed := func(name string, targetURL string) {
		t.Run(name, func(t *testing.T) {
			_, matched := registry.Find(targetURL)
			assert.False(t, matched, "expected no match for %s", targetURL)
		})
	}

	// Real providers from the embedded snapshot
	found("youtube subdomain wildcard", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "https://www.youtube.com/oembed")
	found("youtube nested subdomain", "https://music.youtube.com/watch?v=abc", "https://www.youtube.com/oembed")
	found("youtube short link", "https://youtu.be/dQw4w9WgXcQ", "https://www.youtube.com/oembed")
	found("host matches case-insensitively", "https://WWW.YouTube.COM/watch?v=abc", "https://www.youtube.com/oembed")
	found("format substitution", "https://audioboom.com/posts/12345", "https://audioboom.com/publishing/oembed.json")
	found("scheme-only pattern", "spotify:track:4uLU6hMCjMI75M1A2tKUQC", "https://open.spotify.com/oembed")
	found("vimeo", "https://vimeo.com/1084537", "https://vimeo.com/api/oembed.json")

	// Non-matches, including lookalike attacks
	missed("wildcard cannot cross into the path", "https://evil.com/x.youtube.com/watch?v=1")
	missed("host suffix lookalike", "https://www.youtube.com.evil.com/watch?v=1")
	missed("host without the wildcard dot", "https://evilyoutube.com/watch?v=1")
	missed("path is case-sensitive", "https://www.youtube.com/WATCH?v=abc")
	missed("unknown host", "https://example-nobody-registered.com/video/1")
	missed("empty url", "")
}

func TestNewRegistry(t *testing.T) {

	t.Run("discovery-only endpoints are never scheme-matched", func(t *testing.T) {

		registry := NewRegistry([]Provider{{
			Name: "DiscoveryOnly",
			URL:  "https://example.com/",
			Endpoints: []ProviderEndpoint{{
				URL:       "https://example.com/oembed",
				Discovery: true,
			}},
		}})

		assert.Equal(t, 0, registry.Size())

		_, matched := registry.Find("https://example.com/anything")
		assert.False(t, matched)
	})

	t.Run("first matching scheme wins in registry order", func(t *testing.T) {

		registry := NewRegistry([]Provider{
			{
				Name:      "First",
				Endpoints: []ProviderEndpoint{{Schemes: []string{"https://example.com/*"}, URL: "https://first.com/oembed"}},
			},
			{
				Name:      "Second",
				Endpoints: []ProviderEndpoint{{Schemes: []string{"https://example.com/*"}, URL: "https://second.com/oembed"}},
			},
		})

		endpoint, matched := registry.Find("https://example.com/video")
		require.True(t, matched)
		assert.Equal(t, "https://first.com/oembed", endpoint.URL)
	})

	t.Run("empty provider list", func(t *testing.T) {

		registry := NewRegistry(nil)
		assert.Equal(t, 0, registry.Size())

		_, matched := registry.Find("https://example.com/")
		assert.False(t, matched)
	})
}

func TestResolveEndpoint(t *testing.T) {

	test := func(name string, input ProviderEndpoint, expected Endpoint) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, resolveEndpoint(input))
		})
	}

	test("no formats assumes json, no parameter",
		ProviderEndpoint{URL: "https://example.com/oembed"},
		Endpoint{URL: "https://example.com/oembed", Format: FormatJSON, AddFormatParameter: false})

	test("declared json enables the format parameter",
		ProviderEndpoint{URL: "https://example.com/oembed", Formats: []string{"json"}},
		Endpoint{URL: "https://example.com/oembed", Format: FormatJSON, AddFormatParameter: true})

	test("json preferred when both are offered",
		ProviderEndpoint{URL: "https://example.com/oembed", Formats: []string{"xml", "json"}},
		Endpoint{URL: "https://example.com/oembed", Format: FormatJSON, AddFormatParameter: true})

	test("xml-only provider",
		ProviderEndpoint{URL: "https://example.com/oembed", Formats: []string{"xml"}},
		Endpoint{URL: "https://example.com/oembed", Format: FormatXML, AddFormatParameter: true})

	test("format token substitution",
		ProviderEndpoint{URL: "https://example.com/oembed.{format}", Formats: []string{"json", "xml"}},
		Endpoint{URL: "https://example.com/oembed.json", Format: FormatJSON, AddFormatParameter: false})

	test("format token substitution for xml-only provider",
		ProviderEndpoint{URL: "https://example.com/oembed.{format}", Formats: []string{"xml"}},
		Endpoint{URL: "https://example.com/oembed.xml", Format: FormatXML, AddFormatParameter: false})

	test("unrecognized formats fall back to json",
		ProviderEndpoint{URL: "https://example.com/oembed", Formats: []string{"yaml"}},
		Endpoint{URL: "https://example.com/oembed", Format: FormatJSON, AddFormatParameter: false})
}

func TestCompileSchemePattern(t *testing.T) {

	test := func(name string, pattern string, candidate string, expectMatch bool) {
		t.Run(name, func(t *testing.T) {

			expression, err := compileSchemePattern(pattern)
			require.NoError(t, err)

			assert.Equal(t, expectMatch, expression.MatchString(candidate),
				"pattern %q vs candidate %q", pattern, candidate)
		})
	}

	// Wildcard positions
	test("host wildcard matches subdomain", "https://*.youtube.com/watch*", "https://www.youtube.com/watch?v=1", true)
	test("host wildcard matches nested subdomains", "https://*.youtube.com/watch*", "https://a.b.youtube.com/watch?v=1", true)
	test("host wildcard requires the literal dot", "https://*.youtube.com/watch*", "https://youtube.com/watch?v=1", false)
	test("host wildcard cannot cross a slash", "https://*.youtube.com/watch*", "https://evil.com/x.youtube.com/watch", false)
	test("path wildcard matches query strings", "https://youtu.be/*", "https://youtu.be/abc?t=10", true)
	test("path wildcard matches nested segments", "https://example.com/*", "https://example.com/a/b/c", true)
	test("mid-path wildcard", "https://example.com/*/video", "https://example.com/users/video", true)

	// Anchoring
	test("match is anchored at the start", "https://example.com/*", "xxhttps://example.com/a", false)
	test("match is anchored at the end", "https://example.com/video", "https://example.com/video/extra", false)
	test("exact pattern matches exactly", "https://example.com/video", "https://example.com/video", true)

	// Case sensitivity
	test("scheme and host are case-insensitive", "https://example.com/path", "HTTPS://EXAMPLE.COM/path", true)
	test("path is case-sensitive", "https://example.com/path", "https://example.com/PATH", false)

	// Literal regex characters are quoted
	test("query metacharacters are literal", "https://youtube.com/playlist?list=*", "https://youtube.com/playlist?list=abc", true)
	test("dots are literal", "https://example.com/*", "https://exampleXcom/a", false)

	// Patterns without ://
	test("scheme-only pattern", "spotify:*", "spotify:track:123", true)
	test("scheme-only pattern is case-insensitive", "spotify:*", "SPOTIFY:track:123", true)
	test("scheme-only pattern anchors", "spotify:*", "notspotify:track:123", false)

	// Scheme must match
	test("http pattern rejects https", "http://example.com/*", "https://example.com/a", false)
}

// BenchmarkRegistry_Find_Hit measures a match early in registry order
// (YouTube), the common fast path.
func BenchmarkRegistry_Find_Hit(b *testing.B) {

	b.ReportAllocs()

	// DefaultRegistry is a sync.Once-backed accessor, so calling it in the
	// loop costs nothing measurable against a ~100µs Find.
	for b.Loop() {
		if _, found := DefaultRegistry().Find("https://www.youtube.com/watch?v=dQw4w9WgXcQ"); !found {
			b.Fatal("expected a registry hit")
		}
	}
}

// BenchmarkRegistry_Find_Miss measures the worst case: a URL that matches
// nothing, scanning every precompiled matcher.
func BenchmarkRegistry_Find_Miss(b *testing.B) {

	b.ReportAllocs()

	for b.Loop() {
		if _, found := DefaultRegistry().Find("https://example-nobody-registered.com/video/1"); found {
			b.Fatal("expected a registry miss")
		}
	}
}

// BenchmarkNewRegistry measures parsing and compiling the full embedded
// snapshot — the one-time cost that DefaultRegistry pays lazily.
func BenchmarkNewRegistry(b *testing.B) {

	var providers []Provider

	if err := json.Unmarshal(providersJSON, &providers); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if registry := NewRegistry(providers); registry.Size() == 0 {
			b.Fatal("registry compiled no matchers")
		}
	}
}

func FuzzCompileSchemePattern(f *testing.F) {

	// Seed with real pattern shapes and hostile candidates
	f.Add("https://*.youtube.com/watch*", "https://www.youtube.com/watch?v=1")
	f.Add("spotify:*", "spotify:track:123")
	f.Add("https://youtube.com/playlist?list=*", "https://youtube.com/playlist?list=x")
	f.Add("*://*/*", "https://example.com/a")
	f.Add("", "")
	f.Add("****", "\x00\xff")
	f.Add("https://(evil)/[a-z]+", "https://(evil)/[a-z]+")

	f.Fuzz(func(t *testing.T, pattern string, candidate string) {

		expression, err := compileSchemePattern(pattern)

		// Property: compilation only fails on invalid UTF-8 (regexp rejects it),
		// and never panics.
		if err != nil {

			if utf8.ValidString(pattern) {
				t.Fatalf("valid UTF-8 pattern %q failed to compile: %v", pattern, err)
			}

			return
		}

		// Property: matching never panics.
		_ = expression.MatchString(candidate)

		// Property: a pattern with no wildcards is a literal — it must match itself.
		if !strings.Contains(pattern, "*") {
			if !expression.MatchString(pattern) {
				t.Fatalf("literal pattern %q does not match itself", pattern)
			}
		}
	})
}

// TestDefaultRegistry_BuiltAtInit pins the initialization contract: the
// embedded snapshot is compiled at package init, not lazily, so a Client can
// hold a plain Registry value with no nil check and no first-call stall.
func TestDefaultRegistry_BuiltAtInit(t *testing.T) {

	// The package variable is already populated — nothing here triggers a
	// build, so a non-empty result proves init() did the work.
	assert.NotZero(t, defaultRegistry.Size(), "init() must compile the embedded snapshot")
	assert.Equal(t, defaultRegistry.Size(), DefaultRegistry().Size())

	// The default really is the whole vendored snapshot
	endpoint, found := DefaultRegistry().Find("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	require.True(t, found)
	assert.Equal(t, "https://www.youtube.com/oembed", endpoint.URL)
}

// TestNewClient_RegistryDefaults covers the option/default interaction now that
// Client holds a Registry value rather than a nil-able pointer.
func TestNewClient_RegistryDefaults(t *testing.T) {

	t.Run("NewClient uses the embedded registry", func(t *testing.T) {
		client := NewClient()
		assert.Equal(t, DefaultRegistry().Size(), client.registry.Size())
	})

	t.Run("WithRegistry overrides the default", func(t *testing.T) {

		custom := testRegistry("https://example.com/*", "https://example.com/oembed")
		client := NewClient(WithRegistry(custom))

		assert.Equal(t, custom.Size(), client.registry.Size())
		assert.NotEqual(t, DefaultRegistry().Size(), client.registry.Size())

		// The override resolves against the custom registry, not the default
		_, found := client.registry.Find("https://www.youtube.com/watch?v=1")
		assert.False(t, found, "the default registry must be fully replaced")
	})

	t.Run("an empty registry is a legal override, not a fallback", func(t *testing.T) {

		// Passing an empty Registry must NOT silently restore the default —
		// that was the ambiguity the *Registry pointer used to carry.
		client := NewClient(WithRegistry(NewRegistry(nil)))

		assert.Zero(t, client.registry.Size())

		_, found := client.registry.Find("https://www.youtube.com/watch?v=1")
		assert.False(t, found)
	})
}
