package oembed

type Provider struct {
	Name      string             `json:"provider_name"`
	URL       string             `json:"provider_url"`
	Endpoints []ProviderEndpoint `json:"endpoints"`
}

type ProviderEndpoint struct {
	Schemes   []string `json:"schemes"`
	URL       string   `json:"url"`
	Discovery bool     `json:"discovery"`
	Formats   []string `json:"formats"`
}
