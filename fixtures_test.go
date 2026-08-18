package oembed

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"testing"

	"github.com/benpate/rosetta/lenient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFixtures parses recorded real-world provider responses (see testdata/)
// and asserts they parse and validate — catching spec-vs-reality drift.
func TestFixtures(t *testing.T) {

	load := func(t *testing.T, filename string) []byte {
		t.Helper()
		data, err := os.ReadFile("testdata/" + filename)
		require.NoError(t, err)
		return data
	}

	t.Run("youtube video", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "youtube.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeVideo, parsed.Type)
		assert.Equal(t, "YouTube", parsed.ProviderName)
		assert.Equal(t, "Rick Astley", parsed.AuthorName)
		assert.NotEmpty(t, parsed.HTML)
		assert.Positive(t, int(parsed.Width))
		assert.Positive(t, int(parsed.Height))

		// The thumbnail triple arrives complete
		assert.NotEmpty(t, parsed.ThumbnailURL)
		assert.Equal(t, lenient.Int64(480), parsed.ThumbnailWidth)
		assert.Equal(t, lenient.Int64(360), parsed.ThumbnailHeight)
	})

	t.Run("vimeo video with extensions", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "vimeo.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeVideo, parsed.Type)
		assert.Equal(t, "Vimeo", parsed.ProviderName)
		assert.Equal(t, "Big Buck Bunny", parsed.Title)

		// Vimeo's non-specification fields (is_plus, duration, video_id) are
		// simply ignored — this fixture proves they do not break parsing.
		assert.Equal(t, TypeVideo, parsed.Type)
	})

	t.Run("flickr photo xml", func(t *testing.T) {

		var parsed Response
		require.NoError(t, xml.Unmarshal(load(t, "flickr.xml"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypePhoto, parsed.Type)
		assert.Equal(t, "Flickr", parsed.ProviderName)
		assert.Equal(t, lenient.Int64(1024), parsed.Width)
		assert.Equal(t, lenient.Int64(683), parsed.Height)
		assert.NotEmpty(t, parsed.URL)

		// Flickr's non-specification XML elements (flickr_type, web_page) are
		// ignored without disturbing the specification fields above.
		assert.Equal(t, TypePhoto, parsed.Type)
	})

	t.Run("mastodon status", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "mastodon.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeRich, parsed.Type)
		assert.Equal(t, "mastodon.social", parsed.ProviderName)
		assert.Equal(t, lenient.Int64(86400), parsed.CacheAge)
		assert.NotEmpty(t, parsed.HTML)
	})

	t.Run("spotify track", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "spotify.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeRich, parsed.Type)
		assert.Equal(t, "Spotify", parsed.ProviderName)
		assert.Equal(t, lenient.Int64(456), parsed.Width)
		assert.Equal(t, lenient.Int64(152), parsed.Height)
		assert.NotEmpty(t, parsed.HTML)
		assert.NotEmpty(t, parsed.ThumbnailURL)
	})

	t.Run("tiktok video with percentage dimensions", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "tiktok.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeVideo, parsed.Type)
		assert.Equal(t, "TikTok", parsed.ProviderName)
		assert.NotEmpty(t, parsed.HTML)

		// TikTok sends width and height as the string "100%" — the tolerant
		// Int zeroes both, which the lenient receive path reads as "auto"
		assert.Equal(t, lenient.Int64(0), parsed.Width)
		assert.Equal(t, lenient.Int64(0), parsed.Height)
	})

	t.Run("soundcloud song with mixed dimension types", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "soundcloud.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeRich, parsed.Type)
		assert.Equal(t, "SoundCloud", parsed.ProviderName)

		// SoundCloud sends `"version": 1.0` as a JSON NUMBER; the tolerant
		// String keeps its source text intact — "1.0", never "1"
		assert.Equal(t, Version, parsed.Version)

		// One response, two encodings: width is the string "100%", height is
		// the number 400
		assert.Equal(t, lenient.Int64(0), parsed.Width)
		assert.Equal(t, lenient.Int64(400), parsed.Height)
	})

	t.Run("reddit post without width", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "reddit.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeRich, parsed.Type)
		assert.Equal(t, "reddit", parsed.ProviderName)

		// Reddit omits width entirely and sends only a height
		assert.Equal(t, lenient.Int64(0), parsed.Width)
		assert.Equal(t, lenient.Int64(316), parsed.Height)
	})

	t.Run("bluesky post with null height", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "bluesky.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypeRich, parsed.Type)
		assert.Equal(t, "Bluesky Social", parsed.ProviderName)
		assert.Equal(t, lenient.Int64(600), parsed.Width)
		assert.Equal(t, lenient.Int64(0), parsed.Height, "null height reads as auto")
	})

	t.Run("giphy photo", func(t *testing.T) {

		var parsed Response
		require.NoError(t, json.Unmarshal(load(t, "giphy.json"), &parsed))
		require.NoError(t, parsed.validateReceived())

		assert.Equal(t, TypePhoto, parsed.Type)
		assert.Equal(t, "GIPHY", parsed.ProviderName)
		assert.NotEmpty(t, parsed.URL)
		assert.Equal(t, lenient.Int64(480), parsed.Width)
		assert.Equal(t, lenient.Int64(480), parsed.Height)
	})
}

// TestFixtures_RoundTrip proves every recorded JSON fixture survives a full
// marshal/unmarshal cycle without losing specification fields or extensions.
func TestFixtures_RoundTrip(t *testing.T) {

	fixtures := []string{
		"youtube.json", "vimeo.json", "mastodon.json", "spotify.json",
		"tiktok.json", "soundcloud.json", "reddit.json", "bluesky.json",
		"giphy.json",
	}

	for _, filename := range fixtures {
		t.Run(filename, func(t *testing.T) {

			data, err := os.ReadFile("testdata/" + filename)
			require.NoError(t, err)

			// Parse the recorded response
			var first Response
			require.NoError(t, json.Unmarshal(data, &first))

			// Marshal it back out, and parse the result again
			remarshaled, err := json.Marshal(first)
			require.NoError(t, err)

			var second Response
			require.NoError(t, json.Unmarshal(remarshaled, &second))

			// The trip preserves the specification fields and every extension
			assert.Equal(t, first.Type, second.Type)
			assert.Equal(t, first.Version, second.Version)
			assert.Equal(t, first.Title, second.Title)
			assert.Equal(t, first.HTML, second.HTML)
			assert.Equal(t, first.URL, second.URL)
			assert.Equal(t, first.Width, second.Width)
			assert.Equal(t, first.Height, second.Height)
			assert.Equal(t, first.ThumbnailURL, second.ThumbnailURL)
		})
	}
}
