// Copyright 2022 Google LLC
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

package filesystem

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/differ"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/git"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"kpt.dev/configsync/pkg/util/repo"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reconcileTimeout = time.Minute * 5

var _ reconcile.Reconciler = &reconciler{}

// Reconciler manages Nomos CRs by importing configs from a filesystem tree.
type reconciler struct {
	clusterName string
	// absGitDir is the absolute path to the git repository.
	// Usually contains a symlink.
	absGitDir cmpath.Absolute
	// policyDir is the relative path to the root policy dir within absGitDir.
	policyDir       cmpath.Relative
	parser          ConfigParser
	client          *syncerclient.Client
	discoveryClient discovery.DiscoveryInterface
	converter       *declared.ValueConverter
	decoder         decode.Decoder
	format          SourceFormat
	repoClient      *repo.Client
	cache           cache.Cache
	// parsedGit is populated when the mounted git repo has successfully been
	// parsed.
	parsedGit *gitState
	// appliedGitDir is set to the resolved symlink for the repo once apply has
	// succeeded in order to prevent reprocessing.  On error, this is set to empty
	// string so the importer will retry indefinitely to attempt to recover from
	// an error state.
	appliedGitDir string
	//initTime is the reconciler's instantiation time
	initTime time.Time
}

// gitState contains the parsed state of the mounted git repo at a certain revision.
type gitState struct {
	// rev is the git revision hash when the git repo was parsed.
	rev string
	// filePathList is a list of the paths of all files parsed from the git repo.
	filePathList []string
	// filePaths is a unified string of the paths in filePathList.
	filePaths string
}

// makeGitState generates a new gitState for the given FileObjects read at the specified revision.
func makeGitState(rev string, objs []ast.FileObject) *gitState {
	gs := &gitState{
		rev:          rev,
		filePathList: make([]string, len(objs)),
	}
	for i, obj := range objs {
		gs.filePathList[i] = obj.SlashPath()
	}
	gs.filePaths = strings.Join(gs.filePathList, ",")
	return gs
}

// dumpForFiles returns a string dump of the given list of FileObjects.
func dumpForFiles(objs []ast.FileObject) string {
	b := strings.Builder{}
	for _, obj := range objs {
		b.WriteString(fmt.Sprintf("path=%s ", obj.SlashPath()))
		b.WriteString(fmt.Sprintf("groupversionkind=%s namespace=%s name=%s ", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName()))
		b.WriteString(fmt.Sprintf("object=%v; ", obj.Object))
	}
	return b.String()
}

// NewReconciler returns a new Reconciler.
//
// configDir is the path to the filesystem directory that contains a candidate
// Nomos config directory, which the user intends to be valid but which the
// controller will check for errors.  pollPeriod is the time between two
// successive directory polls. parser is used to convert the contents of
// configDir into a set of Nomos configs.  client is the catch-all client used
// to call configmanagement and other Kubernetes APIs.
func newReconciler(clusterName string, gitDir string, policyDir string, parser ConfigParser, client *syncerclient.Client, discoveryClient discovery.DiscoveryInterface, cache cache.Cache, decoder decode.Decoder, format SourceFormat) (*reconciler, error) {
	repoClient := repo.New(client)

	absGitDir, err := cmpath.AbsoluteOS(gitDir)
	if err != nil {
		return nil, err
	}

	converter, err := declared.NewValueConverter(discoveryClient)
	if err != nil {
		return nil, err
	}

	return &reconciler{
		clusterName:     clusterName,
		absGitDir:       absGitDir,
		policyDir:       cmpath.RelativeOS(policyDir),
		parser:          parser,
		client:          client,
		discoveryClient: discoveryClient,
		converter:       converter,
		repoClient:      repoClient,
		cache:           cache,
		decoder:         decoder,
		format:          format,
		initTime:        time.Now(),
	}, nil
}

// dirError updates repo source status with an error due to failure to read mounted git repo.
func (c *reconciler) dirError(ctx context.Context, startTime time.Time, err error, errorFilePath string) (reconcile.Result, error) {
	importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
	gitSyncError := git.SyncError(reconcilermanager.GitSync, errorFilePath, "app=git-importer")
	errBuilder := status.SourceError
	if err != nil {
		errBuilder = errBuilder.Wrap(err)
	} else {
		err = fmt.Errorf(gitSyncError)
	}
	klog.Errorf("Failed to resolve config directory: %v", status.FormatSingleLine(err))
	sErr := errBuilder.Sprintf("unable to sync repo\n%s", gitSyncError).Build()
	c.updateSourceStatus(ctx, nil, sErr.ToCME())
	return reconcile.Result{}, nil
}

