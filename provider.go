package oembed

// Provider is one entry in the oEmbed provider registry, mirroring the
// structure of the official providers.json published at oembed.com.
type Provider struct {

	// Name is the human-readable provider name (for example "YouTube")
	Name string `json:"provider_name"`

	// URL is the provider's home page
	URL string `json:"provider_url"`

	// Endpoints lists the provider's oEmbed API endpoints
	Endpoints []ProviderEndpoint `json:"endpoints"`
}
