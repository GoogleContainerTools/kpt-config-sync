// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gitproviders

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	// MainBranch is static as behavior when switching branches is never under
	// test.
	MainBranch = "main"

	// GitKeepFileName is a conventional name for an empty file you add to
	// directories in git when you want to make sure the directory is retained even
	// when all the other files are deleted.
	// Without this file, the directory may remain locally, but won't exist in git.
	GitKeepFileName = ".gitkeep"

	// fileMode is the file mode to use for all local operations.
	//
	// Go is inconsistent about which user/group it runs commands under, so
	// anything less will either:
	// 1) Make git operations not work as expected, or
	// 2) Cause ssh-keygen to fail.
	fileMode = os.ModePerm

	// DefaultSyncDir is created in new repositories by default.
	DefaultSyncDir = "acme"
)

// Repository is a simple interface shared by both read-only and read-write Git
// repositories.
type Repository interface {
	// SyncURL returns the git repository URL for Config Sync to sync from.
	SyncURL() string
}

// ReadOnlyRepository is a remote Git repository to fetch & sync.
type ReadOnlyRepository struct {
	// URL is the URL to the Git repository to fetch & sync.
	URL string
}

// SyncURL returns the git repository URL for Config Sync to sync from.
func (r ReadOnlyRepository) SyncURL() string {
	return r.URL
}

// Ensure GitRepository interface is satisfied
var _ Repository = ReadOnlyRepository{}
var _ Repository = &ReadWriteRepository{}

// ReadWriteRepository is a local clone of a remote git repository, that you
// have permission to manage.
//
// We shell out for git commands as the git libraries are difficult to configure
// ssh for, and git-server requires ssh authentication.
type ReadWriteRepository struct {
	// Name of the repository.
	// <NAMESPACE>/<NAME> of the RootSync|RepoSync.
	Name string
	// Root is the location on the machine running the test at which the local
	// repository is stored.
	Root string
	// Format is the source format for parsing the repository (hierarchy or
	// unstructured).
	Format configsync.SourceFormat
	// PrivateKeyPath is the local path to the private key on disk to use to
	// authenticate with the git server.
	PrivateKeyPath string
	// SyncKind refers to the kind of the RSync using the repository: RootSync or RepoSync.
	SyncKind string
	// SafetyNSPath is the path to the safety namespace yaml file.
	SafetyNSPath string
	// SafetyNSName is the name of the safety namespace.
	SafetyNSName string
	// SafetyClusterRolePath is the path to the safety namespace yaml file.
	SafetyClusterRolePath string
	// SafetyClusterRoleName is the name of the safety namespace.
	SafetyClusterRoleName string
	// RemoteRepoName is the name of the remote repository.
	// It is the same as Name for the testing git-server.
	// For other git providers, it appends a UUID to Name for uniqueness.
	RemoteRepoName string
	// GitProvider is the provider that hosts the Git repositories.
	GitProvider GitProvider
	// Scheme used for encoding and decoding objects.
	Scheme *runtime.Scheme
	// Logger for methods to use.
	Logger *testlogger.TestLogger

	defaultWaitTimeout time.Duration
	// isEmpty indicates whether the local repo has any commits
	isEmpty bool
}

// NewRepository creates a remote repo on the git provider.
// Locally, it writes the repository to `tmpdir`/repos/`name`.
// Name is the <NAMESPACE>/<NAME> of the RootSync|RepoSync.
func NewRepository(
	syncKind string,
	syncNN types.NamespacedName,
	sourceFormat configsync.SourceFormat,
	scheme *runtime.Scheme,
	logger *testlogger.TestLogger,
	provider GitProvider,
	tmpDir string,
	privateKeyPath string,
	defaultWaitTimeout time.Duration,
) *ReadWriteRepository {
	namespacedName := syncNN.String()
	safetyName := fmt.Sprintf("safety-%s", strings.ReplaceAll(namespacedName, "/", "-"))
	return &ReadWriteRepository{
		Name:                  namespacedName,
		Root:                  filepath.Join(tmpDir, "repos", namespacedName),
		Format:                sourceFormat,
		SyncKind:              syncKind,
		SafetyNSName:          safetyName,
		SafetyNSPath:          fmt.Sprintf("acme/namespaces/%s/ns.yaml", safetyName),
		SafetyClusterRoleName: safetyName,
		SafetyClusterRolePath: fmt.Sprintf("acme/cluster/cluster-role-%s.yaml", safetyName),
		Scheme:                scheme,
		Logger:                logger,
		GitProvider:           provider,
		PrivateKeyPath:        privateKeyPath,
		defaultWaitTimeout:    defaultWaitTimeout,
		isEmpty:               true,
	}
}