// filesystemError updates repo source status with an error due to inconsistent read from filesystem.
func (c *reconciler) filesystemError(ctx context.Context, rev string) (reconcile.Result, error) {
	sErr := status.SourceError.Sprintf("inconsistent files read from mounted git repo at revision %s", rev).Build()
	c.updateSourceStatus(ctx, &rev, sErr.ToCME())
	importer.Metrics.Violations.Inc()
	return reconcile.Result{}, sErr
}

// Reconcile implements Reconciler.
// It does the following:
//   * Checks for updates to the filesystem that stores config source of truth.
//   * When there are updates, parses the filesystem into AllConfigs, an in-memory
//     representation of desired configs.
//   * Gets the Nomos CRs currently stored in Kubernetes API server.
//   * Compares current and desired Nomos CRs.
//   * Writes updates to make current match desired.
func (c *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	klog.V(4).Infof("Reconciling: %v", request)
	startTime := time.Now()

	// Check if the source configs are synced successfully.
	errorFilePath := filepath.Join(path.Dir(c.absGitDir.OSPath()), git.ErrorFile)
	if _, err := os.Stat(errorFilePath); err == nil || !os.IsNotExist(err) {
		return c.dirError(ctx, startTime, err, errorFilePath)
	}

	// Ensure we're working with the absolute path, as most symlinks are relative
	// paths and filepath.EvalSymlinks does not get the absolute destination.
	absGitDir, err := c.absGitDir.EvalSymlinks()
	if err != nil {
		return c.dirError(ctx, startTime, err, errorFilePath)
	}

	absPolicyDir, err := absGitDir.Join(c.policyDir).EvalSymlinks()
	if err != nil {
		return c.dirError(ctx, startTime, err, errorFilePath)
	}

	// Detect whether symlink has changed, if the reconcile trigger is to periodically poll the filesystem.
	if request.Name == pollFilesystem && c.appliedGitDir == absGitDir.OSPath() {
		klog.V(4).Info("no new changes, nothing to do.")
		return reconcile.Result{}, nil
	}
	klog.Infof("Resolved config dir: %s. Polling config dir: %s", absPolicyDir,
		filepath.Join(c.absGitDir.OSPath(), c.policyDir.OSPath()))
	// Unset applied git dir. Only set this on complete import success.
	c.appliedGitDir = ""

	// Parse the commit hash from the new directory to use as an import token.
	token, err := git.CommitHash(absGitDir.OSPath())
	if err != nil {
		klog.Warningf("Invalid format for config directory format: %v", err)
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		c.updateSourceStatus(ctx, nil, status.SourceError.Wrap(err).Sprintf("unable to parse commit hash").Build().ToCME())
		return reconcile.Result{}, nil
	}

	// Before we start parsing the new directory, update the source token to reflect that this
	// cluster has seen the change even if it runs into issues parsing/importing it.
	repoObj := c.updateSourceStatus(ctx, &token)
	if repoObj == nil {
		klog.Warningf("Repo object is missing. Restarting import of %s.", token)
		// If we failed to get the Repo, restart the controller loop to try to fetch it again.
		return reconcile.Result{}, nil
	}

	currentConfigs, err := namespaceconfig.ListConfigs(ctx, c.cache)
	if err != nil {
		klog.Errorf("failed to list current configs: %v", err)
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		return reconcile.Result{}, nil
	}

	// check git status, blow up if we see issues
	if err = git.CheckClean(absGitDir.OSPath()); err != nil {
		klog.Errorf("git check clean returned error: %v", err)
		logWalkDirectory(absGitDir.OSPath())
		importer.Metrics.Violations.Inc()
		return reconcile.Result{}, err
	}

	klog.Infof("listing policy files in %s", absPolicyDir.OSPath())
	wantFiles, err := git.ListFiles(absPolicyDir)
	if err != nil {
		klog.Error(err)
		return reconcile.Result{}, errors.Wrap(err, "listing git files")
	}
	if c.format == SourceFormatHierarchy {
		wantFiles = FilterHierarchyFiles(absPolicyDir, wantFiles)
	}

	filePaths := reader.FilePaths{
		RootDir:   absPolicyDir,
		PolicyDir: c.policyDir,
		Files:     wantFiles,
	}

	decoder := decode.NewGenericResourceDecoder(scheme.Scheme)
	syncedCRDs, err := clusterconfig.GetCRDs(decoder, currentConfigs.CRDClusterConfig)
	if err != nil {
		klog.Error(err)
		return reconcile.Result{}, errors.Wrap(err, "reading synced CRDs")
	}

	// Parse filesystem tree into in-memory NamespaceConfig and ClusterConfig objects.
	fileObjects, mErr := c.parser.Parse(filePaths)
	if mErr != nil {
		klog.Warningf("Failed to parse: %v", status.FormatSingleLine(mErr))
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		c.updateImportStatus(ctx, repoObj, token, startTime, status.ToCME(mErr))
		return reconcile.Result{}, nil
	}

	options := validate.Options{
		ClusterName:           c.clusterName,
		PolicyDir:             c.policyDir,
		PreviousCRDs:          syncedCRDs,
		BuildScoper:           utildiscovery.ScoperBuilder(c.discoveryClient),
		Converter:             c.converter,
		DefaultNamespace:      metav1.NamespaceDefault,
		IsNamespaceReconciler: false,
	}

	var validationErrs status.MultiError
	if c.format == SourceFormatUnstructured {
		fileObjects, validationErrs = validate.Unstructured(fileObjects, options)
	} else {
		fileObjects, validationErrs = validate.Hierarchical(fileObjects, options)
	}
	if validationErrs != nil {
		klog.Warningf("Failed to validate: %v", status.FormatSingleLine(validationErrs))
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		c.updateImportStatus(ctx, repoObj, token, startTime, status.ToCME(validationErrs))
		if status.HasBlockingErrors(validationErrs) {
			return reconcile.Result{}, nil
		}
	}

	gs := makeGitState(absGitDir.OSPath(), fileObjects)
	if c.parsedGit == nil {
		klog.Infof("Importer state initialized at git revision %s. Unverified file list: %s", gs.rev, gs.filePaths)
	} else if c.parsedGit.rev != gs.rev {
		klog.Infof("Importer state updated to git revision %s. Unverified files list: %s", gs.rev, gs.filePaths)
	} else if c.parsedGit.filePaths == gs.filePaths {
		klog.V(2).Infof("Importer state remains at git revision %s. Verified files hash: %s", gs.rev, gs.filePaths)
	} else {
		klog.Errorf("Importer read inconsistent files at git revision %s. Expected files hash: %s, Diff: %s", gs.rev, c.parsedGit.filePaths, cmp.Diff(c.parsedGit.filePathList, gs.filePathList))
		klog.Errorf("Inconsistent files: %s", dumpForFiles(fileObjects))
		return c.filesystemError(ctx, absGitDir.OSPath())
	}
	c.parsedGit = gs

	desiredConfigs := namespaceconfig.NewAllConfigs(token, metav1.NewTime(startTime), fileObjects)
	if sErr := c.safetyCheck(desiredConfigs, currentConfigs); sErr != nil {
		c.updateSourceStatus(ctx, &token, sErr.ToCME())
		importer.Metrics.Violations.Inc()
		return reconcile.Result{}, sErr
	}

	// Update the SyncState for all NamespaceConfigs and ClusterConfig.
	for n := range desiredConfigs.NamespaceConfigs {
		pn := desiredConfigs.NamespaceConfigs[n]
		pn.Status.SyncState = v1.StateStale
		desiredConfigs.NamespaceConfigs[n] = pn
	}
	desiredConfigs.ClusterConfig.Status.SyncState = v1.StateStale

	if err = c.updateDecoderWithAPIResources(currentConfigs.Syncs, desiredConfigs.Syncs); err != nil {
		klog.Warningf("Failed to parse sync resources: %v", err)
		return reconcile.Result{}, nil
	}

	if errs := differ.Update(ctx, c.client, c.decoder, *currentConfigs, *desiredConfigs, c.initTime); errs != nil {
		klog.Warningf("Failed to apply actions: %v", status.FormatSingleLine(errs))
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		if validationErrs != nil {
			// append non-blocking validation errors
			errs = status.Append(errs, validationErrs)
		}
		c.updateImportStatus(ctx, repoObj, token, startTime, status.ToCME(errs))
		return reconcile.Result{}, nil
	}

	importer.Metrics.NamespaceConfigs.Set(float64(len(desiredConfigs.NamespaceConfigs)))

	if validationErrs != nil {
		// report non-blocking validation errors
		importer.Metrics.CycleDuration.WithLabelValues("error").Observe(time.Since(startTime).Seconds())
		c.updateImportStatus(ctx, repoObj, token, startTime, status.ToCME(validationErrs))

		klog.V(4).Infof("Reconcile completed with non-blocking validation errors")
		return reconcile.Result{}, nil
	}

	c.appliedGitDir = absGitDir.OSPath()
	importer.Metrics.CycleDuration.WithLabelValues("success").Observe(time.Since(startTime).Seconds())
	c.updateImportStatus(ctx, repoObj, token, startTime, nil)

	klog.V(4).Infof("Reconcile completed")
	return reconcile.Result{}, nil
}

