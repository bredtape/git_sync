package git_sync

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type GitPullHandler struct {
	repo    RemoteRepo
	tempDir string
}

func NewGitPullHandler(tempDir string, repo RemoteRepo) *GitPullHandler {
	return &GitPullHandler{tempDir: tempDir, repo: repo}
}

func (h *GitPullHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := slog.With("repo.url", h.repo.URL, "op", "GitPullHandler.ServeHTTP")

	// Extract branch
	xs := mux.Vars(r)
	branch := xs["branch"]
	if branch == "" {
		http.Error(w, "Branch not specified", http.StatusBadRequest)
		return
	}

	log = log.With("branch", branch)
	git := NewGIT(h.tempDir, h.repo, branch)

	opt := BundleOptions{}
	sinceRaw := r.URL.Query().Get("since")
	if sinceRaw != "" {
		d, err := time.ParseDuration(sinceRaw)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid since duration '%s'", sinceRaw), http.StatusBadRequest)
			return
		}
		if d < time.Second {
			http.Error(w, "Since duration must be at least 1 second", http.StatusBadRequest)
			return
		}

		opt.Since = d
		log = log.With("since", d)
	}

	// Clone to local
	worktree, err := git.SyncRepoToLocalTemp()
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("sync to local failed", "err", cmdErr)
		}
		http.Error(w, fmt.Sprintf("failed to sync repository: %v", err), http.StatusInternalServerError)
		return
	}

	if worktree == nil {
		http.Error(w, "remote repository does not exist", http.StatusNotFound)
		return
	}

	exists, err := git.hasLocalBranch()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to check if branch exists: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("branch not found"))
		return
	}

	hasCommits, err := git.hasLocalCommits()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to check if branch has commits: %v", err), http.StatusInternalServerError)
		return
	}

	if !hasCommits {
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("no commits"))
		return
	}

	bundleData, err := git.CreateBundleFromLocal(opt)
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			if opt.Since != 0 && strings.Contains(cmdErr.StdErr, "Refusing to create empty bundle") {
				http.Error(w, fmt.Sprintf("no new commits since %v", time.Now().Add(-opt.Since)), http.StatusNoContent)
				return
			}
			log.Error("bundle failed", "err", cmdErr.Err, "stderr", cmdErr.StdErr)
		}
		http.Error(w, fmt.Sprintf("Failed to create bundle: %v", err), http.StatusInternalServerError)
		return
	}

	// Write the bundle to the response
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=git-bundle")
	w.Write(bundleData)
}
