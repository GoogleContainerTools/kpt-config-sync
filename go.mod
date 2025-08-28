module kpt.dev/configsync

go 1.24.0

require (
	cloud.google.com/go/auth v0.16.5
	cloud.google.com/go/compute/metadata v0.8.0
	cloud.google.com/go/logging v1.13.0
	cloud.google.com/go/monitoring v1.24.2
	cloud.google.com/go/trace v1.11.6
	contrib.go.opencensus.io/exporter/ocagent v0.7.0
	github.com/GoogleContainerTools/kpt-functions-catalog/functions/go/set-namespace v0.4.1-0.20220713210718-d955e7d3a800
	github.com/GoogleContainerTools/kpt-functions-sdk/go/fn v0.0.0-20220706221933-7181f451a663
	github.com/Masterminds/semver v1.5.0
	github.com/Masterminds/semver/v3 v3.4.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/elliotchance/orderedmap/v2 v2.7.0
	github.com/ettle/strcase v0.2.0
	github.com/evanphx/json-patch v5.9.11+incompatible
	github.com/go-logr/logr v1.4.3
	github.com/golang/protobuf v1.5.4
	github.com/google/addlicense v1.2.0
	github.com/google/gnostic-models v0.7.0
	github.com/google/go-cmp v0.7.0
	github.com/google/go-containerregistry v0.20.6
	github.com/google/go-licenses/v2 v2.0.0-alpha.1
	github.com/google/uuid v1.6.0
	github.com/jstemmer/go-junit-report/v2 v2.1.0
	github.com/kylelemons/godebug v1.1.0
	github.com/open-policy-agent/cert-controller v0.13.0
	github.com/prometheus/client_golang v1.23.0
	github.com/prometheus/common v0.65.0
	github.com/spf13/cobra v1.9.1
	github.com/spyzhov/ajson v0.9.6
	github.com/stretchr/testify v1.11.0
	go.opencensus.io v0.24.0
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.0
	golang.org/x/exp v0.0.0-20241217172543-b2144cdd0a67
	golang.org/x/mod v0.27.0
	google.golang.org/api v0.248.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.33.4
	k8s.io/apiextensions-apiserver v0.33.4
	k8s.io/apimachinery v0.33.4
	k8s.io/cli-runtime v0.33.4
	k8s.io/client-go v0.33.4
	k8s.io/cluster-registry v0.0.6
	k8s.io/code-generator v0.33.4
	k8s.io/klog/v2 v2.130.1
	k8s.io/kube-aggregator v0.33.4
	k8s.io/kube-openapi v0.0.0-20250701173324-9bd5c66d9911
	k8s.io/kubectl v0.33.4
	k8s.io/kubernetes v1.33.4
	k8s.io/utils v0.0.0-20241210054802-24370beab758
	sigs.k8s.io/cli-utils v0.37.3-0.20250410211241-63a8e151c476
	sigs.k8s.io/controller-runtime v0.21.0
	sigs.k8s.io/controller-runtime/tools/setup-envtest v0.0.0-20231023142458-b9f29826ee83
	sigs.k8s.io/controller-tools v0.18.0
	sigs.k8s.io/kind v0.30.0
	sigs.k8s.io/kustomize/api v0.20.1
	sigs.k8s.io/kustomize/kyaml v0.20.1
	sigs.k8s.io/structured-merge-diff/v4 v4.7.0
	sigs.k8s.io/yaml v1.6.0
)

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	cloud.google.com/go v0.120.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/iam v1.5.2 // indirect
	cloud.google.com/go/longrunning v0.6.7 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.0.2 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.16.3 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/cli v28.2.2+incompatible // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/emicklei/go-restful/v3 v3.12.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gobuffalo/flect v1.0.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/licenseclassifier/v2 v2.0.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.24.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jonboulle/clockwork v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/onsi/gomega v1.37.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/otiai10/copy v1.14.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/afero v1.11.0 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/vbatts/tar-split v0.12.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.36.0 // indirect
	go.opentelemetry.io/otel/metric v1.36.0 // indirect
	go.opentelemetry.io/otel/trace v1.36.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/term v0.34.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	golang.org/x/tools/go/packages/packagestest v0.1.1-deprecated // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250818200422-3122310a409c // indirect
	google.golang.org/grpc v1.74.2 // indirect
	google.golang.org/protobuf v1.36.7 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apiserver v0.33.4 // indirect
	k8s.io/component-base v0.33.4 // indirect
	k8s.io/component-helpers v0.33.4 // indirect
	k8s.io/controller-manager v0.33.1 // indirect
	k8s.io/gengo/v2 v2.0.0-20250207200755-1244d31929d7 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
)
