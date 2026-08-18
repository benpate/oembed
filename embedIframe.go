package oembed

import (
	"html/template"
	"strings"

	"golang.org/x/net/html"
)

// EmbedIframe is the preferred plan: the provider's HTML was exactly one
// clean iframe, and we rebuilt it ourselves from its attributes.
type EmbedIframe struct {
	Src           string // https iframe source
	Width, Height int    // player dimensions; zero means unset
	Title         string // accessible title, when the provider carried one
}

// embedPlan marks this type as one of the closed set of Embed plans.
func (EmbedIframe) embedPlan() { /* marker only */ }

// Render builds the iframe tag from the extracted attributes.
func (e EmbedIframe) Render() template.HTML {
	return renderTemplate(iframeTemplate, e)
}

// iframeTemplate renders a rebuilt iframe. Attributes are escaped by
// html/template's contextual auto-escaping.
var iframeTemplate = template.Must(template.New("iframe").Parse(
	`<iframe src="{{.Src}}"{{if .Width}} width="{{.Width}}"{{end}}{{if .Height}} height="{{.Height}}"{{end}}{{if .Title}} title="{{.Title}}"{{end}} frameborder="0" allowfullscreen loading="lazy"></iframe>`))

// iframeAllowedAttributes is the allowlist copied from a provider iframe onto
// the rebuilt one. Everything else — event handlers, srcdoc, style — is
// dropped.
var iframeAllowedAttributes = map[string]bool{
	"src":             true,
	"width":           true,
	"height":          true,
	"title":           true,
	"allow":           true,
	"allowfullscreen": true,
	"frameborder":     true,
	"scrolling":       true,
	"loading":         true,
}

// extractIframe accepts provider HTML only when it is exactly one <iframe>
// (whitespace aside) with an https src, no srcdoc, and no event-handler
// attributes. Anything else fails extraction and falls to the next strategy.
func extractIframe(providerHTML string) (EmbedIframe, bool) {

	root, err := html.Parse(strings.NewReader(providerHTML))

	if err != nil {
		return EmbedIframe{}, false
	}

	iframe, ok := soleIframeNode(root)

	if !ok {
		return EmbedIframe{}, false
	}

	result := EmbedIframe{}

	for _, attr := range iframe.Attr {

		key := strings.ToLower(attr.Key)

		// RULE: srcdoc and event handlers disqualify the whole iframe — they
		// mean the markup is trying to run code, not frame a document.
		if key == "srcdoc" {
			return EmbedIframe{}, false
		}

		if strings.HasPrefix(key, "on") {
			return EmbedIframe{}, false
		}

		if !iframeAllowedAttributes[key] {
			continue
		}

		switch key {
		case "src":
			result.Src = strings.TrimSpace(attr.Val)
		case "width":
			result.Width = attributeInt(attr.Val)
		case "height":
			result.Height = attributeInt(attr.Val)
		case "title":
			result.Title = attr.Val
		}
	}

	// RULE: the rebuilt iframe must point at a real https document.
	if !strings.HasPrefix(strings.ToLower(result.Src), "https://") {
		return EmbedIframe{}, false
	}

	return result, true
}

// soleIframeNode returns the single <iframe> element when the parsed document
// contains exactly one element (whitespace aside) and it is an iframe.
func soleIframeNode(root *html.Node) (*html.Node, bool) {

	var iframe *html.Node
	clean := true

	var walk func(node *html.Node)
	walk = func(node *html.Node) {

		if !clean {
			return
		}

		switch node.Type {

		case html.ElementNode:
			switch node.Data {

			// The parser's own wrapper elements are structural, not content.
			case "html", "head", "body":

			case "iframe":
				if iframe != nil {
					clean = false // more than one iframe
					return
				}
				iframe = node

			default:
				clean = false // any other element disqualifies
				return
			}

		case html.TextNode:
			if strings.TrimSpace(node.Data) != "" {
				clean = false // non-whitespace text disqualifies
				return
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	if !clean {
		return nil, false
	}

	if iframe == nil {
		return nil, false
	}

	return iframe, true
}

// attributeInt parses a positive integer attribute, returning 0 for junk.
func attributeInt(value string) int {

	result := 0

	for _, c := range strings.TrimSpace(value) {

		if c < '0' || c > '9' {
			return 0
		}

		result = result*10 + int(c-'0')

		// RULE: a dimension past maxIframeDimension is junk, not a size. The
		// check runs on every digit so a hostile run of them can never
		// overflow the accumulator on its way to a plausible-looking value.
		if result > maxIframeDimension {
			return 0
		}
	}

	return result
}
