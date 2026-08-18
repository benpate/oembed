module github.com/benpate/oembed

go 1.25.0

require (
	github.com/benpate/derp v0.37.0
	github.com/benpate/remote v0.23.0
	github.com/benpate/rosetta v0.33.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/net v0.58.0
)

require (
	github.com/benpate/uri v0.4.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/benpate/rosetta => ../rosetta

replace github.com/benpate/derp => ../derp
