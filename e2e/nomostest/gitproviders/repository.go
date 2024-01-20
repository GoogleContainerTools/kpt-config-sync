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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	jserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/testing/fake"
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

// RepoType represents the type of the source repository.
type RepoType string

const (
	// RootRepo indicates the resources in the repository are cluster-scoped.
	RootRepo RepoType = "root"
	// NamespaceRepo indicates the resources in the repository are namespace-scoped.
	NamespaceRepo RepoType = "namespace"
)

// Repository is a local git repository with a connection to a repository
// on the git-server for the test.
//
// We shell out for git commands as the git libraries are difficult to configure
// ssh for, and git-server requires ssh authentication.
type Repository struct {
	// Name of the repository.
	// <NAMESPACE>/<NAME> of the RootSync|RepoSync.
	Name string
	// Root is the location on the machine running the test at which the local
	// repository is stored.
	Root string
	// Format is the source format for parsing the repository (hierarchy or
	// unstructured).
	Format filesystem.SourceFormat
	// PrivateKeyPath is the local path to the private key on disk to use to
	// authenticate with the git server.
	PrivateKeyPath string
	// Type refers to the type of the repository, i.e. if it is a root repo or a namespace repo.
	Type RepoType
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

	yamlSerializer *jserializer.Serializer

	defaultWaitTimeout time.Duration
	// isEmpty indicates whether the local repo has any commits
	isEmpty bool
}

// NewRepository creates a remote repo on the git provider.
// Locally, it writes the repository to `tmpdir`/repos/`name`.
// Name is the <NAMESPACE>/<NAME> of the RootSync|RepoSync.
func NewRepository(
	repoType RepoType,
	syncNN types.NamespacedName,
	sourceFormat filesystem.SourceFormat,
	scheme *runtime.Scheme,
	logger *testlogger.TestLogger,
	provider GitProvider,
	tmpDir string,
	privateKeyPath string,
	defaultWaitTimeout time.Duration,
) *Repository {
	namespacedName := syncNN.String()
	safetyName := fmt.Sprintf("safety-%s", strings.ReplaceAll(namespacedName, "/", "-"))
	return &Repository{
		Name:                  namespacedName,
		Root:                  filepath.Join(tmpDir, "repos", namespacedName),
		Format:                sourceFormat,
		Type:                  repoType,
		SafetyNSName:          safetyName,
		SafetyNSPath:          fmt.Sprintf("acme/namespaces/%s/ns.yaml", safetyName),
		SafetyClusterRoleName: safetyName,
		SafetyClusterRolePath: fmt.Sprintf("acme/cluster/cluster-role-%s.yaml", safetyName),
		Scheme:                scheme,
		Logger:                logger,
		GitProvider:           provider,
		PrivateKeyPath:        privateKeyPath,
		yamlSerializer:        jserializer.NewYAMLSerializer(jserializer.DefaultMetaFactory, scheme, scheme),
		defaultWaitTimeout:    defaultWaitTimeout,
		isEmpty:               true,
	}
}

// Create the remote repository using the GitProvider, and
// create the local repository with an initial commit.
func (g *Repository) Create() error {
	repoName, err := g.GitProvider.CreateRepository(g.Name)
	if err != nil {
		return errors.Wrapf(err, "creating repo: %s", g.Name)
	}
	g.RemoteRepoName = repoName
	return nil
}

