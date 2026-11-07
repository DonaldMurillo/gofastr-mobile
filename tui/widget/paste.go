package widget

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func pasteText(text string, multiline bool) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if !multiline {
		text = strings.TrimSuffix(text, "\n")
	}
	var out strings.Builder
	out.Grow(len(text))
	for _, char := range text {
		switch {
		case char == '\n' || char == '\t':
			if !multiline {
				char = ' '
			}
		case unicode.IsControl(char):
			continue
		}
		out.WriteRune(char)
	}
	return out.String()
}

func limitInsert(text, retained string, limit int) string {
	if limit <= 0 {
		return text
	}
	remaining := max(limit-utf8.RuneCountInString(retained), 0)
	for offset := range text {
		if remaining == 0 {
			return text[:offset]
		}
		remaining--
	}
	return text
}
