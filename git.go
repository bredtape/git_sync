package git_sync

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/pkg/errors"
)

const (
	remoteName      = "origin"
	afterTimeFormat = "2006-01-02T15:04:05Z"
)

var (
	ErrAuthFailed = errors.New("authentication failed")
)

type GIT struct {
	workDir, tempDir string
	remoteRepo       RemoteRepo
}

func NewGIT(tempDir string, remoteRepo RemoteRepo) (*GIT, error) {
	if tempDir == "" {
		return nil, errors.New("tempDir not set")
	}
	if remoteRepo.URL == "" {
		return nil, errors.New("remoteRepo.URL not set")
	}
	if remoteRepo.Branch == "" {
		return nil, errors.New("branch not set")
	}
	if remoteRepo.Token == "" {
		return nil, errors.New("remoteRepo.Token not set")
	}

	return &GIT{
		workDir:    getWorkDir(tempDir, remoteRepo.URL, remoteRepo.Branch),
		tempDir:    tempDir,
		remoteRepo: remoteRepo}, nil
}

func (g GIT) ExistsLocal() (bool, error) {
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
// Returns nil worktree if remote does not exist
func (g *GIT) SyncRepoToLocalTemp() (*git.Worktree, error) {
	exists, err := g.ExistsLocal()
	if err != nil {
		return nil, err
	}

	if exists {
		return g.pullRepoToLocalTemp()
	}
	return g.cloneRepoToLocalTemp()
}

func (g *GIT) cloneRepoToLocalTemp() (*git.Worktree, error) {
	local, err := git.PlainClone(g.workDir, false, &git.CloneOptions{
		RemoteName:    remoteName,
		URL:           g.remoteRepo.URL,
		ReferenceName: plumbing.NewBranchReferenceName(g.remoteRepo.Branch),
		SingleBranch:  true,
		Auth:          g.getAuth()})
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return g.initLocal()
		}
		if errors.Is(err, transport.ErrRepositoryNotFound) {
			return nil, nil
		}
		if errors.Is(err, transport.ErrAuthenticationRequired) {
			return nil, ErrAuthFailed
		}
		slog.Warn("error type", "type", fmt.Sprintf("%T", err))
		return nil, errors.Wrapf(err, "failed to clone repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}

	return local.Worktree()
}

func (g *GIT) hasLocalBranch() (bool, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return false, err
	}

	b, err := localRepo.Branch(g.remoteRepo.Branch)
	if err != nil {
		return false, nil
	}
	return b != nil, nil
}

func (g *GIT) hasLocalCommits() (bool, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return false, err
	}

	ref, err := localRepo.Reference(plumbing.NewBranchReferenceName(g.remoteRepo.Branch), true)
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

func (g *GIT) initLocal() (*git.Worktree, error) {
	repo, err := git.PlainInit(g.workDir, false)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to init repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: remoteName,
		URLs: []string{g.remoteRepo.URL},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to register remote repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}

	branchRefName := plumbing.NewBranchReferenceName(g.remoteRepo.Branch)

	err = repo.CreateBranch(&config.Branch{
		Name:   g.remoteRepo.Branch,
		Remote: remoteName,
		Merge:  branchRefName})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create branch '%s' for repository %s", g.remoteRepo.Branch, g.remoteRepo.URL)
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

func (g *GIT) pullRepoToLocalTemp() (*git.Worktree, error) {
	w, err := g.getWorktree()
	if err != nil {
		return nil, err
	}

	err = w.Pull(&git.PullOptions{
		RemoteName:    remoteName,
		ReferenceName: plumbing.NewBranchReferenceName(g.remoteRepo.Branch),
		SingleBranch:  true,
		RemoteURL:     g.remoteRepo.URL,
		Auth:          g.getAuth()})

	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return w, nil
		}
		if errors.Is(err, transport.ErrAuthorizationFailed) {
			return nil, ErrAuthFailed
		}
		return nil, errors.Wrapf(err, "failed to pull repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}
	return w, nil
}

func (g *GIT) PushLocalToRemote() error {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return err
	}

	err = localRepo.Push(&git.PushOptions{
		RemoteName: remoteName,
		RemoteURL:  g.remoteRepo.URL,
		Auth:       g.getAuth()})

	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil
		}
		if errors.Is(err, transport.ErrAuthorizationFailed) || errors.Is(err, transport.ErrAuthenticationRequired) {
			return ErrAuthFailed
		}
		return errors.Wrapf(err, "failed to push local repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}
	return nil

}

