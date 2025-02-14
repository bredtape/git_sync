package git_sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricOps = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "git_sync_ops_total",
		Help: "Total number of git sync operations attempted"}, []string{"op", "repository_url"})

	metricOpsError = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "git_sync_ops_error_total",
		Help: "Total number of git sync operations attempted, that resulted in some error"}, []string{"op", "repository_url"})
)

type GitPullHandler struct {
	tempDir string
}

func NewGitPullHandler(tempDir string) *GitPullHandler {
	return &GitPullHandler{tempDir: tempDir}
}

func (h *GitPullHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	log := slog.With("op", "GitPullHandler.ServeHTTP")

	remoteRepo, err := extractArgs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log = log.With("repo.url", remoteRepo.URL, "repo.branch", remoteRepo.Branch)

	opt := BundleOptions{}
	sinceRaw := r.URL.Query().Get("since")
	if sinceRaw != "" {
		d, err := time.ParseDuration(sinceRaw)
		if err != nil {
			log.Error("invalid since duration", "err", err)
			http.Error(w, fmt.Sprintf("Invalid since duration '%s'", sinceRaw), http.StatusBadRequest)
			return
		}
		if d < time.Second {
			log.Error("since duration too short", "duration", d)
			http.Error(w, "Since duration must be at least 1 second", http.StatusBadRequest)
			return
		}

		opt.Since = d
		log = log.With("since", d)
	}

	afterRaw := r.URL.Query().Get("after")
	if afterRaw != "" {
		t, err := time.Parse(time.RFC3339, afterRaw)
		if err != nil {
			log.Error("invalid after time", "err", err)
			http.Error(w, fmt.Sprintf("Invalid after time '%s'", afterRaw), http.StatusBadRequest)
			return
		}
		if t.IsZero() {
			log.Error("zero after time")
			http.Error(w, "After time must be non-zero", http.StatusBadRequest)
			return
		}

		opt.After = t
		log = log.With("after", t)
	}

	metricOps.WithLabelValues("pull", remoteRepo.URL).Inc()
	mErr := metricOpsError.WithLabelValues("pull", remoteRepo.URL)

	success := h.pull(log, remoteRepo, opt, w)
	if !success {
		mErr.Inc()
	}
}

func (h *GitPullHandler) pull(log *slog.Logger, remoteRepo RemoteRepo, opt BundleOptions, w http.ResponseWriter) (success bool) {
	git, err := NewGIT(h.tempDir, remoteRepo)
	if err != nil {
		log.Error("failed to create git", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Clone to local
	worktree, err := git.SyncRepoToLocalTemp()
	if err != nil {
		log.Error("sync to local failed", "err", err)
		if errors.Is(err, ErrAuthFailed) {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("failed to sync repository: %v", err), http.StatusInternalServerError)
		return
	}

	if worktree == nil {
		log.Debug("remote repository does not exist")
		http.Error(w, "remote repository does not exist", http.StatusNotFound)
		return
	}

	exists, err := git.hasLocalBranch()
	if err != nil {
		log.Error("failed to check if branch exists", "err", err)
		http.Error(w, fmt.Sprintf("failed to check if branch exists: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		log.Debug("branch not found")
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("branch not found"))
		return
	}

	hasCommits, err := git.hasLocalCommits()
	if err != nil {
		log.Error("failed to check if branch has commits", "err", err)
		http.Error(w, fmt.Sprintf("failed to check if branch has commits: %v", err), http.StatusInternalServerError)
		return
	}

	if !hasCommits {
		log.Debug("no commits")
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("no commits"))
		return
	}

	bundleData, err := git.CreateBundleFromLocal(opt)
	if err != nil {
		if cmdErr, ok := err.(*CommandError); ok {
			if opt.HasAny() && strings.Contains(cmdErr.StdErr, "Refusing to create empty bundle") {
				log.Debug("no new commits since", "since", opt.Since)
				http.Error(w, fmt.Sprintf("no new commits since %v", time.Now().Add(-opt.Since)), http.StatusNoContent)
				return
			}
			log.Error("bundle failed", "err", cmdErr.Err, "stderr", cmdErr.StdErr)
		}
		http.Error(w, fmt.Sprintf("Failed to create bundle: %v", err), http.StatusInternalServerError)
		return
	}

	heads, err := git.GetBundleListHeads(bundleData)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get bundle info: %v", err), http.StatusInternalServerError)
		return
	}

	if len(heads) != 1 {
		http.Error(w, fmt.Sprintf("Expected exactly one head, got %v", heads), http.StatusInternalServerError)
		return
	}

	commitID := heads[0].CommitID
	w.Header().Set("X-Git-Head", commitID)
	w.Header().Set("X-Git-IsPartial", fmt.Sprintf("%t", opt.HasAny()))
	hash := createHash(heads[0], opt)
	w.Header().Set("X-Git-Hash", hash)

	// Write the bundle to the response
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=git_%s_%s.bundle", commitID, hash))
	w.Write(bundleData)
	log.Debug("bundle created")
	return true
}

func extractArgs(r *http.Request) (RemoteRepo, error) {
	args := RemoteRepo{
		URL:    r.URL.Query().Get("repository"),
		Branch: r.URL.Query().Get("branch")}
	if args.URL == "" {
		return args, errors.New("no 'repository' specified")
	}
	if args.Branch == "" {
		return args, errors.New("no 'branch' specified")
	}

	token, err := extractAuthToken(r)
	args.Token = token
	return args, err
}

func extractAuthToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("no Authorization header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", fmt.Errorf("invalid Authorization header")
	}

	return strings.TrimPrefix(authHeader, "Bearer "), nil
}

func createHash(head Head, opt BundleOptions) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", head.CommitID, opt.After, opt.Since)))
	return hex.EncodeToString(h[:])
}
