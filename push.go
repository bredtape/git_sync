package git_sync

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
)

type GitPushHandler struct {
	repo *gitCmds
}

func NewGitPushHandler(repo *gitCmds) *GitPushHandler {
	return &GitPushHandler{repo: repo}
}

// TODO: Consider when to remove local repo. Which errors should trigger the removal?

func (h *GitPushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := slog.With("repo", h.repo.remoteRepo, "op", "GitPushHandler.ServeHTTP")
	defer r.Body.Close()

	// Extract branch
	xs := mux.Vars(r)
	branch := xs["branch"]
	if branch == "" {
		http.Error(w, "Branch not specified", http.StatusBadRequest)
		return
	}

	// Clone to local
	if _, err := h.repo.SyncRepoToLocalTemp(); err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("sync to local failed", "err", cmdErr)
		}
		http.Error(w, fmt.Sprintf("failed to sync repository: %v", err), http.StatusInternalServerError)
		return
	}

	err := h.repo.ApplyBundleToLocal(r.Body, branch)
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("failed to apply bundle", "err", cmdErr, "message", cmdErr.Message)
		}
		http.Error(w, fmt.Sprintf("failed to apply bundle: %v", err), http.StatusInternalServerError)
		return
	}

	err = h.repo.PushLocalToRemote(branch)
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("failed to push local", "err", cmdErr, "message", cmdErr.Message)
		}
		http.Error(w, fmt.Sprintf("failed to apply bundle: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Bundle successfully pushed"))
}
