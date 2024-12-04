package git_sync

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/gorilla/mux"
)

func TestPullFullBundleEmptyRepo(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)

	h := NewGitPullHandler(newGIT(t.TempDir(), repo, "main"))
	mux := mux.NewRouter()
	mux.Handle("/pull/{branch}", h)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/pull/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Requesting %s", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 204, got status=%d, body='%s'", resp.StatusCode, string(body))
	}
}

func TestPullFullBundleRepoHasCommits(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)
	branch := "main"

	{
		// add commits. Note that the TempDir returns a new directory each time
		g := newGIT(t.TempDir(), repo, branch)
		worktree, err := g.initLocal()
		if err != nil {
			t.Fatal(err)
		}

		filename := filepath.Join(g.workDir, "example.txt")
		err = os.WriteFile(filename, []byte("hello world! "+repo.Name), 0644)
		if err != nil {
			t.Fatal(err)
		}

		_, err = worktree.Add("example.txt")
		if err != nil {
			t.Fatal(err)
		}

		_, err = worktree.Commit("Initial commit", &git.CommitOptions{})
		if err != nil {
			t.Fatal(err)
		}

		err = g.PushLocalToRemote(branch)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("pushed commits to remote repository")
	}

	h := NewGitPullHandler(newGIT(t.TempDir(), repo, branch))
	mux := mux.NewRouter()
	mux.Handle("/pull/{branch}", h)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/pull/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Requesting %s", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d, body %s", resp.StatusCode, string(body))
	}
}