// Create the remote repository using the GitProvider, and
// create the local repository with an initial commit.
func (g *ReadWriteRepository) Create() error {
	repoName, err := g.GitProvider.CreateRepository(g.Name)
	if err != nil {
		return fmt.Errorf("creating repo: %s: %w", g.Name, err)
	}
	g.RemoteRepoName = repoName
	return nil
}

// Git executes the command from the repo root.
// The command is always logged to the debug log.
// If the command errors, the command and output is logged.
func (g *ReadWriteRepository) Git(command ...string) ([]byte, error) {
	// The -C flag executes git from repository root.
	// https://git-scm.com/docs/git#Documentation/git.txt--Cltpathgt
	args := []string{"git", "-C", g.Root}
	args = append(args, command...)
	g.Logger.Debugf("[repo %s] %s", path.Base(g.Root), strings.Join(args, " "))
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if !g.Logger.IsDebugEnabled() {
			g.Logger.Infof("[repo %s] %s", path.Base(g.Root), strings.Join(args, " "))
		}
		g.Logger.Info(string(out))
		return out, err
	}
	return out, nil
}

// BulkGit executes a list of git commands sequentially.
// If any command errors, execution is halted and the error is logged and returned.
func (g *ReadWriteRepository) BulkGit(cmds ...[]string) error {
	for _, cmd := range cmds {
		if _, err := g.Git(cmd...); err != nil {
			return err
		}
	}
	return nil
}

// InitialCommit initializes the Nomos repo with the Repo object.
func (g *ReadWriteRepository) InitialCommit(sourceFormat configsync.SourceFormat) error {
	// Add .gitkeep to retain dir when empty, otherwise configsync will error.
	if err := g.AddEmptyDir(DefaultSyncDir); err != nil {
		return err
	}
	switch g.SyncKind {
	case configsync.RootSyncKind:
		// Add safety Namespace and ClusterRole to avoid errors from the safety
		// check (KNV2006) when deleting all the other remaining objects.
		if err := g.AddSafetyNamespace(); err != nil {
			return err
		}
		if err := g.AddSafetyClusterRole(); err != nil {
			return err
		}
	case configsync.RepoSyncKind:
		// Nothing extra
	default:
		return fmt.Errorf("invalid sync kind: %s", g.SyncKind)
	}
	switch sourceFormat {
	case configsync.SourceFormatHierarchy:
		// Hierarchy format requires a Repo object.
		g.Logger.Infof("[repo %s] Setting repo format to %s", path.Base(g.Root), sourceFormat)
		if err := g.AddRepoObject(DefaultSyncDir); err != nil {
			return err
		}
	case configsync.SourceFormatUnstructured:
		// It is an error for unstructured repos to include the Repo object.
		g.Logger.Infof("[repo %s] Setting repo format to %s", path.Base(g.Root), sourceFormat)
	default:
		return fmt.Errorf("Unrecognized SourceFormat: %q", sourceFormat)
	}
	g.Format = sourceFormat
	return g.CommitAndPush("initial commit")
}

// Init initializes this git repository and configures it to talk to the cluster
// under test.
func (g *ReadWriteRepository) Init() error {
	if err := os.RemoveAll(g.Root); err != nil {
		return fmt.Errorf("deleting root directory: %s: %w", g.Root, err)
	}
	g.isEmpty = true

	err := os.MkdirAll(g.Root, fileMode)
	if err != nil {
		return fmt.Errorf("creating root directory: %s: %w", g.Root, err)
	}

	cmds := [][]string{
		{"init"},
		{"checkout", "-b", MainBranch},
		// We have to configure username/email or else committing to the repository
		// produces errors.
		{"config", "user.name", "E2E Testing"},
		{"config", "user.email", "team@example.com"},
	}
	if g.PrivateKeyPath != "" {
		cmds = append(cmds,
			// Use ssh rather than the default that git uses, as the default does not know
			// how to use private key files.
			[]string{"config", "ssh.variant", "ssh"},
			// Overwrite the ssh command to:
			// 1) Not perform host key checking for git-server, since this isn't set up
			//   properly and we don't care.
			// 2) Use the private key file we generated.
			[]string{"config", "core.sshCommand",
				fmt.Sprintf("ssh -q -o StrictHostKeyChecking=no -i %s",
					g.PrivateKeyPath)},
		)
	}
	if g.GitProvider.Type() == e2e.CSR {
		cmds = append(cmds,
			// Use credential helper script to provide information that Git needs to
			// connect securely to CSR using Google Account credentials.
			[]string{"config", fmt.Sprintf("credential.%s.helper", testing.CSRHost), "gcloud.sh"})
	}
	if g.GitProvider.Type() == e2e.SSM {
		cmds = append(cmds,
			// Use credential helper script to provide information that Git needs to
			// connect securely to SSM using Google Account credentials.
			[]string{"config", "credential.https://*.*.sourcemanager.dev.helper", "gcloud.sh"})
	}
	return g.BulkGit(cmds...)
}

