// Package cmdgen turns (schema, values, device) into the exact argv for the
// mkfs backend, plus a copy-paste-runnable display string. No shell is ever
// involved anywhere (spec §7, §11); argv elements go straight to exec.
package cmdgen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethanpil/cmkfs/internal/schema"
)

// patterns holds the compiled Pattern of every shipped option, compiled once
// at package init (spec §5.2). TestSchemas guarantees they all compile.
var patterns = map[string]*regexp.Regexp{}

func init() {
	for _, s := range schema.Schemas {
		for _, o := range s.Options {
			if o.Pattern != "" {
				patterns[s.ID+"/"+o.ID] = regexp.MustCompile(`\A(?:` + o.Pattern + `)\z`)
			}
		}
	}
}

func compiledPattern(s schema.Schema, o schema.Option) (*regexp.Regexp, error) {
	if p, ok := patterns[s.ID+"/"+o.ID]; ok {
		return p, nil
	}
	return regexp.Compile(`\A(?:` + o.Pattern + `)\z`)
}

var sizeRe = regexp.MustCompile(`^([0-9]+)([kKmMgG]?)$`)

// ParseSize parses a KindSize input: an integer with optional binary suffix
// k, m, g (case-insensitive, 1024-based). Returns the value in bytes.
func ParseSize(s string) (int64, error) {
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%q is not a size (integer with optional k/m/g suffix)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size: %v", s, err)
	}
	var mult int64 = 1
	switch strings.ToLower(m[2]) {
	case "k":
		mult = 1024
	case "m":
		mult = 1024 * 1024
	case "g":
		mult = 1024 * 1024 * 1024
	}
	if n > (1<<63-1)/mult {
		return 0, fmt.Errorf("size %q overflows", s)
	}
	return n * mult, nil
}

// valueOf returns the effective value of an option: the entry in values, or
// the option's Default when absent. It errors when the underlying Go type
// does not match the option's Type.
func valueOf(values map[string]any, o schema.Option) (any, error) {
	v, ok := values[o.ID]
	if !ok {
		return o.Default, nil
	}
	switch o.Type {
	case schema.KindBool:
		if _, ok := v.(bool); !ok {
			return nil, fmt.Errorf("option %s: expected bool, got %T", o.ID, v)
		}
	case schema.KindInt:
		if _, ok := v.(int64); !ok {
			return nil, fmt.Errorf("option %s: expected int64, got %T", o.ID, v)
		}
	case schema.KindEnum, schema.KindString, schema.KindSize:
		if _, ok := v.(string); !ok {
			return nil, fmt.Errorf("option %s: expected string, got %T", o.ID, v)
		}
	default:
		return nil, fmt.Errorf("option %s: unknown kind %d", o.ID, o.Type)
	}
	return v, nil
}

// isSet reports whether a value counts as "set" (non-omit). Empty string
// always omits for string and size kinds (spec §5.2); bools count as set
// when they differ from their default.
func isSet(o schema.Option, v any) bool {
	switch o.Type {
	case schema.KindBool:
		return v != o.Default
	case schema.KindString, schema.KindSize:
		if v == "" {
			return false
		}
	}
	if o.Omit != nil && v == o.Omit {
		return false
	}
	return true
}

// IsSet reports whether a value counts as "set" (non-omit) for an option;
// the UI uses it for conflict/requires dimming (spec §10.3).
func IsSet(o schema.Option, v any) bool { return isSet(o, v) }