// apply bundle to local repo with "git fetch"
func (g *GIT) ApplyBundleToLocal(r io.Reader) error {
	// "git pull" requires that the bundle is stored on disk
	dir, err := g.getRandomTempDir()
	if err != nil {
		return errors.Wrap(err, "failed to create temp dir")
	}
	defer os.RemoveAll(dir)
	tmpFile := filepath.Join(dir, "bundle")
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, "failed to create temp file for bundle")
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	if err != nil {
		return errors.Wrap(err, "failed to write bundle to temp file")
	}

	cmd := exec.Command("git", "-C", g.workDir, "pull", tmpFile, g.remoteRepo.Branch)
	cmd.Stdin = r
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdout := &bytes.Buffer{}
	cmd.Stdout = stdout

	err = cmd.Run()
	if err != nil {
		return &CommandError{
			Message:  fmt.Sprintf("failed to apply bundle for repository %s and branch %s", g.remoteRepo.URL, g.remoteRepo.Branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}

	return nil
}

type BundleOptions struct {
	// since, is the lookback duration for the bundle. Optional.
	Since time.Duration

	// after timestamp, optional
	After time.Time
}

func (opt BundleOptions) HasAny() bool {
	return opt.Since != 0 || !opt.After.IsZero()
}

func (g *GIT) CreateBundleFromLocal(opt BundleOptions) ([]byte, error) {
	log := slog.With("op", "CreateBundleFromLocal", "repo.url", g.remoteRepo.URL, "repo.branch", g.remoteRepo.Branch)
	cmd := exec.Command("git", "-C", g.workDir, "bundle", "create", "-", g.remoteRepo.Branch)
	if opt.Since != 0 {
		cmd = exec.Command("git", "-C", g.workDir, "bundle", "create", "-", fmt.Sprintf("--since=%d.seconds.ago", int64(opt.Since.Seconds())), g.remoteRepo.Branch)
	} else if !opt.After.IsZero() {
		cmd = exec.Command("git", "-C", g.workDir, "bundle", "create", "-", fmt.Sprintf("--after=%s", opt.After.Format(afterTimeFormat)), g.remoteRepo.Branch)
	}
	log.Debug("running command", "cmd", cmd.String())

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{
			Message:  fmt.Sprintf("failed to bundle repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}
	return stdout.Bytes(), nil
}

type BundleInfo struct {
	IsComplete    bool
	ContainsRef   string
	RequiresRef   string
	HashAlgorithm string
	IsOkay        bool
}

func (b BundleInfo) Validate() error {
	if !b.IsOkay {
		return errors.New("bundle is not okay")
	}
	if b.ContainsRef == "" {
		return errors.New("bundle does not contain ref")
	}
	if !b.IsComplete && b.RequiresRef != "" {
		return errors.New("bundle does not specify required ref, but is partial")
	}
	if b.HashAlgorithm == "" {
		return errors.New("bundle does not specify hash algorithm")
	}
	return nil
}

func ParseBundleVerifyOutput(output string) BundleInfo {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var bundle BundleInfo

	for scanner.Scan() {
		line := scanner.Text()

		// Check for complete history
		if line == "The bundle records a complete history." {
			bundle.IsComplete = true
		}

		// Check for hash algorithm
		const hashPrefix = "The bundle uses this hash algorithm: "
		if strings.HasPrefix(line, hashPrefix) {
			bundle.HashAlgorithm = strings.TrimPrefix(line, hashPrefix)
		}

		// Check for contained ref
		if strings.HasPrefix(line, "The bundle contains this ref:") {
			scanner.Scan() // Move to next line which contains the ref
			bundle.ContainsRef = scanner.Text()
		} else if strings.HasPrefix(line, "The bundle requires this ref:") {
			scanner.Scan() // Move to next line which contains the ref
			bundle.RequiresRef = scanner.Text()
		} else if strings.HasPrefix(line, "The bundle records a complete history.") {
			bundle.IsComplete = true
		} else if strings.HasSuffix(line, " is okay") {
			// Ignore final verification message
			bundle.IsOkay = true
		}
	}

	return bundle
}

func (g *GIT) GetBundleInfo(bundleData []byte) (BundleInfo, error) {
	cmd := exec.Command("git", "bundle", "verify", "-")
	cmd.Stdin = bytes.NewReader(bundleData)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return BundleInfo{}, &CommandError{
			Message:  fmt.Sprintf("failed to verify bundle for repository %s and branch %s", g.remoteRepo.URL, g.remoteRepo.Branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}

	info := ParseBundleVerifyOutput(stdout.String())
	if ve := info.Validate(); ve != nil {
		return info, ve
	}
	return info, nil
}

type Head struct {
	CommitID string
	Ref      string
}

func ParseBundleListHeadsOutput(output string) ([]Head, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var heads []Head

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line in bundle list-heads output: %s", line)
		}
		heads = append(heads, Head{CommitID: parts[0], Ref: parts[1]})
	}
	return heads, nil
}

func (g *GIT) GetBundleListHeads(bundleData []byte) ([]Head, error) {
	cmd := exec.Command("git", "bundle", "list-heads", "-")
	cmd.Stdin = bytes.NewReader(bundleData)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{
			Message:  fmt.Sprintf("failed to verify bundle for repository %s and branch %s", g.remoteRepo.URL, g.remoteRepo.Branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}

	return ParseBundleListHeadsOutput(stdout.String())
}

func getWorkDir(tempDir, remoteURL, branch string) string {
	return filepath.Join(tempDir, base64.URLEncoding.EncodeToString([]byte(remoteURL+branch)))
}

// creates a random temp dir. Must be cleaned up by caller
func (g *GIT) getRandomTempDir() (string, error) {
	if g.tempDir == "" {
		return "", errors.New("tempDir not set")
	}
	dir := filepath.Join(g.tempDir, generateRandomString())
	return dir, os.Mkdir(dir, os.ModePerm)
}

func (g *GIT) getWorktree() (*git.Worktree, error) {
	localRepo, err := git.PlainOpen(g.workDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open local repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}

	w, err := localRepo.Worktree()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get worktree for local repository %s for branch %s", g.remoteRepo.URL, g.remoteRepo.Branch)
	}
	return w, nil
}

func (g *GIT) getAuth() http.AuthMethod {
	return &http.BasicAuth{
		Username: "not_used", // must not be empty
		Password: g.remoteRepo.Token}
}