// Add writes a YAML or JSON representation of obj to `path` in the git
// repository, and `git add`s the file. Does not commit/push.
//
// Overwrites the file if it already exists.
// Automatically writes YAML or JSON based on the path's extension.
//
// Don't put multiple manifests in the same file unless parsing multi-manifest
// files is the behavior under test. In that case, use AddFile.
func (g *ReadWriteRepository) Add(path string, obj client.Object) error {
	testkubeclient.AddTestLabel(obj)
	ext := filepath.Ext(path)
	bytes, err := testkubeclient.SerializeObject(obj, ext, g.Scheme)
	if err != nil {
		return err
	}
	return g.AddFile(path, bytes)
}

// Get reads, parses, and returns the specified file as an object.
//
// File must have one of these suffixes: .yaml, .yml, .json
// This is meant to read files written with Add. So it only reads one object per
// file. If you need to parse multiple objects from one file, use GetFile.
func (g *ReadWriteRepository) Get(path string) (client.Object, error) {
	bytes, err := g.GetFile(path)
	if err != nil {
		return nil, err
	}

	uObj := &unstructured.Unstructured{}
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml", ".json":
		if err := yaml.Unmarshal(bytes, uObj); err != nil {
			return nil, fmt.Errorf("decoding yaml/json file as object: %w", err)
		}
	default:
		// If you're seeing this error, use "GetFile" instead to test ignoring
		// files with extensions we ignore.
		return nil, fmt.Errorf("invalid extension to read object from, %q, use .GetFile() instead", ext)
	}

	gvk := uObj.GroupVersionKind()
	if gvk.Empty() {
		return nil, fmt.Errorf("missing GVK in file: %s", path)
	}

	// Lookup type by GVK of those registered with the Scheme
	tObj, err := g.Scheme.New(gvk)
	if err != nil {
		// Return the unstructured object if the GVK is not registered with the Scheme
		if runtime.IsNotRegisteredError(err) {
			return uObj, nil
		}
		return nil, fmt.Errorf("creating typed object from gvk: %w", err)
	}

	obj, ok := tObj.(client.Object)
	if !ok {
		// Return the unstructured object if the typed object is a
		// runtime.Object but not a metav1.Object.
		// Most registered objects should be metav1.Object.
		// But if they aren't, we need to be able to read their metadata.
		return uObj, nil
	}

	// Convert the unstructured object to a typed object
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.UnstructuredContent(), obj)
	if err != nil {
		return nil, fmt.Errorf("converting typed object to unstructured: %w", err)
	}
	return obj, nil
}

// MustGet calls Get and fails the test on error, logging the result.
func (g *ReadWriteRepository) MustGet(t testing.NTB, path string) client.Object {
	t.Helper()
	obj, err := g.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

// GetAll reads, parses, and returns all the files in a specified directory as
// objects.
func (g *ReadWriteRepository) GetAll(dirPath string, recursive bool) ([]client.Object, error) {
	absPath := filepath.Join(g.Root, dirPath)

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %s: %w", absPath, err)
	}

	var objs []client.Object
	for _, entry := range entries {
		entryPath := filepath.Join(dirPath, entry.Name())
		if entry.IsDir() {
			if recursive {
				rObjs, err := g.GetAll(entryPath, recursive)
				if err != nil {
					return nil, err
				}
				objs = append(objs, rObjs...)
			}
		} else {
			ext := filepath.Ext(entry.Name())
			switch ext {
			case ".yaml", ".yml", ".json":
				obj, err := g.Get(entryPath)
				if err != nil {
					return nil, err
				}
				objs = append(objs, obj)
			default:
				// ignore files that aren't yaml or json
			}
		}
	}
	return objs, nil
}

