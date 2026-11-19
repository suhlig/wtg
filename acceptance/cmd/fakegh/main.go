package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// headOid returns the commit that branch points to in the sandbox's bare copy
// of ownerRepo (see the ssh shim in acceptance/harness.go), as GitHub reports
// it in headRefOid. Without a sandbox it falls back to a fixed value.
func headOid(ownerRepo, branch string) string {
	const fallback = "1111111111111111111111111111111111111111"
	remotes := os.Getenv("WTG_ACCEPTANCE_REMOTES")
	if remotes == "" {
		return fallback
	}
	bare := filepath.Join(remotes, filepath.Base(ownerRepo)+".git")
	out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "refs/heads/"+branch).Output()
	if err != nil {
		return fallback
	}
	return strings.TrimSpace(string(out))
}

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

// getStatePath keeps one state file per repository. Like GitHub, PR numbers are
// counted per repository, and wtg calls gh for several repositories in
// parallel: a shared file would make the numbers depend on scheduling and lose
// updates.
func getStatePath(repo string) string {
	stateDir := os.Getenv("FAKE_GH_STATE_DIR")
	if stateDir == "" {
		stateDir = os.Getenv("HOME")
	}
	return filepath.Join(stateDir, ".fake_gh_state_"+strings.ReplaceAll(repo, "/", "_")+".json")
}

func loadState(repo string) GHState {
	p := getStatePath(repo)
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

func saveState(repo string, st GHState) {
	p := getStatePath(repo)
	data, err := json.MarshalIndent(st, "", "  ")
	if err == nil {
		_ = os.WriteFile(p, data, 0644)
	}
}

// archivedMarker is the file whose presence means the repository is archived.
func archivedMarker(repo string) string {
	stateDir := os.Getenv("FAKE_GH_STATE_DIR")
	if stateDir == "" {
		stateDir = os.Getenv("HOME")
	}
	return filepath.Join(stateDir, ".fake_gh_archived_"+strings.ReplaceAll(repo, "/", "_"))
}

func isArchived(repo string) bool {
	_, err := os.Stat(archivedMarker(repo))
	return err == nil
}

func setArchived(repo string, archived bool) {
	if archived {
		_ = os.WriteFile(archivedMarker(repo), nil, 0644)
		return
	}
	_ = os.Remove(archivedMarker(repo))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "fake gh: command required")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]
	st := loadState(flagVal(args, "--repo"))

	switch cmd {
	case "auth":
		if len(args) > 0 && args[0] == "status" {
			fmt.Println("✓ Logged in to github.com account jane (keyring)")
			return
		}

	case "repo":
		if len(args) < 2 {
			os.Exit(1)
		}
		sub, repo := args[0], args[1]
		switch sub {
		case "view":
			fmt.Printf("{\"isArchived\": %t}\n", isArchived(repo))
			return
		case "archive":
			setArchived(repo, true)
			fmt.Println("✓ Archived repository")
			return
		case "unarchive":
			setArchived(repo, false)
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
				HeadRefOid: headOid(repo, head),
				Body:       "",
				Repo:       repo,
			}
			saveState(repo, st)
			fmt.Println(url)
			return

		case "merge":
			// Only the PR's state changes. What a merge does to the base
			// branch is done with git by whoever sets the scene.
			numStr := ""
			if len(subArgs) > 0 && !strings.HasPrefix(subArgs[0], "--") {
				numStr = subArgs[0]
			}
			repo := flagVal(subArgs, "--repo")
			num, _ := strconv.Atoi(numStr)
			key := fmt.Sprintf("%s#%d", repo, num)
			pr, ok := st.PRs[key]
			if !ok {
				fmt.Fprintf(os.Stderr, "fake gh: no pull request #%d in %s\n", num, repo)
				os.Exit(1)
			}
			pr.State = "MERGED"
			st.PRs[key] = pr
			saveState(repo, st)
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
			if flagVal(subArgs, "--jq") == ".body" {
				fmt.Println(pr.Body)
				return
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
			saveState(repo, st)
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
