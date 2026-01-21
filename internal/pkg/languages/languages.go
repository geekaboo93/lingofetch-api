package languages

import (
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
