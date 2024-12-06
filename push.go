package git_sync

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type GitPushHandler struct {
	tempDir string
}

func NewGitPushHandler(tempDir string) *GitPushHandler {
	return &GitPushHandler{tempDir: tempDir}
}

// TODO: Consider when to remove local repo. Which errors should trigger the removal?

func (h *GitPushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	remoteRepo, err := extractArgs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log := slog.With("op", "GitPushHandler.ServeHTTP", "repo.url", remoteRepo.URL, "repo.branch", remoteRepo.Branch, "repo.token", remoteRepo.Token)

	metricOps.WithLabelValues("push", remoteRepo.URL).Inc()
	mErr := metricOpsError.WithLabelValues("push", remoteRepo.URL)

	success := h.push(log, remoteRepo, r.Body, w)
	if !success {
		mErr.Inc()
	}
}

func (h *GitPushHandler) push(log *slog.Logger, remoteRepo RemoteRepo, bundleData io.Reader, w http.ResponseWriter) (success bool) {
	git, err := NewGIT(h.tempDir, remoteRepo)
	if err != nil {
		log.Error("failed to create git", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
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
		log.Debug("remote repository does not exist")
		http.Error(w, fmt.Sprintf("remote repository (%s) does not exist", git.remoteRepo.URL), http.StatusNotFound)
		return
	}

	err = git.ApplyBundleToLocal(bundleData)
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

	err = git.PushLocalToRemote()
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
