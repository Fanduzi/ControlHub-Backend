// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: raw template SQL text
// output: TemplatePlaceholder, ScanTemplatePlaceholders — G8 single-pass lexical scan
// pos: G8 template placeholder scan (T11) — recognizes :name markers only in code
//
//	positions; '..' E'..' U&'..' ".." literals and quoted identifiers, -- and
//	nested /* */ comments, $tag$ dollar-quoted bodies, and [..] array slices
//	never yield placeholders; :: stays a cast; native $n parameters are
//	rejected so generated $k bindings can never collide with user text
//
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"strings"
)

// ErrTemplateScanInvalid marks a malformed template statement: an
// unterminated string, quoted identifier, block comment, or dollar-quoted
// body. The compiler maps it to its controlled invalid-statement error.
var ErrTemplateScanInvalid = errors.New("pgsql: invalid template statement")

// ErrTemplateNativeParameter marks a user-typed $n parameter. Templates only
// accept declared :name markers; generated $k bindings must never collide
// with text the user wrote.
var ErrTemplateNativeParameter = errors.New("pgsql: native $n parameters are not accepted; declare :name parameters instead")

// TemplatePlaceholder is one :name marker found outside strings, quoted
// identifiers, comments, dollar-quoted bodies, and bracket subscripts.
// Offset/Length index the ':'..name bytes in the original statement.
type TemplatePlaceholder struct {
	Name   string
	Offset int
	Length int
}

// ScanTemplatePlaceholders performs one byte-level pass over a user statement
// and returns every :name placeholder in source order. PostgreSQL lexical
// constructs are honored so a colon inside a string, quoted identifier,
// comment, dollar-quoted body, or array slice is never a placeholder, and ::
// stays a cast. A native $n parameter is a controlled rejection: template
// output uses generated $k positions, so accepting user-typed $n would let
// text collide with bound arguments.
func ScanTemplatePlaceholders(statement string) ([]TemplatePlaceholder, error) {
	var out []TemplatePlaceholder
	brackets := 0
	i := 0
	for i < len(statement) {
		c := statement[i]
		switch {
		case c == '\'':
			i = skipPGString(statement, i)
		case c == '"':
			i = skipPGQuoted(statement, i+1, '"', false)
		case c == '-' && i+1 < len(statement) && statement[i+1] == '-':
			i = skipPGLineComment(statement, i+2)
		case c == '/' && i+1 < len(statement) && statement[i+1] == '*':
			i = skipPGBlockComment(statement, i+2)
		case c == '[':
			brackets++
			i++
		case c == ']':
			if brackets > 0 {
				brackets--
			}
			i++
		case c == '$' && !pgIdentByteBefore(statement, i):
			next, err := skipPGDollarOrRejectParam(statement, i)
			if err != nil {
				return nil, err
			}
			i = next
		case c == ':':
			if i+1 < len(statement) && statement[i+1] == ':' {
				i += 2 // :: stays a cast
				continue
			}
			if brackets > 0 || i+1 >= len(statement) || !pgIdentStart(statement[i+1]) {
				i++ // slice colon or a literal ':' — not a marker
				continue
			}
			end := i + 2
			for end < len(statement) && pgIdentPart(statement[end]) {
				end++
			}
			out = append(out, TemplatePlaceholder{
				Name:   statement[i+1 : end],
				Offset: i,
				Length: end - i,
			})
			i = end
		default:
			i++
		}
	}
	if i > len(statement) {
		// skipPG* helpers return one past the terminator position so the loop
		// lands exactly at the offending construct; overshoot means unterminated.
		return nil, ErrTemplateScanInvalid
	}
	return out, nil
}