// ValidateValue checks a single option's value against its bounds, pattern,
// byte length, and enum membership. Omitted values are always valid.
func ValidateValue(s schema.Schema, o schema.Option, v any) error {
	if !isSet(o, v) {
		return nil
	}
	switch o.Type {
	case schema.KindEnum:
		sv := v.(string)
		for _, ev := range o.Values {
			if ev.Value == sv {
				return nil
			}
		}
		return fmt.Errorf("%s: %q is not a valid choice", o.Name, sv)
	case schema.KindInt:
		iv := v.(int64)
		if o.Min != nil && iv < *o.Min {
			return fmt.Errorf("%s: must be between %s and %s", o.Name, boundStr(o.Min), boundStr(o.Max))
		}
		if o.Max != nil && iv > *o.Max {
			return fmt.Errorf("%s: must be between %s and %s", o.Name, boundStr(o.Min), boundStr(o.Max))
		}
	case schema.KindSize:
		bytes, err := ParseSize(v.(string))
		if err != nil {
			return fmt.Errorf("%s: %v", o.Name, err)
		}
		if o.Min != nil && bytes < *o.Min {
			return fmt.Errorf("%s: must be between %s and %s bytes", o.Name, boundStr(o.Min), boundStr(o.Max))
		}
		if o.Max != nil && bytes > *o.Max {
			return fmt.Errorf("%s: must be between %s and %s bytes", o.Name, boundStr(o.Min), boundStr(o.Max))
		}
	case schema.KindString:
		sv := v.(string)
		if o.MaxBytes > 0 && len(sv) > o.MaxBytes {
			return fmt.Errorf("%s exceeds %d bytes", o.Name, o.MaxBytes)
		}
		if o.Pattern != "" {
			p, err := compiledPattern(s, o)
			if err != nil {
				return fmt.Errorf("%s: internal pattern error: %v", o.Name, err)
			}
			if !p.MatchString(sv) {
				return fmt.Errorf("%s: invalid value", o.Name)
			}
		}
	}
	return nil
}

// Validate checks every option value plus the cross-option rules: conflicts,
// requires, and the composite all-or-none rule.
func Validate(s schema.Schema, values map[string]any) error {
	byID := map[string]schema.Option{}
	setByID := map[string]bool{}
	for _, o := range s.Options {
		byID[o.ID] = o
		v, err := valueOf(values, o)
		if err != nil {
			return err
		}
		if err := ValidateValue(s, o, v); err != nil {
			return err
		}
		setByID[o.ID] = isSet(o, v)
	}
	for _, o := range s.Options {
		if !setByID[o.ID] {
			continue
		}
		for _, ref := range o.Conflicts {
			if setByID[ref] {
				return fmt.Errorf("%s conflicts with %s: only one may be set", o.Name, byID[ref].Name)
			}
		}
		for _, ref := range o.Requires {
			if !setByID[ref] {
				return fmt.Errorf("%s requires %s to be set", o.Name, byID[ref].Name)
			}
		}
	}
	for _, c := range s.Composites {
		set := 0
		for _, ref := range c.Requires {
			if setByID[ref] {
				set++
			}
		}
		if set != 0 && set != len(c.Requires) {
			names := make([]string, len(c.Requires))
			for i, ref := range c.Requires {
				names[i] = byID[ref].Name
			}
			return fmt.Errorf("set all of %s together, or none", strings.Join(names, ", "))
		}
	}
	return nil
}

func boundStr(p *int64) string {
	if p == nil {
		return "?"
	}
	return strconv.FormatInt(*p, 10)
}

// emitString renders a set value for flag substitution.
func emitString(o schema.Option, v any) (string, error) {
	switch o.Type {
	case schema.KindEnum, schema.KindString:
		return v.(string), nil
	case schema.KindInt:
		return strconv.FormatInt(v.(int64), 10), nil
	case schema.KindSize:
		raw := v.(string)
		if o.EmitAs == "suffixed" {
			return raw, nil
		}
		bytes, err := ParseSize(raw)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(bytes, 10), nil
	}
	return "", fmt.Errorf("option %s: kind %d has no value emission", o.ID, o.Type)
}

// CheckExtraToken enforces the extra-argument guardrails from spec §7 rule 5.
// A nil error means the token may be added to the list.
func CheckExtraToken(s schema.Schema, device, tok string) error {
	if strings.TrimSpace(tok) == "" {
		return fmt.Errorf("extra argument must not be empty or whitespace-only")
	}
	if strings.ContainsAny(tok, "\n\r") {
		return fmt.Errorf("extra argument must not contain a newline")
	}
	if device != "" && tok == device {
		return fmt.Errorf("extra argument must not be the target device path")
	}
	if strings.HasPrefix(tok, "/dev/") {
		return fmt.Errorf("extra argument must not be a /dev/ path")
	}
	if tok == s.ForceFlag {
		return fmt.Errorf("extra argument must not be the force flag (%s)", s.ForceFlag)
	}
	if s.WholeDiskFlag != "" && tok == s.WholeDiskFlag {
		return fmt.Errorf("extra argument must not be the whole-disk flag (%s)", s.WholeDiskFlag)
	}
	return nil
}

