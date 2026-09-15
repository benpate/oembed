package oembed

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
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
	matchers    []schemeMatcher  // every provider scheme, in registry order
	byAuthority map[string][]int // literal authority ("vimeo.com") -> matcher indexes
	bySuffix    map[string][]int // "*." authority, keyed by its suffix (".youtube.com")
	alwaysCheck []int            // matcher indexes for patterns that cannot be bucketed
}

// schemeMatcher pairs one precompiled scheme pattern with its already-resolved
// endpoint, so matching a URL never re-parses patterns.
type schemeMatcher struct {
	expression *regexp.Regexp // anchored form of one providers.json scheme pattern
	endpoint   Endpoint       // endpoint this pattern resolves to
}

// hostKeyKind names the Registry index bucket that one scheme pattern lands in.
type hostKeyKind int

const (
	hostKeyNone   hostKeyKind = iota // pattern cannot be bucketed; always check it
	hostKeyExact                     // authority is a literal: "vimeo.com"
	hostKeySuffix                    // authority is "*." + a literal: ".youtube.com"
)

// hostKey is the index bucket and map key for one scheme pattern. The kind is
// what keeps an empty value unambiguous: an authority may legally be empty.
type hostKey struct {
	kind  hostKeyKind
	value string
}

// NewRegistry builds a Registry from a provider list, precompiling every
// scheme pattern and pre-resolving every endpoint. Endpoints with no schemes
// are discovery-only and are never scheme-matched.
func NewRegistry(providers []Provider) Registry {

	registry := Registry{
		matchers:    make([]schemeMatcher, 0, len(providers)),
		byAuthority: make(map[string][]int),
		bySuffix:    make(map[string][]int),
	}

	for _, provider := range providers {
		for _, providerEndpoint := range provider.Endpoints {

			endpoint := resolveEndpoint(providerEndpoint)

			for _, pattern := range providerEndpoint.Schemes {

				expression, key, err := compileSchemePattern(pattern)

				// A pattern that fails to compile (invalid UTF-8, for example)
				// is dropped: it simply never matches. Every pattern in the
				// embedded snapshot compiles, so this only affects hostile or
				// corrupted caller-supplied registries.
				if err != nil {
					continue
				}

				registry.add(schemeMatcher{expression: expression, endpoint: endpoint}, key)
			}
		}
	}

	return registry
}

// add appends one matcher to the Registry and files its index under the host
// key, so every matcher lands in exactly one lookup bucket.
func (registry *Registry) add(matcher schemeMatcher, key hostKey) {

	// RULE: read the index BEFORE the append. Folding this into the switch
	// below (scopeguard suggests it) would read it after, filing every
	// matcher one slot past itself.
	index := len(registry.matchers)

	registry.matchers = append(registry.matchers, matcher)

	switch key.kind {

	case hostKeyExact:
		registry.byAuthority[key.value] = append(registry.byAuthority[key.value], index)

	case hostKeySuffix:
		registry.bySuffix[key.value] = append(registry.bySuffix[key.value], index)

	default:
		registry.alwaysCheck = append(registry.alwaysCheck, index)
	}
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

	authority, indexable := candidateAuthority(targetURL)

	// RULE: an input the index cannot key exactly ("spotify:track:x", a
	// non-ASCII authority) falls back to the full scan. "No authority" must
	// never be read as "no match".
	if !indexable {
		return registry.findLinear(targetURL)
	}

	// A stack buffer keeps the common lookup allocation-free: the average
	// authority is eligible for ~1.7 patterns plus the always-check list.
	var buffer [16]int
	candidates := registry.appendCandidates(buffer[:0], authority)

	// RULE: Find answers with the FIRST match in registry order, so candidates
	// gathered from several buckets must be replayed by matcher index.
	slices.Sort(candidates)

	for _, index := range candidates {

		// RULE: the index is a PREFILTER, never the decision. The pattern is
		// what stops "https://*.youtube.com/*" from matching
		// "https://evil.com/x.youtube.com/".
		if matcher := registry.matchers[index]; matcher.expression.MatchString(targetURL) {
			return matcher.endpoint, true
		}
	}

	return Endpoint{}, false
}

// findLinear evaluates every pattern in registry order. Find falls back to it
// for inputs the index cannot key, and the equivalence fuzz keeps it as the
// oracle that makes the index auditable.
func (registry Registry) findLinear(targetURL string) (Endpoint, bool) {

	for _, matcher := range registry.matchers {
		if matcher.expression.MatchString(targetURL) {
			return matcher.endpoint, true
		}
	}

	return Endpoint{}, false
}

