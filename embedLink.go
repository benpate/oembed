package oembed

import (
	"html/template"
)

// EmbedLink is the degraded plan: render the response as a plain link —
// title, thumbnail, anchor.
//
// This is where every other plan falls back to, because a boring preview
// beats an injected script.
type EmbedLink struct {
	Title        string
	ThumbnailURL string
	TargetURL    string
}

// embedPlan marks this type as one of the closed set of Embed plans.
func (EmbedLink) embedPlan() { /* marker only */ }

// Render emits the plain-link fallback.
func (e EmbedLink) Render() template.HTML {
	return renderTemplate(linkTemplate, e)
}

// linkTemplate renders the degraded plan as a plain anchor.
var linkTemplate = template.Must(template.New("link").Parse(
	`<a href="{{.TargetURL}}" rel="noopener noreferrer" target="_blank">{{if .Title}}{{.Title}}{{else}}{{.TargetURL}}{{end}}</a>`))
