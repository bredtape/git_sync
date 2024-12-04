package git_sync

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/pkg/errors"
)

const remoteName = "origin"

type gitCmds struct {
	branch, workDir string
	remoteRepo      RemoteRepo
}

func newGIT(tempDir string, remoteRepo RemoteRepo, branch string) *gitCmds {
	return &gitCmds{
		workDir:    getWorkDir(tempDir, remoteRepo.Name, branch),
		remoteRepo: remoteRepo,
		branch:     branch}
}

func (g gitCmds) ExistsLocal() (bool, error) {
	_, err := git.PlainOpen(g.workDir)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return false, nil
		}
		return false, errors.Wrapf(err, "failed to determine if local repo exists at %s", g.workDir)
	}
	return true, nil

}

// clones repo from remoteURL if not exists, otherwise pulls the latest changes
func (g *gitCmds) SyncRepoToLocalTemp() (*git.Worktree, error) {
	exists, err := g.ExistsLocal()
	if err != nil {
		return nil, err
	}

	if exists {
		return g.pullRepoToLocalTemp()
	}
	return g.cloneRepoToLocalTemp()
}

func (g *gitCmds) cloneRepoToLocalTemp() (*git.Worktree, error) {
	local, err := git.PlainClone(g.workDir, false, &git.CloneOptions{
		RemoteName:    remoteName,
		URL:           g.remoteRepo.URL,
		ReferenceName: plumbing.NewBranchReferenceName(g.branch),
		SingleBranch:  true,
		Auth:          g.getAuth()})
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return g.initLocal()
		}
		return nil, errors.Wrapf(err, "failed to clone repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}

	return local.Worktree()
}

func (g *gitCmds) hasLocalBranch() (bool, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return false, err
	}

	b, err := localRepo.Branch(g.branch)
	if err != nil {
		return false, nil
	}
	return b != nil, nil
}

func (g *gitCmds) hasLocalCommits() (bool, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return false, err
	}

	ref, err := localRepo.Reference(plumbing.NewBranchReferenceName(g.branch), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return false, nil
		}
		return false, err
	}
	if ref == nil {
		return false, nil
	}
	commit, err := localRepo.CommitObject(ref.Hash())
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return false, nil
		}
		return false, err
	}

	return commit != nil, nil
}

func (g *gitCmds) initLocal() (*git.Worktree, error) {
	repo, err := git.PlainInit(g.workDir, false)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to init repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: remoteName,
		URLs: []string{g.remoteRepo.URL},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to register remote repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}

	branchRefName := plumbing.NewBranchReferenceName(g.branch)

	err = repo.CreateBranch(&config.Branch{
		Name:   g.branch,
		Remote: remoteName,
		Merge:  branchRefName})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create branch '%s' for repository %s", g.branch, g.remoteRepo.URL)
	}

	// here I swapped HEAD and the branch name
	symRef := plumbing.NewSymbolicReference(plumbing.ReferenceName("HEAD"), branchRefName)
	err = repo.Storer.SetReference(symRef)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to set HEAD for repository %s", g.remoteRepo.URL)
	}

	// printing the references to make sure it's there
	// it should look like this: "ref: refs/heads/test-orphan-branch HEAD"
	refs, _ := repo.Storer.IterReferences()
	refs.ForEach(func(ref *plumbing.Reference) error {
		fmt.Println(ref)
		return nil
	})

	// headRef, err := repo.Head()
	// if err != nil {
	// 	if errors.Is(err, plumbing.ErrReferenceNotFound) {
	// 		headRef = plumbing.NewHashReference(branchRefName, plumbing.ZeroHash)
	// 		// fall through
	// 	} else {
	// 		return nil, errors.Wrapf(err, "failed to get head for repository %s", g.remoteRepo.URL)
	// 	}
	// }
	// log = log.With("head", headRef.Hash())

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get worktree for repository %s", g.remoteRepo.URL)
	}

	// log.Debug("git checkout branch")
	// err = worktree.Checkout(&git.CheckoutOptions{
	// 	Hash:   headRef.Hash(),
	// 	Branch: branchRefName,
	// 	Force:  true,
	// 	Create: true})
	// if err != nil {
	// 	return nil, errors.Wrapf(err, "failed to checkout branch '%s' for repository %s", g.branch, g.remoteRepo.URL)
	// }

	return worktree, nil
}

func (g *gitCmds) pullRepoToLocalTemp() (*git.Worktree, error) {
	w, err := g.getWorktree()
	if err != nil {
		return nil, err
	}

	err = w.Pull(&git.PullOptions{
		RemoteName:    remoteName,
		ReferenceName: plumbing.NewBranchReferenceName(g.branch),
		SingleBranch:  true,
		RemoteURL:     g.remoteRepo.URL,
		Auth:          g.getAuth()})

	if err != nil {
		return nil, errors.Wrapf(err, "failed to pull repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}
	return w, nil
}

func (g *gitCmds) PushLocalToRemote(branch string) error {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return err
	}

	err = localRepo.Push(&git.PushOptions{
		RemoteName: remoteName,
		RemoteURL:  g.remoteRepo.URL,
		Auth:       g.getAuth()})

	if err != nil {
		return errors.Wrapf(err, "failed to push local repository %s for branch %s", g.remoteRepo.URL, branch)
	}
	return nil

}

// apply bundle to local repo with "git fetch"
func (g *gitCmds) ApplyBundleToLocal(r io.Reader, branch string) error {
	cmd := exec.Command("git", "-C", g.workDir, "fetch", "/dev/stdin", fmt.Sprintf("%s:%s", branch, branch))
	cmd.Stdin = r
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdout := &bytes.Buffer{}
	cmd.Stdout = stdout

	err := cmd.Run()
	if err != nil {
		return &CommandError{
			Message:  fmt.Sprintf("failed to apply bundle for repository %s and branch %s", g.remoteRepo.URL, branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}

	return nil
}

type BundleOptions struct {
	// since, is the lookback duration for the bundle. Optional.
	Since time.Duration
}

func (g *gitCmds) CreateBundleFromLocal(opt BundleOptions) ([]byte, error) {
	log := slog.With("op", "CreateBundleFromLocal", "repo", g.remoteRepo, "branch", g.branch)
	cmd := exec.Command("git", "-C", g.workDir, "bundle", "create", "-", g.branch)
	if opt.Since != 0 {
		cmd = exec.Command("git", "-C", g.workDir, "bundle", "create", "-", fmt.Sprintf("--since=%d.seconds.ago", int64(opt.Since.Seconds())), g.branch)
	}
	log.Debug("running command", "cmd", cmd.String())

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{
			Message:  fmt.Sprintf("failed to bundle repository %s for branch %s", g.remoteRepo.URL, g.branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}
	return stdout.Bytes(), nil
}

func getWorkDir(tempDir, remoteURL, branch string) string {
	return path.Join(tempDir, base64.URLEncoding.EncodeToString([]byte(remoteURL+branch)))
}

func (g *gitCmds) getWorktree() (*git.Worktree, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open local repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}

	w, err := localRepo.Worktree()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get worktree for local repository %s for branch %s", g.remoteRepo.URL, g.branch)
	}
	return w, nil
}

func (g *gitCmds) getAuth() http.AuthMethod {
	return &http.BasicAuth{
		Username: "not_used", // must not be empty
		Password: g.remoteRepo.Token}
}
