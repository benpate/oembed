package oembed

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/benpate/derp"
	"github.com/benpate/rosetta/lenient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResponse(t *testing.T) {

	result := NewResponse(TypeVideo)

	assert.Equal(t, TypeVideo, result.Type)
	assert.Equal(t, Version, result.Version)
}

func TestResponse_JSONRoundTrip(t *testing.T) {

	original := NewResponse(TypeVideo)
	original.Title = "Test Video"
	original.AuthorName = "Test Author"
	original.AuthorURL = "https://example.com/author"
	original.ProviderName = "Example"
	original.ProviderURL = "https://example.com/"
	original.CacheAge = 3600
	original.ThumbnailURL = "https://example.com/thumb.jpg"
	original.ThumbnailWidth = 100
	original.ThumbnailHeight = 50
	original.HTML = `<iframe src="https://example.com/embed/1"></iframe>`
	original.Width = 640
	original.Height = 360

	// Marshal, then unmarshal into a fresh struct
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var parsed Response
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Every specification field survives the trip
	assert.Equal(t, original.Type, parsed.Type)
	assert.Equal(t, original.Version, parsed.Version)
	assert.Equal(t, original.Title, parsed.Title)
	assert.Equal(t, original.AuthorName, parsed.AuthorName)
	assert.Equal(t, original.AuthorURL, parsed.AuthorURL)
	assert.Equal(t, original.ProviderName, parsed.ProviderName)
	assert.Equal(t, original.ProviderURL, parsed.ProviderURL)
	assert.Equal(t, original.CacheAge, parsed.CacheAge)
	assert.Equal(t, original.ThumbnailURL, parsed.ThumbnailURL)
	assert.Equal(t, original.ThumbnailWidth, parsed.ThumbnailWidth)
	assert.Equal(t, original.ThumbnailHeight, parsed.ThumbnailHeight)
	assert.Equal(t, original.HTML, parsed.HTML)
	assert.Equal(t, original.Width, parsed.Width)
	assert.Equal(t, original.Height, parsed.Height)
}

func TestResponse_JSONRequiredFieldsAlwaysEmitted(t *testing.T) {

	// Even a zero struct emits type and version (no omitempty on required fields)
	data, err := json.Marshal(Response{})
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Contains(t, raw, "type")
	assert.Contains(t, raw, "version")

	// Genuinely optional fields stay omitted
	assert.NotContains(t, raw, "title")
	assert.NotContains(t, raw, "width")
	assert.NotContains(t, raw, "cache_age")
}

func TestResponse_JSONUnmarshalErrors(t *testing.T) {

	test := func(name string, input string) {
		t.Run(name, func(t *testing.T) {
			var parsed Response
			assert.Error(t, json.Unmarshal([]byte(input), &parsed))
		})
	}

	test("not an object", `[1,2,3]`)
	test("truncated", `{"type": "photo"`)
	test("empty input", ``)
	test("object in a string field", `{"version": {"nested": true}}`)

	// Postel's law: a garbage numeric field is tolerated (zeroed), not an error
	t.Run("bad width tolerated", func(t *testing.T) {
		var parsed Response
		require.NoError(t, json.Unmarshal([]byte(`{"type":"photo","width":"abc"}`), &parsed))
		assert.Equal(t, lenient.Int64(0), parsed.Width)
	})
}

func TestResponse_JSONMistypedFields(t *testing.T) {

	// Tolerance is a property of the field types, not of the document: Version
	// is a String and Height is an Int, so both accept a mistyped scalar.
	t.Run("tolerant fields accept mistyped scalars", func(t *testing.T) {

		input := `{
			"type": "rich",
			"version": 1.0,
			"height": "400",
			"custom_number": 7
		}`

		var parsed Response
		require.NoError(t, json.Unmarshal([]byte(input), &parsed))

		assert.Equal(t, Version, parsed.Version, "numeric version keeps its source text")
		assert.Equal(t, lenient.Int64(400), parsed.Height, "quoted height parses")
		assert.Equal(t, TypeRich, parsed.Type, "correctly-typed fields pass through")
	})

	// The plain string fields are NOT tolerant. Only Version is a String,
	// because it is the only field providers are known to mistype.
	t.Run("plain string fields reject mistyped scalars", func(t *testing.T) {

		var parsed Response
		require.Error(t, json.Unmarshal([]byte(`{"type":"rich","title":42}`), &parsed))
	})
}

