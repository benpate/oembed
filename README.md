# 🖼️ oEmbed

[![Go Reference](https://pkg.go.dev/badge/github.com/benpate/oembed.svg)](https://pkg.go.dev/github.com/benpate/oembed)
[![Version](https://img.shields.io/github/v/release/benpate/oembed?include_prereleases&style=flat-square&color=brightgreen)](https://github.com/benpate/oembed/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/benpate/oembed/go.yml?branch=main)](https://github.com/benpate/oembed/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/benpate/oembed?style=flat-square)](https://goreportcard.com/report/github.com/benpate/oembed)
[![Codecov](https://img.shields.io/codecov/c/github/benpate/oembed.svg?style=flat-square)](https://codecov.io/gh/benpate/oembed)

## oEmbed Metadata for Go

`oembed` is intended to retrieve [oEmbed](https://oembed.com) metadata — the title, author, thumbnail, and embeddable HTML that providers like YouTube, Vimeo, and Flickr publish for their content.

## Not Implemented Yet

This package is an early stub: the lookup and parsing functions (`LookupWithURI`, `ParseResponse`, `ParseGoQuery`) are currently commented out and return nothing. It does not work yet. The `OEmbed`, `Provider`, and `ProviderEndpoint` types describe the intended data model, but there is no working implementation behind them.

## Credits

Portions of this code are inspired or duplicated from:

* https://github.com/dyatlov/go-oembed
* https://github.com/artyom/oembed
* https://oembed.com/

## Pull Requests Welcome

I'm trying to make oembed the best it can be, and your help is greatly appreciated. If you find a bug or have an idea for a new feature, please open an issue or submit a pull request. We're all in this together! 🖼️

