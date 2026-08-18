package oembed

import (
	"fmt"
)

func ExampleNewResponse() {

	response := NewResponse(TypePhoto)
	response.URL = "https://example.com/photo.jpg"
	response.Width = 1024
	response.Height = 683

	fmt.Println(response.Version, response.Type, response.Validate() == nil)
	// Output: 1.0 photo true
}

func ExampleRegistry_Find() {

	registry := DefaultRegistry()

	endpoint, found := registry.Find("https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	fmt.Println(found, endpoint.Format, endpoint.URL)
	// Output: true json https://www.youtube.com/oembed
}