func TestResponse_XMLRoundTrip(t *testing.T) {

	original := NewResponse(TypeVideo)
	original.Title = "Test Video"
	original.AuthorName = "Test Author"
	original.ProviderName = "Example"
	original.CacheAge = 3600
	original.ThumbnailURL = "https://example.com/thumb.jpg"
	original.ThumbnailWidth = 100
	original.ThumbnailHeight = 50
	original.HTML = `<iframe src="https://example.com/embed/1"></iframe>`
	original.Width = 640
	original.Height = 360

	data, err := xml.Marshal(original)
	require.NoError(t, err)

	// The root element is <oembed>, per the specification
	assert.True(t, strings.HasPrefix(string(data), "<oembed>"), "got %s", data)

	var parsed Response
	require.NoError(t, xml.Unmarshal(data, &parsed))

	assert.Equal(t, original.Type, parsed.Type)
	assert.Equal(t, original.Version, parsed.Version)
	assert.Equal(t, original.Title, parsed.Title)
	assert.Equal(t, original.AuthorName, parsed.AuthorName)
	assert.Equal(t, original.ProviderName, parsed.ProviderName)
	assert.Equal(t, original.CacheAge, parsed.CacheAge)
	assert.Equal(t, original.ThumbnailURL, parsed.ThumbnailURL)
	assert.Equal(t, original.ThumbnailWidth, parsed.ThumbnailWidth)
	assert.Equal(t, original.ThumbnailHeight, parsed.ThumbnailHeight)
	assert.Equal(t, original.HTML, parsed.HTML)
	assert.Equal(t, original.Width, parsed.Width)
	assert.Equal(t, original.Height, parsed.Height)
}

func TestResponse_XMLUnmarshalErrors(t *testing.T) {

	test := func(name string, input string) {
		t.Run(name, func(t *testing.T) {
			var parsed Response
			assert.Error(t, xml.Unmarshal([]byte(input), &parsed))
		})
	}

	test("not xml", `this is not xml`)
	test("truncated", `<oembed><type>photo`)
	test("empty input", ``)

	// Postel's law: a garbage numeric element is tolerated (zeroed), not an error
	t.Run("bad width tolerated", func(t *testing.T) {
		var parsed Response
		require.NoError(t, xml.Unmarshal([]byte(`<oembed><width>abc</width></oembed>`), &parsed))
		assert.Equal(t, lenient.Int64(0), parsed.Width)
	})
}

