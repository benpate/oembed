# oEmbed API Reference

Every exported symbol in `github.com/benpate/oembed`, with the behavior that isn't obvious from the signature. For orientation and worked examples see [README.md](README.md); for the rules that bind contributors see [AGENTS.md](AGENTS.md).

The package is a **consumer** of oEmbed 1.0. It resolves endpoints, fetches and parses responses, and classifies provider markup into safe render plans. It also carries the response *document* type, so you can author one — but request parsing and endpoint advertisement are deliberately out of scope.

## Contents

- [Client](#client) — resolving a URL into metadata
- [Client options](#client-options) — configuring the client
- [Registry](#registry) — the embedded provider snapshot
- [Endpoint](#endpoint) — a resolved endpoint
- [Response](#response) — the oEmbed document
- [Authoring](#authoring) — builders, validation, writing
- [Embedding](#embedding) — turning a response into safe markup
- [Constants](#constants)
- [Errors](#errors)

---

## Client

### `type Client`

```go
type Client struct { /* unexported fields */ }
```

Resolves target URLs into validated oEmbed metadata. Construct with `NewClient`. Immutable after construction and safe for concurrent use, and cheap to copy — it holds a `Registry` by value, which is one slice header pointing at shared, immutable matchers.

The zero `Client{}` is not usable: it has no registry, no user agent, and a zero body cap. Always construct with `NewClient`.

### `func NewClient(options ...ClientOption) Client`

Returns a client configured by the given options. **`NewClient()` with no options is a sane production client**: it uses the embedded provider registry, blocks non-public IP addresses, and caps response bodies at `DefaultMaxBodySize`.

Defaults are set first and options applied after, so any option overrides them. `WithRegistry` fully replaces the default — passing an empty `Registry` gives you a client that matches no providers and falls through to discovery every time, which is a legal choice rather than a silent fallback.

### `func (Client) Fetch(ctx, targetURL string) (Response, error)`

The main entry point. Resolves `targetURL` into a validated `Response` by walking a three-step ladder and **stopping at the first hit**:

1. **Registry match** — no network request at all for the ~369 known providers.
2. **`Link` header** — the page is fetched; if its HTTP `Link` header advertises an endpoint, that endpoint wins outright and the body is never parsed.
3. **HTML discovery** — the `<head>` is stream-parsed for `<link rel="alternate">` endpoints (plus the also-seen `rel="alternative"`) whose type is `application/json+oembed` or `text/xml+oembed`. **Parsing stops at `</head>` or `<body>`**, so a multi-megabyte page body is never read, and malformed, misnested, or truncated HTML is tolerated. JSON endpoints are preferred over XML; document order breaks ties within a format.

The resolved endpoint is then called, its response parsed **by the endpoint's declared format**, and validated with the lenient receive-side rules (see [Validation, two ways](#validation-two-ways)). A response whose `Content-Type` contradicts the requested format is rejected rather than guessed at — the body is never sniffed.

Non-`http(s)` references are dropped wherever a URL enters — `javascript:`, `data:`, and host-less forms like `http:` — so a hostile page cannot steer the fetch off-protocol.

Returns a **not-found** error when no endpoint can be resolved by any of the three steps.

### `func (Client) FetchHTML(ctx, pageURL string, header http.Header, reader io.Reader) (Response, error)`

Exactly `Fetch`, minus the page request — for crawlers and link-preview services that already hold the response. The same registry → `Link` header → HTML ladder runs against what you supply.

- `pageURL` does double duty: relative discovery references resolve against it, **and** it is the target URL sent to the endpoint.
- `header` should be the response's own header so `Link`-header discovery works. **`nil` is valid** and simply skips that step, which is what mocks and tests usually want. The full RFC 8288 grammar is handled: commas inside `<uri>` and quoted parameters, quoted-pair escapes, case-insensitive `rel`/`type`, first-occurrence-wins.
- `reader` is read only as far as `</head>`.

Note that this still makes one outbound request — to the *endpoint* — so the SSRF guard and body cap remain in force.

### What is deliberately *not* public

`Fetch` and `FetchHTML` are the entire client. The intermediate steps are unexported on purpose:

| Formerly | Now | Why |
|---|---|---|
| `Discover` | `discover` | HTML `<link rel="alternate">` scanning |
| `DiscoverLinkHeader` | `discoverLinkHeader` | RFC 8288 `Link` header parsing |
| `Client.FetchEndpoint` | `Client.fetchEndpoint` | calling a resolved endpoint |

There is one way to fetch, so there is no way to assemble a half-guarded pipeline by hand — no calling an endpoint that skipped the scheme allowlist, no discovery that forgot the body cap. It also keeps the surface honest: `Discover` returned endpoints *and* an error together, a contract that is easy to misread and now nobody's problem but ours.

If you need to interpose policy between resolution and the endpoint call, that is a feature request, not a workaround — say what the rule is and it belongs in the client.

---

## Client options

All are `ClientOption`, applied by `NewClient`.

```go
type ClientOption func(*Client)
```

| Option | Default | Behavior |
|---|---|---|
| `WithRegistry(Registry)` | embedded snapshot | Resolve providers against your own registry — a fresher snapshot, or a private list. |
| `WithMaxWidth(int)` | unset | Send `maxwidth` with every endpoint request. Zero omits the parameter. |
| `WithMaxHeight(int)` | unset | Send `maxheight` with every endpoint request. Zero omits the parameter. |
| `WithMaxBodySize(int64)` | 1 MB | Cap bytes read from any response body, page or endpoint. **Zero or less restores the default** rather than meaning "unlimited". |
| `WithAllowPrivateIPs(bool)` | `false` | Permit fetches to loopback, private, and link-local addresses. This is the SSRF guard — leave it off in production. Tests against `httptest` servers must set it `true`. |
| `WithUserAgent(string)` | `oembed-client/1.0 (+…)` | Identify yourself. Some providers rate-limit or block unfamiliar agents. |
| `WithRoundTripper(func(next http.RoundTripper) http.RoundTripper)` | none | Wrap the guarded transport with caching or instrumentation middleware. Your wrapper receives the base transport as `next` and **must delegate to it**; the private-IP guard stays underneath and cannot be removed this way. |

---

## Registry

### `type Registry`

```go
type Registry struct { /* unexported fields */ }
```

Matches candidate URLs against provider scheme patterns. **Immutable after construction and safe for concurrent use without locks** — all pattern compilation and endpoint resolution happens in `NewRegistry`, so `Find` is a lock-free loop over ready matchers.

### `func DefaultRegistry() Registry`

The registry built from the embedded `providers.json` snapshot (369 providers, 838 URL patterns), compiled once at package initialization and shared by every `Client`.

That build costs about **5.9 ms** and retains about **3.9 MB** — 838 compiled patterns — paid at process start rather than on the first request. A corrupt embedded snapshot **panics** during initialization: the file is compiled into the binary, so it can only be a build defect, and failing loudly beats degrading into "no provider ever matches" for the life of the process.

The registry earns its place: Vimeo, Spotify, TikTok, Reddit, GIPHY, Dailymotion, and Instagram serve **no** discovery links in their HTML, so discovery alone cannot resolve them.

### `func NewRegistry(providers []Provider) Registry`

Builds a registry from your own provider list, precompiling every scheme pattern and pre-resolving every endpoint (`{format}` substitution, format choice, format-parameter decision).

- Endpoints with no schemes are discovery-only and never scheme-matched.
- Patterns that fail to compile are **silently dropped** — they simply never match.

### `func (Registry) Find(targetURL string) (Endpoint, bool)`

Returns the endpoint of the first provider scheme that matches, in registry order. Reports `false` when nothing matches.

A `*` in the authority compiles to `[^/]*` and **must not cross a `/`** — this is a security boundary, or `https://*.youtube.com/...` would match `https://evil.com/x.youtube.com/...`. Path wildcards compile to `.*` on purpose, since they legitimately match query strings. Scheme and host match case-insensitively; path matches case-sensitively.

### `func (Registry) Size() int`

The number of precompiled scheme matchers.

### `type Provider` / `type ProviderEndpoint`

Mirror the official `providers.json` structure, with the JSON tags to match.

```go
type Provider struct {
	Name      string             `json:"provider_name"`
	URL       string             `json:"provider_url"`
	Endpoints []ProviderEndpoint `json:"endpoints"`
}

type ProviderEndpoint struct {
	Schemes   []string `json:"schemes"`   // wildcard URL patterns; empty means discovery-only
	URL       string   `json:"url"`       // may contain a {format} placeholder
	Discovery bool     `json:"discovery"` // provider advertises <link rel="alternate">
	Formats   []string `json:"formats"`   // empty makes no promise; JSON is assumed
}
```

Refresh the embedded snapshot at build time — the library never fetches it at runtime:

```console
$ go generate ./...
```

Then update `ProvidersSnapshotDate` to the day you pulled it.

---

## Endpoint

```go
type Endpoint struct {
	URL                string // may already carry query parameters
	Format             string // FormatJSON or FormatXML
	AddFormatParameter bool   // pass format as a "format" query parameter
}
```

A resolved endpoint. `Registry.Find` is the only public producer; the discovery paths build these internally.

`AddFormatParameter` is set by `Registry.Find` only when the provider's declared formats say the parameter is supported. A pre-baked `url` parameter on a discovered endpoint is **preserved, not overwritten**: the client merges into the existing query rather than appending a second `?`.

---

## Response

```go
type Response struct {
	XMLName         xml.Name       `json:"-"                          xml:"oembed"`
	Type            string         `json:"type"                       xml:"type"`
	Version         lenient.String `json:"version"                    xml:"version"`
	Title           string         `json:"title,omitempty"            xml:"title,omitempty"`
	AuthorName      string         `json:"author_name,omitempty"      xml:"author_name,omitempty"`
	AuthorURL       string         `json:"author_url,omitempty"       xml:"author_url,omitempty"`
	ProviderName    string         `json:"provider_name,omitempty"    xml:"provider_name,omitempty"`
	ProviderURL     string         `json:"provider_url,omitempty"     xml:"provider_url,omitempty"`
	CacheAge        lenient.Int64  `json:"cache_age,omitempty"        xml:"cache_age,omitempty"`
	ThumbnailURL    string         `json:"thumbnail_url,omitempty"    xml:"thumbnail_url,omitempty"`
	ThumbnailWidth  lenient.Int64  `json:"thumbnail_width,omitempty"  xml:"thumbnail_width,omitempty"`
	ThumbnailHeight lenient.Int64  `json:"thumbnail_height,omitempty" xml:"thumbnail_height,omitempty"`
	URL             string         `json:"url,omitempty"              xml:"url,omitempty"`
	HTML            string         `json:"html,omitempty"             xml:"html,omitempty"`
	Width           lenient.Int64  `json:"width,omitempty"            xml:"width,omitempty"`
	Height          lenient.Int64  `json:"height,omitempty"           xml:"height,omitempty"`
}
```

Every parameter in oEmbed 1.0 §2.3.4. Marshals to and from both JSON and XML (root element `<oembed>`) through the standard library's default encoders — there are no custom marshalers. Provider extension fields (Flickr's `flickr_type`, Vimeo's `duration`) are **discarded**, not retained.

> ### ⚠️ `HTML` is a trust boundary
>
> It is arbitrary, provider-controlled markup — usually iframe- or script-bearing by design — and this library **intentionally does not sanitize it**, because stripping the scripts and iframes strips the embed. Writing `response.HTML` into your own page hands your DOM to a third party you discovered at runtime. Use [`Embed`](#embedding).

### Tolerant field types

Three fields are typed for what providers actually send, using [`rosetta/lenient`](https://github.com/benpate/rosetta/tree/main/lenient):

- **`lenient.Int64`** (dimensions, cache age) — accepts quoted integers (`"480"`), floats (truncating), `null` and `"100%"` (both → 0, meaning "auto"). Out-of-range values clamp; nothing errors. Top-level integers are read from their source text, so values above 2^53 stay exact.
- **`lenient.String`** (`Version` only) — keeps a JSON number's exact source text, which is why SoundCloud's `"version": 1.0` becomes `"1.0"` and never `"1"`.

`Version` is the *only* tolerant string field. The other string fields reject a mistyped scalar, failing the document.

### Validation, two ways

The package validates differently depending on direction — Postel's law, made explicit.

| | Send side | Receive side |
|---|---|---|
| Function | `Response.Validate()` (exported) | internal, used by the fetch methods |
| Rule | Strict oEmbed 1.0 | Known type + essential payload only |
| Missing dimensions | Rejected where required | Mean "auto" — Mastodon and X send `"height": null` |

Do not route received documents through `Validate()`, and do not loosen `Validate()` to match the receive path.

### `func (Response) Validate() error`

The strict send-side check: `"1.0"` version, a known type, that type's required fields, non-negative cache age, and the all-or-none thumbnail rule. Returns a **validation** error.

---

## Authoring

The response document is the same type in both directions, so the builders live here even though serving oEmbed is out of scope.

### `func NewResponse(oembedType string) Response`

A response of the given type with `Version` pre-stamped.

### `func NewLink(title string) Response`
### `func NewPhoto(url string, width, height int) Response`
### `func NewVideo(html string, width, height int) Response`
### `func NewRich(html string, width, height int) Response`

Valid-by-construction builders: each stamps the version, the type, and that type's required fields, so a correctly-constructed value cannot be spec-invalid.

### `func (*Response) SetThumbnail(url string, width, height int)`

Stamps all three thumbnail fields **together**, so the spec's all-or-none rule cannot be broken piecemeal. An empty `url` is a no-op — a record without an icon simply has no thumbnail.

### `func WriteResponse(w http.ResponseWriter, response Response, format string) error`

Validates, encodes, and writes with the correct `Content-Type`, in one call.

- `format` is `FormatJSON`, `FormatXML`, or `""` (meaning JSON). Anything else is a **not-implemented** error, matching the spec's 501 rule.
- **Validates first**: a spec-invalid document is an error with *nothing written*.
- **Encodes before touching the `ResponseWriter`**: an encoding failure never leaves a half-written 200 behind.
- Content types are `application/json; charset=utf-8` and `text/xml; charset=utf-8` (§2.3.1).

Both orderings are load-bearing. Keep them.

---

## Embedding

`Embed` is the safe path from provider markup to a page. It is **total** — every response classifies to exactly one plan, and it never errors, because degradation *is* the error path.

### `func (Response) Embed(policy EmbedPolicy) Embed`

### `type Embed`

```go
type Embed interface {
	Render() template.HTML
	// contains unexported methods
}
```

A closed interface: the unexported method means only this package's three plans satisfy it, so raw provider HTML cannot escape the classifier unwrapped. Switch on the concrete type.

### The three plans, in descending order of safety

| Plan | When | What renders |
|---|---|---|
| `EmbedIframe` | `HTML` is exactly one `<iframe>` (whitespace aside) with an `https` `src`, no `srcdoc`, no event handlers | The iframe rebuilt by us from an attribute allowlist. Provider content runs on the provider's origin, isolated by ordinary cross-origin rules; their markup never enters your page. |
| `EmbedSandbox` | Messier markup, **and** `policy.AllowSandbox` is true | `<iframe sandbox="allow-scripts allow-popups" srcdoc="…">` |
| `EmbedLink` | Everything else | A plain anchor with title and thumbnail. A boring preview beats an injected script. |

```go
type EmbedIframe struct {
	Src           string
	Width, Height int    // zero means unset
	Title         string
}

type EmbedSandbox struct {
	HTML          string // escaped into srcdoc by Render
	Width, Height int
}

type EmbedLink struct {
	Title        string
	ThumbnailURL string
	TargetURL    string
}
```

Each `Render()` builds markup from a trusted template with full escaping. The sandbox attribute is a **constant, not a parameter** — `allow-scripts` combined with `allow-same-origin` lets embedded script reach up and rewrite the host page, which un-sandboxes everything.

### `type EmbedPolicy`

```go
type EmbedPolicy struct {
	AllowSandbox bool
	MaxWidth     int // zero means no clamp
	MaxHeight    int
}
```

> ### ⚠️ `AllowSandbox` is a product decision, not a trust ranking
>
> It answers "do script-bearing embeds render at all on my site?" — applied uniformly to every provider. **This library never infers it from how an endpoint was found, and deliberately does not report that provenance.**
>
> The containment ordering inverts most people's intuition. `EmbedSandbox` runs in an iframe with a unique **opaque origin**: no cookies, no storage, no parent DOM, no navigation. `EmbedIframe` — permitted for everyone — loads the provider's own origin with **no sandbox at all**. Setting `AllowSandbox` to false therefore blocks the *more* contained of the two plans. It does not make embedding safer.

Your page's own CSP remains the outer wall. `frame-src` should allow the player hosts you expect and nothing else; a strict `script-src` matters too, since srcdoc iframes inherit the embedding page's CSP.

---

## Constants

### Specification

| Constant | Value |
|---|---|
| `Version` | `"1.0"` — typed `lenient.String` to match the field |
| `TypePhoto` | `"photo"` — requires url, width, height |
| `TypeVideo` | `"video"` — requires html, width, height |
| `TypeLink` | `"link"` — no embeddable content |
| `TypeRich` | `"rich"` — requires html, width, height |
| `FormatJSON` | `"json"` |
| `FormatXML` | `"xml"` |
| `ContentTypeJSONOEmbed` | `"application/json+oembed"` |
| `ContentTypeXMLOEmbed` | `"text/xml+oembed"` |

### Registry

| Constant | Value |
|---|---|
| `ProvidersSnapshotDate` | `"2026-08-14"` — when the embedded snapshot was vendored |

### Client

| Constant | Value |
|---|---|
| `DefaultMaxBodySize` | `1 << 20` (1 MB) |

---

## Errors

Every error is a [`derp`](https://github.com/benpate/derp) error carrying a location and an HTTP-shaped code. Branch on kind with `derp.IsNotFound(err)` and friends — **there are no exported sentinel values**.

| Kind | Raised when |
|---|---|
| **NotFound** | No oEmbed endpoint could be resolved for the target — by registry, `Link` header, or HTML discovery. |
| **BadRequest** | A URL's scheme is not `http(s)`, a URL has no host, or an endpoint declares an unrecognized format. |
| **Internal** | The endpoint's response `Content-Type` contradicts the format we requested. |
| **NotImplemented** | `WriteResponse` was handed a format that is neither JSON nor XML — the spec's 501 case. |
| **Validation** | `Response.Validate()` rejected a document you authored. |

Every public entry point returns either a usable `Response` or an error, never both. (Internally, HTML discovery returns partial results alongside a parse error so that a truncated page still resolves — but that contract stops at the package boundary.)