// pgIdentStart reports whether b can start a PostgreSQL identifier piece —
// ASCII letters, underscore, and any multi-byte UTF-8 lead byte (non-ASCII
// identifiers are legal). A parameter name is validated separately by the
// compiler's stricter templateParameterNamePattern.
func pgIdentStart(b byte) bool {
	return b == '_' || b >= 0x80 || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// pgIdentPart reports whether b continues an identifier: start characters
// plus digits and '$' (PostgreSQL identifiers may contain '$').
func pgIdentPart(b byte) bool {
	return pgIdentStart(b) || (b >= '0' && b <= '9') || b == '$'
}

// pgIdentByteBefore reports whether the byte before pos continues an
// identifier, which means a '$' at pos is part of that identifier (e.g.
// "a$b$" is one identifier) and never starts a parameter or dollar quote.
func pgIdentByteBefore(s string, pos int) bool {
	return pos > 0 && pgIdentPart(s[pos-1])
}

// skipPGString consumes a '..' literal starting at the opening quote plus any
// continuation segments. Only an E'..' prefix enables backslash escapes —
// U&'..' never lets an escape character swallow a quote: with a custom
// UESCAPE the backslash is plain content, and with the default one \' is an
// invalid escape either way, so quote handling is identical to a plain
// string. When the literal closes and the whitespace/comment gap before the
// next ' contains at least one newline, the segment continues in the SAME
// escape state (SQL string continuation — an E literal keeps escaping).
func skipPGString(s string, pos int) int {
	escapes := pgEStringPrefix(s, pos)
	i := skipPGQuoted(s, pos+1, '\'', escapes)
	for i <= len(s) {
		j, sawNewline, ok := pgStringGap(s, i)
		if !ok || !sawNewline || j >= len(s) || s[j] != '\'' {
			return i
		}
		i = skipPGQuoted(s, j+1, '\'', escapes)
	}
	return i
}

// pgEStringPrefix reports whether the ' at pos is E'..' prefixed: a bare e/E
// at an identifier boundary. U&'..' is intentionally absent — its escape
// character cannot be a quote.
func pgEStringPrefix(s string, pos int) bool {
	if pos == 0 {
		return false
	}
	prev := s[pos-1]
	return (prev == 'e' || prev == 'E') && (pos-1 == 0 || !pgIdentPart(s[pos-2]))
}

// pgStringGap scans the whitespace/comments between a closed string literal
// and whatever follows. It reports where non-gap input resumes and whether
// the gap contained a newline (\n or \r — PostgreSQL treats both as line
// terminators). An unterminated /* inside the gap invalidates the whole
// statement, same as in the main scan.
func pgStringGap(s string, i int) (int, bool, bool) {
	j := i
	sawNewline := false
	for j < len(s) {
		switch c := s[j]; {
		case c == '\n' || c == '\r':
			sawNewline = true
			j++
		case c == ' ' || c == '\t' || c == '\f' || c == '\v':
			j++
		case c == '-' && j+1 < len(s) && s[j+1] == '-':
			j = skipPGLineComment(s, j+2)
		case c == '/' && j+1 < len(s) && s[j+1] == '*':
			end := skipPGBlockComment(s, j+2)
			if end > len(s) {
				return j, sawNewline, false
			}
			if strings.IndexAny(s[j:end], "\n\r") >= 0 {
				sawNewline = true
			}
			j = end
		default:
			return j, sawNewline, true
		}
	}
	return j, sawNewline, true
}

// skipPGQuoted consumes a '..' or ".." body starting just after the open
// quote. ”/"" doubles are escapes; when escapes is set, \x also escapes.
func skipPGQuoted(s string, i int, quote byte, escapes bool) int {
	for i < len(s) {
		switch s[i] {
		case quote:
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		case '\\':
			if escapes && i+1 < len(s) {
				i += 2
				continue
			}
		}
		i++
	}
	return len(s) + 1 // unterminated
}

// skipPGLineComment consumes a -- comment body. PostgreSQL ends a line
// comment on '\n' OR '\r' — a lone carriage return already returns to code,
// so placeholders and native $n after it must be scanned normally.
func skipPGLineComment(s string, i int) int {
	for i < len(s) && s[i] != '\n' && s[i] != '\r' {
		i++
	}
	return i
}

// skipPGBlockComment consumes a /* .. */ body with nesting (PostgreSQL nests
// block comments, unlike MySQL). Returns len+1 when unterminated.
func skipPGBlockComment(s string, i int) int {
	depth := 1
	for i < len(s) {
		switch {
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			depth++
			i += 2
		case s[i] == '*' && i+1 < len(s) && s[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(s) + 1 // unterminated
}

// skipPGDollarOrRejectParam handles a '$' at pos (not inside an identifier).
// $tag$ opens a dollar-quoted body that runs to its matching close tag;
// $<digits> is a native parameter and is rejected; anything else is a literal
// byte (e.g. an operator character) and consumes one position.
func skipPGDollarOrRejectParam(s string, pos int) (int, error) {
	i := pos + 1
	if i < len(s) && s[i] >= '0' && s[i] <= '9' {
		end := i
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		return 0, ErrTemplateNativeParameter
	}
	tagEnd := i
	for tagEnd < len(s) && pgIdentPartDollarless(s[tagEnd]) {
		tagEnd++
	}
	if tagEnd < len(s) && s[tagEnd] == '$' {
		tag := s[pos : tagEnd+1] // includes both '$' delimiters
		rest := s[tagEnd+1:]
		idx := strings.Index(rest, tag)
		if idx < 0 {
			return 0, ErrTemplateScanInvalid
		}
		return tagEnd + 1 + idx + len(tag), nil
	}
	return pos + 1, nil
}

// pgIdentPartDollarless is pgIdentPart minus '$' — dollar-quote tags cannot
// contain '$'.
func pgIdentPartDollarless(b byte) bool {
	return pgIdentStart(b) || (b >= '0' && b <= '9')
}
