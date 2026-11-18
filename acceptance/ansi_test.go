package acceptance

import (
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "colored text",
			input:    "\x1b[32mhello\x1b[0m \x1b[1;31mworld\x1b[0m",
			expected: "hello world",
		},
		{
			name:     "cursor movements and colors",
			input:    "\x1b[2K\r\x1b[32m✓\x1b[0m clean",
			expected: "✓ clean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.expected {
				t.Errorf("StripANSI() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAnsiToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text with html characters",
			input:    "foo <bar> & baz",
			expected: "foo &lt;bar&gt; &amp; baz",
		},
		{
			name:     "green text",
			input:    "\x1b[32m✓ clean\x1b[0m",
			expected: `<span class="ansi-fg-green">✓ clean</span>`,
		},
		{
			name:     "bold red text",
			input:    "\x1b[1;31mError\x1b[0m",
			expected: `<span class="ansi-bold ansi-fg-red">Error</span>`,
		},
		{
			name:     "256 color",
			input:    "\x1b[38;5;208mwarning\x1b[0m",
			expected: `<span class="ansi-fg-color-208">warning</span>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnsiToHTML(tt.input)
			if got != tt.expected {
				t.Errorf("AnsiToHTML() = %q, want %q", got, tt.expected)
			}
		})
	}
}
