package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type PRRecord struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Head       string `json:"head"`
	Base       string `json:"base"`
	HeadRefOid string `json:"headRefOid"`
	Body       string `json:"body"`
	Repo       string `json:"repo"`
}

type GHState struct {
	NextPR int                 `json:"next_pr"`
	PRs    map[string]PRRecord `json:"prs"` // key: repo#number
}

func getStatePath() string {
	stateDir := os.Getenv("FAKE_GH_STATE_DIR")
	if stateDir == "" {
		stateDir = os.Getenv("HOME")
	}
	return filepath.Join(stateDir, ".fake_gh_state.json")
}

func loadState() GHState {
	p := getStatePath()
	var st GHState
	st.PRs = make(map[string]PRRecord)
	st.NextPR = 1
	data, err := os.ReadFile(p)
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.PRs == nil {
		st.PRs = make(map[string]PRRecord)
	}
	if st.NextPR <= 0 {
		st.NextPR = 1
	}
	return st
}

func saveState(st GHState) {
	p := getStatePath()
	data, err := json.MarshalIndent(st, "", "  ")
	if err == nil {
		_ = os.WriteFile(p, data, 0644)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "fake gh: command required")
		os.Exit(1)
	}

	st := loadState()
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "auth":
		if len(args) > 0 && args[0] == "status" {
			fmt.Println("✓ Logged in to github.com account jane (keyring)")
			return
		}

	case "repo":
		if len(args) == 0 {
			os.Exit(1)
		}
		sub := args[0]
		switch sub {
		case "view":
			fmt.Println(`{"isArchived": false}`)
			return
		case "archive":
			fmt.Println("✓ Archived repository")
			return
		case "unarchive":
			fmt.Println("✓ Unarchived repository")
			return
		}

	case "pr":
		if len(args) == 0 {
			os.Exit(1)
		}
		sub := args[0]
		subArgs := args[1:]
		switch sub {
		case "list":
			repo := flagVal(subArgs, "--repo")
			head := flagVal(subArgs, "--head")
			stateFilter := flagVal(subArgs, "--state")

			var results []map[string]any
			for _, pr := range st.PRs {
				if repo != "" && pr.Repo != repo {
					continue
				}
				if head != "" && pr.Head != head {
					continue
				}
				if stateFilter == "open" && pr.State != "OPEN" {
					continue
				}
				results = append(results, map[string]any{
					"number":     pr.Number,
					"url":        pr.URL,
					"state":      pr.State,
					"headRefOid": pr.HeadRefOid,
				})
			}
			out, _ := json.Marshal(results)
			fmt.Println(string(out))
			return

		case "create":
			repo := flagVal(subArgs, "--repo")
			head := flagVal(subArgs, "--head")
			base := flagVal(subArgs, "--base")
			num := st.NextPR
			st.NextPR++
			url := fmt.Sprintf("https://github.com/%s/pull/%d", repo, num)
			key := fmt.Sprintf("%s#%d", repo, num)
			st.PRs[key] = PRRecord{
				Number:     num,
				URL:        url,
				State:      "OPEN",
				Head:       head,
				Base:       base,
				HeadRefOid: "1111111111111111111111111111111111111111",
				Body:       "",
				Repo:       repo,
			}
			saveState(st)
			fmt.Println(url)
			return

		case "view":
			numStr := ""
			if len(subArgs) > 0 && !strings.HasPrefix(subArgs[0], "--") {
				numStr = subArgs[0]
			}
			repo := flagVal(subArgs, "--repo")
			num, _ := strconv.Atoi(numStr)
			key := fmt.Sprintf("%s#%d", repo, num)
			pr, ok := st.PRs[key]
			if !ok {
				pr = PRRecord{Number: num, Repo: repo, Body: ""}
			}
			out, _ := json.Marshal(map[string]string{
				"body": pr.Body,
			})
			fmt.Println(string(out))
			return

		case "edit":
			numStr := ""
			if len(subArgs) > 0 && !strings.HasPrefix(subArgs[0], "--") {
				numStr = subArgs[0]
			}
			repo := flagVal(subArgs, "--repo")
			body := flagVal(subArgs, "--body")
			num, _ := strconv.Atoi(numStr)
			key := fmt.Sprintf("%s#%d", repo, num)
			pr := st.PRs[key]
			pr.Body = body
			st.PRs[key] = pr
			saveState(st)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "fake gh: unhandled command %v\n", os.Args[1:])
	os.Exit(1)
}

func flagVal(args []string, flag string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == flag && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(args[i], flag+"=") {
			return strings.TrimPrefix(args[i], flag+"=")
		}
	}
	return ""
}
