package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/suhlig/wtg/internal/trace"
)

// isDry reports whether a command is previewing (--dry-run): either the git
// runner or the filesystem skips mutations. See ADR 0021.
func isDry(runner any, f trace.FS) bool {
	return trace.IsDry(runner) || trace.IsDryRun(f)
}

// confirm writes a [y/N] prompt and returns true only if the user types "y" or "Y".
func confirm(r *bufio.Reader, out io.Writer, message string) (bool, error) {
	_, _ = fmt.Fprintf(out, "%s [y/N]: ", message)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}
