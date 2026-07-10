// Package schema defines the data-only filesystem schemas that drive the
// cmkfs option forms and command generation. The schemas describe data,
// never logic (spec §5.1): relationships like Conflicts and Requires are
// declared here as data; their enforcement lives in the code that consumes
// the schema.
package schema

// Kind is the type of an option's value.
type Kind int

const (
	KindBool Kind = iota
	KindEnum
	KindInt
	KindString
	KindSize
)

// Schema describes one filesystem backend and its curated option set.
type Schema struct {
	ID, Name, Description string
	Binary                string
	ForceFlag             string // "" when the backend overwrites signatures unconditionally (spec §9)
	WholeDiskFlag         string // injected when the target is a whole disk, e.g. mkfs.fat -I; "" for most
	MinVersion            string // informational; soft warning only (spec §8.3)
	MinSizeBytes          int64
	Options               []Option
	Composites            []Composite
}

// Option is one form field.
type Option struct {
	ID, Name, Description, LongHelp string
	Type                            Kind
	Default                         any // bool | int64 | string, must match Type
	Flag                            string
	FlagTrue                        string
	FlagFalse                       string
	Values                          []EnumValue
	Omit                            any
	Min, Max                        *int64
	Pattern                         string
	MaxBytes                        int
	Placeholder                     string // dim format/default hint shown in an empty text field
	Conflicts                       []string
	Requires                        []string
	EmitAs                          string
	CompositeOnly                   bool
}

// EnumValue is one choice of a KindEnum option.
type EnumValue struct{ Value, Label, Help string }

// Composite is a multi-option flag emitted only when every option in
// Requires is set; placeholders are {option_id}.
type Composite struct {
	Flag     string
	Requires []string
}

// i64 is the one permitted helper for definitions.go (spec §6).
func i64(v int64) *int64 { return &v }