// MustGetAll calls GetAll and fails the test on error, logging the result.
func (g *ReadWriteRepository) MustGetAll(t testing.NTB, dirPath string, recursive bool) []client.Object {
	t.Helper()
	objs, err := g.GetAll(dirPath, recursive)
	if err != nil {
		t.Fatal(err)
	}
	return objs
}

// AddFile writes `bytes` to `file` in the git repository.
// This function should only be directly used for testing the literal YAML/JSON
// parsing logic.
//
// Path is relative to the Git repository root.
// Overwrites `file` if it already exists.
// Does not commit/push.
func (g *ReadWriteRepository) AddFile(path string, bytes []byte) error {
	absPath := filepath.Join(g.Root, path)
	if err := testkubeclient.WriteToFile(absPath, bytes); err != nil {
		return err
	}
	// Add the file to Git.
	_, err := g.Git("add", absPath)
	return err
}

// AddEmptyDir creates an empty dir containing an empty .gitkeep file, so the
// empty dir will be retained in git.
//
// Use this when creating empty sync directories, otherwise Config Sync will
// error that the directory doesn't exist.
func (g *ReadWriteRepository) AddEmptyDir(path string) error {
	return g.AddFile(filepath.Join(path, GitKeepFileName), []byte{})
}

// GetFile reads and returns the specified file.
func (g *ReadWriteRepository) GetFile(path string) ([]byte, error) {
	absPath := filepath.Join(g.Root, path)

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %s: %w", absPath, err)
	}

	return bytes, nil
}

// MustGetFile calls GetFile and fails the test on error, logging the result.
func (g *ReadWriteRepository) MustGetFile(t testing.NTB, path string) []byte {
	t.Helper()
	body, err := g.GetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Copy copies the file or directory from source to destination.
// Overwrites the file if it already exists.
// Does not commit/push.
func (g *ReadWriteRepository) Copy(sourceDir, destDir string) error {
	absDestPath := filepath.Join(g.Root, destDir)
	parentDir := filepath.Dir(absDestPath)
	if absDestPath != parentDir {
		if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directory: %s", parentDir)
		}
	}
	if out, err := exec.Command("cp", "-r", sourceDir, absDestPath).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy directory: %s", string(out))
	}
	// Add the directory to Git.
	_, err := g.Git("add", absDestPath)
	return err
}

// RemoveAll removes all files in the repository.
func (g *ReadWriteRepository) RemoveAll() error {
	return g.BulkGit(
		[]string{"rm", "-f", "-r", "*"},
	)
}

// Remove deletes `file` from the git repository.
// If `file` is a directory, deletes the directory.
// Returns error if the file does not exist.
// Does not commit/push.
func (g *ReadWriteRepository) Remove(path string) error {
	absPath := filepath.Join(g.Root, path)

	if err := os.RemoveAll(absPath); err != nil {
		return fmt.Errorf("deleting path: %s: %w", absPath, err)
	}

	_, err := g.Git("add", absPath)
	return err
}

// Exists returns true if the file or directory exists at the specified path.
func (g *ReadWriteRepository) Exists(path string) (bool, error) {
	absPath := filepath.Join(g.Root, path)

	_, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("describing file path: %s: %w", absPath, err)
	}
	return true, nil
}

// CommitAndPush commits any changes to the git repository, and
// pushes them to the git server.
// We don't care about differentiating between committing and pushing
// for tests.
func (g *ReadWriteRepository) CommitAndPush(msg string) error {
	return g.CommitAndPushBranch(msg, MainBranch)
}

// CommitAndPushBranch commits any changes to the git branch, and
// pushes them to the git server.
func (g *ReadWriteRepository) CommitAndPushBranch(msg, branch string) error {
	g.Logger.Infof("[repo %s] committing: %s", path.Base(g.Root), msg)
	if _, err := g.Git("commit", "-m", msg); err != nil {
		return err
	}
	commit, err := g.Hash()
	if err != nil {
		return err
	}
	g.Logger.Infof("[repo %s] pushing commit: %s", path.Base(g.Root), commit)
	if err := g.Push(branch, "-f"); err != nil {
		return err
	}
	g.isEmpty = false
	return nil
}

