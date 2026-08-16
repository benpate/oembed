package oembed

import (
	"encoding/json"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/benpate/derp"
	"github.com/benpate/rosetta/convert"
)

// Int is a tolerant integer that accepts whatever numeric encoding providers
// actually send. Parsing semantics are rosetta's convert.Int, applied per
// Postel's law: JSON numbers and quoted integer strings parse, floats
// truncate toward zero, out-of-range values clamp to the int bounds, and
// null, empty, or unparseable values quietly become zero ("not provided").
// Output is always a plain JSON number.
type Int int

// MarshalJSON encodes the value as a plain JSON number.
func (i Int) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(i))), nil
}

// UnmarshalJSON decodes any JSON value into the integer, tolerantly.
func (i *Int) UnmarshalJSON(data []byte) error {

	const location = "oembed.Int.UnmarshalJSON"

	// Decode to a raw value so numbers and strings can share one path.
	var raw any

	if err := json.Unmarshal(data, &raw); err != nil {
		return derp.Wrap(err, location, "Invalid JSON value", string(data))
	}

	// Strip the padding some providers put inside quoted numbers ("  480 ").
	if text, isString := raw.(string); isString {
		raw = strings.TrimSpace(text)
	}

	// rosetta does the rest; garbage becomes zero, by policy.
	*i = Int(convert.Int(raw))
	return nil
}

// UnmarshalXML decodes the element's character data with the same tolerant
// numeric parsing as UnmarshalJSON.
func (i *Int) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {

	const location = "oembed.Int.UnmarshalXML"

	// Read the element's character data as a raw string.
	var text string
	if err := decoder.DecodeElement(&text, &start); err != nil {
		return derp.Wrap(err, location, "Invalid XML element")
	}

	// Trim chardata whitespace (pretty-printed XML pads values), then rosetta
	// does the rest; garbage becomes zero, by policy.
	*i = Int(convert.Int(strings.TrimSpace(text)))
	return nil
}