// safetyCheck reports if the importer would cause the cluster to drop to zero
// NamespaceConfigs from anything other than zero or one on the cluster currently.
// That is too dangerous of a change to actually carry out.
func (c *reconciler) safetyCheck(desired, current *namespaceconfig.AllConfigs) status.Error {
	if len(desired.NamespaceConfigs) == 0 {
		count := len(current.NamespaceConfigs)
		if count > 1 {
			klog.Errorf("Importer parsed 0 NamespaceConfigs from mounted git repo but detected %d NamespaceConfigs on the cluster. This is a dangerous change, so it will be rejected.", count)
			return status.EmptySourceError(count, "namespaces")
		}
		klog.Warningf("Importer did not parse any NamespaceConfigs in git repo. Cluster currently has %d NamespaceConfigs, so this will proceed.", count)
	}
	if desired.ClusterScopedCount() == 0 {
		count := current.ClusterScopedCount()
		if count > 1 {
			klog.Errorf("Importer parsed 0 cluster-scoped resources from mounted git repo but detected %d resources on the cluster. This is a dangerous change, so it will be rejected.", count)
			return status.EmptySourceError(count, "cluster-scoped resources")
		}
		klog.Warningf("Importer did not parse any cluster-scoped resources in git repo. Cluster currently has %d resources, so this will proceed.", count)
	}
	return nil
}