// BenchmarkResponse_UnmarshalJSON measures parsing a real recorded response (YouTube).
func BenchmarkResponse_UnmarshalJSON(b *testing.B) {

	data, err := os.ReadFile("testdata/youtube.json")
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var parsed Response

		if err := json.Unmarshal(data, &parsed); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResponse_MarshalJSON measures encoding a real recorded response (Vimeo).
func BenchmarkResponse_MarshalJSON(b *testing.B) {

	data, err := os.ReadFile("testdata/vimeo.json")
	if err != nil {
		b.Fatal(err)
	}

	var parsed Response

	if err := json.Unmarshal(data, &parsed); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := json.Marshal(parsed); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResponse_UnmarshalXML measures parsing a real recorded XML response (Flickr).
func BenchmarkResponse_UnmarshalXML(b *testing.B) {

	data, err := os.ReadFile("testdata/flickr.xml")
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var parsed Response

		if err := xml.Unmarshal(data, &parsed); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzResponse_UnmarshalJSON(f *testing.F) {

	// Seed with real fixture documents and hostile shapes
	for _, filename := range []string{"youtube.json", "vimeo.json", "tiktok.json", "reddit.json"} {
		if data, err := os.ReadFile("testdata/" + filename); err == nil {
			f.Add(data)
		}
	}

	f.Add([]byte(`{"type":"photo","width":"1024","flickr_type":"photo"}`))
	f.Add([]byte(`{"type":{"nested":"object"}}`))
	f.Add([]byte(`{"html":"<script>alert(1)</script>"}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{`))

	f.Fuzz(func(t *testing.T, data []byte) {

		var parsed Response

		// Property: never panics; on success the document re-marshals to valid
		// JSON that parses again (round-trip stability)
		if err := json.Unmarshal(data, &parsed); err != nil {
			return
		}

		remarshaled, err := json.Marshal(parsed)

		if err != nil {
			t.Fatalf("marshal failed after successful unmarshal of %q: %v", data, err)
		}

		var reparsed Response

		if err := json.Unmarshal(remarshaled, &reparsed); err != nil {
			t.Fatalf("re-parse failed for %q (from %q): %v", remarshaled, data, err)
		}
	})
}

func FuzzResponse_UnmarshalXML(f *testing.F) {

	if data, err := os.ReadFile("testdata/flickr.xml"); err == nil {
		f.Add(data)
	}

	f.Add([]byte(`<oembed><type>link</type><version>1.0</version></oembed>`))
	f.Add([]byte(`<oembed><width>480.9</width><custom>x</custom></oembed>`))
	f.Add([]byte(`<oembed><a><b><c></c></b></a></oembed>`))
	f.Add([]byte(`<oembed`))
	f.Add([]byte(`<?xml version="1.0"?><oembed>&lt;&amp;</oembed>`))

	f.Fuzz(func(t *testing.T, data []byte) {

		// Bound the input: deeply-nested XML exercises encoding/xml recursion
		// limits that are the standard library's concern, not this package's.
		if len(data) > 1<<16 {
			return
		}

		var parsed Response

		// Property: never panics; on success the document re-marshals cleanly
		if err := xml.Unmarshal(data, &parsed); err != nil {
			return
		}

		if _, err := xml.Marshal(parsed); err != nil {
			t.Fatalf("marshal failed after successful unmarshal of %q: %v", data, err)
		}
	})
}

func TestBuilders(t *testing.T) {

	t.Run("NewLink", func(t *testing.T) {

		response := NewLink("A Title")

		assert.Equal(t, Version, response.Version)
		assert.Equal(t, TypeLink, response.Type)
		assert.Equal(t, "A Title", response.Title)
		assert.NoError(t, response.Validate())
	})

	t.Run("NewLink with empty title still validates", func(t *testing.T) {
		// The spec makes title optional even for link responses.
		assert.NoError(t, NewLink("").Validate())
	})

	t.Run("NewPhoto", func(t *testing.T) {

		response := NewPhoto("https://example.com/photo.jpg", 800, 600)

		assert.Equal(t, TypePhoto, response.Type)
		assert.Equal(t, "https://example.com/photo.jpg", response.URL)
		assert.Equal(t, lenient.Int64(800), response.Width)
		assert.Equal(t, lenient.Int64(600), response.Height)
		assert.NoError(t, response.Validate())
	})

	t.Run("NewVideo", func(t *testing.T) {

		response := NewVideo(`<iframe src="https://example.com/embed/1"></iframe>`, 640, 360)

		assert.Equal(t, TypeVideo, response.Type)
		assert.NotEmpty(t, response.HTML)
		assert.NoError(t, response.Validate())
	})

	t.Run("NewRich", func(t *testing.T) {

		response := NewRich("<blockquote>hi</blockquote>", 500, 200)

		assert.Equal(t, TypeRich, response.Type)
		assert.NoError(t, response.Validate())
	})

	t.Run("Validate is still the backstop", func(t *testing.T) {

		// Builders can't stop a caller passing zero dimensions; Validate can.
		assert.Error(t, NewPhoto("https://example.com/p.jpg", 0, 600).Validate())
		assert.Error(t, NewVideo("", 640, 360).Validate())
	})
}

func TestResponse_SetThumbnail(t *testing.T) {

	t.Run("sets all three fields together", func(t *testing.T) {

		response := NewLink("Thumbs")
		response.SetThumbnail("https://example.com/thumb.png", 300, 200)

		assert.Equal(t, "https://example.com/thumb.png", response.ThumbnailURL)
		assert.Equal(t, lenient.Int64(300), response.ThumbnailWidth)
		assert.Equal(t, lenient.Int64(200), response.ThumbnailHeight)
		assert.NoError(t, response.Validate())
	})

	t.Run("empty url is a no-op", func(t *testing.T) {

		response := NewLink("No Thumbs")
		response.SetThumbnail("", 300, 200)

		assert.Empty(t, response.ThumbnailURL)
		assert.Zero(t, response.ThumbnailWidth)
		assert.Zero(t, response.ThumbnailHeight)
		assert.NoError(t, response.Validate())
	})
}

func TestWriteResponse(t *testing.T) {

	t.Run("json with explicit format", func(t *testing.T) {

		recorder := httptest.NewRecorder()
		require.NoError(t, WriteResponse(recorder, NewLink("JSON Doc"), FormatJSON))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))

		var parsed Response
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &parsed))
		assert.Equal(t, "JSON Doc", parsed.Title)
	})

	t.Run("empty format means json", func(t *testing.T) {

		recorder := httptest.NewRecorder()
		require.NoError(t, WriteResponse(recorder, NewLink("Default Doc"), ""))

		assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	})

	t.Run("xml", func(t *testing.T) {

		recorder := httptest.NewRecorder()
		require.NoError(t, WriteResponse(recorder, NewLink("XML Doc"), FormatXML))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "text/xml; charset=utf-8", recorder.Header().Get("Content-Type"))
		assert.True(t, strings.HasPrefix(recorder.Body.String(), xml.Header))

		var parsed Response
		require.NoError(t, xml.Unmarshal(recorder.Body.Bytes(), &parsed))
		assert.Equal(t, "XML Doc", parsed.Title)
	})

	t.Run("invalid document writes nothing", func(t *testing.T) {

		recorder := httptest.NewRecorder()

		// A photo with no url fails strict Validate.
		err := WriteResponse(recorder, NewResponse(TypePhoto), FormatJSON)

		require.Error(t, err)
		assert.Zero(t, recorder.Body.Len(), "an invalid document must not reach the wire")
	})

	t.Run("unsupported format writes nothing", func(t *testing.T) {

		recorder := httptest.NewRecorder()
		err := WriteResponse(recorder, NewLink("Nope"), "yaml")

		require.Error(t, err)
		assert.Equal(t, http.StatusNotImplemented, derp.ErrorCode(err))
		assert.Zero(t, recorder.Body.Len())
	})
}
