package sources_test

import (
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/cashapp/hermit/errors"
	"github.com/cashapp/hermit/redact"
	"github.com/cashapp/hermit/sources"
	"github.com/cashapp/hermit/ui"
	"github.com/cashapp/hermit/util"
)

type FailingGit struct {
	err error
}

func (f *FailingGit) RunInDir(_ *ui.Task, _ string, _ ...redact.Value) error {
	return f.err
}

func (f *FailingGit) CaptureInDir(_ ui.Logger, _ string, _ ...redact.Value) ([]byte, error) {
	return nil, f.err
}

type CapturingGit struct {
	commands [][]string
}

func (c *CapturingGit) RunInDir(_ *ui.Task, _ string, args ...redact.Value) error {
	c.commands = append(c.commands, redact.Reveal(args))
	return nil
}

func (c *CapturingGit) CaptureInDir(_ ui.Logger, _ string, args ...redact.Value) ([]byte, error) {
	c.commands = append(c.commands, redact.Reveal(args))
	return nil, nil
}

// sourceDirs returns the names of real source directories in dir, ignoring
// Hermit's lock files and sync scratch directories. Source directory names
// are bare hex SHA256 hashes (see util.Hash); every scratch/lock entry
// contains a ".", so this distinction is unambiguous.
func sourceDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	var dirs []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, entry.Name())
	}
	return dirs
}

func TestGitDoesNotRemoveSourceAfterSyncFailure(t *testing.T) {
	git := &FailingGit{}
	sourceDir := t.TempDir()
	source := sources.NewGitSource(redact.URL("git://test"), sourceDir, git)

	// Create the initial directory for sources by successfully syncing
	u, _ := ui.NewForTesting()
	_, err := source.Sync(u, true)
	assert.NoError(t, err)
	dirs := sourceDirs(t, sourceDir)
	assert.Equal(t, len(dirs), 1)
	gitDir := dirs[0]

	// Fail the sync
	git.err = errors.New("failing git fails")
	_, err = source.Sync(u, true)

	// no error as it was not an initial clone
	assert.NoError(t, err)

	// the directory should still be in place after git failed to update
	dirs = sourceDirs(t, sourceDir)
	assert.Equal(t, len(dirs), 1)
	assert.Equal(t, gitDir, dirs[0])
}

func TestGitSyncUsesCredentialsButDisplaysRedactedURI(t *testing.T) {
	git := &CapturingGit{}
	source := sources.NewGitSource(redact.URL("https://x-access-token:sekret@github.com/owner/repo.git"), t.TempDir(), git)
	assert.Equal(t, "https://github.com/owner/repo.git", source.URI())

	u, buf := ui.NewForTesting()
	_, err := source.Sync(u, true)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(git.commands))
	assert.Contains(t, strings.Join(git.commands[0], " "), "https://x-access-token:sekret@github.com/owner/repo.git")
	assert.NotContains(t, buf.String(), "sekret")
}

func TestGitSyncDoesNotLeakURLCredentials(t *testing.T) {
	u, buf := ui.NewForTesting()
	source := sources.NewGitSource(redact.URL("https://x-access-token:sekret@127.0.0.1:1/owner/repo.git"), t.TempDir(), &util.RealCommandRunner{})

	_, err := source.Sync(u, true)
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "sekret")
	assert.Contains(t, err.Error(), "https://127.0.0.1:1/owner/repo.git")
	assert.NotContains(t, buf.String(), "sekret")
}
