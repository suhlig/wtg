package acceptance

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\r`)

// StripANSI removes all ANSI escape sequences and carriage returns from a string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// AnsiToHTML converts ANSI escape sequences in text to HTML spans with class names.
// It escapes raw HTML special characters so the output is safe to embed in HTML.
func AnsiToHTML(input string) string {
	if input == "" {
		return ""
	}

	// First clean carriage returns
	input = strings.ReplaceAll(input, "\r", "")

	var b strings.Builder
	openSpans := 0

	// State
	bold := false
	dim := false
	italic := false
	underline := false
	fgColor := ""
	bgColor := ""

	updateSpans := func() {
		for openSpans > 0 {
			b.WriteString("</span>")
			openSpans--
		}

		var classes []string
		if bold {
			classes = append(classes, "ansi-bold")
		}
		if dim {
			classes = append(classes, "ansi-dim")
		}
		if italic {
			classes = append(classes, "ansi-italic")
		}
		if underline {
			classes = append(classes, "ansi-underline")
		}
		if fgColor != "" {
			classes = append(classes, "ansi-fg-"+fgColor)
		}
		if bgColor != "" {
			classes = append(classes, "ansi-bg-"+bgColor)
		}

		if len(classes) > 0 {
			fmt.Fprintf(&b, `<span class="%s">`, strings.Join(classes, " "))
			openSpans++
		}
	}

	i := 0
	for i < len(input) {
		if input[i] == '\x1b' && i+1 < len(input) && input[i+1] == '[' {
			// Find end of ANSI sequence
			end := i + 2
			for end < len(input) && (input[end] >= '0' && input[end] <= '9' || input[end] == ';') {
				end++
			}
			if end < len(input) {
				cmd := input[end]
				params := input[i+2 : end]
				if cmd == 'm' {
					// SGR command
					codes := parseAnsiCodes(params)
					if len(codes) == 0 {
						codes = []int{0}
					}
					for idx := 0; idx < len(codes); idx++ {
						code := codes[idx]
						switch code {
						case 0:
							bold, dim, italic, underline = false, false, false, false
							fgColor, bgColor = "", ""
						case 1:
							bold = true
						case 2:
							dim = true
						case 3:
							italic = true
						case 4:
							underline = true
						case 22:
							bold, dim = false, false
						case 23:
							italic = false
						case 24:
							underline = false
						case 30:
							fgColor = "black"
						case 31:
							fgColor = "red"
						case 32:
							fgColor = "green"
						case 33:
							fgColor = "yellow"
						case 34:
							fgColor = "blue"
						case 35:
							fgColor = "magenta"
						case 36:
							fgColor = "cyan"
						case 37:
							fgColor = "white"
						case 39:
							fgColor = ""
						case 90:
							fgColor = "bright-black"
						case 91:
							fgColor = "bright-red"
						case 92:
							fgColor = "bright-green"
						case 93:
							fgColor = "bright-yellow"
						case 94:
							fgColor = "bright-blue"
						case 95:
							fgColor = "bright-magenta"
						case 96:
							fgColor = "bright-cyan"
						case 97:
							fgColor = "bright-white"
						case 40:
							bgColor = "black"
						case 41:
							bgColor = "red"
						case 42:
							bgColor = "green"
						case 43:
							bgColor = "yellow"
						case 44:
							bgColor = "blue"
						case 45:
							bgColor = "magenta"
						case 46:
							bgColor = "cyan"
						case 47:
							bgColor = "white"
						case 49:
							bgColor = ""
						case 38:
							// 38;5;n (256 color) or 38;2;r;g;b (RGB)
							if idx+2 < len(codes) && codes[idx+1] == 5 {
								fgColor = fmt.Sprintf("color-%d", codes[idx+2])
								idx += 2
							}
						case 48:
							if idx+2 < len(codes) && codes[idx+1] == 5 {
								bgColor = fmt.Sprintf("color-%d", codes[idx+2])
								idx += 2
							}
						}
					}
					updateSpans()
				}
				i = end + 1
				continue
			}
		}

		// Regular character
		// Collect text until next ESC or special char
		start := i
		for i < len(input) && input[i] != '\x1b' {
			i++
		}
		text := input[start:i]
		b.WriteString(html.EscapeString(text))
	}

	for openSpans > 0 {
		b.WriteString("</span>")
		openSpans--
	}

	return b.String()
}

func parseAnsiCodes(params string) []int {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	var codes []int
	for _, p := range parts {
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			codes = append(codes, n)
		}
	}
	return codes
}