// Build returns the exec argv (argv[0] = schema.Binary) and a display string.
// force injects schema.ForceFlag immediately after argv[0]; an empty
// ForceFlag means the backend has no signature gate and force is a no-op —
// the safety layer's typed confirmation is the guard (spec §9). wholeDisk
// injects schema.WholeDiskFlag when set (e.g. mkfs.fat -I). extra carries
// the Extra Arguments list (spec §10.3): each element is already exactly one
// argv token, entered as such by the user — there is no tokenizer in cmkfs,
// by design. Pass nil when the list is empty.
func Build(s schema.Schema, values map[string]any, extra []string, device string, force, wholeDisk bool) (argv []string, display string, err error) {
	if s.Binary == "" {
		return nil, "", fmt.Errorf("schema %s has no binary", s.ID)
	}
	if device == "" {
		return nil, "", fmt.Errorf("no device given")
	}
	if err := Validate(s, values); err != nil {
		return nil, "", err
	}

	argv = []string{s.Binary}
	if force && s.ForceFlag != "" {
		argv = append(argv, strings.Fields(s.ForceFlag)...)
	}
	if wholeDisk && s.WholeDiskFlag != "" {
		argv = append(argv, strings.Fields(s.WholeDiskFlag)...)
	}

	// Options in schema order.
	for _, o := range s.Options {
		if o.CompositeOnly {
			continue
		}
		v, err := valueOf(values, o)
		if err != nil {
			return nil, "", err
		}
		if o.Type == schema.KindBool {
			var f string
			if v.(bool) {
				f = o.FlagTrue
			} else {
				f = o.FlagFalse
			}
			if f != "" {
				argv = append(argv, strings.Fields(f)...)
			}
			continue
		}
		if !isSet(o, v) {
			continue
		}
		val, err := emitString(o, v)
		if err != nil {
			return nil, "", err
		}
		// Split the template on spaces first, then substitute per element,
		// so a value containing spaces stays one argv entry.
		for _, field := range strings.Fields(o.Flag) {
			argv = append(argv, strings.ReplaceAll(field, "{value}", val))
		}
	}

	// Composites in schema order; all-or-none already validated above.
	for _, c := range s.Composites {
		allSet := true
		for _, ref := range c.Requires {
			o, ok := optionByID(s, ref)
			if !ok {
				return nil, "", fmt.Errorf("composite references unknown option %q", ref)
			}
			v, err := valueOf(values, o)
			if err != nil {
				return nil, "", err
			}
			if !isSet(o, v) {
				allSet = false
				break
			}
		}
		if !allSet {
			continue
		}
		fields := strings.Fields(c.Flag)
		for _, ref := range c.Requires {
			o, _ := optionByID(s, ref)
			v, err := valueOf(values, o)
			if err != nil {
				return nil, "", err
			}
			val, err := emitString(o, v)
			if err != nil {
				return nil, "", err
			}
			for i := range fields {
				fields[i] = strings.ReplaceAll(fields[i], "{"+ref+"}", val)
			}
		}
		argv = append(argv, fields...)
	}

	// Extra tokens, verbatim, after all schema-derived flags (spec §7 rule 5).
	for _, tok := range extra {
		if err := CheckExtraToken(s, device, tok); err != nil {
			return nil, "", err
		}
		argv = append(argv, tok)
	}

	// The device path must appear exactly once: as the final element.
	for _, a := range argv {
		if a == device {
			return nil, "", fmt.Errorf("a value equals the device path %s", device)
		}
	}
	argv = append(argv, device)

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return argv, strings.Join(quoted, " "), nil
}

func optionByID(s schema.Schema, id string) (schema.Option, bool) {
	for _, o := range s.Options {
		if o.ID == id {
			return o, true
		}
	}
	return schema.Option{}, false
}

// ShellQuote exposes the display quoting for UI rendering (the confirm
// screen highlights extra-argument tokens inside the command preview).
func ShellQuote(s string) string { return shellQuote(s) }

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote makes one argv element copy-paste safe for a POSIX shell:
// elements containing anything outside [A-Za-z0-9_@%+=:,./-] are wrapped in
// single quotes, with embedded single quotes escaped.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
