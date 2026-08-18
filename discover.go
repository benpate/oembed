package oembed

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/benpate/derp"
	"golang.org/x/net/html"
)

// discover streams an HTML document, collecting the oEmbed endpoints
// advertised by <link rel="alternate"> (and the also-seen rel="alternative")
// elements whose type is application/json+oembed or text/xml+oembed. Relative
// hrefs are resolved against baseURL. Parsing stops at </head> or <body>, so
// a multi-megabyte page body is never read.
//
// JSON endpoints sort before XML endpoints; document order is preserved
// within each format. Malformed, misnested, or truncated HTML is tolerated:
// whatever endpoints were found are returned even when an error is.
func discover(reader io.Reader, baseURL string) ([]Endpoint, error) {

	const location = "oembed.discover"

	// Parse the base URL that relative hrefs resolve against.
	base, err := url.Parse(baseURL)

	if err != nil {
		return nil, derp.Wrap(err, location, "Invalid base URL", baseURL)
	}

	// Stream tokens, bounding single-token memory against hostile input.
	tokenizer := html.NewTokenizer(reader)
	tokenizer.SetMaxBuf(maxDiscoverTokenSize)

	result := make([]Endpoint, 0)
	var parseError error

	for {

		tokenType := tokenizer.Next()

		// End of input. io.EOF is success; any other error (reader failure,
		// oversized token) is reported alongside the partial results.
		if tokenType == html.ErrorToken {

			if err := tokenizer.Err(); err != io.EOF {
				parseError = derp.Wrap(err, location, "Reading HTML document")
			}

			break
		}

		// Only tag tokens can advertise an endpoint or end the head.
		if !isTagToken(tokenType) {
			continue
		}

		endpoint, found, headIsOver := scanTag(tokenizer, tokenType, base)

		if headIsOver {
			break
		}

		if found {
			result = append(result, endpoint)
		}
	}

	return sortEndpoints(result), parseError
}

// isTagToken reports whether a token type is an opening, self-closing, or
// closing tag.
func isTagToken(tokenType html.TokenType) bool {

	switch tokenType {
	case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
		return true
	}

	return false
}

// scanTag inspects one tag token: it returns the endpoint a <link> element
// advertises (if any), and reports whether the head section is over (</head>
// or an opening <body>).
func scanTag(tokenizer *html.Tokenizer, tokenType html.TokenType, base *url.URL) (Endpoint, bool, bool) {

	name, hasAttributes := tokenizer.TagName()

	// A closing </head> means we've seen every head element.
	if tokenType == html.EndTagToken {
		return Endpoint{}, false, string(name) == "head"
	}

	switch string(name) {

	// An opening <body> means the <head> is over.
	case "body":
		return Endpoint{}, false, true

	// A <link> element may advertise an oEmbed endpoint.
	case "link":
		endpoint, found := linkEndpoint(tokenizer, hasAttributes, base)
		return endpoint, found, false
	}

	return Endpoint{}, false, false
}

// linkEndpoint reads the attributes of one <link> token and returns the
// endpoint it advertises, if any.
func linkEndpoint(tokenizer *html.Tokenizer, hasAttributes bool, base *url.URL) (Endpoint, bool) {

	if !hasAttributes {
		return Endpoint{}, false
	}

	// Gather the three attributes that matter.
	var rel, linkType, href string

	for {
		key, value, more := tokenizer.TagAttr()

		switch string(key) {
		case "rel":
			rel = string(value)
		case "type":
			linkType = string(value)
		case "href":
			href = string(value)
		}

		if !more {
			break
		}
	}

	// RULE: the rel list must contain "alternate" (or the widely-seen "alternative")
	if !relIsAlternate(rel) {
		return Endpoint{}, false
	}

	// RULE: the type must name an oEmbed format
	format, isOEmbedType := formatFromLinkType(linkType)

	if !isOEmbedType {
		return Endpoint{}, false
	}

	// Resolve the href against the base URL (hrefs are often relative).
	if href == "" {
		return Endpoint{}, false
	}

	resolved, err := base.Parse(href)

	if err != nil {
		return Endpoint{}, false
	}

	// RULE: only fetchable http(s) endpoints are usable. javascript:, data:,
	// and host-less references like "http:" are dropped.
	if err := validateHTTPURL(resolved.String()); err != nil {
		return Endpoint{}, false
	}

	return Endpoint{
		URL:    resolved.String(),
		Format: format,
	}, true
}

// relIsAlternate reports whether a (space-separated) rel attribute contains
// "alternate" or the non-standard-but-common "alternative".
func relIsAlternate(rel string) bool {

	for _, value := range strings.Fields(rel) {

		if strings.EqualFold(value, "alternate") {
			return true
		}

		if strings.EqualFold(value, "alternative") {
			return true
		}
	}

	return false
}

// formatFromLinkType maps a link's type attribute to an oEmbed format,
// ignoring any media-type parameters after a semicolon.
func formatFromLinkType(linkType string) (string, bool) {

	mediaType, _, _ := strings.Cut(linkType, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))

	switch mediaType {

	case ContentTypeJSONOEmbed:
		return FormatJSON, true

	case ContentTypeXMLOEmbed:
		return FormatXML, true
	}

	return "", false
}

