package oembed

/* TODO: These implementations are not complete...

// LookupWithURI uses a URI to find the oEmbed endpoint
func LookupWithURI(uri string) (string, error) {
	return "", nil
}

// ParseResponse searches an http.Response for oEmbed medadata
func ParseResponse(response *http.Response) (OEmbed, error) {

	const location = "oembed.ParseResponse"

	// Read the document body
	body, err := re.ReadResponseBody(response)

	if err != nil {
		return OEmbed{}, derp.Wrap(err, location, "Error reading request body")
	}

	// Parse the document body as a GoQuery object
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))

	if err != nil {
		return OEmbed{}, derp.Wrap(err, location, "Error parsing HTML document")
	}

	// Return result from parsed GoQuery document
	return ParseGoQuery(document)
}


// ParseGoQuery searches a goquery document for oEmbed metadata.
// This is provided as a shortcut for use cases where the caller
// has already parsed the HTML document into a goquery object.
func ParseGoQuery(goquery *goquery.Document) (OEmbed, error) {
	return OEmbed{}, nil
}
*/
