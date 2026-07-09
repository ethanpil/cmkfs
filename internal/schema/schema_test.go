package schema

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// validate runs every semantic rule from spec §5.3 against a schema set and
// returns one message per violation. Each message names the schema (and
// option) it concerns.
func validate(schemas []Schema) []string {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	minVersionRe := regexp.MustCompile(`^\d+(\.\d+)*$`)
	seenSchemaIDs := map[string]bool{}

	for _, s := range schemas {
		// Rule 1: schema ID and Binary non-empty; IDs unique across schemas.
		if s.ID == "" {
			add("schema %q: empty ID", s.Name)
		}
		if s.Binary == "" {
			add("schema %s: empty Binary", s.ID)
		}
		if seenSchemaIDs[s.ID] {
			add("schema %s: duplicate schema ID", s.ID)
		}
		seenSchemaIDs[s.ID] = true

		// Rule 8 (schema part): MinVersion format.
		if s.MinVersion != "" && !minVersionRe.MatchString(s.MinVersion) {
			add("schema %s: MinVersion %q does not match ^\\d+(\\.\\d+)*$", s.ID, s.MinVersion)
		}

		// Rule 2: option IDs unique; references resolve; no self-refs;
		// conflicts symmetric.
		opts := map[string]Option{}
		for _, o := range s.Options {
			if _, dup := opts[o.ID]; dup {
				add("schema %s option %s: duplicate option ID", s.ID, o.ID)
			}
			opts[o.ID] = o
		}
		for _, o := range s.Options {
			for _, ref := range o.Conflicts {
				if ref == o.ID {
					add("schema %s option %s: Conflicts self-reference", s.ID, o.ID)
					continue
				}
				other, ok := opts[ref]
				if !ok {
					add("schema %s option %s: Conflicts references unknown option %q", s.ID, o.ID, ref)
					continue
				}
				if !contains(other.Conflicts, o.ID) {
					add("schema %s option %s: Conflicts with %s is not declared symmetrically", s.ID, o.ID, ref)
				}
			}
			for _, ref := range o.Requires {
				if ref == o.ID {
					add("schema %s option %s: Requires self-reference", s.ID, o.ID)
				} else if _, ok := opts[ref]; !ok {
					add("schema %s option %s: Requires references unknown option %q", s.ID, o.ID, ref)
				}
			}
		}

		for _, o := range s.Options {
			// Rule 3: type-appropriate fields only.
			if o.Type == KindEnum && len(o.Values) == 0 {
				add("schema %s option %s: KindEnum requires Values", s.ID, o.ID)
			}
			if o.Type != KindEnum && len(o.Values) > 0 {
				add("schema %s option %s: Values present on non-enum option", s.ID, o.ID)
			}
			if o.Type == KindBool {
				if o.FlagTrue == "" && o.FlagFalse == "" {
					add("schema %s option %s: KindBool requires at least one of FlagTrue/FlagFalse", s.ID, o.ID)
				}
			} else if o.FlagTrue != "" || o.FlagFalse != "" {
				add("schema %s option %s: FlagTrue/FlagFalse on non-bool option", s.ID, o.ID)
			}
			if !typeMatches(o.Type, o.Default) {
				add("schema %s option %s: Default type does not match option Type", s.ID, o.ID)
			}
			if o.Omit != nil && !typeMatches(o.Type, o.Omit) {
				add("schema %s option %s: Omit type does not match option Type", s.ID, o.ID)
			}

			// Rule 8 (option part): Pattern compiles.
			var pat *regexp.Regexp
			if o.Pattern != "" {
				var err error
				pat, err = regexp.Compile(o.Pattern)
				if err != nil {
					add("schema %s option %s: Pattern does not compile: %v", s.ID, o.ID, err)
				}
			}

			// Rule 4: Default is valid for the option (a Default equal to
			// Omit means "backend default" and needs no further validity).
			if o.Omit == nil || o.Default != o.Omit {
				switch o.Type {
				case KindEnum:
					if dv, ok := o.Default.(string); ok {
						found := false
						for _, v := range o.Values {
							if v.Value == dv {
								found = true
							}
						}
						if !found {
							add("schema %s option %s: Default %q not in Values", s.ID, o.ID, dv)
						}
					}
				case KindInt:
					if dv, ok := o.Default.(int64); ok {
						if o.Min != nil && dv < *o.Min {
							add("schema %s option %s: Default %d below Min %d", s.ID, o.ID, dv, *o.Min)
						}
						if o.Max != nil && dv > *o.Max {
							add("schema %s option %s: Default %d above Max %d", s.ID, o.ID, dv, *o.Max)
						}
					}
				case KindString:
					if dv, ok := o.Default.(string); ok && dv != "" {
						if o.MaxBytes > 0 && len(dv) > o.MaxBytes {
							add("schema %s option %s: Default exceeds MaxBytes", s.ID, o.ID)
						}
						if pat != nil && !pat.MatchString(dv) {
							add("schema %s option %s: Default does not match Pattern", s.ID, o.ID)
						}
					}
				}
			}

			// Rule 5: flag shape.
			if o.Type == KindBool {
				if o.Flag != "" {
					add("schema %s option %s: bool option must not have Flag", s.ID, o.ID)
				}
			} else if o.CompositeOnly {
				if o.Flag != "" {
					add("schema %s option %s: CompositeOnly option must not have Flag", s.ID, o.ID)
				}
			} else {
				if strings.Count(o.Flag, "{value}") != 1 {
					add("schema %s option %s: Flag must contain {value} exactly once", s.ID, o.ID)
				}
			}

			// Rule 7: help length caps.
			if len(o.Description) > 200 {
				add("schema %s option %s: Description exceeds 200 bytes (%d)", s.ID, o.ID, len(o.Description))
			}
			if len(o.LongHelp) > 2000 {
				add("schema %s option %s: LongHelp exceeds 2000 bytes (%d)", s.ID, o.ID, len(o.LongHelp))
			}
		}

		// Rules 2 + 6: composite references resolve, placeholders match
		// Requires, referenced options are CompositeOnly.
		placeholderRe := regexp.MustCompile(`\{([a-z0-9_]+)\}`)
		for i, c := range s.Composites {
			for _, ref := range c.Requires {
				o, ok := opts[ref]
				if !ok {
					add("schema %s composite %d: Requires references unknown option %q", s.ID, i, ref)
					continue
				}
				if !o.CompositeOnly {
					add("schema %s composite %d: referenced option %s is not CompositeOnly", s.ID, i, ref)
				}
				if !strings.Contains(c.Flag, "{"+ref+"}") {
					add("schema %s composite %d: required option %s has no placeholder in Flag", s.ID, i, ref)
				}
			}
			for _, m := range placeholderRe.FindAllStringSubmatch(c.Flag, -1) {
				if !contains(c.Requires, m[1]) {
					add("schema %s composite %d: placeholder {%s} does not match a Requires entry", s.ID, i, m[1])
				}
			}
		}
	}
	return errs
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func typeMatches(k Kind, v any) bool {
	switch k {
	case KindBool:
		_, ok := v.(bool)
		return ok
	case KindInt:
		_, ok := v.(int64)
		return ok
	case KindEnum, KindString, KindSize:
		_, ok := v.(string)
		return ok
	}
	return false
}

// TestSchemas runs every semantic rule against the shipped Schemas var.
func TestSchemas(t *testing.T) {
	for _, e := range validate(Schemas) {
		t.Error(e)
	}
}

// TestValidateCatchesBrokenSchemas exercises each rule checker against
// deliberately broken schema literals, asserting the specific error.
func TestValidateCatchesBrokenSchemas(t *testing.T) {
	base := func(mod func(*Schema)) []Schema {
		s := Schema{
			ID:     "t",
			Binary: "mkfs.t",
			Options: []Option{
				{ID: "label", Name: "L", Type: KindString, Default: "", Flag: "-L {value}"},
			},
		}
		mod(&s)
		return []Schema{s}
	}

	cases := []struct {
		name    string
		schemas []Schema
		want    string
	}{
		{
			name:    "empty schema ID",
			schemas: []Schema{{Name: "x", Binary: "b"}},
			want:    "empty ID",
		},
		{
			name:    "empty binary",
			schemas: []Schema{{ID: "x"}},
			want:    "empty Binary",
		},
		{
			name:    "duplicate schema IDs",
			schemas: []Schema{{ID: "x", Binary: "b"}, {ID: "x", Binary: "b"}},
			want:    "duplicate schema ID",
		},
		{
			name: "dangling conflict reference",
			schemas: base(func(s *Schema) {
				s.Options[0].Conflicts = []string{"nope"}
			}),
			want: `Conflicts references unknown option "nope"`,
		},
		{
			name: "asymmetric conflicts",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "other", Type: KindString, Default: "", Flag: "-o {value}"})
				s.Options[0].Conflicts = []string{"other"}
			}),
			want: "not declared symmetrically",
		},
		{
			name: "conflict self-reference",
			schemas: base(func(s *Schema) {
				s.Options[0].Conflicts = []string{"label"}
			}),
			want: "Conflicts self-reference",
		},
		{
			name: "duplicate option IDs",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "label", Type: KindString, Default: "", Flag: "-X {value}"})
			}),
			want: "duplicate option ID",
		},
		{
			name: "enum without values",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "e", Type: KindEnum, Default: "a", Flag: "-e {value}"})
			}),
			want: "KindEnum requires Values",
		},
		{
			name: "values on non-enum",
			schemas: base(func(s *Schema) {
				s.Options[0].Values = []EnumValue{{Value: "x"}}
			}),
			want: "Values present on non-enum option",
		},
		{
			name: "bool without either flag",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "b", Type: KindBool, Default: true})
			}),
			want: "at least one of FlagTrue/FlagFalse",
		},
		{
			name: "flag sides on non-bool",
			schemas: base(func(s *Schema) {
				s.Options[0].FlagTrue = "-x"
			}),
			want: "FlagTrue/FlagFalse on non-bool option",
		},
		{
			name: "default type mismatch",
			schemas: base(func(s *Schema) {
				s.Options[0].Default = int64(1)
			}),
			want: "Default type does not match option Type",
		},
		{
			name: "omit type mismatch",
			schemas: base(func(s *Schema) {
				s.Options[0].Omit = true
			}),
			want: "Omit type does not match option Type",
		},
		{
			name: "enum default not in values",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{
					ID: "e", Type: KindEnum, Default: "zzz", Flag: "-e {value}",
					Values: []EnumValue{{Value: "a", Label: "A"}},
				})
			}),
			want: `Default "zzz" not in Values`,
		},
		{
			name: "int default out of bounds",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{
					ID: "n", Type: KindInt, Default: int64(1), Flag: "-n {value}", Min: i64(10),
				})
			}),
			want: "Default 1 below Min 10",
		},
		{
			name: "missing {value} in flag",
			schemas: base(func(s *Schema) {
				s.Options[0].Flag = "-L"
			}),
			want: "Flag must contain {value} exactly once",
		},
		{
			name: "bool with flag",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "b", Type: KindBool, Default: true, FlagTrue: "-y", Flag: "-b {value}"})
			}),
			want: "bool option must not have Flag",
		},
		{
			name: "oversized description",
			schemas: base(func(s *Schema) {
				s.Options[0].Description = strings.Repeat("x", 201)
			}),
			want: "Description exceeds 200 bytes",
		},
		{
			name: "oversized long help",
			schemas: base(func(s *Schema) {
				s.Options[0].LongHelp = strings.Repeat("x", 2001)
			}),
			want: "LongHelp exceeds 2000 bytes",
		},
		{
			name: "bad min version",
			schemas: base(func(s *Schema) {
				s.MinVersion = "v1.2"
			}),
			want: "MinVersion",
		},
		{
			name: "pattern does not compile",
			schemas: base(func(s *Schema) {
				s.Options[0].Pattern = "["
			}),
			want: "Pattern does not compile",
		},
		{
			name: "composite references non-composite-only option",
			schemas: base(func(s *Schema) {
				s.Composites = []Composite{{Flag: "-d x={label}", Requires: []string{"label"}}}
			}),
			want: "is not CompositeOnly",
		},
		{
			name: "composite placeholder not in requires",
			schemas: base(func(s *Schema) {
				s.Options = append(s.Options, Option{ID: "su", Type: KindSize, Default: "", CompositeOnly: true})
				s.Composites = []Composite{{Flag: "-d su={su},sw={sw}", Requires: []string{"su"}}}
			}),
			want: "placeholder {sw} does not match a Requires entry",
		},
		{
			name: "composite requires unknown option",
			schemas: base(func(s *Schema) {
				s.Composites = []Composite{{Flag: "-d x={ghost}", Requires: []string{"ghost"}}}
			}),
			want: `Requires references unknown option "ghost"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validate(tc.schemas)
			for _, e := range errs {
				if strings.Contains(e, tc.want) {
					return
				}
			}
			t.Fatalf("expected an error containing %q, got %v", tc.want, errs)
		})
	}
}