// Git executes the command from the repo root.
// The command is always logged to the debug log.
// If the command errors, the command and output is logged.
func (g *Repository) Git(command ...string) ([]byte, error) {
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
func (g *Repository) BulkGit(cmds ...[]string) error {
	for _, cmd := range cmds {
		if _, err := g.Git(cmd...); err != nil {
			return err
		}
	}
	return nil
}

// InitialCommit initializes the Nomos repo with the Repo object.
func (g *Repository) InitialCommit(sourceFormat filesystem.SourceFormat) error {
	// Add .gitkeep to retain dir when empty, otherwise configsync will error.
	if err := g.AddEmptyDir(DefaultSyncDir); err != nil {
		return err
	}
	if g.Type == RootRepo {
		// Add safety Namespace and ClusterRole to avoid errors from the safety
		// check (KNV2006) when deleting all the other remaining objects.
		if err := g.AddSafetyNamespace(); err != nil {
			return err
		}
		if err := g.AddSafetyClusterRole(); err != nil {
			return err
		}
	}
	switch sourceFormat {
	case filesystem.SourceFormatHierarchy:
		// Hierarchy format requires a Repo object.
		g.Logger.Infof("[repo %s] Setting repo format to %s", path.Base(g.Root), sourceFormat)
		if err := g.AddRepoObject(DefaultSyncDir); err != nil {
			return err
		}
	case filesystem.SourceFormatUnstructured:
		// It is an error for unstructured repos to include the Repo object.
		g.Logger.Infof("[repo %s] Setting repo format to %s", path.Base(g.Root), sourceFormat)
	default:
		return errors.Errorf("Unrecognized SourceFormat: %q", sourceFormat)
	}
	g.Format = sourceFormat
	return g.CommitAndPush("initial commit")
}

// Init initializes this git repository and configures it to talk to the cluster
// under test.
func (g *Repository) Init() error {
	if err := os.RemoveAll(g.Root); err != nil {
		return errors.Wrapf(err, "deleting root directory: %s", g.Root)
	}
	g.isEmpty = true

	err := os.MkdirAll(g.Root, fileMode)
	if err != nil {
		return errors.Wrapf(err, "creating root directory: %s", g.Root)
	}

	return g.BulkGit(
		[]string{"init"},
		[]string{"checkout", "-b", MainBranch},
		// We have to configure username/email or else committing to the repository
		// produces errors.
		[]string{"config", "user.name", "E2E Testing"},
		[]string{"config", "user.email", "team@example.com"},

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

// Add writes a YAML or JSON representation of obj to `path` in the git
// repository, and `git add`s the file. Does not commit/push.
//
// Overwrites the file if it already exists.
// Automatically writes YAML or JSON based on the path's extension.
//
// Don't put multiple manifests in the same file unless parsing multi-manifest
// files is the behavior under test. In that case, use AddFile.
func (g *Repository) Add(path string, obj client.Object) error {
	testkubeclient.AddTestLabel(obj)

	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml":
		// We must convert through JSON/Unstructured to avoid "omitempty" fields
		// from being specified.
		var err error
		uObj, err := reconcile.AsUnstructuredSanitized(obj)
		if err != nil {
			return errors.Wrap(err, "sanitizing object")
		}
		// Encode converts to json then yaml, to handle "omitempty" directives.
		bytes, err := runtime.Encode(g.yamlSerializer, uObj)
		if err != nil {
			return errors.Wrap(err, "encoding object as yaml")
		}
		return g.AddFile(path, bytes)
	case ".json":
		var err error
		uObj, err := reconcile.AsUnstructuredSanitized(obj)
		if err != nil {
			return errors.Wrap(err, "sanitizing object")
		}
		bytes, err := json.MarshalIndent(uObj, "", "  ")
		if err != nil {
			return errors.Wrap(err, "encoding object as json")
		}
		return g.AddFile(path, bytes)
	default:
		// If you're seeing this error, use "AddFile" instead to test ignoring
		// files with extensions we ignore.
		return fmt.Errorf("invalid extension to write object to, %q, use .AddFile() instead", ext)
	}
}

// Get reads, parses, and returns the specified file as an object.
//
// File must have one of these suffixes: .yaml, .yml, .json
// This is meant to read files written with Add. So it only reads one object per
// file. If you need to parse multiple objects from one file, use GetFile.
func (g *Repository) Get(path string) (client.Object, error) {
	bytes, err := g.GetFile(path)
	if err != nil {
		return nil, err
	}

	uObj := &unstructured.Unstructured{}
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml", ".json":
		if err := yaml.Unmarshal(bytes, uObj); err != nil {
			return nil, errors.Wrap(err, "decoding yaml/json file as object")
		}
	default:
		// If you're seeing this error, use "GetFile" instead to test ignoring
		// files with extensions we ignore.
		return nil, errors.Errorf("invalid extension to read object from, %q, use .GetFile() instead", ext)
	}

	gvk := uObj.GroupVersionKind()
	if gvk.Empty() {
		return nil, errors.Errorf("missing GVK in file: %s", path)
	}

	// Lookup type by GVK of those registered with the Scheme
	tObj, err := g.Scheme.New(gvk)
	if err != nil {
		// Return the unstructured object if the GVK is not registered with the Scheme
		if runtime.IsNotRegisteredError(err) {
			return uObj, nil
		}
		return nil, errors.Wrap(err, "creating typed object from gvk")
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
		return nil, errors.Wrap(err, "converting typed object to unstructured")
	}
	return obj, nil
}

// MustGet calls Get and fails the test on error, logging the result.
func (g *Repository) MustGet(t testing.NTB, path string) client.Object {
	t.Helper()
	obj, err := g.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

// GetAll reads, parses, and returns all the files in a specified directory as
// objects.
func (g *Repository) GetAll(dirPath string, recursive bool) ([]client.Object, error) {
	absPath := filepath.Join(g.Root, dirPath)

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, errors.Wrapf(err, "reading directory: %s", absPath)
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
func (g *Repository) MustGetAll(t testing.NTB, dirPath string, recursive bool) []client.Object {
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
func (g *Repository) AddFile(path string, bytes []byte) error {
	absPath := filepath.Join(g.Root, path)

	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, fileMode); err != nil {
		return errors.Wrapf(err, "creating directory: %s", parentDir)
	}

	// Write bytes to file.
	if err := os.WriteFile(absPath, bytes, fileMode); err != nil {
		return errors.Wrapf(err, "writing file: %s", absPath)
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
func (g *Repository) AddEmptyDir(path string) error {
	return g.AddFile(filepath.Join(path, GitKeepFileName), []byte{})
}

// GetFile reads and returns the specified file.
func (g *Repository) GetFile(path string) ([]byte, error) {
	absPath := filepath.Join(g.Root, path)

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		return nil, errors.Wrapf(err, "reading file: %s", absPath)
	}

	return bytes, nil
}

// MustGetFile calls GetFile and fails the test on error, logging the result.
func (g *Repository) MustGetFile(t testing.NTB, path string) []byte {
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
func (g *Repository) Copy(sourceDir, destDir string) error {
	absDestPath := filepath.Join(g.Root, destDir)
	parentDir := filepath.Dir(absDestPath)
	if absDestPath != parentDir {
		if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
			return errors.Errorf("failed to create directory: %s", parentDir)
		}
	}
	if out, err := exec.Command("cp", "-r", sourceDir, absDestPath).CombinedOutput(); err != nil {
		return errors.Errorf("failed to copy directory: %s", string(out))
	}
	// Add the directory to Git.
	_, err := g.Git("add", absDestPath)
	return err
}

// Remove deletes `file` from the git repository.
// If `file` is a directory, deletes the directory.
// Returns error if the file does not exist.
// Does not commit/push.
func (g *Repository) Remove(path string) error {
	absPath := filepath.Join(g.Root, path)

	if err := os.RemoveAll(absPath); err != nil {
		return errors.Wrapf(err, "deleting path: %s", absPath)
	}

	_, err := g.Git("add", absPath)
	return err
}

// Exists returns true if the file or directory exists at the specified path.
func (g *Repository) Exists(path string) (bool, error) {
	absPath := filepath.Join(g.Root, path)

	_, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "describing file path: %s", absPath)
	}
	return true, nil
}

// CommitAndPush commits any changes to the git repository, and
// pushes them to the git server.
// We don't care about differentiating between committing and pushing
// for tests.
func (g *Repository) CommitAndPush(msg string) error {
	return g.CommitAndPushBranch(msg, MainBranch)
}

// CommitAndPushBranch commits any changes to the git branch, and
// pushes them to the git server.
func (g *Repository) CommitAndPushBranch(msg, branch string) error {
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
func (g *Repository) Push(args ...string) error {
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
		return errors.Wrapf(err, "failed to push after retrying for %v", took)
	}
	g.Logger.Infof("took %v to push", took)
	return nil
}

func (g *Repository) push(remoteURL string, args ...string) error {
	_, err := g.Git(append([]string{"push", remoteURL}, args...)...)
	return err
}

// PushAllBranches push all local branches to the git server.
// This is currently intended to only be called from the OnReadyCallback for the
// in-cluster git server. Accepts a remoteURL to avoid calls to LocalPort, as this
// would lead to a deadlock
func (g *Repository) PushAllBranches(remoteURL string) error {
	if g.isEmpty {
		// empty repository, nothing to push
		return nil
	}
	return g.push(remoteURL, "--all")
}

// CreateBranch creates and checkouts a new branch at once.
func (g *Repository) CreateBranch(branch string) error {
	if _, err := g.Git("branch", branch); err != nil {
		return err
	}
	return g.CheckoutBranch(branch)
}

// CheckoutBranch checkouts a branch.
func (g *Repository) CheckoutBranch(branch string) error {
	_, err := g.Git("checkout", branch)
	return err
}

// RenameBranch renames the current branch with a new one both locally and remotely.
// The old branch will be deleted from remote.
func (g *Repository) RenameBranch(current, new string) error {
	if _, err := g.Git("branch", "-m", current, new); err != nil {
		return err
	}
	if err := g.Push(new); err != nil {
		return err
	}
	return g.Push(current, "--delete")
}

// CurrentBranch returns the name of the current branch.
// Note: this will not work if not checked out to a branch (e.g. detached HEAD).
func (g *Repository) CurrentBranch() (string, error) {
	out, err := g.Git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Hash returns the current hash of the git repository.
//
// Immediately ends the test on error.
func (g *Repository) Hash() (string, error) {
	out, err := g.Git("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", errors.Wrap(err, "getting the current local commit hash")
	}
	return strings.TrimSpace(string(out)), nil
}

// MustHash calls Hash and fails the test on error, logging the result.
func (g *Repository) MustHash(t testing.NTB) string {
	t.Helper()
	commit, err := g.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

// AddSafetyNamespace adds a Namespace to prevent the mono-repo safety check
// (KNV2006) from preventing deletion of other objects.
func (g *Repository) AddSafetyNamespace() error {
	return g.Add(g.SafetyNSPath, fake.NamespaceObject(g.SafetyNSName))
}

// RemoveSafetyNamespace removes the safety Namespace.
func (g *Repository) RemoveSafetyNamespace() error {
	return g.Remove(g.SafetyNSPath)
}

// AddSafetyClusterRole adds a ClusterRole to prevent the mono-repo safety check
// (KNV2006) from preventing deletion of other objects.
func (g *Repository) AddSafetyClusterRole() error {
	return g.Add(g.SafetyClusterRolePath, fake.ClusterRoleObject(core.Name(g.SafetyClusterRoleName)))
}

// RemoveSafetyClusterRole removes the safety ClusterRole.
func (g *Repository) RemoveSafetyClusterRole() error {
	return g.Remove(g.SafetyClusterRolePath)
}

// AddRepoObject adds a system.repo.yaml under the specified directory path.
// Use this for structured repositories.
func (g *Repository) AddRepoObject(syncDir string) error {
	return g.Add(filepath.Join(syncDir, "system", "repo.yaml"), fake.RepoObject())
}