// updateImportStatus write an updated RepoImportStatus based upon the given arguments.
func (c *reconciler) updateImportStatus(ctx context.Context, repoObj *v1.Repo, token string, loadTime time.Time, errs []v1.ConfigManagementError) {
	// Try to get a fresh copy of Repo since it is has high contention with syncer.
	freshRepoObj, err := c.repoClient.GetOrCreateRepo(ctx)
	if err != nil {
		klog.Errorf("failed to get fresh Repo: %v", err)
	} else {
		repoObj = freshRepoObj
	}

	repoObj.Status.Import.Token = token
	repoObj.Status.Import.LastUpdate = metav1.NewTime(loadTime)
	repoObj.Status.Import.Errors = errs

	if _, err = c.repoClient.UpdateImportStatus(ctx, repoObj); err != nil {
		klog.Errorf("failed to update Repo import status: %v", err)
	}
}

// updateSourceStatus writes the updated Repo.Source.Status field.  A new repo
// is loaded every time before updating.  If errs is nil,
// Repo.Source.Status.Errors will be cleared.  if token is nil, it will not be
// updated so as to preserve any prior content.
func (c *reconciler) updateSourceStatus(ctx context.Context, token *string, errs ...v1.ConfigManagementError) *v1.Repo {
	r, err := c.repoClient.GetOrCreateRepo(ctx)
	if err != nil {
		klog.Errorf("failed to get fresh Repo: %v", err)
		return nil
	}
	if token != nil {
		r.Status.Source.Token = *token
	}
	r.Status.Source.Errors = errs

	if _, err = c.repoClient.UpdateSourceStatus(ctx, r); err != nil {
		klog.Errorf("failed to update Repo source status: %v", err)
	}
	return r
}

// updateDecoderWithAPIResources uses the discovery API and the set of existing
// syncs on cluster to update the set of resource types the Decoder is able to decode.
func (c *reconciler) updateDecoderWithAPIResources(syncMaps ...map[string]v1.Sync) error {
	resources, discoveryErr := utildiscovery.GetResources(c.discoveryClient)
	if discoveryErr != nil {
		return discoveryErr
	}

	// We need to populate the scheme with the latest resources on cluster in order to decode GenericResources in
	// NamespaceConfigs and ClusterConfigs.
	apiInfo, err := utildiscovery.NewAPIInfo(resources)
	if err != nil {
		return errors.Wrap(err, "failed to parse server resources")
	}

	var syncList []*v1.Sync
	for _, m := range syncMaps {
		for n := range m {
			sync := m[n]
			syncList = append(syncList, &sync)
		}
	}
	gvks := apiInfo.GroupVersionKinds(syncList...)

	// Update the decoder with all sync-enabled resource types on the cluster.
	c.decoder.UpdateScheme(gvks)
	return nil
}
