package oembed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzExtractIframe(f *testing.F) {

	f.Add(`<iframe src="https://x.example/v" width="1" height="2"></iframe>`)
	f.Add(`<iframe srcdoc="<b>x</b>"></iframe>`)
	f.Add(`<div><iframe src="https://x.example/v"></iframe></div>`)
	f.Add(`<<<>>`)

	f.Fuzz(func(t *testing.T, markup string) {

		iframe, ok := extractIframe(markup)

		if !ok {
			return
		}

		require.True(t, strings.HasPrefix(strings.ToLower(iframe.Src), "https://"))
		require.NotContains(t, strings.ToLower(string(iframe.Render())), " onload=")
	})
}
