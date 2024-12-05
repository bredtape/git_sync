package git_sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
)

const user = "sync"
const password = "computer"
const baseURL = "http://localhost:3000"

func TestCreateRepoAndPushSomeCommits(t *testing.T) {
	gogsAdmin := NewGogsAdmin(user, password, baseURL)
	repo, err := gogsAdmin.CreateRandomRepo()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created repository, name=%s, cloneURL=%s", repo.Name, repo.URL)
	branch := "main"
	g := NewGIT(t.TempDir(), repo, branch)
	worktree, err := g.SyncRepoToLocalTemp()
	if err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(g.workDir, "example.txt")
	err = os.WriteFile(filename, []byte("hello world!"), 0644)
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

	t.Log("Pushed to remote repository")
}
