package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type TranscriptData struct {
	ID    string `json:"id"`
	Steps []struct {
		Index       int    `json:"index"`
		Command     string `json:"command"`
		OutputRaw   string `json:"output_raw"`
		OutputPlain string `json:"output_plain"`
	} `json:"steps"`
}

var shortcodeRegex = regexp.MustCompile(`(?:\{\{\s*transcript\(\s*id="([^"]+)",\s*step=(\d+)\s*\)\s*\}\}|<transcript\s+id="([^"]+)"\s+step=(\d+)\s*/>|\{%\s*set\s+id\s*=\s*"([^"]+)"\s*%\}\s*\{%\s*set\s+step\s*=\s*(\d+)\s*%\}\s*\{%\s*include\s+"transcript\.html"\s*%\})`)
var forbiddenBlockRegex = regexp.MustCompile("(?s)```(?:sh|bash|zsh)?\n(.*?)```")

func main() {
	contentDir := "site/content"
	dataDir := "site/data/transcripts"

	if err := checkTranscripts(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "transcript check error: %v\n", err)
		os.Exit(1)
	}

	if err := checkContent(contentDir, dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "content check error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Documentation and transcripts checks passed successfully.")
}

func checkTranscripts(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("read transcripts dir: %w", err)
	}

	tempPatterns := []string{
		"/var/folders",
		"/private/var",
		"/tmp/wtg-acceptance",
		"wtg-acceptance-",
		"/Users/",
		"/home/",
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dataDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var t TranscriptData
		if err := json.Unmarshal(data, &t); err != nil {
			return fmt.Errorf("invalid json in %s: %w", path, err)
		}

		for _, step := range t.Steps {
			for _, pat := range tempPatterns {
				if strings.Contains(step.OutputPlain, pat) || strings.Contains(step.OutputRaw, pat) {
					return fmt.Errorf("path leak found in %s step %d: contains %q", path, step.Index, pat)
				}
			}
		}
	}
	return nil
}

func checkContent(contentDir, dataDir string) error {
	loadedTranscripts := make(map[string]map[int]bool)

	// Load available transcripts
	entries, _ := os.ReadDir(dataDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dataDir, e.Name()))
		if err != nil {
			continue
		}
		var t TranscriptData
		if err := json.Unmarshal(data, &t); err == nil {
			steps := make(map[int]bool)
			for _, s := range t.Steps {
				steps[s.Index] = true
			}
			loadedTranscripts[id] = steps
		}
	}

	var errors []string

	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		// Check for shortcode targets
		matches := shortcodeRegex.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			var id, stepStr string
			if m[1] != "" {
				id, stepStr = m[1], m[2]
			} else if m[3] != "" {
				id, stepStr = m[3], m[4]
			} else if len(m) > 6 && m[5] != "" {
				id, stepStr = m[5], m[6]
			}
			var step int
			_, _ = fmt.Sscanf(stepStr, "%d", &step)
			steps, exists := loadedTranscripts[id]
			if !exists {
				errors = append(errors, fmt.Sprintf("%s: referenced transcript %q not found", path, id))
			} else if !steps[step] {
				errors = append(errors, fmt.Sprintf("%s: step %d in transcript %q not found", path, step, id))
			}
		}

		// Check for hand-typed wtg command blocks in scenario guides
		// Allowed in reference/ or install/shell integration blocks
		if strings.Contains(path, "guides/first-steps") ||
			strings.Contains(path, "guides/new-workspace") ||
			strings.Contains(path, "guides/sync-and-rebase") ||
			strings.Contains(path, "guides/land-and-delete") ||
			strings.Contains(path, "guides/add-and-remove") ||
			strings.Contains(path, "guides/read-only") ||
			strings.Contains(path, "guides/preview-and-audit") {
			blocks := forbiddenBlockRegex.FindAllStringSubmatch(content, -1)
			for _, b := range blocks {
				blockContent := b[1]
				lines := strings.Split(blockContent, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "wtg ") && !strings.Contains(line, "completion") && !strings.Contains(line, "path") {
						errors = append(errors, fmt.Sprintf("%s: found hand-typed command block %q instead of transcript shortcode", path, line))
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	if len(errors) > 0 {
		return fmt.Errorf("check failed:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}
