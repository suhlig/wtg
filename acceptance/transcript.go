package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type StepTranscript struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Command     string `json:"command"`
	Cwd         string `json:"cwd"`
	ExitCode    int    `json:"exit_code"`
	OutputRaw   string `json:"output_raw"`
	OutputPlain string `json:"output_plain"`
	OutputHTML  string `json:"output_html"`
	Explanation string `json:"explanation,omitempty"`
}

type ScenarioTranscript struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Steps       []StepTranscript `json:"steps"`
}

type TranscriptRegistry struct {
	mu          sync.Mutex
	scenarios   map[string]*ScenarioTranscript
	scenarioOrd []string
}

var GlobalTranscripts = &TranscriptRegistry{
	scenarios: make(map[string]*ScenarioTranscript),
}

func (r *TranscriptRegistry) AddScenario(id, title, desc string) *ScenarioTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	sc, exists := r.scenarios[id]
	if !exists {
		sc = &ScenarioTranscript{
			ID:          id,
			Title:       title,
			Description: desc,
			Steps:       make([]StepTranscript, 0),
		}
		r.scenarios[id] = sc
		r.scenarioOrd = append(r.scenarioOrd, id)
	} else {
		if title != "" {
			sc.Title = title
		}
		if desc != "" {
			sc.Description = desc
		}
	}
	return sc
}

func (r *TranscriptRegistry) RecordStep(scenarioID, title, desc string, step StepTranscript) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sc, exists := r.scenarios[scenarioID]
	if !exists {
		sc = &ScenarioTranscript{
			ID:          scenarioID,
			Title:       title,
			Description: desc,
			Steps:       make([]StepTranscript, 0),
		}
		r.scenarios[scenarioID] = sc
		r.scenarioOrd = append(r.scenarioOrd, scenarioID)
	}
	step.Index = len(sc.Steps) + 1
	sc.Steps = append(sc.Steps, step)
}

func (r *TranscriptRegistry) SaveAll(outputDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	for id, sc := range r.scenarios {
		data, err := json.MarshalIndent(sc, "", "  ")
		if err != nil {
			return err
		}
		target := filepath.Join(outputDir, fmt.Sprintf("%s.json", id))
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}
	}
	return nil
}
