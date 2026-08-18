package oembed

import (
	"html/template"
)

// EmbedSandbox is the fallback plan: provider HTML that could not be reduced
// to a single iframe, rendered only inside a sandboxed srcdoc iframe.
//
// RULE: the sandbox attribute NEVER combines allow-scripts with
// allow-same-origin — together they let embedded script reach up and rewrite
// the host page, which un-sandboxes everything.
type EmbedSandbox struct {
	HTML          string // provider markup, escaped into srcdoc by Render
	Width, Height int
}

// embedPlan marks this type as one of the closed set of Embed plans.
func (EmbedSandbox) embedPlan() { /* marker only */ }

// Render wraps the provider HTML in the sandboxed srcdoc iframe.
func (e EmbedSandbox) Render() template.HTML {
	return renderTemplate(sandboxTemplate, e)
}

// sandboxTemplate renders provider HTML inside a sandboxed srcdoc iframe.
// The sandbox attribute is baked into the template so no caller can add
// allow-same-origin.
var sandboxTemplate = template.Must(template.New("sandbox").Parse(
	`<iframe sandbox="allow-scripts allow-popups" srcdoc="{{.HTML}}"{{if .Width}} width="{{.Width}}"{{end}}{{if .Height}} height="{{.Height}}"{{end}} frameborder="0" loading="lazy"></iframe>`))