// Push pushes the provided refspec to the git server.
// Performs a retry using RemoteURL, which may change if the port forwarding restarts.
func (g *ReadWriteRepository) Push(args ...string) error {
	timeout := 1 * time.Minute
	if g.GitProvider.Type() == e2e.Local {
		// Wait long enough for the local git-server pod to be rescheduled
		// on GKE Autopilot, which may involve spinning up a new node.
		timeout = g.defaultWaitTimeout
	}
	took, err := retry.Retry(timeout, func() error {
		remoteURL, err := g.GitProvider.RemoteURL(g.RemoteRepoName)
		if err != nil {
			return err
		}
		return g.push(remoteURL, args...)
	})
	if err != nil {
		return fmt.Errorf("failed to push after retrying for %v: %w", took, err)
	}
	g.Logger.Infof("took %v to push", took)
	return nil
}

func (g *ReadWriteRepository) push(remoteURL string, args ...string) error {
	_, err := g.Git(append([]string{"push", remoteURL}, args...)...)
	return err
}

// PushAllBranches push all local branches to the git server.
// This is currently intended to only be called from the OnReadyCallback for the
// in-cluster git server. Accepts a remoteURL to avoid calls to LocalPort, as this
// would lead to a deadlock
func (g *ReadWriteRepository) PushAllBranches(remoteURL string) error {
	if g.isEmpty {
		// empty repository, nothing to push
		return nil
	}
	return g.push(remoteURL, "--all")
}

// CreateBranch creates and checkouts a new branch at once.
func (g *ReadWriteRepository) CreateBranch(branch string) error {
	if _, err := g.Git("branch", branch); err != nil {
		return err
	}
	return g.CheckoutBranch(branch)
}

// CheckoutBranch checkouts a branch.
func (g *ReadWriteRepository) CheckoutBranch(branch string) error {
	_, err := g.Git("checkout", branch)
	return err
}

// RenameBranch renames the current branch with a new one both locally and remotely.
// The old branch will be deleted from remote.
func (g *ReadWriteRepository) RenameBranch(currentBranch, newBranch string) error {
	if _, err := g.Git("branch", "-m", currentBranch, newBranch); err != nil {
		return err
	}
	if err := g.Push(newBranch); err != nil {
		return err
	}
	return g.Push(currentBranch, "--delete")
}

// CurrentBranch returns the name of the current branch.
// Note: this will not work if not checked out to a branch (e.g. detached HEAD).
func (g *ReadWriteRepository) CurrentBranch() (string, error) {
	out, err := g.Git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Hash returns the current hash of the git repository.
//
// Immediately ends the test on error.
func (g *ReadWriteRepository) Hash() (string, error) {
	out, err := g.Git("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting the current local commit hash: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MustHash calls Hash and fails the test on error, logging the result.
func (g *ReadWriteRepository) MustHash(t testing.NTB) string {
	t.Helper()
	commit, err := g.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

// AddSafetyNamespace adds a Namespace to prevent the mono-repo safety check
// (KNV2006) from preventing deletion of other objects.
func (g *ReadWriteRepository) AddSafetyNamespace() error {
	return g.Add(g.SafetyNSPath, k8sobjects.NamespaceObject(g.SafetyNSName))
}

// RemoveSafetyNamespace removes the safety Namespace.
func (g *ReadWriteRepository) RemoveSafetyNamespace() error {
	return g.Remove(g.SafetyNSPath)
}

// AddSafetyClusterRole adds a ClusterRole to prevent the mono-repo safety check
// (KNV2006) from preventing deletion of other objects.
func (g *ReadWriteRepository) AddSafetyClusterRole() error {
	return g.Add(g.SafetyClusterRolePath, k8sobjects.ClusterRoleObject(core.Name(g.SafetyClusterRoleName)))
}

// RemoveSafetyClusterRole removes the safety ClusterRole.
func (g *ReadWriteRepository) RemoveSafetyClusterRole() error {
	return g.Remove(g.SafetyClusterRolePath)
}

// AddRepoObject adds a system.repo.yaml under the specified directory path.
// Use this for structured repositories.
func (g *ReadWriteRepository) AddRepoObject(syncDir string) error {
	return g.Add(filepath.Join(syncDir, "system", "repo.yaml"), k8sobjects.RepoObject())
}

// SyncURL returns the git repository URL for Config Sync to sync from.
func (g *ReadWriteRepository) SyncURL() string {
	return g.GitProvider.SyncURL(g.RemoteRepoName)
}
