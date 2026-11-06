package domain

import "fmt"

// ParseTheme validates a theme name.
func ParseTheme(s string) (Theme, error) {
	switch t := Theme(s); t {
	case ThemeLight, ThemeDark, ThemeSystem:
		return t, nil
	}
	return "", fmt.Errorf("%w: %q, use light, dark or system", ErrInvalidTheme, s)
}
