package oembed

const TypePhoto = "photo"
const TypeVideo = "video"
const TypeLink = "link"
const TypeRich = "rich"

type OEmbed struct {
	Version      string `json:"version,omitempty"`
	Type         string `json:"type,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	ProviderURL  string `json:"provider_url,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Title        string `json:"title,omitempty"`
	AuthorName   string `json:"author_name,omitempty"`
	AuthorURL    string `json:"author_url,omitempty"`
	HTML         string `json:"html,omitempty"`
}
