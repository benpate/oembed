package oembed

import (
	"testing"

	"github.com/benpate/rosetta/lenient"
	"github.com/stretchr/testify/assert"
)

func TestResponse_Validate(t *testing.T) {

	// valid returns a spec-complete response of the given type
	valid := func(oembedType string) Response {

		result := NewResponse(oembedType)

		switch oembedType {

		case TypePhoto:
			result.URL = "https://example.com/photo.jpg"
			result.Width = 1024
			result.Height = 683

		case TypeVideo, TypeRich:
			result.HTML = `<iframe src="https://example.com/embed"></iframe>`
			result.Width = 640
			result.Height = 360
		}

		return result
	}

	test := func(name string, value Response, expectError bool) {
		t.Run(name, func(t *testing.T) {

			err := value.Validate()

			if expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}

	mutate := func(value Response, change func(*Response)) Response {
		change(&value)
		return value
	}

	// Every type validates when complete
	test("valid link", valid(TypeLink), false)
	test("valid photo", valid(TypePhoto), false)
	test("valid video", valid(TypeVideo), false)
	test("valid rich", valid(TypeRich), false)

	// Version rules
	test("empty version", mutate(valid(TypeLink), func(o *Response) { o.Version = "" }), true)
	test("wrong version", mutate(valid(TypeLink), func(o *Response) { o.Version = "2.0" }), true)

	// Type rules
	test("empty type", Response{Version: Version}, true)
	test("unknown type", mutate(valid(TypeLink), func(o *Response) { o.Type = "carousel" }), true)

	// Photo requirements
	test("photo missing url", mutate(valid(TypePhoto), func(o *Response) { o.URL = "" }), true)
	test("photo missing width", mutate(valid(TypePhoto), func(o *Response) { o.Width = 0 }), true)
	test("photo missing height", mutate(valid(TypePhoto), func(o *Response) { o.Height = 0 }), true)
	test("photo negative width", mutate(valid(TypePhoto), func(o *Response) { o.Width = -5 }), true)

	// Video / rich requirements — Validate is the strict SEND-side check, so
	// the spec's height rule applies here (the lenient receive path does not)
	test("video missing html", mutate(valid(TypeVideo), func(o *Response) { o.HTML = "" }), true)
	test("video missing width", mutate(valid(TypeVideo), func(o *Response) { o.Width = 0 }), true)
	test("video missing height", mutate(valid(TypeVideo), func(o *Response) { o.Height = 0 }), true)
	test("rich missing html", mutate(valid(TypeRich), func(o *Response) { o.HTML = "" }), true)
	test("rich missing height", mutate(valid(TypeRich), func(o *Response) { o.Height = 0 }), true)

	// Link requires none of the type-specific fields
	test("link without dimensions", NewResponse(TypeLink), false)

	// Cache age rules
	test("negative cache_age", mutate(valid(TypeLink), func(o *Response) { o.CacheAge = -1 }), true)
	test("positive cache_age", mutate(valid(TypeLink), func(o *Response) { o.CacheAge = 3600 }), false)

	// Thumbnail all-or-none rule
	withThumbnail := func(url string, width lenient.Int64, height lenient.Int64) Response {
		return mutate(valid(TypeLink), func(o *Response) {
			o.ThumbnailURL = url
			o.ThumbnailWidth = width
			o.ThumbnailHeight = height
		})
	}

	test("complete thumbnail", withThumbnail("https://example.com/t.jpg", 100, 50), false)
	test("no thumbnail", withThumbnail("", 0, 0), false)
	test("thumbnail url only", withThumbnail("https://example.com/t.jpg", 0, 0), true)
	test("thumbnail missing height", withThumbnail("https://example.com/t.jpg", 100, 0), true)
	test("thumbnail missing url", withThumbnail("", 100, 50), true)
	test("thumbnail width only", withThumbnail("", 100, 0), true)
}

func TestResponse_ValidateReceived(t *testing.T) {

	test := func(name string, value Response, expectError bool) {
		t.Run(name, func(t *testing.T) {

			err := value.validateReceived()

			if expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}

	// The receive path is forgiving (Postel's law): only the essential payload
	// is required, so imperfect-but-usable documents are accepted as-is
	test("photo with url only", Response{Type: TypePhoto, URL: "https://example.com/p.jpg"}, false)
	test("video with html only", Response{Type: TypeVideo, HTML: "<iframe></iframe>"}, false)
	test("rich with auto height", Response{Type: TypeRich, Version: "1.0", HTML: "<blockquote/>", Width: 400}, false)
	test("bare link", Response{Type: TypeLink}, false)
	test("wrong version tolerated", Response{Type: TypeLink, Version: "2.0"}, false)
	test("incomplete thumbnail tolerated", Response{Type: TypeLink, ThumbnailURL: "https://example.com/t.jpg"}, false)
	test("negative cache_age tolerated", Response{Type: TypeLink, CacheAge: -1}, false)

	// The essentials are still essential
	test("photo without url", Response{Type: TypePhoto, Width: 100, Height: 100}, true)
	test("video without html", Response{Type: TypeVideo, Width: 100, Height: 100}, true)
	test("rich without html", Response{Type: TypeRich}, true)
	test("unknown type", Response{Type: "carousel"}, true)
	test("empty type", Response{}, true)
}
