package git_sync

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type GitPushHandler struct {
	repoPath string
}

func NewGitPushHandler(repoPath string) *GitPushHandler {
	return &GitPushHandler{repoPath: repoPath}
}

func (h *GitPushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract branch and from commit from URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}
	branch := parts[2]
	//fromCommit := parts[3]

	// Read the uploaded bundle
	// Temp. dir root may be set with environment variable TMPDIR
	bundleFile, err := os.CreateTemp("", "git-bundle-*")
	if err != nil {
		http.Error(w, "Failed to create temporary file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(bundleFile.Name())

	// Copy the uploaded bundle to the temporary file
	_, err = io.Copy(bundleFile, r.Body)
	if err != nil {
		http.Error(w, "Failed to read bundle", http.StatusBadRequest)
		return
	}
	bundleFile.Close()

	// Verify the bundle
	verifyCmd := exec.Command("git", "-C", h.repoPath, "bundle", "verify", bundleFile.Name())
	if err := verifyCmd.Run(); err != nil {
		http.Error(w, "Invalid git bundle", http.StatusBadRequest)
		return
	}

	// Fetch the bundle
	fetchCmd := exec.Command("git", "-C", h.repoPath, "fetch", bundleFile.Name(), branch)
	if err := fetchCmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch bundle: %v", err), http.StatusInternalServerError)
		return
	}

	// Merge the fetched commits
	mergeCmd := exec.Command("git", "-C", h.repoPath, "merge", "--ff-only", "FETCH_HEAD")
	if err := mergeCmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to merge bundle: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Bundle successfully pushed"))
}
