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

// tests assumes that integrationtest/gogs-dev is running

func createTestServerWithPullHandler(t *testing.T) (*http.Client, string) {
	h := NewGitPullHandler(t.TempDir())
	mux := mux.NewRouter()
	mux.Handle("/pull", h)
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		server.Close()
	})

	return server.Client(), server.URL + "/pull"
}

func TestPullRemoteRepoDoesNotExist(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}

	repo.URL += "_not"

	t.Logf("non-existing repo, cloneURL=%s, branch=%s", repo.URL, repo.Branch)

	client, serverURL := createTestServerWithPullHandler(t)

	req := createPullHTTPRequest(t, serverURL, repo, 0, time.Time{})
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

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, cloneURL=%s, branch=%s", repo.URL, repo.Branch)
	client, serverURL := createTestServerWithPullHandler(t)
	req := createPullHTTPRequest(t, serverURL, repo, 0, time.Time{})
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

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, cloneURL=%s, branch=%s", repo.URL, repo.Branch)

	{
		// add commits. Note that the TempDir returns a new directory each time
		tempDir := t.TempDir()
		t.Logf("using tempDir=%s", tempDir)
		g, err := NewGIT(tempDir, repo)
		if err != nil {
			t.Fatal(err)
		}
		worktree, err := g.initLocal()
		if err != nil {
			t.Fatal(err)
		}

		filename := filepath.Join(g.workDir, "example.txt")
		err = os.WriteFile(filename, []byte("hello world! "+generateRandomString()), 0644)
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

		err = g.PushLocalToRemote()
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("pushed commits to remote repository")
	}

	client, serverURL := createTestServerWithPullHandler(t)

	{
		req := createPullHTTPRequest(t, serverURL, repo, 0, time.Time{})
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

	{
		req := createPullHTTPRequest(t, serverURL, repo, time.Hour, time.Time{})
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

	// pull with 'since' parameter
	{
		t.Logf("sleeping for 2 seconds, because 'since' minimum value is 1s")
		time.Sleep(2 * time.Second)
		req := createPullHTTPRequest(t, serverURL, repo, time.Second, time.Time{})
		t.Logf("Requesting %s", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 204, got %d, body %s", resp.StatusCode, string(body))
		}
	}

	// pull with 'after' parameter
	{
		t.Logf("sleeping for 2 seconds, because 'after' minimum value is 1s")
		time.Sleep(2 * time.Second)
		req := createPullHTTPRequest(t, serverURL, repo, 0, time.Now().Add(-time.Second))
		t.Logf("Requesting %s", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 204, got %d, body %s", resp.StatusCode, string(body))
		}
	}

	// pull with incorrect token
	{
		repo.Token = "incorrect"
		req := createPullHTTPRequest(t, serverURL, repo, 0, time.Time{})
		t.Logf("pull with incorrect token %s. But this does not fail. Pull does not require auth here", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		expectedStatus := http.StatusOK
		if resp.StatusCode != expectedStatus {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status %d, got %d, body %s", expectedStatus, resp.StatusCode, string(body))
		}
	}
}

func createPullHTTPRequest(t *testing.T, serverURL string, repo RemoteRepo, since time.Duration, after time.Time) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, serverURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+repo.Token)
	q := req.URL.Query()
	q.Add("repository", repo.URL)
	q.Add("branch", repo.Branch)
	if since.Seconds() > 0 {
		q.Add("since", since.String())
	}
	if !after.IsZero() {
		q.Add("after", after.UTC().Format(time.RFC3339))
	}
	req.URL.RawQuery = q.Encode()
	return req
}
