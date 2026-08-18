package oembed

import (
	"encoding/json"
	"encoding/xml"
	"net/http"

	"github.com/benpate/derp"
	"github.com/benpate/rosetta/lenient"
)

// Response is a single oEmbed response document, covering every parameter in
// oEmbed 1.0 §2.3.4. It marshals to and from both JSON and XML (root element
// "oembed"). Provider extension fields are discarded.
type Response struct {

	// XMLName fixes the XML root element to <oembed>. It is ignored by JSON.
	XMLName xml.Name `json:"-" xml:"oembed"`

	// Type is the resource type: "photo", "video", "link", or "rich". Required.
	Type string `json:"type" xml:"type"`

	// Version is the oEmbed version, always "1.0". Required. It is a tolerant
	// String because providers send it as a JSON number often enough to matter.
	Version lenient.String `json:"version" xml:"version"`

	// Title is a text title describing the resource. Optional.
	Title string `json:"title,omitempty" xml:"title,omitempty"`

	// AuthorName is the name of the author/owner of the resource. Optional.
	AuthorName string `json:"author_name,omitempty" xml:"author_name,omitempty"`

	// AuthorURL is a URL for the author/owner of the resource. Optional.
	AuthorURL string `json:"author_url,omitempty" xml:"author_url,omitempty"`

	// ProviderName is the name of the resource provider. Optional.
	ProviderName string `json:"provider_name,omitempty" xml:"provider_name,omitempty"`

	// ProviderURL is the URL of the resource provider. Optional.
	ProviderURL string `json:"provider_url,omitempty" xml:"provider_url,omitempty"`

	// CacheAge is the suggested cache lifetime for this resource, in seconds. Optional.
	CacheAge lenient.Int64 `json:"cache_age,omitempty" xml:"cache_age,omitempty"`

	// ThumbnailURL is a URL to a thumbnail image representing the resource. If
	// any thumbnail field is present, all three must be (enforced by Validate).
	ThumbnailURL string `json:"thumbnail_url,omitempty" xml:"thumbnail_url,omitempty"`

	// ThumbnailWidth is the width of the thumbnail, in pixels.
	ThumbnailWidth lenient.Int64 `json:"thumbnail_width,omitempty" xml:"thumbnail_width,omitempty"`

	// ThumbnailHeight is the height of the thumbnail, in pixels.
	ThumbnailHeight lenient.Int64 `json:"thumbnail_height,omitempty" xml:"thumbnail_height,omitempty"`

	// URL is the source URL of the image. Required for type "photo".
	URL string `json:"url,omitempty" xml:"url,omitempty"`

	// HTML is provider-supplied embed markup, required for types "video" and
	// "rich". SECURITY: this is arbitrary third-party markup — usually iframe-
	// or script-bearing by design — and this library intentionally does NOT
	// sanitize it, because stripping scripts and iframes strips the embed.
	// Render it only inside a sandboxed iframe or an equivalent CSP boundary;
	// never write it directly into your own page.
	HTML string `json:"html,omitempty" xml:"html,omitempty"`

	// Width is the width in pixels required to display the resource. Required
	// for types "photo", "video", and "rich".
	Width lenient.Int64 `json:"width,omitempty" xml:"width,omitempty"`

	// Height is the height in pixels required to display the resource. Required
	// for types "photo", "video", and "rich".
	Height lenient.Int64 `json:"height,omitempty" xml:"height,omitempty"`
}

// NewResponse returns a Response of the given type with the Version pre-stamped, so a
// correctly-constructed value cannot be spec-invalid.
func NewResponse(oembedType string) Response {
	return Response{
		Type:    oembedType,
		Version: Version,
	}
}

/******************************************
 * Response Builders
 ******************************************/

// NewLink returns a valid-by-construction "link" response: version, type, and
// title pre-stamped.
func NewLink(title string) Response {

	result := NewResponse(TypeLink)
	result.Title = title

	return result
}

// NewPhoto returns a valid-by-construction "photo" response: version, type,
// and the required url, width, and height pre-stamped.
func NewPhoto(url string, width int, height int) Response {

	result := NewResponse(TypePhoto)
	result.URL = url
	result.Width = lenient.Int64(width)
	result.Height = lenient.Int64(height)

	return result
}

// NewVideo returns a valid-by-construction "video" response: version, type,
// and the required html, width, and height pre-stamped.
func NewVideo(html string, width int, height int) Response {

	result := NewResponse(TypeVideo)
	result.HTML = html
	result.Width = lenient.Int64(width)
	result.Height = lenient.Int64(height)

	return result
}

// NewRich returns a valid-by-construction "rich" response: version, type, and
// the required html, width, and height pre-stamped.
func NewRich(html string, width int, height int) Response {

	result := NewResponse(TypeRich)
	result.HTML = html
	result.Width = lenient.Int64(width)
	result.Height = lenient.Int64(height)

	return result
}

// SetThumbnail stamps all three thumbnail fields together, so the spec's
// all-or-none thumbnail rule cannot be violated piecemeal. An empty url is a
// no-op: a record without an icon simply has no thumbnail.
func (response *Response) SetThumbnail(url string, width int, height int) {

	if url == "" {
		return
	}

	response.ThumbnailURL = url
	response.ThumbnailWidth = lenient.Int64(width)
	response.ThumbnailHeight = lenient.Int64(height)
}

/******************************************
 * Response Encoding
 ******************************************/

// WriteResponse encodes an oEmbed document to an HTTP response in the
// requested format (FormatJSON, FormatXML, or "" meaning JSON), with the
// correct Content-Type. The document is validated first — be strict in what
// you send — so a spec-invalid document is an error, and nothing is written.
func WriteResponse(w http.ResponseWriter, response Response, format string) error {

	const location = "oembed.WriteResponse"

	// RULE: never serve a spec-invalid document
	if err := response.Validate(); err != nil {
		return derp.Wrap(err, location, "Refusing to serve an invalid oEmbed document")
	}

	// Encode the body BEFORE touching the ResponseWriter, so an encoding
	// error never leaves a half-written response behind.
	body, contentType, err := encodeResponse(response, format)

	if err != nil {
		return derp.Wrap(err, location, "Encoding oEmbed document", format)
	}

	// Write the response
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(body); err != nil {
		return derp.Wrap(err, location, "Writing oEmbed response")
	}

	// Another satisfied customer.
	return nil
}

// encodeResponse renders an oEmbed document in the requested format,
// returning the body and its Content-Type.
func encodeResponse(response Response, format string) ([]byte, string, error) {

	const location = "oembed.encodeResponse"

	switch format {

	// JSON is both an explicit choice and the "no preference" default.
	case "", FormatJSON:

		body, err := json.Marshal(response)

		if err != nil {
			return nil, "", derp.Wrap(err, location, "Marshaling JSON")
		}

		return body, "application/json; charset=utf-8", nil

	// The spec (§2.3.1) serves XML responses as text/xml.
	case FormatXML:

		body, err := xml.Marshal(response)

		if err != nil {
			return nil, "", derp.Wrap(err, location, "Marshaling XML")
		}

		return append([]byte(xml.Header), body...), "text/xml; charset=utf-8", nil
	}

	// RULE: the specification (§2.2) answers an unknown format with a 501
	return nil, "", derp.NotImplemented(location, "Unsupported oEmbed format", format)
}
