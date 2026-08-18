# Project Plan: oEmbed Client Library

Goal: make `benpate/oembed` a top-tier oEmbed **consumer** library for Go — complete against the [oEmbed 1.0 spec](https://oembed.com), idiomatic and ergonomic to call, fast, and secure against the hostile inputs that link-unfurling invites. The package purpose in one sentence: *given a URL, find its oEmbed endpoint and return validated oEmbed metadata.*

## Current State (2026-08-14, post-build)

Phases 1–6 are BUILT and green (see the Build Log at the end of this file for the deviations). The package is a working consumer library: spec-complete dual-encoding types, streaming discovery, a precompiled provider registry from an embedded `providers.json` snapshot, a hardened `Client`, and a full test suite (95% coverage, three fuzz targets, real-provider fixtures in `testdata/`). `lookup.go` is deleted (4.4). Still open: Phase 7 benchmarks, Phase 8 ecosystem adoption (Emissary/sherlock still don't use this package), and Phases 9–10.

Original pre-build state, for reference: the package was a stub — a partial `OEmbed` struct, unused `Provider` types, `lookup.go` entirely commented out, no tests, no dependencies.

## Known Issues Found in Audit

These are the specific defects and gaps identified on 2026-08-14. Every one maps to a numbered step in the phases below.

1. **Missing `url` field** — required for `type=photo` responses; the struct has no field for it at all. → Step 1.1
2. **Missing thumbnail triple** — `thumbnail_url`, `thumbnail_width`, `thumbnail_height`. The spec says if any one is present, all three must be. Emissary's private struct had to re-declare these. → Steps 1.1, 1.5
3. **Missing `cache_age`** — also re-declared privately by Emissary. → Step 1.1
4. **Required fields marked `omitempty`** — `version` (must be `"1.0"`) and `type` are required by spec, and `width`/`height` are required for photo/video/rich; `omitempty` silently emits spec-invalid documents when values are zero. → Steps 1.2, 1.5
5. **No XML support** — the struct carries only `json` tags, but the spec defines an XML response format (`<oembed>` root) and real endpoints serve it. → Step 1.3
6. **Extension fields dropped** — the spec allows providers to add arbitrary extra keys; a flat struct discards them on unmarshal. → Step 1.4
7. **Strict `int` numerics break interop** — providers infamously return `"width": "480"` (string) or floats; plain `int` fails the whole unmarshal. Applies to width, height, thumbnail dimensions, and `cache_age`. → Step 1.6
8. **No per-type validation** — photo requires url/width/height; video and rich require html/width/height; the type constants enforce nothing. → Step 1.5
9. **No discovery** — no parsing of `<link rel="alternate" type="application/json+oembed">` (or the `text/xml+oembed` variant) from HTML. → Phase 2
10. **No provider registry** — `providers.json` is never loaded, scheme wildcards (`https://*.youtube.com/watch*`) are never matched, and `{format}` endpoint substitution (e.g. Flickr) is never performed. → Phase 3
11. **No request building** — nothing composes the endpoint call with `url`, `maxwidth`, `maxheight`, `format`. → Step 4.2
12. **No fetch layer** — no HTTP client at all, and therefore none of the SSRF/bounding protections a fetch layer must carry. → Phases 4, 5
13. **`HTML` field trust boundary undocumented** — provider-supplied embed markup is hostile input that consumers must sandbox; the field says nothing. → Step 5.4
14. **No tests** — zero coverage on a package headed for untrusted-input parsing. → Phase 6

## Serving-Side Survey (2026-08-14)

We publish content that others should be able to unfurl, so the provider side matters too. What exists today, all in Emissary:

- **A working provider endpoint.** `GET /.oembed` (registered in `server.go`, implemented in `handler/oembed.go`) serves JSON and XML, returns 501 for unsupported formats, restricts lookups to its own domain, resolves the home page / `@user` profiles / stream tokens, hides non-public records behind 404, and attaches mediaserver-resized thumbnails with `cache_age`. It has regression tests for the XML-marshaling trap. It is solid but hand-rolled: request parsing, the response struct, and encoding are all private to the handler.
- **No discovery links in CORE templates** (corrected 2026-08-15). Nothing in `_embed/templates` emits `<link rel="alternate" type="application/json+oembed">` — but the EXTERNAL template packages do: bandwagon (album, song, outbox views) and qwertylicious (outbox) emit real discovery links via the `{{.OEmbedJSON}}`/`{{.OEmbedXML}}` builder helpers. So `/.oembed` is LIVE and discoverable on bandwagon.fm today (the handler's regression test pins a real-world bug report), those builder helpers are NOT dead code despite having no in-repo callers, and the route cannot move without a compat alias (same lesson as the `/sse` route move). The remaining 9.6 work is adding the links to `theme-global`/`theme-default` includes-head so core Emissary sites advertise it too.
- **`maxwidth`/`maxheight` ignored.** The spec says thumbnails must respect these request parameters; Emissary hardcodes 300px and never reads them. Minor deviation, easy to fix once parsing is shared.
- **Mastodon API stub.** `handler/mastodon/oembed.go` (`GET /api/oembed`, the instance-level provider method) returns NotImplemented. It could be built on the same record-resolution logic as `/.oembed`.

Conclusion: the serving logic (what records exist, who may see them) rightly stays in Emissary, but the spec mechanics — request parsing, valid-by-construction responses, encoding, discovery links — are generic and belong in this library. That is Phase 9.

## Design Decisions

- **Single flat package.** The domain is small and cohesive; sub-packages would be ceremony. All exported API lives in `package oembed`.
- **Parse HTML with `golang.org/x/net/html`, not goquery.** Discovery only needs to walk `<link>` elements in `<head>`. `x/net/html` is the stdlib-adjacent choice with no dependency tree; goquery would force a large transitive graph on every importer. Callers who already have a goquery document can hand us `io.Reader` bytes instead.
- **Fetch through `benpate/remote`.** Consistency with the rest of the ecosystem, and it already carries the `IsPublicIP` SSRF guard with an `AllowPrivateIPs` escape hatch for loopback test servers.
- **Errors through `benpate/derp`**, mapping the spec's error semantics (404 not found, 401 private resource, 501 format unsupported) to derp status codes.
- **Embed a `providers.json` snapshot with `go:embed`.** The zero-config path works offline and deterministically; a functional option lets callers supply a fresher or custom registry. The library never fetches the registry on its own — no network I/O the caller didn't ask for.
- **No built-in response cache.** Caching policy belongs to the caller (Emissary already has its own cache layers); we expose `CacheAge` and document it. Revisit only if a real consumer needs more.
- **Constructor injection, no globals.** `NewClient(options ...Option)` holds all configuration; no package-level mutable state, no work in `init()` beyond the embedded registry parse (and prefer lazy/`sync.Once` if that parse is nontrivial).

## Phase 1: Data Model — Spec-Complete Types

- [x] **1.1 Complete the `OEmbed` struct.** Add `URL`, `ThumbnailURL`, `ThumbnailWidth`, `ThumbnailHeight`, and `CacheAge`. Field set must cover every parameter in oEmbed 1.0 §2.3.4: type, version, title, author_name, author_url, provider_name, provider_url, cache_age, thumbnail_url, thumbnail_width, thumbnail_height, plus the type-specific url, html, width, height.
- [x] **1.2 Fix required-field marshaling.** Remove `omitempty` from `version` and `type`. Keep `omitempty` on genuinely optional fields. Provide `func New(oembedType string) OEmbed` that stamps `Version: "1.0"` so a correctly-constructed value can't be spec-invalid.
- [x] **1.3 Add XML support.** Dual-tag every field (`json:` and `xml:`), add ``XMLName xml.Name `xml:"oembed"` ``, and add round-trip tests for both encodings. Keep it a struct — `encoding/xml` cannot marshal maps (lesson already learned in Emissary's handler).
- [x] **1.4 Preserve extension fields.** Custom `UnmarshalJSON` that captures unknown keys into an `Extra map[string]any` field, and `MarshalJSON` that re-emits them. XML extras are best-effort; document the asymmetry.
- [x] **1.5 Add `Validate() error`.** Enforce version == "1.0", a known type, per-type requirements (photo: url/width/height; video, rich: html/width/height), and the all-or-none thumbnail-triple rule. The client calls this on every parsed response; standalone use stays available for provider-side callers like Emissary.
- [x] **1.6 Tolerant numerics.** Introduce an internal integer type whose `UnmarshalJSON`/`UnmarshalXML` accepts JSON numbers, numeric strings, and floats (truncating), and use it for width, height, thumbnail dimensions, and cache_age. Marshal output is always a plain JSON number. Table-test the ugly real-world inputs: `480`, `"480"`, `480.0`, `""`, `null`.

## Phase 2: Discovery

- [x] **2.1 `Discover(r io.Reader, baseURL string) ([]DiscoveredEndpoint, error)`.** Stream-parse with `x/net/html`, collecting `<link rel="alternate">` (and the also-seen `rel="alternative"`) whose `type` is `application/json+oembed` or `text/xml+oembed`. Return href + format for each, resolved against `baseURL` (hrefs are often relative). Stop parsing at `</head>` when possible; never load the whole body into memory as a requirement of the API.
- [x] **2.2 Preference order.** JSON endpoints before XML when both are advertised; preserve document order within a format. Document this.
- [x] **2.3 Malformed-input safety.** The parser must return useful results from truncated, misnested, or hostile HTML (x/net/html tolerates this; tests must prove we do too). Fuzz test the extractor (Phase 6).

## Phase 3: Provider Registry

- [x] **3.1 Embed a snapshot.** Vendor `providers.json` from oembed.com via `go:embed`, with a documented `make`/`go generate` step to refresh it. Record the snapshot date in the file or a sibling constant.
- [x] **3.2 Registry type and construction.** `NewRegistry(providers []Provider)` plus `DefaultRegistry()` for the embedded snapshot. Registries are immutable after construction — safe for concurrent use with no locks.
- [x] **3.3 Scheme matching.** Implement wildcard matching for the `schemes` patterns: `*` matches within a segment (`https://*.youtube.com/watch*`), and match against the full candidate URL. Case-insensitive host, case-sensitive path, per URL norms. Endpoints with no `schemes` but `"discovery": true` are discovery-only and never scheme-matched. Precompile every pattern at registry construction (see 7.1).
- [x] **3.4 `{format}` substitution.** When an endpoint URL contains `{format}`, substitute the requested format; when it doesn't, request format via the `format` query parameter only if the endpoint's `formats` list says it's supported.
- [x] **3.5 `Registry.Find(url string) (Endpoint, bool)`.** First scheme match wins, in registry order. Return the concrete endpoint URL ready for request building.

## Phase 4: Client

- [x] **4.1 `Client` with functional options.** `NewClient(options ...Option)` — options for `WithRegistry`, `WithMaxWidth`/`WithMaxHeight` defaults, `WithAllowPrivateIPs(bool)` (threaded into benpate/remote, default false), `WithMaxBodySize`, `WithMaxRedirects`, `WithUserAgent`, `WithHTTPClient` (escape hatch). A zero-config `NewClient()` must work well.
- [x] **4.2 Request building.** Compose endpoint calls with `url` (required), `maxwidth`, `maxheight`, `format` per spec §2.2. Always URL-encode; never string-concatenate the target URL into the query.
- [x] **4.3 Resolution pipeline.** `Client.Fetch(ctx context.Context, targetURL string) (OEmbed, error)`: (1) try registry match — no page fetch needed; (2) on miss, fetch the page and run discovery; (3) call the resolved endpoint; (4) parse JSON or XML by endpoint format and Content-Type; (5) `Validate()`; return. Every network call takes the caller's `ctx`.
- [x] **4.4 Split entry points for callers mid-pipeline.** Export the intermediate steps — `Registry.Find`, `Discover`, `Client.FetchEndpoint(ctx, endpoint, targetURL)` — so sherlock (which already has the page body in hand) can skip straight to discovery without a second fetch. This resurrects the intent of the commented-out `ParseResponse` in `lookup.go`; delete that dead block when this lands.
- [x] **4.5 Error semantics.** Map provider responses to derp errors: 404 → not found, 401 → unauthorized (private resource), 501 → format not implemented, non-2xx otherwise → wrapped with status. No-endpoint-found is a distinct, testable error value.

## Phase 5: Security Hardening

- [x] **5.1 SSRF guard on every fetch.** Both the discovery page fetch and the endpoint fetch go through benpate/remote's public-IP guard. `WithAllowPrivateIPs(true)` exists for tests against `httptest` loopback servers — same convention as hannibal. Redirects must re-validate the target (confirm benpate/remote does; if not, cap and check per hop).
- [x] **5.2 Scheme allowlist.** Discovered and registry-resolved endpoint URLs must be `http` or `https` before fetching; reject anything else. Prefer https when a provider advertises both.
- [x] **5.3 Bound everything.** Response bodies capped via `re.ReadResponseBody` with a configurable maximum (default on the order of 1 MB — oEmbed documents are small); redirect count capped; rely on caller `ctx` plus a sane default timeout for the total request. Applies to the discovery page fetch too, which reads arbitrary remote HTML.
- [x] **5.4 Document the `HTML` trust boundary.** Doc comment on the `HTML` field and in the README: it is arbitrary provider-controlled markup, intentionally not sanitized (embeds are legitimately iframe/script heavy), and must be rendered only inside a sandboxed iframe or equivalent CSP boundary. The library must never be the component that "made it safe."
- [x] **5.5 Content-Type discipline.** Parse the endpoint response by expected format; refuse to interpret a response whose Content-Type contradicts it (e.g. `text/html` where JSON was requested) rather than sniffing. Log-friendly error, not a guess.

## Phase 6: Testing

- [x] **6.1 Unit tests per go-testing standards** — table tests with testify for: struct round-trips (JSON and XML), tolerant numerics, extension-field preservation, `Validate()` per type, scheme wildcard matching, `{format}` substitution, discovery extraction, and error mapping.
- [x] **6.2 Fixture-driven interop tests.** Recorded real responses (YouTube, Flickr, Vimeo, a Mastodon status) as testdata; assert they parse and validate. These catch spec-vs-reality drift.
- [x] **6.3 `httptest` integration tests** for the full `Fetch` pipeline — registry hit, discovery fallback, XML-only endpoint, 401/404/501 mapping — with `AllowPrivateIPs(true)`.
- [x] **6.4 Fuzz tests** for the discovery HTML extractor, the scheme matcher, and the tolerant-numeric unmarshaler — the three parsers that eat untrusted bytes.
- [x] **6.5 Race + coverage + full go-vet gates** before any phase is declared done.

## Phase 7: Performance

- [x] **7.1 Precompiled scheme matchers.** Compile wildcard patterns once at registry construction (the embedded registry has thousands of schemes); matching a URL must not re-parse patterns. Benchmark `Registry.Find` and keep it allocation-light.
- [x] **7.2 Lazy default registry.** Parse the embedded snapshot once via `sync.Once`, not at `init()`.
- [x] **7.3 Streaming discovery.** The discovery parser tokenizes and stops at `</head>`/first-match where correct, rather than materializing a DOM for a multi-megabyte page.
- [x] **7.4 Benchmarks in-repo** for registry match, discovery parse, and response unmarshal, so regressions are visible.

## Phase 8: Documentation & Ecosystem Adoption

- [x] **8.1 Rewrite README.md** per readme-files conventions: quick-start (`NewClient` + `Fetch`), the trust-boundary warning for `HTML`, registry refresh instructions, and the security defaults. Delete the "Not Implemented Yet" section when it stops being true.
- [x] **8.2 Add AGENTS.md** capturing the non-obvious rules: struct-not-map for XML, tolerant numerics rationale, registry immutability, SSRF/AllowPrivateIPs threading.
- [x] **8.3 Emissary adoption (provider side).** Once 1.1–1.6 land, migrate Emissary's private `oEmbedResponse` in `handler/oembed.go` to `oembed.OEmbed` — one canonical spec struct instead of two. Emissary keeps its own handler logic; it only adopts the type.
- [x] **8.4 Sherlock adoption (consumer side).** Wire discovery + `FetchEndpoint` into sherlock's metadata pipeline, closing its "oEmbed: not implemented" gap. Sherlock supplies the page body it already fetched; no double fetch.
- [ ] **8.5 Tag a release** after Phases 1–6 are green, then bump consumers. Pre-1.0 versioning, but treat the exported API as add-only from the first tag. Phase 9 can land as a later additive minor release.

## Phase 9: Server Primitives

Generic spec mechanics for anyone *serving* oEmbed, extracted from what Emissary hand-rolls today. Record resolution and access control stay with the application; this library owns the protocol.

- [x] **9.1 Request parsing.** `ParseRequest(query url.Values) (Request, error)` returning a typed `Request{URL, MaxWidth, MaxHeight, Format}`. Empty format means JSON; unsupported format returns a derp 501, matching the spec rule Emissary already enforces by hand. Framework-agnostic: takes `url.Values`, not an echo/steranko context.
- [x] **9.2 Valid-by-construction response builders.** `NewLink(title)`, `NewPhoto(url, width, height)`, `NewVideo(html, width, height)`, `NewRich(html, width, height)` — each stamps `Version: "1.0"` and the correct type so the per-type required fields can't be forgotten; `Validate()` from Step 1.5 is the backstop. Fluent-or-plain setters for the optional fields (provider, author, cache_age, thumbnail — the thumbnail setter takes all three values, enforcing the all-or-none rule).
- [x] **9.3 Response encoding.** `WriteResponse(w http.ResponseWriter, response OEmbed, format string) error` — correct Content-Type (`application/json` or `text/xml`), correct body per format, one place to get it right. Built on `net/http` so any framework can adapt it.
- [x] **9.4 Thumbnail clamping.** A helper that applies the request's `maxwidth`/`maxheight` to thumbnail dimensions, fixing the spec deviation Emissary currently ships (hardcoded 300px, request parameters ignored).
- [x] **9.5 Discovery link primitives.** Generate the `<link rel="alternate" type="application/json+oembed" ...>` (and `text/xml+oembed`) tags for a page head, given the endpoint URL and the page's canonical URL — returned as data and as pre-escaped HTML for templates. Include the equivalent HTTP `Link` header value for non-HTML resources. This is the consumer-side `Discover` (Phase 2) run in reverse, so the two share constants and get tested against each other.
- [ ] **9.6 Emissary integration.** *Partial (2026-08-17): `handler/oembed.go` and its tests are rebuilt on the primitives that survived the server-half cut — `Response`, `NewLink`, `SetThumbnail`, `WriteResponse`, and the `Format*` constants.* Remaining: add discovery links to `theme-global/includes-head.html` and the `theme-default` copy (9.5 was cut too, so those are hand-written now), and optionally implement the Mastodon `GET /api/oembed` stub. `maxwidth`/`maxheight` stay unhonored: every document Emissary serves is a `link` with a fixed 300px thumbnail and no embed dimensions to clamp, and `ClampSize` went with the server half.
- [x] **9.7 Round-trip test.** Serve a document with the Phase 9 primitives, discover and fetch it with the Phase 2–4 client against `httptest`, and assert the result validates — the library eating its own output is the best interop test we can automate.

## Explicitly Out of Scope (for now)

- **Record resolution and access control for served documents** — which URLs exist and who may see them is application logic (Emissary's handler keeps it); the library provides only the protocol mechanics (Phase 9).
- **Response caching** — caller's job; we surface `CacheAge`.
- **Automatic providers.json refresh over the network** — a build-time refresh step only; no runtime background fetches.
- **HTML sanitization of embed markup** — general-purpose sanitization cannot be done meaningfully for embeds; the boundary is documented instead (5.4), and the structured alternatives live in Phase 10.

## Phase 10: Rich HTML Embeds — Rendering Safely

The `html` field on `video` and `rich` responses is the whole point of oEmbed and also its whole attack surface: it is arbitrary markup, usually script-bearing, authored by a third party we discovered at runtime. "Sanitize it" is a false comfort — strip the scripts and iframes and you've stripped the embed. The honest strategies, in descending order of safety:

1. **Iframe extraction (preferred).** In practice most providers' `html` is exactly one `<iframe src="https://provider/embed/...">`. Parse it; if it is a single iframe and nothing else, discard the provider's markup entirely and rebuild a clean iframe from an allowlist of attributes (`src`, `width`, `height`, `title`, `allow`, `allowfullscreen`), enforcing an `https` src. The provider's content then runs on the provider's origin, isolated by the browser's own cross-origin rules — we never inject their markup into our page at all.
2. **Sandbox wrapping (fallback).** When `html` is more than one clean iframe (scripts, blockquotes, widget loaders), render it only inside `<iframe srcdoc="..." sandbox="allow-scripts allow-popups">`. The load-bearing rule: **never combine `allow-scripts` with `allow-same-origin` on srcdoc content** — together they let the embedded script reach up and rewrite the host page, which un-sandboxes everything. Size the frame from the response's `width`/`height`, clamped.
3. **Degrade to link (last resort).** When policy forbids both (or extraction fails and the caller opts out of srcdoc), fall back to rendering the response as `type=link` — title, thumbnail, and an anchor. A boring preview beats an injected script.

Trust should also be tiered by *how we found the endpoint*: a registry match against the vendored `providers.json` is a curated, known provider; a discovery `<link>` on an arbitrary page is self-asserted and deserves stricter policy (extraction-or-link only, no srcdoc). The embedding page's CSP (`frame-src`) remains the consumer's outer wall; we document what to allow.

- [x] **10.1 `Embed()` classifier.** `func (o OEmbed) Embed(policy EmbedPolicy) (Embed, error)` returning a closed set of render plans: `EmbedIframe{Src, Width, Height, Allow, Title}` (extraction succeeded), `EmbedSandbox{HTML, Width, Height}` (srcdoc wrapping permitted), or `EmbedLink{Title, ThumbnailURL, TargetURL}` (degraded). Callers switch on the type; illegal states (raw html escaping the classifier) are unrepresentable.
- [x] **10.2 Iframe extractor.** Parse `html` with `x/net/html`; accept only a document that is exactly one `<iframe>` (whitespace aside), copy allowlisted attributes, require `https` src, and reject `srcdoc`/`javascript:`/event-handler attributes. Fuzz this parser (extends Step 6.4) — it eats the most hostile bytes in the library.
- [x] **10.3 `EmbedPolicy`.** Small config struct: allow srcdoc-sandbox yes/no, max width/height clamp, per-trust-tier rules (registry-matched vs discovery-found — the client records provenance on the `OEmbed` result so the policy can read it), optional per-provider iframe-host allowlist.
- [x] **10.4 Renderers.** Helpers that turn each plan into markup: the clean iframe tag, and the sandboxed wrapper with the correct `sandbox` attribute baked in so no caller hand-assembles it (and no caller can accidentally add `allow-same-origin`). Emit as `template.HTML` from trusted, fully-escaped templates only.
- [x] **10.5 Documentation.** Extend the 5.4 trust-boundary docs into a "rendering embeds" README section: the three strategies, the srcdoc rule, the CSP `frame-src` guidance, and an explicit statement that passing `OEmbed.HTML` to `innerHTML`/`template.HTML` directly is always wrong.
- [ ] **10.6 Emissary/sherlock wiring.** When link previews render remote embeds, they go through `Embed()` with a policy set from an **operator-facing UX setting, applied uniformly to every provider** — see the D-1 decision below; the original "sandbox only for registry-matched" plan is REJECTED. On the serving side (Phase 9), any rich/video documents we publish build their `html` from our own escaped templates, so consumers running strategy 1 extract cleanly from us.

### D-1 (2026-08-16): embed policy is UX, never provenance — REJECTED the trust tier

Ben's ruling. `AllowSandbox` must **not** be derived from registry membership, and this library must **not** report provenance at all (the proposed `Endpoint.Source` field and the `(Response, Endpoint, error)` signature change are dropped).

Two reasons, and the first is the load-bearing one:

1. **The tier is backwards.** `EmbedSandbox` renders in an iframe with a unique opaque origin — no cookies, no storage, no parent DOM, no navigation. `EmbedIframe`, which is allowed for *every* provider regardless of provenance, loads the provider's own origin with **no sandbox at all**. Gating sandbox on provenance blocks the more contained plan while leaving the less contained one open, so it buys no security.
2. **It is the wrong question.** Whether script-bearing embeds render at all is a product decision for the site operator, not a trust ranking derived from who appears in a vendored `providers.json` snapshot.

Live mismatch to fix on the way back up the stack: sherlock's `classifyEmbed` still passes `AllowSandbox: fromRegistry`. A loud `TODO` marks it in `metadata/extract-oembed.go`. The `fromRegistry` plumbing through `findOEmbedEndpoint` exists only to feed that call and dies with it.

## Build Log (2026-08-16) — Link header discovery + argument-order fixes (Ben's direction)

- **`DiscoverLinkHeader(header, baseURL)`** added — the consumer-side counterpart to `AdvertiseLinkHeader`, parsing RFC 8288 `Link` headers (multiple headers, comma-separated link-values, quoted parameters containing commas, quoted-pair escapes, case-insensitive `rel`/`type`, first-occurrence-wins per RFC). Reuses the existing `relIsAlternate`, `formatFromLinkType`, and `sortEndpoints` helpers.
- **Precedence is registry → Link header → HTML body, short-circuiting at the first hit** (Ben's call; I had proposed merging header and body results, which was overruled). Applies identically in `Fetch` and `FetchHTML`.
- **`FetchHTML` gained an `http.Header` argument** — `(ctx, pageURL, header, reader)`. If you have the HTML you have the headers; mocks pass `nil`, which skips the step.
- **`FetchEndpoint` arguments swapped** to `(ctx, targetURL, endpoint)`, so all three fetch methods now lead with the URL being resolved.
- **Fuzz found a real bug, latent in the HTML path too:** both discovery paths did an inline scheme check that accepted host-less references like `http:`. Both now call `validateHTTPURL` (scheme *and* host). Crasher checked in at `testdata/fuzz/FuzzDiscoverLinkHeader/`.
- Known limitation, deliberately not built: when a body exceeds `MaxBodySize`, `remote` errors from `Send()` and `discoverEndpoints` returns before inspecting headers — precisely the large non-HTML resources the header serves. The headers *are* recoverable (`remote` assigns the response before reading the body), so this can be revisited if a real provider hits it.

## Build Log (2026-08-16) — Provider trio renamed to `Advertise*` (Ben's direction)

`Discover` (consumer: reads discovery links) and `DiscoveryLinks*` (provider: writes them) sat adjacent in godoc and read as one family while pointing in opposite directions. The provider trio is now named for the action it performs:

- `DiscoveryLinks` → **`AdvertiseLinks`**, `DiscoveryLinksHTML` → **`AdvertiseLinksHTML`**, `DiscoveryLinkHeader` → **`AdvertiseLinkHeader`** (renamed with `gopls`).
- **The `DiscoveryLink` type keeps its name** — it names the artifact, and "discovery link" is the standard term for `<link rel="alternate" type="application/json+oembed">`. The verbs describe what you do (advertise); the noun describes the thing (a discovery link). `discoveryLink.go` therefore keeps its filename under the one-type-per-file rule, as does the private `discoveryHref` helper.
- Tests, fuzz target, and the example renamed to match (`TestAdvertiseLinks`, `TestAdvertiseLinks_ParityWithDiscover`, `FuzzAdvertiseLinks`, `ExampleAdvertiseLinksHTML`). No checked-in fuzz corpus existed for the renamed target, so no seeds were orphaned. Earlier build-log entries still cite the old names as the historical record.

## Build Log (2026-08-16) — `OEmbed` renamed to `Response` (Ben's direction)

The package's central type was `oembed.OEmbed`, which stutters (go-quality §1: "no stutter") and had no counterpart to the `Request` type Phase 9 added. Completed task text above still says `OEmbed`; it is left as the historical record.

- **`OEmbed` → `Response`**, renamed with `gopls` so only real references moved (`isOEmbedType` and the `ContentTypeJSONOEmbed`/`ContentTypeXMLOEmbed` media-type constants correctly kept their names). `Request`/`Response` now pair, and `WriteResponse` finally names the type it takes.
- **`New` → `NewResponse`**; `plainOEmbed` → `plainResponse`. The four typed builders (`NewLink`/`NewPhoto`/`NewVideo`/`NewRich`) are unchanged and remain the preferred constructors.
- **Method receivers `o` → `response`**, matching the package convention (`client`, `request`, `link`, `registry`); `o` stood for the old type name and meant nothing after the rename.
- **`oembed.go` → `response.go`** per the one-type-per-file rule, with the package doc extracted to `doc.go` and rewritten — it still described a "consumer library," which Phase 9 made wrong. It now documents both the consuming and providing halves, the Postel split, and the HTML trust boundary.
- Tests followed: `oembed_test.go` → `response_test.go`, `TestOEmbed_*`/`BenchmarkOEmbed_*`/`FuzzOEmbed_*` → `TestResponse_*` etc., `ExampleNew` → `ExampleNewResponse`.
- **Consumer repos deliberately NOT updated** (Ben's instruction). Emissary's `handler/oembed.go` + its test and sherlock's `metadata/extract-oembed.go` still reference `oembed.OEmbed`/`oembed.New`, so both fail to compile against the local `replace` until that pass runs.

## Build Log (2026-08-16) — Structural cleanup (Ben's direction)

Four layout/naming corrections, now recorded as ecosystem-wide conventions (memory: go-file-organization-conventions):

- **All package-level constants consolidated into `constants.go`**, grouped by concern (spec, registry, client); function-scoped `const location` declarations stay with their functions.
- **Every exported type moved to its own dedicated camelCase file**, holding the type, its methods, its constructors, and the helpers only it uses: `request.go` (Request + ParseRequest + ClampSize), `endpoint.go`, `discoveryLink.go` (was links.go), `providerEndpoint.go`, `embedPolicy.go`, `embedIframe.go`, `embedSandbox.go`, `embedLink.go`. `embed.go` keeps only the `Embed` interface and the classifier; `server.go` keeps only free functions (builders + WriteResponse).
- **`Option` renamed to `ClientOption`** and moved, with all seven `With*` option functions, into its own `clientOption.go`.
- **The `ErrNoEndpoint` sentinel (`errors.New`) removed** per go-errors conventions: the no-endpoint case returns `derp.NotFound` directly, and callers test the kind with `derp.IsNotFound`. No external consumer referenced the sentinel.
- Test files re-mirrored to match: `request_test.go` and `embedIframe_test.go` split out, `discoveryLink_test.go` renamed.

## Build Log (2026-08-15) — Phase 9 Server Primitives

9.1–9.5 and 9.7 built (server.go, links.go + mirrored tests); 9.6 (Emissary integration) is the remaining task, gated on approval to work outside this folder. Decisions and deviations:

- **`WriteResponse` validates before encoding (Ben's call: "validate before we deliver content").** An invalid document returns an error with nothing written; encoding also happens before the `ResponseWriter` is touched, so an encode failure can't leave a half-written 200.
- **Discovery links always emit both formats (Ben's call: "both, as we currently do")**, JSON first, matching what bandwagon/qwertylicious templates advertise today. `discoveryHref` preserves the exact URL shape of Emissary's live `oEmbedURL` helper (`?url=<escaped>&format=<f>`).
- **Clamping is `Request.ClampSize(width, height)` — general-purpose, not thumbnail-only (9.4).** Proportional fit within `MaxWidth`/`MaxHeight`; zero constraint = unbounded; non-positive ("auto") dimensions pass through; never rounds a visible dimension to zero. Serves embed dimensions and thumbnails alike.
- **Builders stay minimal (9.2).** The four `New*` constructors plus `SetThumbnail(url, w, h)` (all-or-none rule made unviolable; empty url = no-op, repurposed from Emissary's `setOEmbedThumbnail` guard). No fluent setter chain — the optional fields are exported and plain assignment reads fine.
- **`DiscoveryLink.HTML()` escapes by hand** (`template.HTMLEscapeString`) because html/template's contextual escapers entity-encode the `+` in the media type. Parity with `Discover` is pinned by test and fuzz (`FuzzDiscoveryLinks` round-trips arbitrary page URLs through generate→discover byte-for-byte).
- **`ParseRequest` checks `format` before `url`**, so the 501 capability answer outranks the missing-parameter 400 — Emissary's live ordering.
- Also removed a leftover no-op self-`replace` directive from go.mod.

## Build Log (2026-08-15) — Phase 10 Embed classifier

10.1–10.5 built; 10.6 (sherlock wiring) landed in sherlock's metadata engine the same day. Deviations from the plan text:

- **`Embed(policy)` returns no error (10.1).** The classifier is total: every response classifies to one of the three plans, with `EmbedLink` as the safe floor. An error return would just be a fourth, worse way to spell "degrade to link".
- **Provenance rides on `EmbedPolicy`, not on the `OEmbed` result (10.3).** The caller knows how it found the endpoint (registry vs discovery) and sets `AllowSandbox` accordingly; adding a non-spec field to `OEmbed` would have dragged the xmlEnvelope parity rule into it for nothing.
- **Renderers are `Render()` methods on each plan (10.4)**, all built from constant `html/template` templates so escaping is contextual and the sandbox attribute (`allow-scripts allow-popups`, never `allow-same-origin`) cannot be assembled by hand.
- **Fuzzed (10.2):** `FuzzExtractIframe` asserts extraction never yields a non-https src and no event handler survives into rendered output.

## Build Log (2026-08-14)

Phases 1–6 built in one pass; all go-vet gates green. Deliberate deviations from the plan text above, each grounded in something discovered during the build:

- **`Int` is exported, not internal (1.6).** Provider-side callers (Emissary, Phase 8.3) must construct `Width: 300` values directly; an exported struct field of an unexported type is hostile API. Untyped constants still assign cleanly.
- **One `Endpoint` type, not `DiscoveredEndpoint` (2.1).** `Registry.Find`, `Discover`, and `Client.FetchEndpoint` (4.4) all speak the same `Endpoint` struct, so mid-pipeline callers need no conversions.
- **`WithMaxRedirects` and `WithHTTPClient` don't exist (4.1).** benpate/remote hard-caps redirects at 5 inside its guarded transport and deliberately doesn't accept a foreign `http.Client` (that would disarm the SSRF guard). `WithRoundTripper` is the escape hatch instead, mirroring remote's own.
- **Validation is split: strict send, lenient receive (1.5, revised per Ben's review).** `Validate()` is the strict spec check for documents we author (provider side, Phase 9). The client accepts received documents through a forgiving internal check instead — known type plus essential payload (photo url, video/rich html); missing/null dimensions mean "auto". Motivating case: Mastodon and Twitter/X send `"height": null` for auto-height embeds; pinned by `testdata/mastodon.json`.
- **`Int` parsing IS rosetta `convert.Int` (1.6, revised twice per Ben's review).** The type keeps only the JSON/XML plumbing; all conversion semantics are rosetta's, including clamping out-of-range values to the int bounds and zeroing garbage (Postel's law — unmarshal errors only on structurally invalid documents). rosetta's `null.Int` was evaluated as an off-the-shelf replacement but is strict (`strconv.Atoi` only); promoting a tolerant nullable int into rosetta remains the candidate long-term home.
- **"Prefer https when both advertised" (5.2) is not a reordering.** Preference order stays as 2.2 defines it (JSON before XML, document order within format); the scheme allowlist (http/https only) is enforced everywhere. Many providers.json endpoints are plain http and still must work.
- **Phase 7 status:** 7.1's precompilation and 7.2/7.3 landed as design constraints of Phases 2–3 (checked above); the 7.1/7.4 *benchmarks* are the remaining Phase 7 work.
- **8.1/8.2 landed early in reduced form.** The stale "Not Implemented Yet" README would have been lying the moment this merged, and repo conventions require README/AGENTS sync with code changes. Full Phase 8 (adoption + release) remains open.
- **Fuzzing found one real behavior:** `regexp.Compile` rejects invalid UTF-8, so scheme patterns can fail to compile. `NewRegistry` drops such patterns (they never match); the crasher is checked in at `testdata/fuzz/`.

## Registry Value Analysis (2026-08-14, per Ben's review)

Question: does the embedded providers.json registry earn its keep, or does discovery cover everyone? Two measurements:

**Self-reported census:** 282 of 375 scheme-matched endpoints declare `"discovery": true`; **93 declare no discovery support** — a quarter of the registry says the registry is the only way to find it. That set includes Twitter/X, Instagram, Facebook, Reddit, TikTok, SoundCloud, Tumblr, CodePen, and Kickstarter.

**Live probes (19 major providers, browser UA, residential IP):** fetched a real content page, checked the served HTML for `json+oembed` links, and called the registry endpoint.

- **Discovery links actually present:** YouTube, Flickr, SoundCloud, Twitter/X (verified real `<link rel="alternate" type="application/json+oembed">` on x.com — its `discovery:false` self-report is wrong), TED, Bluesky, Pinterest, Mastodon.
- **No discovery links, but working registry endpoint (registry-only in practice):** **Vimeo** (no `rel=alternate` links at all in served HTML, despite its `discovery:true` self-report), **Spotify**, **TikTok**, **Reddit**, **GIPHY**, **Dailymotion**, **Instagram** (endpoint even answered without a token in this probe).
- **Dead registry entry:** Tumblr (no links, endpoint 404).
- **Bot-blocked for curl entirely:** CodePen, Kickstarter (page 403 means server-side discovery is impossible there regardless; inconclusive on the endpoint).

**Conclusion: keep the registry.** Vimeo, Spotify, TikTok, Reddit, GIPHY, and Instagram are major providers that a discovery-only client cannot resolve. Probes ran from a residential IP with a browser UA — a real server in a datacenter hits more bot walls, making discovery *less* reliable than measured and the registry more valuable. Staleness is real but cheap to manage (`go generate` refresh, `ProvidersSnapshotDate`, and Fetch falls back to discovery on any registry miss, so a stale entry degrades gracefully unless the endpoint itself moved — c.f. Tumblr).

## Phase 11: Emissary Metadata Consumption (via sherlock, added 2026-08-15)

Ben's directive: for everything CONSUMER-side, Emissary should use sherlock's new `metadata` package rather than calling `oembed.Client` directly. `sherlock.Client.Metadata(ctx, url)` returns a `metadata.Card` — strictly richer than a raw oEmbed response (description, language, authors, published/modified dates, provider icon, same-origin-verified canonical URL, and a Kind/Embed split with an iframe-first mode ladder), already merged from oEmbed + Open Graph + Twitter Cards + ActivityStreams + native HTML, and already threaded through sherlock's UA/SSRF/body-cap config that Emissary's ActivityStream service constructs today.

**The boundary stays:** the PROVIDER side (`/.oembed`, Phase 9 primitives) keeps `oembed.Response` — that is the spec wire format. `metadata.Card` is explicitly a rebuildable cache entry, never persisted or federated, and has no wire encoding. Emissary serves `oembed.Response`, consumes `Card`, and never touches `oembed.Client` itself.

- [ ] **11.1 URL-metadata endpoint.** Emissary has NO backend for remote-URL metadata today (`stream-article-editorjs/editor.html` ships its linkTool `endpoint` commented out). Add an authenticated handler (e.g. `GET /.metadata?url=...`) that calls `sherlock.Client.Metadata` and returns the EditorJS linkTool response shape (`{success, link, meta: {title, description, image}}`) built from the Card. Authenticated-only and rate-limited — it is a fetch-on-behalf-of proxy; sherlock's SSRF guard stays ON.
- [ ] **11.2 Wire the editor.** Point the linkTool `endpoint` config in `stream-article-editorjs/editor.html` at 11.1, un-commenting the feature.
- [ ] **11.3 RSS-following restore enrichment.** When the POLL/RSS following path is restored (emissary-specs/projects/RSS-FOLLOWING-RESTORE.md), use Cards to enrich synthesized documents for non-ActivityPub followings (title/thumbnail/author for plain web pages), replacing the legacy sherlock.Load synthesis for preview purposes.
- [ ] **11.4 Render Card embeds through a policy.** When Emissary renders a Card's `Embed` (link previews in streams), construct iframes from `IframeURL`/`StreamURL` first; provider HTML only sandboxed, never written raw into a page. The sandbox-or-link choice comes from the operator-facing setting in **D-1**, applied to every provider alike — do NOT reintroduce provenance tiering here.
- [ ] **11.5 Cache Cards.** Cards are rebuildable cache entries by design — store them in an Emissary cache keyed by URL with `FetchedAt` + a TTL, so the editor and previews don't re-fetch on every render.

## Build Log (2026-08-16) — Tolerant scalars moved to `rosetta/lenient` (Ben's direction)

**The reflection hack is gone.** `Response.UnmarshalJSON` used to catch a failed decode, walk the struct tags via `reflect` (`stringJSONKeys`, memoized with `sync.OnceValue`), stringify every mistyped scalar sitting in a string-typed field, and retry. That machinery — plus `stringifyMistypedFields` and the `plainResponse` alias — is deleted. `Response` now has NO custom marshaler or unmarshaler in either format; the `<oembed>` root element comes from its `XMLName` tag, which works precisely *because* no `MarshalXML` overrides it.

**Tolerance is now a property of the field types**, and those types live in `github.com/benpate/rosetta/lenient` (new package, Ben's call): `lenient.Int64` and `lenient.String`. `oembed.Int`/`oembed.String` are deleted. go.mod carries `replace github.com/benpate/rosetta => ../rosetta` until Ben tags a rosetta release containing `lenient`.

**`Version` is the only `lenient.String` field.** The old hack made every string field tolerant, so this narrows behavior: `{"title": 42}` now fails the document where it used to parse as `"42"`. Scoped deliberately — `version` is the only field providers are known to mistype (SoundCloud sends `1.0` as a JSON number). Widening it would drag `TypePhoto`/`TypeRich`/`FormatJSON`/etc. into the named type and force conversions at every comparison. `const Version` is now typed `lenient.String` to match the field. Both halves are pinned by `TestResponse_JSONMistypedFields`.

**FUZZ FOUND A REAL BUG in `Int`, latent since Phase 1.** `UnmarshalJSON` decoded into `any` via `json.Unmarshal`, which makes every JSON number a `float64` — so any integer above 2^53 was silently rounded. `10000000000000001` parsed as `10000000000000000`. The round-trip property in `FuzzJSONProperties` caught it in 14 seconds. Fixed with an exact-integer fast path (`strconv.ParseInt` on the source text) taken only when the decoded value is a `float64`, so `json.Unmarshal` still does the JSON validation and every documented tolerance — quoted strings, floats truncating, bools coercing, single-element arrays unwrapping, out-of-range clamping — is unchanged. Crasher checked into `rosetta/lenient/testdata/fuzz/FuzzJSONProperties/`. A nested integer (`[9007199254740993]`) still rounds; documented in the type comment, not fixed.

`lenient` ships nine fuzz targets, including two universal ones (`FuzzJSONProperties`, `FuzzStructFields`) that any future lenient type inherits by being added to the `jsonTargets` table.

**`Int` became `Int64` (Ben's call, same day).** `convert.Int` clamps to the *platform* int width — `maxIntAsFloat = boundaryIf(math.MaxInt == math.MaxInt32, ...)` — so on a 32-bit target the type silently capped at 2^31, and the exact fast path (`ParseInt` with `bitSize: 0`) capped with it. `GOARCH=wasm` is 32-bit, so this was reachable. Proof it was real: `GOOS=linux GOARCH=386 go vet ./...` failed to *compile* the test file, because `test("large number preserved", 2147483648, ...)` overflows a 32-bit `int` constant. The type is now `Int64 int64`, backed by `convert.Int64`, parsing with `bitSize: 64`, and `386`/`arm`/`wasm` all vet clean. Naming follows rosetta's existing `null.Int`/`null.Int64` convention rather than redefining what `Int` means. Ben's ruling: precision beyond int64 is explicitly NOT a goal — values past the int64 range clamp, and an integer nested inside an array still rounds through `float64`. `TestInt64_RangeIsPlatformIndependent` guards the range; the cross-arch vet is the static half of that guarantee and belongs in the gate list.

## Build Log (2026-08-16) — `DiscoveryLink` removed, docs realigned to client-only (Ben's direction)

`DiscoveryLink` and its `HTML()` method are **deleted** (`discoveryLink.go`, `discoveryLink_test.go`). They were the last fragment of the provider half: after the `Advertise*` trio was cut, nothing in the package constructed one, leaving an exported type with a render method and no producer. The hand-escaping rule it carried (`template.HTMLEscapeString`, because html/template entity-encodes the `+` in `application/json+oembed`) went with it — that constraint now has no code to constrain, and its AGENTS.md entry is removed. If the provider half is ever rebuilt, that rule must be rediscovered from this entry.

**Docs realigned to what actually ships.** `doc.go` no longer claims "both sides of the protocol"; it now reads Consuming / Rendering / Authoring, and says plainly that request parsing and endpoint advertisement are out of scope. The README tagline changed from "Consumer and Provider" to "The Careful oEmbed Client for Go", and the `## Server` section — which documented `ParseRequest`, `Request.ClampSize`, `AdvertiseLinksHTML`, and `AdvertiseLinkHeader`, none of which exist — is replaced by a shorter `## Authoring a Response` covering what genuinely remains: the `New*` builders, `SetThumbnail`, `Validate`, and `WriteResponse`.

**One doc bug fixed on the way through:** the README's embed-strategy list still described `AllowSandbox` as "meant for registry-matched providers only" — the exact provenance framing D-1 rejected, contradicting the paragraph three lines below it that calls it a product decision and not a trust ranking. The provenance clause is gone.

**Current public API surface** (43 symbols): `Client` + `NewClient` + 7 `ClientOption`s; `Fetch`/`FetchHTML`/`FetchEndpoint`; `Discover`/`DiscoverLinkHeader`; `Registry`/`NewRegistry`/`DefaultRegistry`/`Find`/`Size`; `Provider`/`ProviderEndpoint`/`Endpoint`; `Response` + `NewResponse`/`NewLink`/`NewPhoto`/`NewVideo`/`NewRich`/`SetThumbnail`/`Validate`/`Embed` + `WriteResponse`; `Embed`/`EmbedPolicy`/`EmbedIframe`/`EmbedSandbox`/`EmbedLink` + `Render`; and the 11 spec/registry/client constants.

## Build Log (2026-08-16) — Client API narrowed to `Fetch` + `FetchHTML` (Ben's direction)

**`Discover`, `DiscoverLinkHeader`, and `Client.FetchEndpoint` are now unexported** (`discover`, `discoverLinkHeader`, `fetchEndpoint`), renamed with `gopls`. `Fetch` and `FetchHTML` are the entire client.

**Why all three together, not just `Discover`:** they are one story. `FetchEndpoint` is only useful if something public can *produce* an `Endpoint`, and `Discover` is only useful if something public can *consume* one. Unexporting either alone strands the other. The security argument is the same shape: with one entry point there is no way to assemble a pipeline that skipped the `validateHTTPURL` allowlist or the body cap.

**The justification that did NOT survive scrutiny:** "callers need it." sherlock's `findOEmbedEndpoint` was the only external caller, and it is replaceable line-for-line by `FetchHTML(ctx, doc.FinalURL, doc.Header, bytes.NewReader(doc.Body))` — the same registry → Link header → HTML ladder in one call. The only thing it added was the `fromRegistry` provenance flag that D-1 already condemned.

**Fallout in this repo:** `ExampleDiscover` and `ExampleDiscoverLinkHeader` were deleted — a godoc `Example` must name an exported identifier, and `go vet` fails otherwise. Their cases are already covered by `TestDiscover` and `TestDiscoverLinkHeader`. A local `discover := func(...)` helper inside `TestDiscover` was renamed `discoverEndpoints` because it would have shadowed the newly-unexported function (gopls refused the rename until it was). Fuzz targets keep their `FuzzDiscoverLinkHeader` name so the checked-in `testdata/fuzz` corpus stays valid.

**Fallout in sherlock (BLOCKED, needs approval):** `metadata/extract-oembed.go` calls `oembed.Discover` directly and will not compile once this ships. It is already broken against the local replace (`oembed.OEmbed` → `Response`), so this joins the same consumer pass: replace `findOEmbedEndpoint` + `FetchEndpoint` with a single `FetchHTML` call, and delete the `fromRegistry` plumbing with it (D-1).

**Still public and NOT decided:** `Registry.Find`, `Registry.Size`, `DefaultRegistry`, `NewRegistry`, `Provider`, `ProviderEndpoint`. `WithRegistry` requires most of them, but `Find`/`Size` could follow the others private — sherlock uses `DefaultRegistry().Find` as a cheap "is this a known provider?" pre-check, which is the only argument for keeping `Find` exported. Ben's call.

Public surface after this change: **49 exported symbols**, down from 52.

## Build Log (2026-08-16) — Default registry built at `init()`, `Client.registry` de-pointered (Ben's direction)

**`sync.OnceValue` → `init()`.** The embedded snapshot now compiles at package initialization into a plain private `var defaultRegistry Registry`. Ben's call, and the right one for the 99% case: nearly every consumer uses the default, so the choice was only ever *when* to pay — and paying at boot beats stalling one unlucky first request. Measured cost: **5.87ms to build, 3.85MB retained** (838 compiled patterns). The JSON parse is only 1.04ms of that; regex compilation is the other 5.06ms, which is why "define the registry as a Go literal instead of JSON" was rejected — see the analysis below.

**A corrupt snapshot now PANICS** instead of falling back to `Registry{}`. The file is compiled into the binary via `go:embed`, so an unparseable snapshot can only be a build defect, never a runtime condition. The old silent fallback degraded into "no provider ever matches" for the life of the process — the worst kind of quiet.

**`Client.registry` is now a `Registry` value, not `*Registry`.** `NewClient` sets the default first and applies options after, so `WithRegistry` overrides naturally. `registryOrDefault()` is deleted. The pointer's real cost was not indirection but ambiguity: it encoded three states (nil / set / set-to-empty), which made `WithRegistry(NewRegistry(nil))` — an explicitly empty registry — indistinguishable from "no registry configured". That is now a legal, honored choice, pinned by `TestNewClient_RegistryDefaults`.

**Rejected: defining the default registry as a Go literal instead of `providers.json`.** Two reasons. (1) The stated benefit — "save hassle when we deliver an executable" — already exists: `//go:embed` compiles the bytes into the binary. Verified by building a program in a separate module, confirming `youtube.com/oembed` appears in its strings, and running it from a directory containing no `providers.json` (reported all 838 matchers). (2) The cost it would remove is the small half: JSON is 18% of the build time and 2% of the allocations; regex compilation is 86%/98% and a Go literal cannot avoid it. A package-level `[]Provider{...}` literal also cannot live in read-only data — slices need runtime construction — so the generated init code would repeat most of the allocation anyway. Against a realistic few-hundred-microsecond saving: a ~6-9k-line generated file, a code generator to maintain, unreviewable snapshot diffs, and slower compiles.

**Open optimization, NOT done — full spec at `emissary-specs/projects/OEMBED-REGISTRY-HOST-INDEX.md`:** `Find` is a linear scan over all 838 compiled regexes (~100µs/call; sherlock calls it up to 4× per page). Bucketing matchers by literal host — `map[string][]schemeMatcher`, extracted from the same parse that builds the regex — would cut that to ~1µs and make lazy per-bucket compilation viable, which would also shrink the init cost. **The regex must still run; the map is a prefilter, never the decision** — the wildcard-in-authority rule is a security boundary. Wants its own tests and its own fuzz pass.
