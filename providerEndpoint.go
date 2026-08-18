package oembed

// ProviderEndpoint is one oEmbed API endpoint offered by a Provider.
type ProviderEndpoint struct {

	// Schemes are wildcard URL patterns (like "https://*.youtube.com/watch*")
	// naming the content URLs this endpoint can resolve. An endpoint with no
	// schemes is discovery-only and is never scheme-matched.
	Schemes []string `json:"schemes"`

	// URL is the endpoint address. It may contain a {format} placeholder to be
	// replaced with the requested response format.
	URL string `json:"url"`

	// Discovery is TRUE when the provider supports discovery via
	// <link rel="alternate"> elements on its content pages.
	Discovery bool `json:"discovery"`

	// Formats lists the response formats the endpoint supports ("json",
	// "xml"). An empty list makes no promise; JSON is assumed.
	Formats []string `json:"formats"`
}
