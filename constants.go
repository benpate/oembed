package oembed

import "github.com/benpate/rosetta/lenient"

/******************************************
 * Specification Constants
 ******************************************/

// Version is the only version of the oEmbed specification, required in every response.
const Version lenient.String = "1.0"

// TypePhoto identifies a static photo response (requires url, width, height)
const TypePhoto = "photo"

// TypeVideo identifies a playable video response (requires html, width, height)
const TypeVideo = "video"

// TypeLink identifies a plain link response with no embeddable content
const TypeLink = "link"

// TypeRich identifies a rich HTML response (requires html, width, height)
const TypeRich = "rich"

// FormatJSON identifies the JSON oEmbed response format
const FormatJSON = "json"

// FormatXML identifies the XML oEmbed response format
const FormatXML = "xml"

// ContentTypeJSONOEmbed is the link type advertising a JSON oEmbed endpoint
const ContentTypeJSONOEmbed = "application/json+oembed"

// ContentTypeXMLOEmbed is the link type advertising an XML oEmbed endpoint
const ContentTypeXMLOEmbed = "text/xml+oembed"

/******************************************
 * Registry Constants
 ******************************************/

// ProvidersSnapshotDate records when the embedded providers.json snapshot was
// vendored from oembed.com. Update it whenever the snapshot is regenerated.
const ProvidersSnapshotDate = "2026-08-14"

// formatToken is the placeholder in provider endpoint URLs (for example
// "https://audioboom.com/publishing/oembed.{format}") that is replaced with
// the concrete response format.
const formatToken = "{format}"

/******************************************
 * Client Constants
 ******************************************/

// DefaultMaxBodySize is the default cap (1MB) on response bodies read by the
// Client — generous for oEmbed documents, which are small, and for the HTML
// head section that discovery reads.
const DefaultMaxBodySize = 1 << 20

// defaultUserAgent identifies this library in outbound requests when the
// caller has not set one via WithUserAgent.
const defaultUserAgent = "oembed-client/1.0 (+https://github.com/benpate/oembed)"

// maxDiscoverTokenSize caps a single HTML token at 1MB, so one hostile
// attribute cannot balloon memory while the rest of the body is streamed.
const maxDiscoverTokenSize = 1 << 20

/******************************************
 * Embed Constants
 ******************************************/

// maxIframeDimension caps a width or height copied off a provider iframe at
// 32768 pixels — far past any real player, and comfortably inside int on
// every platform, so parsing one cannot overflow. Anything larger is treated
// as junk and dropped rather than clamped, because a provider claiming a
// 100000px player is not reporting a size we should half-believe.
const maxIframeDimension = 1 << 15
