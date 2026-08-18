# 🖼️ oEmbed

[![Go Reference](https://pkg.go.dev/badge/github.com/benpate/oembed.svg)](https://pkg.go.dev/github.com/benpate/oembed)
[![Version](https://img.shields.io/github/v/release/benpate/oembed?include_prereleases&style=flat-square&color=brightgreen)](https://github.com/benpate/oembed/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/benpate/oembed/go.yml?branch=main)](https://github.com/benpate/oembed/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/benpate/oembed?style=flat-square)](https://goreportcard.com/report/github.com/benpate/oembed)
[![Codecov](https://img.shields.io/codecov/c/github/benpate/oembed.svg?style=flat-square)](https://codecov.io/gh/benpate/oembed)

## The Careful oEmbed Client for Go

`oembed` turns a URL into validated metadata — the title, author, thumbnail, and embeddable HTML that providers like YouTube, Vimeo, Flickr, and Mastodon publish for their content. It resolves endpoints, tolerates what providers actually send, and treats their markup as the untrusted input it is.

Everything meets at one type, `oembed.Response` — the spec's response document, in memory.

This README is the tour; [API.md](API.md) is the full reference for every exported symbol.

## Client

Give it a URL. It resolves the endpoint against the [embedded provider registry](#registry) first (zero page fetches on a hit), then fetches the page and looks for an advertised endpoint — first in the HTTP `Link` header, then by stream-parsing the `<head>` for `<link rel="alternate" type="application/json+oembed">` tags. Then it calls the endpoint and hands back a checked `Response`.

```go
// Zero configuration: embedded registry, SSRF guard on, 1MB body cap
client := oembed.NewClient()

// Fetch resolves the URL (registry first, discovery fallback),
// calls the endpoint, and validates the response
response, err := client.Fetch(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
if err != nil {
    return err
}

fmt.Println(response.Title, response.AuthorName, response.ThumbnailURL)
```

Already have the response in hand? `FetchHTML` runs the same pipeline against what you supply instead of the network:

```go
// Same result as Fetch, minus the page request you already made
response, err := client.FetchHTML(ctx, pageURL, header, bytes.NewReader(pageBody))
```

Pass the response's own `http.Header` so `Link`-header discovery works — a `nil` header is fine and simply skips that step, which is what mocks and tests usually want.

Discovery tolerates truncated, misnested, and outright hostile HTML, and stops at `</head>` rather than reading megabytes of body. When no endpoint can be found at all, the error reports as not-found.

`Fetch` and `FetchHTML` are the whole client. The intermediate steps — header parsing, HTML discovery, calling a resolved endpoint — are unexported, so there is one way to fetch and no way to assemble a half-guarded pipeline by hand.

`NewClient` takes functional options, and the zero-configuration `NewClient()` is a sane client on its own:

| Option | Default | What it does |
|---|---|---|
| `WithRegistry(registry)` | embedded snapshot | Resolve providers against your own registry instead of the vendored `providers.json` — a fresher snapshot, or a private list. |
| `WithMaxWidth(pixels)` | unset | Send `maxwidth` with every endpoint request, asking providers to size embeds and thumbnails to fit. Zero omits the parameter. |
| `WithMaxHeight(pixels)` | unset | Send `maxheight` with every endpoint request. Zero omits the parameter. |
| `WithMaxBodySize(bytes)` | 1 MB | Cap how many bytes are read from any response body, page or endpoint. Zero or less restores the default. |
| `WithAllowPrivateIPs(bool)` | `false` | Permit fetches to non-public addresses. **Leave this off in production** — it is the SSRF guard. Turn it on for tests against loopback servers. |
| `WithUserAgent(string)` | `oembed-client/1.0 (+…)` | Identify yourself to providers. Some rate-limit or block unfamiliar agents, so set this to something you own. |
| `WithRoundTripper(wrap)` | none | Wrap the guarded transport with your own middleware for caching, instrumentation, or extra headers. Your wrapper receives the base transport as `next` and must delegate to it; the private-IP guard stays underneath. |

## Authoring a Response

This is a client library, so serving oEmbed is not its job — there is no request parser and no endpoint advertiser. What it does carry is the response *document*, because that is the same type either way, and writing one correctly is easy to get wrong.

```go
// Version and type are pre-stamped, so the document can't be spec-invalid
response := oembed.NewLink("My Page Title")
response.ProviderName = "My Site"

// All three thumbnail values move together, or none do
response.SetThumbnail("https://example.com/thumb.png", 600, 400)

// Validate, encode, and set the right Content-Type in one call
err := oembed.WriteResponse(w, response, oembed.FormatJSON)
```

`NewLink`, `NewPhoto`, `NewVideo`, and `NewRich` stamp the version and type so a document can't be spec-invalid by construction, and `SetThumbnail` takes all three thumbnail values together so the spec's all-or-none rule can't be broken piecemeal. `WriteResponse` validates before it writes: an invalid document is an error with nothing on the wire, never a half-written response.

## Registry

An embedded snapshot of the official `providers.json` (369 providers, 838 URL patterns) compiles into fast matchers on first use, so the common case — a YouTube or Vimeo link — resolves with **zero page fetches**. Wildcard schemes (`https://*.youtube.com/watch*`), `{format}` substitution, and format negotiation all work as the registry intends. Registries are immutable after construction and safe for concurrent use with no locks.

The registry earns its keep: Vimeo, Spotify, TikTok, Reddit, GIPHY, Dailymotion, and Instagram serve **no** discovery links in their HTML, so discovery alone cannot resolve them.

The snapshot refreshes at build time only — the library never fetches it over the network on its own:

```console
$ go generate ./...   # re-downloads providers.json from oembed.com
```

Then update `ProvidersSnapshotDate` to match the day you pulled it. Callers who need a fresher or private list can supply their own:

```go
client := oembed.NewClient(oembed.WithRegistry(oembed.NewRegistry(providers)))
```

## Security

**Outbound requests are guarded by default.** Every fetch goes through [benpate/remote](https://github.com/benpate/remote)'s SSRF protection: non-public addresses (loopback, private ranges, cloud metadata IPs) are refused unless you explicitly opt in with `WithAllowPrivateIPs(true)`, and the check lives in the transport dialer, so it re-runs on every redirect hop and survives DNS rebinding. Response bodies are capped at 1MB by default, redirects are bounded, and endpoints must be `http(s)` — `javascript:`, `data:`, and friends are dropped wherever a URL enters, including hrefs found during discovery.

**Content types are refused, never sniffed.** A response whose `Content-Type` contradicts the format we asked for is rejected rather than guessed at.

**The `HTML` field is a trust boundary.** It is arbitrary, provider-controlled markup — usually iframe- or script-bearing by design — and this library intentionally does **not** sanitize it, because stripping the scripts and iframes strips the embed. Writing `response.HTML` into your own page hands your DOM to a third party you discovered at runtime. Never do that; use the `Embed()` classifier, which turns a response into one of three render plans and never lets raw provider markup escape unwrapped:

```go
policy := oembed.EmbedPolicy{
    AllowSandbox: siteAllowsScriptEmbeds, // your UX call, applied to every provider alike
    MaxWidth:     800,
    MaxHeight:    600,
}

switch plan := response.Embed(policy).(type) {
case oembed.EmbedIframe:  // provider HTML was exactly one clean https iframe — rebuilt by us
    page.Write(plan.Render())
case oembed.EmbedSandbox: // messier markup, wrapped in <iframe sandbox srcdoc="...">
    page.Write(plan.Render())
case oembed.EmbedLink:    // everything else degrades to a plain anchor
    page.Write(plan.Render())
}
```

The three strategies, in descending order of safety:

1. **Extract** — when `HTML` is exactly one `<iframe>` (whitespace aside) with an https `src`, no `srcdoc`, and no event handlers, the iframe is rebuilt from an attribute allowlist. The provider's content then runs on the provider's origin, isolated by the browser's own cross-origin rules; their markup never enters your page.
2. **Sandbox** — when policy permits (`AllowSandbox`), the markup renders inside `<iframe sandbox="allow-scripts allow-popups" srcdoc="...">`. The sandbox attribute is baked into a constant template so no caller can add `allow-same-origin`, which combined with `allow-scripts` would let the embed reach up and rewrite the host page.
3. **Degrade** — everything else becomes a plain link, because a boring preview beats an injected script.

`AllowSandbox` is a **product decision, not a trust ranking** — do script-bearing embeds render at all on your site? — and this library never infers it from how the endpoint was found. Note the containment ordering before you reach for it, because it inverts most people's intuition: `EmbedSandbox` runs in an iframe with a unique opaque origin (no cookies, no storage, no parent DOM, no navigation), while `EmbedIframe` loads the provider's own origin with **no sandbox at all**. Turning `AllowSandbox` off blocks the more contained of the two plans; it does not make embedding safer.

Your page's own CSP remains the outer wall — `frame-src` should allow the player hosts you expect and nothing else. A strict `script-src` matters too: srcdoc iframes inherit the embedding page's CSP, so a strict policy blocks inline script inside a sandboxed embed entirely.

## Other

### Strict on send, liberal on receive

Providers infamously send `"width": "480"` as a string, floats where integers belong, `null` heights for auto-sized embeds, and — in SoundCloud's case — `"version": 1.0` as a JSON *number*. Two tolerant field types from [rosetta/lenient](https://github.com/benpate/rosetta/tree/main/lenient) absorb all of it: `lenient.Int64` for the dimensions and `lenient.String` for the version, which keeps a number's exact source text so `1.0` stays `"1.0"`. Received documents are then accepted whenever their essential payload is usable; a missing dimension simply means "auto". `Response.Validate()` stays the strict spec check for documents *you* author, so tolerance never leaks into what you publish.

### Formats

Both JSON and XML round-trip in both directions (XML root element `<oembed>`), through the standard library's default encoders. Provider extension fields — Flickr's `flickr_type`, Vimeo's `duration` — are discarded rather than retained.

### Credits

Portions of this code are inspired or duplicated from:

* https://github.com/dyatlov/go-oembed
* https://github.com/artyom/oembed
* https://oembed.com/

## No Warranty

This software is provided as-is, without any warranty of any kind. Use it at your own risk.

## Pull Requests Welcome

I'm trying to make oembed the best it can be, and your help is greatly appreciated. If you find a bug or have an idea for a new feature, please open an issue or submit a pull request. We're all in this together! 🖼️
