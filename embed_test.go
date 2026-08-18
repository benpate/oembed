package oembed

import (
	"testing"

	"github.com/benpate/rosetta/lenient"
	"github.com/stretchr/testify/require"
)

func TestEmbed_CleanIframeExtracts(t *testing.T) {

	response := Response{
		Type:   TypeVideo,
		Title:  "A Video",
		HTML:   `  <iframe src="https://player.example.com/v/1" width="640" height="360" allowfullscreen></iframe>  `,
		Width:  lenient.Int64(853),
		Height: lenient.Int64(480),
	}

	plan := response.Embed(EmbedPolicy{})

	iframe, ok := plan.(EmbedIframe)
	require.True(t, ok)
	require.Equal(t, "https://player.example.com/v/1", iframe.Src)
	require.Equal(t, 640, iframe.Width) // iframe's own attributes win
	require.Equal(t, 360, iframe.Height)
	require.Equal(t, "A Video", iframe.Title) // backfilled from the response

	rendered := string(iframe.Render())
	require.Contains(t, rendered, `src="https://player.example.com/v/1"`)
	require.Contains(t, rendered, `width="640"`)
	require.NotContains(t, rendered, "srcdoc")
}

func TestEmbed_HostileIframesDegrade(t *testing.T) {

	hostile := []string{
		`<iframe src="http://plain.example.com/v"></iframe>`,                                     // not https
		`<iframe src="javascript:alert(1)"></iframe>`,                                            // javascript: src
		`<iframe srcdoc="<script>alert(1)</script>"></iframe>`,                                   // srcdoc
		`<iframe src="https://x.example/v" onload="alert(1)"></iframe>`,                          // event handler
		`<iframe src="https://x.example/v"></iframe><script>bad()</script>`,                      // extra element
		`<iframe src="https://a.example/1"></iframe><iframe src="https://b.example/2"></iframe>`, // two iframes
		`click <iframe src="https://x.example/v"></iframe>`,                                      // stray text
		`<script>document.write("x")</script>`,                                                   // no iframe at all
		`<iframe></iframe>`,                                                                      // no src
	}

	for _, markup := range hostile {

		response := Response{Type: TypeRich, Title: "T", URL: "https://example.com/page", HTML: markup}

		// Without sandbox permission, every hostile fixture degrades to a link.
		plan := response.Embed(EmbedPolicy{AllowSandbox: false})
		link, ok := plan.(EmbedLink)
		require.True(t, ok, "expected EmbedLink for %q", markup)
		require.Equal(t, "https://example.com/page", link.TargetURL)
	}
}

func TestEmbed_SandboxTierWrapsInsteadOfDegrading(t *testing.T) {

	response := Response{
		Type:   TypeRich,
		HTML:   `<blockquote class="widget">Complex</blockquote><script async src="https://widgets.example.com/w.js"></script>`,
		Width:  lenient.Int64(500),
		Height: lenient.Int64(600),
	}

	plan := response.Embed(EmbedPolicy{AllowSandbox: true})

	sandbox, ok := plan.(EmbedSandbox)
	require.True(t, ok)

	rendered := string(sandbox.Render())
	require.Contains(t, rendered, `sandbox="allow-scripts allow-popups"`)
	require.NotContains(t, rendered, "allow-same-origin")
	require.Contains(t, rendered, "srcdoc=")
	// The provider markup is escaped INTO the attribute, not emitted raw.
	require.NotContains(t, rendered, `<script async`)
}

func TestEmbed_NonEmbeddableTypesLink(t *testing.T) {

	photo := Response{Type: TypePhoto, URL: "https://example.com/p.jpg", Title: "Pic"}
	plan := photo.Embed(EmbedPolicy{AllowSandbox: true})

	link, ok := plan.(EmbedLink)
	require.True(t, ok)
	require.Equal(t, "Pic", link.Title)

	rendered := string(link.Render())
	require.Contains(t, rendered, `rel="noopener noreferrer"`)
}

func TestEmbed_DimensionClamp(t *testing.T) {

	response := Response{
		Type:   TypeVideo,
		HTML:   `<iframe src="https://player.example.com/v/1" width="9999" height="9999"></iframe>`,
		Width:  lenient.Int64(9999),
		Height: lenient.Int64(9999),
	}

	plan := response.Embed(EmbedPolicy{MaxWidth: 800, MaxHeight: 450})

	iframe, ok := plan.(EmbedIframe)
	require.True(t, ok)
	require.Equal(t, 800, iframe.Width)
	require.Equal(t, 450, iframe.Height)
}

// TestEmbed_NegativeDimensionsCollapse pins that a provider's negative
// width/height never reaches a rendered attribute, on either plan that
// carries dimensions.
func TestEmbed_NegativeDimensionsCollapse(t *testing.T) {

	{
		response := Response{
			Type:   TypeVideo,
			HTML:   `<iframe src="https://player.example.com/v/1"></iframe>`,
			Width:  lenient.Int64(-1),
			Height: lenient.Int64(-99),
		}

		plan := response.Embed(EmbedPolicy{MaxWidth: 800, MaxHeight: 450})

		iframe, ok := plan.(EmbedIframe)
		require.True(t, ok)
		require.Equal(t, 0, iframe.Width)
		require.Equal(t, 0, iframe.Height)
		require.NotContains(t, string(iframe.Render()), `width="-`)
		require.NotContains(t, string(iframe.Render()), `height="-`)
	}

	{
		response := Response{
			Type:   TypeRich,
			HTML:   `<b>hi</b><script>alert(1)</script>`,
			Width:  lenient.Int64(-1),
			Height: lenient.Int64(-99),
		}

		plan := response.Embed(EmbedPolicy{AllowSandbox: true})

		sandbox, ok := plan.(EmbedSandbox)
		require.True(t, ok)
		require.Equal(t, 0, sandbox.Width)
		require.Equal(t, 0, sandbox.Height)
		require.NotContains(t, string(sandbox.Render()), `width="-`)
		require.NotContains(t, string(sandbox.Render()), `height="-`)
	}
}

func TestEmbed_RenderEscapesTitles(t *testing.T) {

	response := Response{
		Type:  TypeVideo,
		Title: `"><script>alert(1)</script>`,
		HTML:  `<iframe src="https://player.example.com/v/1"></iframe>`,
	}

	plan := response.Embed(EmbedPolicy{})
	rendered := string(plan.Render())

	require.NotContains(t, rendered, "<script>")
}

// FuzzExtractIframe asserts the iframe extractor never panics, and that any
// successful extraction yields an https src with no event handlers surviving.
