package git_sync

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bredtape/git_sync/testdata"
	"github.com/gorilla/mux"
)

// tests assumes that integrationtest/gogs-dev is running

/*
To create a complete bundle:
# git bundle create full.bundle main
To create a partial bundle with the last n=1 commit:
# git bundle create last.bundle main~1..main
*/

func createTestServerWithPushHandler(t *testing.T) (*http.Client, string) {
	h := NewGitPushHandler(t.TempDir())
	mux := mux.NewRouter()
	mux.Handle("/push", h)
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		server.Close()
	})

	return server.Client(), server.URL + "/push"
}

func TestPushFullBundleExistingRepo(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, cloneURL=%s, branch=%s", repo.URL, repo.Branch)

	client, serverURL := createTestServerWithPushHandler(t)

	// full bundle
	{
		req := createPushHTTPRequest(t, serverURL, repo, testdata.FullBundle)
		t.Logf("pushing full bundle to %s", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		expectedStatus := http.StatusOK
		if resp.StatusCode != expectedStatus {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status %d, got %d, body: %s", expectedStatus, resp.StatusCode, string(body))
		}
	}

	{
		req := createPushHTTPRequest(t, serverURL, repo, testdata.LastBundle)
		t.Logf("pushing partial bundle (that already should have been pushed) to %s", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		expectedStatus := http.StatusOK
		if resp.StatusCode != expectedStatus {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status %d, got %d, body: %s", expectedStatus, resp.StatusCode, string(body))
		}
	}
}

func TestPushPartialBundleMissingHistoryToExistingRepo(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, cloneURL=%s, branch=%s", repo.URL, repo.Branch)

	client, serverURL := createTestServerWithPushHandler(t)
	req := createPushHTTPRequest(t, serverURL, repo, testdata.LastBundle)
	t.Logf("pushing partial bundle (that already should have been pushed) to %s", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	expectedStatus := http.StatusConflict
	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d, body: %s", expectedStatus, resp.StatusCode, string(body))
	}
}

func TestPushFullBundleExistingRepoTokenIncorrect(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	branch := "main"
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo(branch)
	if err != nil {
		t.Fatal(err)
	}
	repo.Token = "incorrect"

	t.Logf("Created repository, cloneURL=%s, branch=%s", repo.URL, repo.Branch)

	client, serverURL := createTestServerWithPushHandler(t)

	// full bundle
	{
		req := createPushHTTPRequest(t, serverURL, repo, testdata.FullBundle)
		t.Logf("pushing full bundle to %s", req.URL.String())

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		expectedStatus := http.StatusUnauthorized
		if resp.StatusCode != expectedStatus {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status %d, got %d, body: %s", expectedStatus, resp.StatusCode, string(body))
		}
	}
}

func createPushHTTPRequest(t *testing.T, serverURL string, repo RemoteRepo, bundleData []byte) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewReader(bundleData))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+repo.Token)
	q := req.URL.Query()
	q.Add("repository", repo.URL)
	q.Add("branch", repo.Branch)
	req.URL.RawQuery = q.Encode()
	return req
}
