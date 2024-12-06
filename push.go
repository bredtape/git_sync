package git_sync

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

type GitPushHandler struct {
	repo    RemoteRepo
	tempDir string
}

func NewGitPushHandler(tempDir string, repo RemoteRepo) *GitPushHandler {
	return &GitPushHandler{tempDir: tempDir, repo: repo}
}

// TODO: Consider when to remove local repo. Which errors should trigger the removal?

func (h *GitPushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	metricOps.WithLabelValues("push").Inc()
	metricOpsError.WithLabelValues("push")

	success := h.push(w, r)
	if !success {
		metricOpsError.WithLabelValues("push").Inc()
	}
}

func (h *GitPushHandler) push(w http.ResponseWriter, r *http.Request) (success bool) {
	log := slog.With("repo.url", h.repo.URL, "op", "GitPushHandler.ServeHTTP")

	// Extract branch
	xs := mux.Vars(r)
	branch := xs["branch"]
	if branch == "" {
		log.Debug("Branch not specified")
		http.Error(w, "Branch not specified", http.StatusBadRequest)
		return
	}

	log = log.With("branch", branch)
	git := NewGIT(h.tempDir, h.repo, branch)

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
		log.Debug("remote repository does not exist")
		http.Error(w, fmt.Sprintf("remote repository (%s) does not exist", git.remoteRepo.URL), http.StatusNotFound)
		return
	}

	err = git.ApplyBundleToLocal(r.Body, branch)
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			log.Error("failed to apply bundle", "err", cmdErr, "message", cmdErr.Message, "stderr", cmdErr.StdErr)
			if strings.Contains(cmdErr.StdErr, "Repository lacks these prerequisite commits") {
				http.Error(w, "failed to apply bundle, some prerequisites are missing. You must provide a bundle that overlaps with commits in the remote repository", http.StatusConflict)
				return
			}
		}
		http.Error(w, fmt.Sprintf("failed to apply bundle: %v", err), http.StatusInternalServerError)
		return
	}

	err = git.PushLocalToRemote(branch)
	if err != nil {
		log.Error("failed to push local to remote", "err", err)
		http.Error(w, fmt.Sprintf("failed to apply bundle: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Bundle successfully pushed"))
	log.Debug("bundle pushed successfully")
	return success
}
