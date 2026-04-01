package config

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// PromptPassword reads a password from stdin without echoing.
// Falls back to env var NOTBBG_PASSWORD if set.
func PromptPassword(prompt string) string {
	if pw := os.Getenv("NOTBBG_PASSWORD"); pw != "" {
		return pw
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return ""
	}
	return string(pw)
}
