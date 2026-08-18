package oembed

// EmbedPolicy tells Embed which render plans the caller is willing to
// display. It is a product decision, not a security ranking: this library
// never infers policy from how an endpoint was found, and deliberately does
// not report that provenance.
type EmbedPolicy struct {

	// AllowSandbox permits the EmbedSandbox plan: rendering non-extractable
	// provider HTML inside a sandboxed srcdoc iframe. FALSE degrades that
	// markup to a plain link instead.
	//
	// This is a UX choice — do script-bearing embeds render at all? — and NOT
	// a trust decision about the provider. Note the containment ordering,
	// which is the opposite of most people's intuition: EmbedSandbox runs in
	// an iframe with a unique opaque origin (no cookies, no storage, no
	// parent DOM, no navigation), while EmbedIframe loads the provider's own
	// origin with no sandbox at all. Setting this FALSE therefore blocks the
	// MORE contained of the two plans; it does not make embedding safer.
	AllowSandbox bool

	// MaxWidth and MaxHeight clamp plan dimensions. Zero means no clamp.
	MaxWidth  int
	MaxHeight int
}

// clampDimensions applies the policy's maximum dimensions, preserving zero
// (unset) values.
func clampDimensions(width int, height int, policy EmbedPolicy) (int, int) {

	// RULE: a negative dimension is provider junk, not a size — collapse it to
	// "unset" so it never reaches a width/height attribute.
	if width < 0 {
		width = 0
	}

	if height < 0 {
		height = 0
	}

	if policy.MaxWidth > 0 && width > policy.MaxWidth {
		width = policy.MaxWidth
	}

	if policy.MaxHeight > 0 && height > policy.MaxHeight {
		height = policy.MaxHeight
	}

	return width, height
}
