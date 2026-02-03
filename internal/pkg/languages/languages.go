package languages

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// GetLanguageName returns the display name of a language code in English
// e.g., "en" -> "English", "zh" -> "Chinese", "fr" -> "French"
func GetLanguageName(code string) string {
	tag, err := language.Parse(code)
	if err != nil {
		// Fallback to uppercase code if parsing fails
		return code
	}

	// Get the name in English
	name := display.English.Languages().Name(tag)
	if name == "" {
		return code
	}
	return name
}

// GetLanguageCode attempts to normalize a language name or tag to its ISO 639-1 code
// e.g., "English (US)" -> "en", "Chinese (Simplified)" -> "zh"
func GetLanguageCode(input string) string {
	tag, err := language.Parse(input)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(input))
	}
	base, _ := tag.Base()
	return base.String()
}