// sortEndpoints orders endpoints JSON-before-XML, preserving document order
// within each format.
func sortEndpoints(endpoints []Endpoint) []Endpoint {

	slices.SortStableFunc(endpoints, func(a Endpoint, b Endpoint) int {
		return formatRank(a.Format) - formatRank(b.Format)
	})

	return endpoints
}

// formatRank returns the preference order of a format: JSON first.
func formatRank(format string) int {

	if format == FormatJSON {
		return 0
	}

	return 1
}

/******************************************
 * HTTP Link Header Discovery (RFC 8288)
 ******************************************/

// discoverLinkHeader extracts the oEmbed endpoints advertised in a response's
// HTTP Link headers (RFC 8288). Relative URI references resolve against
// baseURL, and JSON endpoints sort before XML ones.
//
// The oEmbed specification defines discovery only through HTML <link>
// elements. Link headers are a widely-used convention on top of it, and the
// only way a resource with no HTML head — a PDF, an image, a JSON document —
// can advertise an endpoint at all. A nil or absent header yields no
// endpoints, never an error.
func discoverLinkHeader(header http.Header, baseURL string) ([]Endpoint, error) {

	const location = "oembed.discoverLinkHeader"

	// A response with no Link header simply advertises nothing.
	values := header.Values("Link")

	if len(values) == 0 {
		return make([]Endpoint, 0), nil
	}

	// Parse the base URL that relative references resolve against.
	base, err := url.Parse(baseURL)

	if err != nil {
		return nil, derp.Wrap(err, location, "Invalid base URL", baseURL)
	}

	// A response may carry several Link headers, each holding several
	// comma-separated link-values.
	result := make([]Endpoint, 0, len(values))

	for _, value := range values {
		for _, field := range splitLinkList(value, ',') {

			if endpoint, found := linkHeaderEndpoint(field, base); found {
				result = append(result, endpoint)
			}
		}
	}

	return sortEndpoints(result), nil
}

// linkHeaderEndpoint reads one RFC 8288 link-value ("<uri>; rel=...; type=...")
// and returns the oEmbed endpoint it advertises, if any.
func linkHeaderEndpoint(field string, base *url.URL) (Endpoint, bool) {

	field = strings.TrimSpace(field)

	// RULE: a link-value always begins with a bracketed URI reference
	reference, parameters, bracketed := strings.Cut(strings.TrimPrefix(field, "<"), ">")

	if !bracketed || !strings.HasPrefix(field, "<") {
		return Endpoint{}, false
	}

	rel, linkType := linkHeaderParameters(parameters)

	// RULE: the rel list must contain "alternate" (or the widely-seen "alternative")
	if !relIsAlternate(rel) {
		return Endpoint{}, false
	}

	// RULE: the type must name an oEmbed format
	format, isOEmbedType := formatFromLinkType(linkType)

	if !isOEmbedType {
		return Endpoint{}, false
	}

	// Resolve the reference against the base URL (references are often relative).
	resolved, err := base.Parse(strings.TrimSpace(reference))

	if err != nil {
		return Endpoint{}, false
	}

	// RULE: only fetchable http(s) endpoints are usable. javascript:, data:,
	// and host-less references like "http:" are dropped.
	if err := validateHTTPURL(resolved.String()); err != nil {
		return Endpoint{}, false
	}

	return Endpoint{
		URL:    resolved.String(),
		Format: format,
	}, true
}

// linkHeaderParameters pulls the rel and type parameters out of one
// link-value's parameter list. Per RFC 8288 the first occurrence of each
// parameter wins; later repeats are ignored.
func linkHeaderParameters(parameters string) (string, string) {

	var rel, linkType string

	for _, parameter := range splitLinkList(parameters, ';') {

		name, value, found := strings.Cut(parameter, "=")

		if !found {
			continue
		}

		value = unquoteParameter(strings.TrimSpace(value))

		switch strings.ToLower(strings.TrimSpace(name)) {

		case "rel":
			if rel == "" {
				rel = value
			}

		case "type":
			if linkType == "" {
				linkType = value
			}
		}
	}

	return rel, linkType
}

// splitLinkList splits a Link header value on a separator, ignoring
// separators that sit inside a <URI-Reference> or a quoted parameter value —
// both of which legitimately contain commas and semicolons.
func splitLinkList(value string, separator rune) []string {

	result := make([]string, 0, 2)

	start := 0
	inBrackets := false
	inQuotes := false
	escaped := false

	for index, character := range value {

		// A backslash inside a quoted string escapes the next character.
		if escaped {
			escaped = false
			continue
		}

		switch character {

		case '\\':
			escaped = inQuotes

		case '"':
			inQuotes = !inQuotes

		case '<':
			inBrackets = !inQuotes

		case '>':
			if !inQuotes {
				inBrackets = false
			}

		case separator:
			if !inBrackets && !inQuotes {
				result = append(result, value[start:index])
				start = index + len(string(separator))
			}
		}
	}

	return append(result, value[start:])
}

// unquoteParameter strips the surrounding quotes from a quoted parameter
// value, leaving unquoted values untouched.
func unquoteParameter(value string) string {

	if len(value) < 2 {
		return value
	}

	if value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}

	return strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`)
}
