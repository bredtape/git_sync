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

/*
To create a complete bundle:
# git bundle create full.bundle main
To create a partial bundle with the last n=1 commit:
# git bundle create last.bundle main~1..main
*/

func createTestServerWithPushHandler(t *testing.T, repo RemoteRepo, branch string) (*http.Client, string) {
	h := NewGitPushHandler(t.TempDir(), repo)
	mux := mux.NewRouter()
	mux.Handle("/push/{branch}", h)
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		server.Close()
	})

	return server.Client(), server.URL + "/push/" + branch
}

func TestPushFullBundleExistingRepo(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)

	branch := "main"
	client, serverURL := createTestServerWithPushHandler(t, repo, branch)

	// full bundle
	{
		req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewReader(testdata.FullBundle))
		if err != nil {
			t.Fatal(err)
		}

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
		req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewReader(testdata.LastBundle))
		if err != nil {
			t.Fatal(err)
		}

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

	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)

	branch := "main"
	client, serverURL := createTestServerWithPushHandler(t, repo, branch)

	req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewReader(testdata.LastBundle))
	if err != nil {
		t.Fatal(err)
	}

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
