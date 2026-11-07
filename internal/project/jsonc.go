package project

import "bytes"

// stripJSONC removes // and /* */ comments and trailing commas from JSON.
//
// A configuration file people are told to record decisions in — "turn a
// rule off, and say why next to it" — has to accept a comment, or the
// advice is a syntax error. encoding/json does not, so the text is cleaned
// before it is parsed.
//
// The scan is character by character rather than a regular expression
// because the only thing that matters is getting STRINGS right: a value
// containing "//" (a URL, a Windows path, a regex) must survive untouched,
// and that needs tracking whether the cursor is inside a string and
// whether the previous character was an escape.
//
// Byte positions are preserved: every removed character becomes a space,
// so a parse error still points at the right line and column.
func stripJSONC(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)

	const (
		normal = iota
		inString
		inLine
		inBlock
	)
	state := normal
	escaped := false

	blank := func(i int) {
		if out[i] != '\n' && out[i] != '\r' {
			out[i] = ' '
		}
	}

	for i := 0; i < len(in); i++ {
		c := in[i]
		switch state {
		case normal:
			switch {
			case c == '"':
				state = inString
			case c == '/' && i+1 < len(in) && in[i+1] == '/':
				state = inLine
				blank(i)
				blank(i + 1)
				i++
			case c == '/' && i+1 < len(in) && in[i+1] == '*':
				state = inBlock
				blank(i)
				blank(i + 1)
				i++
			}
		case inString:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				state = normal
			}
		case inLine:
			if c == '\n' {
				state = normal
			} else {
				blank(i)
			}
		case inBlock:
			if c == '*' && i+1 < len(in) && in[i+1] == '/' {
				blank(i)
				blank(i + 1)
				i++
				state = normal
			} else {
				blank(i)
			}
		}
	}

	return stripTrailingCommas(out)
}

// stripTrailingCommas removes a comma that is followed only by whitespace
// and a closing brace or bracket.
//
// Every editor that formats JSON leaves one eventually, and refusing the
// file over it teaches nobody anything.
func stripTrailingCommas(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)

	inStr, escaped := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case inStr && escaped:
			escaped = false
		case inStr && c == '\\':
			escaped = true
		case inStr && c == '"':
			inStr = false
		case !inStr && c == '"':
			inStr = true
		case !inStr && c == ',':
			j := i + 1
			for j < len(out) && isJSONSpace(out[j]) {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				out[i] = ' '
			}
		}
	}
	return out
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// hasComments reports whether cleaning changed anything, for diagnostics.
func hasComments(raw, cleaned []byte) bool { return !bytes.Equal(raw, cleaned) }
