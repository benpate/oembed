package oembed

import (
	"encoding/json"
	"encoding/xml"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInt_UnmarshalJSON(t *testing.T) {

	test := func(name string, input string, expected int, expectError bool) {
		t.Run(name, func(t *testing.T) {

			var value Int
			err := json.Unmarshal([]byte(input), &value)

			if expectError {
				require.Error(t, err, "input %q should not parse", input)
				return
			}

			require.NoError(t, err, "input %q should parse", input)
			assert.Equal(t, expected, int(value), "input %q", input)
		})
	}

	// The ugly real-world inputs named in the project plan
	test("plain integer", `480`, 480, false)
	test("quoted integer", `"480"`, 480, false)
	test("float", `480.0`, 480, false)
	test("empty string", `""`, 0, false)
	test("null", `null`, 0, false)

	// More corner cases
	test("negative integer", `-3`, -3, false)
	test("quoted negative", `"-3"`, -3, false)
	test("float truncates toward zero", `480.9`, 480, false)
	test("negative float truncates toward zero", `-480.9`, -480, false)
	test("quoted whitespace-padded", `"  480  "`, 480, false)
	test("whitespace-only string", `"   "`, 0, false)
	test("exponent", `1e3`, 1000, false)
	test("zero", `0`, 0, false)
	test("large number preserved", `2147483648`, 2147483648, false)
	test("large negative preserved", `-2147483648`, -2147483648, false)

	// Postel's law (via rosetta convert.Int): garbage quietly becomes zero,
	// quoted floats are beyond ParseInt and zero out, huge values clamp
	test("quoted float zeroes", `"480.5"`, 0, false)
	test("quoted exponent zeroes", `"1e3"`, 0, false)
	test("non-numeric string zeroes", `"abc"`, 0, false)
	test("pixel suffix zeroes", `"480px"`, 0, false)
	test("boolean coerces", `true`, 1, false)
	test("single-element array unwraps", `[480]`, 480, false)
	test("object zeroes", `{}`, 0, false)
	test("huge exponent clamps", `1e300`, math.MaxInt, false)

	// Only structurally invalid JSON is an error
	test("bare garbage", `!!!`, 0, true)
}

func TestInt_UnmarshalXML(t *testing.T) {

	// wrapper receives the element under test
	type wrapper struct {
		Value Int `xml:"value"`
	}

	test := func(name string, input string, expected int, expectError bool) {
		t.Run(name, func(t *testing.T) {

			var wrapped wrapper
			err := xml.Unmarshal([]byte("<wrapper><value>"+input+"</value></wrapper>"), &wrapped)

			if expectError {
				require.Error(t, err, "input %q should not parse", input)
				return
			}

			require.NoError(t, err, "input %q should parse", input)
			assert.Equal(t, expected, int(wrapped.Value), "input %q", input)
		})
	}

	test("plain integer", `480`, 480, false)
	test("padded integer", `  480  `, 480, false)
	test("negative", `-3`, -3, false)
	test("empty element", ``, 0, false)
	test("whitespace only", `   `, 0, false)
	test("large number preserved", `2147483648`, 2147483648, false)

	// XML chardata goes through convert.Int's string path (ParseInt), so
	// non-integer text quietly zeroes rather than erroring
	test("float zeroes", `480.9`, 0, false)
	test("exponent zeroes", `1e3`, 0, false)
	test("non-numeric zeroes", `abc`, 0, false)
}

func TestInt_MarshalJSON(t *testing.T) {

	test := func(name string, input Int, expected string) {
		t.Run(name, func(t *testing.T) {
			result, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, expected, string(result))
		})
	}

	// Output is always a plain JSON number
	test("positive", Int(480), `480`)
	test("zero", Int(0), `0`)
	test("negative", Int(-3), `-3`)
}

func TestInt_MarshalXML(t *testing.T) {

	type wrapper struct {
		Value Int `xml:"value"`
	}

	result, err := xml.Marshal(wrapper{Value: 480})
	require.NoError(t, err)
	assert.Equal(t, `<wrapper><value>480</value></wrapper>`, string(result))
}

func TestInt_JSONRoundTrip(t *testing.T) {

	// A marshaled Int always re-parses to the same value
	for _, value := range []Int{-100, -1, 0, 1, 480, 2147483647} {

		data, err := json.Marshal(value)
		require.NoError(t, err)

		var parsed Int
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, value, parsed)
	}
}

func FuzzInt_UnmarshalXML(f *testing.F) {

	// Seed with the unit-test chardata shapes, including framing escapes
	f.Add(`480`)
	f.Add(`  480  `)
	f.Add(`480.9`)
	f.Add(`-3`)
	f.Add(``)
	f.Add(`abc`)
	f.Add(`</value><value>1`)
	f.Add("\x00\xff")

	f.Fuzz(func(t *testing.T, chardata string) {

		type wrapper struct {
			Value Int `xml:"value"`
		}

		var wrapped wrapper

		// Property: never panics; on success the value re-marshals to valid XML
		if err := xml.Unmarshal([]byte("<wrapper><value>"+chardata+"</value></wrapper>"), &wrapped); err != nil {
			return
		}

		if _, err := xml.Marshal(wrapped); err != nil {
			t.Fatalf("marshal failed after successful unmarshal of %q: %v", chardata, err)
		}
	})
}

func FuzzInt_UnmarshalJSON(f *testing.F) {

	// Seed with the interesting unit-test cases
	f.Add([]byte(`480`))
	f.Add([]byte(`"480"`))
	f.Add([]byte(`480.0`))
	f.Add([]byte(`""`))
	f.Add([]byte(`null`))
	f.Add([]byte(`-2147483648`))
	f.Add([]byte(`1e300`))
	f.Add([]byte(`"1e3"`))
	f.Add([]byte(`NaN`))
	f.Add([]byte(`"-"`))
	f.Add([]byte(`!!!`))

	f.Fuzz(func(t *testing.T, data []byte) {

		var value Int

		// Property: never panics; on success the value marshals back to a valid JSON number
		if err := json.Unmarshal(data, &value); err != nil {
			return
		}

		remarshaled, err := json.Marshal(value)

		if err != nil {
			t.Fatalf("marshal failed after successful unmarshal of %q: %v", data, err)
		}

		if !json.Valid(remarshaled) {
			t.Fatalf("re-marshaled value %q is not valid JSON (input %q)", remarshaled, data)
		}
	})
}
