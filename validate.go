package oembed

import (
	"github.com/benpate/derp"
)

// Validate confirms that the response satisfies oEmbed 1.0 strictly: a "1.0"
// version, a known type, that type's required fields, and the all-or-none
// thumbnail rule. This is the SEND-side check — use it as a backstop before
// serving a document you authored (be strict in what you send). Received
// documents go through the Client's more forgiving acceptance instead (be
// flexible in what you receive).
func (response Response) Validate() error {

	const location = "oembed.Response.Validate"

	// RULE: version is required and must be exactly "1.0"
	if response.Version != Version {
		return derp.Validation("Version must be '1.0'", string(response.Version))
	}

	// RULE: cache_age may not be negative
	if response.CacheAge < 0 {
		return derp.Validation("Cache age may not be negative", int(response.CacheAge))
	}

	// RULE: if any thumbnail field is present, all three must be
	if err := response.validateThumbnail(); err != nil {
		return derp.Wrap(err, location, "Invalid thumbnail")
	}

	// RULE: each type carries its own required fields
	switch response.Type {

	case TypeLink:
		return nil

	case TypePhoto:

		if response.URL == "" {
			return derp.Validation("Photo responses require a url")
		}

		return response.validateDimensions()

	case TypeVideo, TypeRich:

		if response.HTML == "" {
			return derp.Validation("Video and rich responses require html")
		}

		return response.validateDimensions()
	}

	return derp.Validation("Unrecognized oEmbed type", response.Type)
}

// validateReceived is the RECEIVE-side acceptance check, deliberately more
// forgiving than Validate (Postel's law): a response is usable when its type
// is recognized and the type's essential payload is present — url for photos,
// html for video and rich. Everything else (version quirks, missing or null
// dimensions meaning "auto", incomplete thumbnails) is tolerated as-is; major
// providers such as Mastodon and Twitter/X send null heights for auto-sized
// embeds.
func (response Response) validateReceived() error {

	switch response.Type {

	case TypeLink:
		return nil

	case TypePhoto:

		if response.URL == "" {
			return derp.Validation("Photo responses require a url")
		}

		return nil

	case TypeVideo, TypeRich:

		if response.HTML == "" {
			return derp.Validation("Video and rich responses require html")
		}

		return nil
	}

	return derp.Validation("Unrecognized oEmbed type", response.Type)
}

// validateDimensions confirms the positive width/height that the spec
// requires for the photo, video, and rich types.
func (response Response) validateDimensions() error {

	if response.Width <= 0 {
		return derp.Validation("Width is required and must be positive", int(response.Width))
	}

	if response.Height <= 0 {
		return derp.Validation("Height is required and must be positive", int(response.Height))
	}

	// These proportions are acceptable.
	return nil
}

// validateThumbnail enforces the all-or-none rule: if any of thumbnail_url,
// thumbnail_width, or thumbnail_height is present, all three must be.
func (response Response) validateThumbnail() error {

	hasURL := response.ThumbnailURL != ""
	hasWidth := response.ThumbnailWidth > 0
	hasHeight := response.ThumbnailHeight > 0

	// No thumbnail at all is fine.
	if !hasURL && !hasWidth && !hasHeight {
		return nil
	}

	// A complete thumbnail is fine, too.
	if hasURL && hasWidth && hasHeight {
		return nil
	}

	return derp.Validation("Thumbnail requires url, width, and height together",
		response.ThumbnailURL, int(response.ThumbnailWidth), int(response.ThumbnailHeight))
}
