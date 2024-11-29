package git_sync

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"time"
)

type git struct {
	repoURL, branch, workDir string
}

func newGIT(tempDir, remoteURL, branch string) *git {
	return &git{
		workDir: getWorkDir(tempDir, remoteURL, branch),
		repoURL: remoteURL,
		branch:  branch}
}

func (g git) ExistsLocal() bool {
	_, err := os.Stat(path.Join(g.workDir, ".git"))
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

// clones repo from remoteURL if not exists, otherwise pulls the latest changes
func (g *git) SyncRepoToLocalTemp() error {
	if !g.ExistsLocal() {
		return g.cloneRepoToLocalTemp()
	}

	// err := os.MkdirAll(g.workDir, 0755)
	// if err != nil {
	// 	return errors.Wrapf(err, "failed to create work directory '%s'", g.workDir)
	// }
	return g.pullRepoToLocalTemp()
}

func (g *git) pullRepoToLocalTemp() error {
	log := slog.With("op", "pull", "repo", g.repoURL, "branch", g.branch, "workDir", g.workDir)
	cmd := exec.Command("git", "-C", g.workDir, "pull", "origin", g.branch)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	log.Debug("executing git pull", "cmd", cmd.String())
	err := cmd.Run()
	if err != nil {
		log.Error("failed to pull repository", "err", err, "stderr", stderr.String(), "exitCode", cmd.ProcessState.ExitCode())
		return &CommandError{
			Message:  fmt.Sprintf("failed to pull repository %s for branch %s", g.repoURL, g.branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}
	return nil
}

func (g *git) cloneRepoToLocalTemp() error {
	log := slog.With("op", "clone", "repo", g.repoURL, "branch", g.branch, "workDir", g.workDir)
	cmd := exec.Command("git", "clone", g.repoURL, "-b", g.branch, g.workDir)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	log.Debug("executing git clone", "cmd", cmd.String())
	err := cmd.Run()
	if err != nil {
		log.Error("failed to clone repository", "err", err, "stderr", stderr.String(), "exitCode", cmd.ProcessState.ExitCode())
		return &CommandError{
			Message:  fmt.Sprintf("failed to clone repository %s for branch %s", g.repoURL, g.branch),
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

func (g *git) BundleLocal(opt BundleOptions) ([]byte, error) {
	cmd := exec.Command("git", "-C", g.workDir, "bundle", "create", "-", g.branch)
	if opt.Since != 0 {
		cmd = exec.Command("git", "-C", g.workDir, "bundle", "create", "-", fmt.Sprintf("--since=%d.seconds.ago", int64(opt.Since.Seconds())), g.branch)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{
			Message:  fmt.Sprintf("failed to bundle repository %s for branch %s", g.repoURL, g.branch),
			Err:      err,
			StdErr:   stderr.String(),
			ExitCode: cmd.ProcessState.ExitCode()}
	}
	return stdout.Bytes(), nil
}

func getWorkDir(tempDir, remoteURL, branch string) string {
	return path.Join(tempDir, base64.URLEncoding.EncodeToString([]byte(remoteURL+branch)))
}
