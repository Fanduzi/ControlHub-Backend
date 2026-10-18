// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: raw user SQL text
// output: SafeStatementPreview — a disclosure-safe statement preview for
// execution-history rows
// pos: T10-C-R1 history preview masking — reuses the G8 lexical scan
// constructs: '..' E'..' B'..' X'..' U&'..' literals, $tag$ dollar-quoted
// bodies, -- and nested /* */ comments, ".." quoted identifiers, and numeric
// tokens. Literal bodies and comment text never survive into the preview;
// keywords and identifiers (quoted or not) do.
// note: if this file changes, update header and README.md
package pgsql

import (
	"strings"
	"unicode/utf8"
)

// Fixed markers a safe preview emits in place of user-supplied content.
// Comments are dropped entirely — the space keeps adjacent tokens apart.
const (
	statementPreviewLiteral = "<literal>"
	statementPreviewNumber  = "<number>"
)

// SafeStatementPreview renders statement as a disclosure-safe history
// preview: keywords, identifiers (including quoted ones), and punctuation
// survive; every literal body (quoted string, dollar-quoted body, numeric
// token) is replaced by a fixed marker and every comment is dropped, so no
// user value or comment text can ride the preview into history. The scan
// reuses the same byte-level PostgreSQL lexical model as the G8 template
// scanner and never fails: an unterminated construct masks everything after
// it. Output is cut at a rune boundary once it exceeds maxRunes (0 = no
// limit); it never falls back to the original bytes.
func SafeStatementPreview(statement string, maxRunes int) string {
	var b strings.Builder
	b.Grow(min(len(statement), 4096))
	i := 0
	for i < len(statement) {
		c := statement[i]
		switch {
		case c == '\'':
			b.WriteString(statementPreviewLiteral)
			i = skipPGString(statement, i)
		case c == '"':
			end := skipPGQuoted(statement, i+1, '"', false)
			if end > len(statement) {
				// Unterminated quoted identifier — the scan cannot tell
				// where name bytes end and statement text begins, so the
				// tail is content, not structure: mask it.
				b.WriteString(statementPreviewLiteral)
				i = len(statement)
			} else {
				b.WriteString(statement[i:end])
				i = end
			}
		case c == '-' && i+1 < len(statement) && statement[i+1] == '-':
			b.WriteByte(' ')
			i = skipPGLineComment(statement, i+2)
		case c == '/' && i+1 < len(statement) && statement[i+1] == '*':
			b.WriteByte(' ')
			i = skipPGBlockComment(statement, i+2)
		case c == '$' && !pgIdentByteBefore(statement, i):
			next, isBody := scanPGDollarBody(statement, i)
			if isBody {
				b.WriteString(statementPreviewLiteral)
			} else {
				b.WriteString(statement[i:next])
			}
			i = next
		case c >= '0' && c <= '9' && !pgIdentByteBefore(statement, i):
			b.WriteString(statementPreviewNumber)
			i = skipPGNumberToken(statement, i)
		case pgStringPrefixLen(statement, i) > 0:
			b.WriteString(statementPreviewLiteral)
			i = skipPGString(statement, i+pgStringPrefixLen(statement, i))
		default:
			b.WriteByte(c)
			i++
		}
	}
	out := b.String()
	if maxRunes > 0 && utf8.RuneCountInString(out) > maxRunes {
		return truncateRunes(out, maxRunes)
	}
	return out
}

// pgStringPrefixLen reports the length of a string-literal prefix starting at
// pos: one byte for E'..', B'..', and X'..' (which all lex like a plain
// string except that E enables backslash escapes — skipPGString detects that
// itself), two bytes for U&'..'. The prefix must sit at an identifier
// boundary, so e.g. the e inside "some" is never a prefix.
func pgStringPrefixLen(s string, pos int) int {
	if pos > 0 && pgIdentPart(s[pos-1]) {
		return 0
	}
	c := s[pos]
	if pos+1 < len(s) && s[pos+1] == '\'' {
		switch c {
		case 'e', 'E', 'b', 'B', 'x', 'X':
			return 1
		}
	}
	if (c == 'u' || c == 'U') && pos+2 < len(s) && s[pos+1] == '&' && s[pos+2] == '\'' {
		return 2
	}
	return 0
}

// scanPGDollarBody reports the end offset of the $tag$..$tag$ body opening at
// pos (isBody); when the tag never closes it consumes the rest of the
// statement so the unterminated body is still masked. A $ that opens no body
// — a native $n parameter or an operator byte — is plain text (isBody false)
// and only its own run is consumed.
func scanPGDollarBody(s string, pos int) (int, bool) {
	i := pos + 1
	if i < len(s) && s[i] >= '0' && s[i] <= '9' {
		end := i
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		return end, false
	}
	tagEnd := i
	for tagEnd < len(s) && pgIdentPartDollarless(s[tagEnd]) {
		tagEnd++
	}
	if tagEnd < len(s) && s[tagEnd] == '$' {
		tag := s[pos : tagEnd+1] // includes both '$' delimiters
		idx := strings.Index(s[tagEnd+1:], tag)
		if idx < 0 {
			return len(s), true // unterminated: mask the rest
		}
		return tagEnd + 1 + idx + len(tag), true
	}
	return pos + 1, false
}

// skipPGNumberToken consumes one numeric literal run starting on a digit at
// an identifier boundary: digits plus the identifier/dotted bytes PostgreSQL
// folds into the token (0x1F, 1.5, 1e10, 42::int keeps :: for the cast).
func skipPGNumberToken(s string, i int) int {
	i++
	for i < len(s) && (pgIdentPart(s[i]) || s[i] == '.') {
		i++
	}
	return i
}

// truncateRunes cuts s at the n-th rune boundary.
func truncateRunes(s string, n int) string {
	end := 0
	for i := 0; i < n; i++ {
		_, size := utf8.DecodeRuneInString(s[end:])
		end += size
	}
	return s[:end]
}
