package oembed

// Endpoint is a resolved oEmbed endpoint, ready for request building — found
// either by registry match (Registry.Find) or by discovery.
type Endpoint struct {

	// URL is the concrete endpoint URL to call. It may already carry query
	// parameters (discovery links often bake in "url" and "format").
	URL string

	// Format is the response format this endpoint will produce: FormatJSON or
	// FormatXML.
	Format string

	// AddFormatParameter is TRUE when the request should pass the format as a
	// "format" query parameter — set by Registry.Find only when the provider's
	// declared formats say the parameter is supported.
	AddFormatParameter bool
}
