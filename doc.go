// Package oembed implements the oEmbed 1.0 specification
// (https://oembed.com) as a consumer.
//
// Given a URL, Client.Fetch finds the matching oEmbed endpoint — via the
// embedded provider registry, the response's Link header, or HTML discovery,
// in that order — calls it, and returns a validated Response. Callers that
// already hold the response use Client.FetchHTML instead, which runs the same
// pipeline against the supplied header and body.
//
// Those two are the whole client. The intermediate steps — Link-header
// parsing, HTML discovery, and calling a resolved endpoint — are deliberately
// unexported, so there is one way to fetch and no way to assemble a
// half-guarded pipeline by hand.
//
// # Rendering
//
// Response.HTML is arbitrary provider-controlled markup and is never
// sanitized, because sanitizing it would strip the embed. Response.Embed
// classifies a response into one of three render plans — EmbedIframe,
// EmbedSandbox, or EmbedLink — so that markup reaches a page only inside a
// frame. Never write Response.HTML directly.
//
// # Authoring
//
// The NewLink, NewPhoto, NewVideo, and NewRich builders produce documents that
// cannot be spec-invalid by construction, Response.Validate is the strict
// specification check for documents you author, and WriteResponse validates
// and encodes one to an http.ResponseWriter in JSON or XML. These are the
// response document itself, not a provider framework: request parsing and
// endpoint advertisement are out of scope.
package oembed
