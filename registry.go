package oembed

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// providersJSON is the vendored providers.json snapshot from
// https://oembed.com/providers.json. Refresh it with `go generate` (below) and
// update ProvidersSnapshotDate to match.
//
//go:generate curl -sSL -o providers.json https://oembed.com/providers.json
//go:embed providers.json
var providersJSON []byte

// Registry matches candidate URLs against provider scheme patterns and
// returns the provider's oEmbed endpoint. Registries are immutable after
// construction and safe for concurrent use without locks.
type Registry struct {
	matchers []schemeMatcher
}

// schemeMatcher pairs one precompiled scheme pattern with its already-resolved
// endpoint, so matching a URL never re-parses patterns.
type schemeMatcher struct {
	expression *regexp.Regexp
	endpoint   Endpoint
}

// NewRegistry builds a Registry from a provider list, precompiling every
// scheme pattern and pre-resolving every endpoint. Endpoints with no schemes
// are discovery-only and are never scheme-matched.
func NewRegistry(providers []Provider) Registry {

	matchers := make([]schemeMatcher, 0, len(providers))

	for _, provider := range providers {
		for _, providerEndpoint := range provider.Endpoints {

			endpoint := resolveEndpoint(providerEndpoint)

			for _, pattern := range providerEndpoint.Schemes {

				expression, err := compileSchemePattern(pattern)

				// A pattern that fails to compile (invalid UTF-8, for example)
				// is dropped: it simply never matches. Every pattern in the
				// embedded snapshot compiles, so this only affects hostile or
				// corrupted caller-supplied registries.
				if err != nil {
					continue
				}

				matchers = append(matchers, schemeMatcher{
					expression: expression,
					endpoint:   endpoint,
				})
			}
		}
	}

	return Registry{matchers: matchers}
}

// defaultRegistry is the compiled form of the embedded providers.json
// snapshot, built once at package initialization and shared by every Client.
// Registries are immutable, so sharing one costs nothing and locks nothing.
var defaultRegistry Registry

// init compiles the embedded snapshot. Nearly every caller uses it, so paying
// for it at startup buys a predictable boot cost instead of a slow first
// request, and lets Client hold a plain Registry with no lazy resolution.
func init() {

	var providers []Provider

	// RULE: a snapshot that will not parse is a corrupt BUILD, not a runtime
	// condition — the file is compiled into the binary and validated by unit
	// tests. Failing loudly here beats a silently empty registry, which would
	// degrade into "no provider ever matches" for the life of the process.
	if err := json.Unmarshal(providersJSON, &providers); err != nil {
		panic("oembed: embedded providers.json is corrupt: " + err.Error())
	}

	defaultRegistry = NewRegistry(providers)
}

// DefaultRegistry returns the Registry built from the embedded providers.json
// snapshot, compiled once at package initialization and shared.
func DefaultRegistry() Registry {
	return defaultRegistry
}

// Find returns the endpoint of the first provider scheme that matches the
// candidate URL, in registry order. It reports FALSE when no scheme matches.
func (registry Registry) Find(targetURL string) (Endpoint, bool) {

	for _, matcher := range registry.matchers {
		if matcher.expression.MatchString(targetURL) {
			return matcher.endpoint, true
		}
	}

	return Endpoint{}, false
}

// Size returns the number of precompiled scheme matchers in this Registry.
func (registry Registry) Size() int {
	return len(registry.matchers)
}

// resolveEndpoint converts a ProviderEndpoint into a concrete, ready-to-call
// Endpoint: it picks the response format (JSON unless the provider only
// offers XML), substitutes {format} in the endpoint URL, and decides whether
// the request should carry an explicit "format" query parameter.
func resolveEndpoint(providerEndpoint ProviderEndpoint) Endpoint {

	// Prefer JSON; fall back to XML only when the provider is XML-only. An
	// empty Formats list promises nothing, so it lands on JSON here too.
	format := FormatJSON

	if !slices.Contains(providerEndpoint.Formats, FormatJSON) {
		if slices.Contains(providerEndpoint.Formats, FormatXML) {
			format = FormatXML
		}
	}

	// A {format} placeholder in the URL carries the format by itself. Without
	// one, pass the "format" parameter only when the provider's declared
	// formats say it's understood.
	endpointURL := providerEndpoint.URL
	addFormatParameter := false

	if strings.Contains(endpointURL, formatToken) {
		endpointURL = strings.ReplaceAll(endpointURL, formatToken, format)
	} else if slices.Contains(providerEndpoint.Formats, format) {
		addFormatParameter = true
	}

	return Endpoint{
		URL:                endpointURL,
		Format:             format,
		AddFormatParameter: addFormatParameter,
	}
}

// compileSchemePattern converts one providers.json scheme pattern (for
// example "https://*.youtube.com/watch*") into an anchored regular
// expression. A "*" in the authority matches within that segment only (it
// cannot cross a "/"), while a "*" in the path matches the rest of the URL,
// query string included. The scheme and host match case-insensitively; the
// path matches case-sensitively, per URL norms.
func compileSchemePattern(pattern string) (*regexp.Regexp, error) {

	// Patterns without "://" (like "spotify:*") match as one case-insensitive unit.
	scheme, rest, hasScheme := strings.Cut(pattern, "://")

	if !hasScheme {
		return regexp.Compile("^(?i:" + wildcardToRegexp(pattern, ".*") + ")$")
	}

	// Split the remainder into authority and path at the first "/".
	authority, path, hasPath := strings.Cut(rest, "/")

	// Authority wildcards must not escape the authority.
	expression := "^(?i:" +
		wildcardToRegexp(scheme, "[a-z0-9+.-]*") + "://" +
		wildcardToRegexp(authority, "[^/]*") + ")"

	if hasPath {
		expression += "/" + wildcardToRegexp(path, ".*")
	}

	return regexp.Compile(expression + "$")
}

// wildcardToRegexp quotes a pattern segment for use in a regular expression,
// replacing each "*" wildcard with the given regexp fragment.
func wildcardToRegexp(pattern string, replacement string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, replacement)
}
