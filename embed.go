package oembed

import (
	"html/template"
	"strings"
)

// Embed is a render plan for an oEmbed response — a closed set of ways the
// response may safely reach a page. Callers switch on the concrete type;
// raw provider HTML can never escape the classifier unwrapped.
type Embed interface {

	// Render returns safe markup for this plan, built from trusted templates
	// with full escaping. No caller ever hand-assembles the sandbox attribute.
	Render() template.HTML

	// embedPlan closes the set. The three implementations — EmbedIframe,
	// EmbedSandbox, and EmbedLink — each declare it in their own file.
	embedPlan()
}

// Embed classifies this response into a render plan under the given policy:
// a rebuilt iframe when the provider HTML is exactly one clean iframe, a
// sandboxed srcdoc wrapper when policy permits, and a plain link otherwise.
// The result is always renderable — degradation is the error path.
func (response Response) Embed(policy EmbedPolicy) Embed {

	link := EmbedLink{
		Title:        response.Title,
		ThumbnailURL: response.ThumbnailURL,
		TargetURL:    response.URL,
	}

	// Only video and rich responses carry embed HTML at all.
	if response.Type != TypeVideo && response.Type != TypeRich {
		return link
	}

	if response.HTML == "" {
		return link
	}

	width, height := clampDimensions(int(response.Width), int(response.Height), policy)

	// Strategy 1: extract the single clean iframe and rebuild it ourselves.
	if iframe, ok := extractIframe(response.HTML); ok {
		iframe.Title = firstNonEmpty(iframe.Title, response.Title)
		if iframe.Width == 0 {
			iframe.Width = width
		}
		if iframe.Height == 0 {
			iframe.Height = height
		}
		iframe.Width, iframe.Height = clampDimensions(iframe.Width, iframe.Height, policy)
		return iframe
	}

	// Strategy 2: srcdoc sandbox, when the policy's trust tier permits it.
	if policy.AllowSandbox {
		return EmbedSandbox{HTML: response.HTML, Width: width, Height: height}
	}

	// Strategy 3: degrade to a link.
	return link
}

// renderTemplate executes a trusted template, returning empty markup when it
// fails.
func renderTemplate(t *template.Template, data any) template.HTML {

	builder := strings.Builder{}

	// A blank spot beats broken markup, and every template here is a compiled
	// constant — a failure means a bug, not bad provider data.
	if err := t.Execute(&builder, data); err != nil {
		return ""
	}

	// #nosec G203 -- each plan's template is a trusted constant and every value
	// it interpolates is contextually escaped by html/template.
	return template.HTML(builder.String())
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {

	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
