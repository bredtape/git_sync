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
	tempDir string
	repoURL string
}

func NewGitPullHandler(tempDir, repoURL string) *GitPullHandler {
	return &GitPullHandler{tempDir: tempDir, repoURL: repoURL}
}

func (h *GitPullHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := slog.With("repo", h.repoURL, "op", "GitPullHandler.ServeHTTP")

	// Extract branch and from commit from URL
	xs := mux.Vars(r)
	branch := xs["branch"]

	if branch == "" {
		http.Error(w, "Branch not specified", http.StatusBadRequest)
		return
	}

	log = log.With("branch", branch)

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

	git := newGIT(h.tempDir, h.repoURL, branch)
	// Clone to local
	if err := git.SyncRepoToLocalTemp(); err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("sync to local failed", "err", cmdErr)
		}
		http.Error(w, fmt.Sprintf("Failed to sync repository: %v", err), http.StatusInternalServerError)
		return
	}

	bundleData, err := git.BundleLocal(opt)
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
