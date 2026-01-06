package markdown

import (
	"reflect"
	"testing"
)

func TestParseInlinePlain(t *testing.T) {
	got := ParseInline("hello")
	want := []Span{{Text: "hello"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected spans: %#v", got)
	}
}

func TestParseInlineBoldItalicCode(t *testing.T) {
	got := ParseInline("a **bold** and *ital* and `code`")
	want := []Span{
		{Text: "a "},
		{Text: "bold", Bold: true},
		{Text: " and "},
		{Text: "ital", Italic: true},
		{Text: " and "},
		{Text: "code", Code: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected spans: %#v", got)
	}
}

func TestParseInlineEscapes(t *testing.T) {
	got := ParseInline(`\*not italic\*`)
	want := []Span{{Text: "*not italic*"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected spans: %#v", got)
	}
}

func TestParseInlineUnclosedMarkersLiteral(t *testing.T) {
	got := ParseInline("**bold *oops")
	want := []Span{{Text: "**bold *oops"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected spans: %#v", got)
	}
}