// appendCandidates appends the matcher indexes eligible for one authority: its
// exact bucket, one bucket per dotted suffix, and the always-check list.
func (registry Registry) appendCandidates(candidates []int, authority string) []int {

	candidates = append(candidates, registry.byAuthority[authority]...)

	// RULE: "*.example.com" compiles to `[^/]*\.example\.com`, which requires
	// the literal dot — so every dot in the authority opens one suffix bucket,
	// and the apex "example.com" opens none of them.
	for offset := range len(authority) {
		if authority[offset] == '.' {
			candidates = append(candidates, registry.bySuffix[authority[offset:]]...)
		}
	}

	return append(candidates, registry.alwaysCheck...)
}

// Size returns the number of precompiled scheme matchers in this Registry.
func (registry Registry) Size() int {
	return len(registry.matchers)
}

// candidateAuthority returns the lowercased authority a URL's index keys are
// built from — the text between "://" and the next "/". It reports FALSE when
// the URL has no authority the index can key exactly.
func candidateAuthority(targetURL string) (string, bool) {

	_, rest, hasScheme := strings.Cut(targetURL, "://")

	if !hasScheme {
		return "", false
	}

	authority, _, _ := strings.Cut(rest, "/")

	// RULE: (?i) folds a few ASCII letters onto non-ASCII runes ("k" matches
	// U+212A KELVIN SIGN) while ToLower does not, so a non-ASCII authority
	// cannot be keyed exactly and takes the linear scan instead.
	if !isASCII(authority) {
		return "", false
	}

	return strings.ToLower(authority), true
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
// expression, and reports the index bucket it belongs in. A "*" in the
// authority matches within that segment only (it cannot cross a "/"), while a
// "*" in the path matches the rest of the URL, query string included. The
// scheme and host match case-insensitively; the path matches case-sensitively,
// per URL norms.
func compileSchemePattern(pattern string) (*regexp.Regexp, hostKey, error) {

	// Patterns without "://" (like "spotify:*") match as one case-insensitive
	// unit, and have no authority to index.
	scheme, rest, hasScheme := strings.Cut(pattern, "://")

	if !hasScheme {
		expression, err := regexp.Compile("^(?i:" + wildcardToRegexp(pattern, ".*") + ")$")
		return expression, hostKey{kind: hostKeyNone}, err
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

	compiled, err := regexp.Compile(expression + "$")

	return compiled, patternHostKey(scheme, authority), err
}

// patternHostKey derives the index bucket for one scheme pattern from the
// scheme and authority that compileSchemePattern just parsed. Anything it
// cannot key exactly falls back to hostKeyNone, which is always correct and
// merely slower.
func patternHostKey(scheme string, authority string) hostKey {

	// RULE: a scheme carrying ":" or "/" can match candidate text containing
	// "://", so the candidate's FIRST "://" would no longer be the one this
	// pattern splits on and the two authorities would disagree. No real
	// provider scheme does this.
	if strings.ContainsAny(scheme, ":/") {
		return hostKey{kind: hostKeyNone}
	}

	// A non-ASCII authority cannot be keyed exactly — see candidateAuthority.
	if !isASCII(authority) {
		return hostKey{kind: hostKeyNone}
	}

	// "*.example.com" is the one wildcard shape that buckets: it matches every
	// authority ending in ".example.com".
	if suffix, isSuffixPattern := strings.CutPrefix(authority, "*."); isSuffixPattern {

		if strings.Contains(suffix, "*") {
			return hostKey{kind: hostKeyNone}
		}

		return hostKey{kind: hostKeySuffix, value: "." + strings.ToLower(suffix)}
	}

	// Any other wildcard — a bare "*", "a*.com", "*.x.*.com" — is unbucketable.
	if strings.Contains(authority, "*") {
		return hostKey{kind: hostKeyNone}
	}

	return hostKey{kind: hostKeyExact, value: strings.ToLower(authority)}
}

// wildcardToRegexp quotes a pattern segment for use in a regular expression,
// replacing each "*" wildcard with the given regexp fragment.
func wildcardToRegexp(pattern string, replacement string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, replacement)
}

// isASCII returns TRUE when every byte in the value is a single-byte rune.
func isASCII(value string) bool {

	for offset := range len(value) {
		if value[offset] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}
