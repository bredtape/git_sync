package git_sync

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/gorilla/mux"
)

func createTestServerWithPullHandler(t *testing.T, repo RemoteRepo, branch string) (*http.Client, string) {
	h := NewGitPullHandler(t.TempDir(), repo)
	mux := mux.NewRouter()
	mux.Handle("/pull/{branch}", h)
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		server.Close()
	})

	return server.Client(), server.URL + "/pull/" + branch
}

func TestPullRemoteRepoDoesNotExist(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	repo.URL += "_not"
	repo.Name += "_not"

	t.Logf("non-existing repo, name=%s, cloneURL=%s", repo.Name, repo.URL)

	branch := "main"
	client, serverURL := createTestServerWithPullHandler(t, repo, branch)

	req, err := http.NewRequest(http.MethodGet, serverURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Requesting %s", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	expectedStatus := http.StatusNotFound
	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got status=%d, body='%s'", expectedStatus, resp.StatusCode, string(body))
	}
}

func TestPullFullBundleEmptyRepo(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)

	branch := "main"
	client, serverURL := createTestServerWithPullHandler(t, repo, branch)

	req, err := http.NewRequest(http.MethodGet, serverURL, nil)
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
		tempDir := t.TempDir()
		t.Logf("using tempDir=%s", tempDir)
		g := NewGIT(tempDir, repo, branch)
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

	client, serverURL := createTestServerWithPullHandler(t, repo, branch)

	req, err := http.NewRequest(http.MethodGet, serverURL, nil)
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

	// pull with 'since' parameter
	req, err = http.NewRequest(http.MethodGet, serverURL+"?since=1h", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Requesting %s", req.URL.String())

	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d, body %s", resp.StatusCode, string(body))
	}

	// pull with 'since' parameter
	t.Logf("sleeping for 2 seconds, because 'since' minimum value is 1s")
	time.Sleep(2 * time.Second)
	req, err = http.NewRequest(http.MethodGet, serverURL+"?since=1s", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Requesting %s", req.URL.String())

	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 204, got %d, body %s", resp.StatusCode, string(body))
	}
}
