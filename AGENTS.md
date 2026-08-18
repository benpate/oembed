# oembed — Agent Notes

See [README.md](README.md) for the tour and [API.md](API.md) for the full reference. These are the non-obvious rules.

**API.md is exhaustive by design** — this package is the exception to the usual no-API-doc rule (Ben's call). When you add, remove, or change an exported symbol, update API.md in the same change or it becomes a liability.

## Response carries NO custom marshalers — the tolerant field types do that job

`Response` marshals and unmarshals through the default encoders in both formats; the `<oembed>` root comes from its `XMLName` tag, which only works *because* there is no `MarshalXML` to override it. Tolerance lives entirely in the `lenient.Int64` and `lenient.String` field types. Don't add a `Response.UnmarshalJSON` back to "fix" a mistyped provider field — give that field a tolerant type instead.

## Tolerant scalars live in rosetta now — don't tighten them, and don't fork them

`lenient.Int64` and `lenient.String` are `github.com/benpate/rosetta/lenient`; they used to live here. `Int64` absorbs quoted integers, floats, nulls, and `"100%"` — and it is `Int64`, not `Int`, because `convert.Int` clamps to the *platform* int width and would cap at 2^31 on a 32-bit target; `String` keeps a JSON number's **exact source text**, which is why SoundCloud's `"version": 1.0` becomes `"1.0"` and never `"1"`. Don't replace these fields with plain `int`/`string` — the first `"width": "480"` from a provider would fail the whole unmarshal — and don't re-add a local copy when a provider does something new. Fix it in rosetta, where the fuzz suite is.

`Version` is the only `lenient.String` field, because it is the only field providers are known to mistype. Widening that is a deliberate decision, not a cleanup — every plain `string` field made tolerant also drags its comparison constants into the named type.

**go.mod carries a local `replace` for rosetta** until `lenient` ships in a tag. Don't run `go mod tidy` and keep the result.

## Strict on send, lenient on receive — two validators, on purpose

`Validate()` is the strict spec check for documents *we author* (the `New*` builders + `WriteResponse`). The client accepts received documents through the forgiving `validateReceived()` instead: known type plus the essential payload (photo url, video/rich html); missing/null dimensions mean "auto" (Mastodon and Twitter/X send `"height": null` — `testdata/mastodon.json` pins this). Don't route received documents through `Validate()`, and don't loosen `Validate()` to match the receive path.

## Registries are immutable; all pattern work happens at construction

`NewRegistry` precompiles every scheme pattern and pre-resolves every endpoint ({format} substitution, format choice, format-parameter decision), so `Find` is a lock-free loop over ready matchers. Never add post-construction mutation. Patterns that fail to compile (invalid UTF-8 — a real fuzz finding) are silently dropped: they simply never match.

`Find` is a LINEAR SCAN over all 838 patterns (~93µs/call, 0 allocs) — a host-index optimization is specced but NOT built: `emissary-specs/projects/OEMBED-REGISTRY-HOST-INDEX.md`. If you implement it, the map is a PREFILTER and the regex must still run; the wildcard-in-authority rule above is why.

The embedded snapshot is compiled in `init()` (~5.9ms, ~3.9MB retained) and `Client.registry` is a plain `Registry` VALUE, defaulted in `NewClient`. Don't reintroduce a `*Registry` or lazy resolution to "save" that cost — the nil-able field carried a three-way ambiguity (unset / set / set-to-empty) that made an explicitly empty `WithRegistry` indistinguishable from no registry at all. A corrupt snapshot panics at init on purpose; it is a build defect, not a runtime condition.

## Wildcard scope is a security boundary

In `compileSchemePattern`, a `*` in the authority compiles to `[^/]*` — it must not cross a `/`, or `https://*.youtube.com/...` would match `https://evil.com/x.youtube.com/...`. Path wildcards compile to `.*` on purpose (they legitimately match query strings). Scheme+host match case-insensitively, path case-sensitively.

## The client builds its own request URLs — remote's Query() would stack them

`remote.Transaction.RequestURL()` appends `?` + query blindly, but discovered endpoints usually already carry a query string (`.../oembed?url=...`). `buildRequestURL` merges into the existing query via `url.Values` and hands remote a finished URL. A pre-baked `url` parameter on a discovered endpoint is preserved, not overwritten.

## SSRF guard is inherited from benpate/remote — don't re-implement it

The private-IP guard lives in remote's transport dialer (DNS-rebinding-safe, re-runs per redirect hop). This package only threads `WithAllowPrivateIPs` through; tests against `httptest` servers must set it TRUE. The additional rules here are the http(s)-only scheme allowlist (`validateHTTPURL`, applied to targets, discovered hrefs, and endpoints) and the body cap.

## Content-Type discipline: refuse, never sniff

`fetchEndpoint` reads the raw body (`Result(&[]byte)`) precisely so remote's content-type-driven decoding is bypassed; the response is parsed by the *endpoint's declared format* only after `contentTypeMatchesFormat` agrees. Don't switch to remote's typed Result decoding — it would silently parse mislabeled responses.

## WriteResponse validates first and encodes before writing — keep both orderings

An invalid document must return an error with **nothing on the wire** (strict in what we send), and encoding happens before the `ResponseWriter` is touched so an encode failure never leaves a half-written 200 behind.

## Embed() is total, and its trust knob belongs to the caller

The classifier never errors — `EmbedLink` is the safe floor, and every response classifies to exactly one of the three plans. **This library does not report provenance** (registry-matched vs discovery-found), and that is a decision, not an omission: `AllowSandbox` is a UX choice about whether script-bearing embeds render at all, applied uniformly to every provider — never a trust ranking derived from registry membership. Don't add an `Endpoint.Source` field or thread provenance out of the fetch methods; it was proposed and rejected (2026-08-16). The containment ordering is also the reverse of intuition: `EmbedSandbox` is an opaque-origin iframe, while `EmbedIframe` loads the provider's own origin unsandboxed, so refusing sandbox blocks the *more* contained plan. Never render `Response.HTML` outside a plan's `Render()`, and never touch the sandbox template — `allow-scripts` + `allow-same-origin` un-sandboxes everything, which is why the attribute is a constant, not a parameter.

## Discovery precedence is registry → Link header → HTML body, and the header short-circuits

`Fetch` and `FetchHTML` are the ONLY public entry points — `discover`, `discoverLinkHeader`, and `fetchEndpoint` are unexported on purpose (Ben's call), so no caller can assemble a pipeline that skips the scheme allowlist or the body cap. Don't re-export them to make a test or a consumer easier; widen `Fetch`/`FetchHTML` instead. Both follow the same ladder and **stop at the first hit** — a Link-header endpoint is used outright without parsing the HTML body (Ben's call). Don't "improve" this by merging header and body results: the short-circuit is deliberate, and merging would change which endpoint answers. `FetchHTML` takes the header explicitly because a body alone has none; `nil` is a valid argument that skips the step.

## Discovered URLs are validated with validateHTTPURL, never an inline scheme check

Both `linkEndpoint` (HTML) and `linkHeaderEndpoint` (Link header) must call `validateHTTPURL` on the resolved reference. An inline `scheme != "http" && scheme != "https"` test looks equivalent but lets host-less references like `http:` through — a real fuzz finding (`FuzzDiscoverLinkHeader` corpus pins it). The validator checks scheme *and* host.
