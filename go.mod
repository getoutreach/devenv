module github.com/getoutreach/devenv

go 1.16

require (
	github.com/AlecAivazis/survey/v2 v2.2.12
	github.com/aws/aws-sdk-go-v2 v1.6.0
	github.com/aws/aws-sdk-go-v2/config v1.1.4
	github.com/aws/aws-sdk-go-v2/internal/ini v1.0.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.4.0
	github.com/cenkalti/backoff/v4 v4.1.0
	github.com/docker/docker v20.10.8+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/getoutreach/gobox v1.11.0
	github.com/getoutreach/localizer v1.12.0
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.1
	github.com/google/btree v1.0.1 // indirect
	github.com/googleapis/gnostic v0.5.4 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/hashicorp/vault/api v1.1.0
	github.com/jetstack/cert-manager v1.2.0
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/klauspost/compress v1.11.13 // indirect
	github.com/manifoldco/promptui v0.8.0
	github.com/minio/minio-go/v7 v7.0.10
	github.com/mitchellh/go-wordwrap v1.0.1
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/novln/docker-parser v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.20.0 // indirect
	github.com/schollz/progressbar/v3 v3.8.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3 // indirect
	github.com/urfave/cli/v2 v2.3.0
	github.com/versent/saml2aws/v2 v2.30.0
	github.com/vmihailenco/tagparser v0.1.2 // indirect
	github.com/vmware-tanzu/velero v1.6.0
	go.uber.org/atomic v1.7.0 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20210716133855-ce7ef5c701ea // indirect
	google.golang.org/grpc v1.39.0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b

	// Kubernetes dependencies
	k8s.io/api v0.21.0
	k8s.io/apiextensions-apiserver v0.20.5
	k8s.io/apimachinery v0.21.0
	k8s.io/cli-runtime v0.20.5
	k8s.io/client-go v0.21.0
	k8s.io/component-base v0.20.5
	k8s.io/kubectl v0.20.5
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.0 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.20.5
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.20.5
	k8s.io/apimachinery => k8s.io/apimachinery v0.20.5
	k8s.io/apiserver => k8s.io/apiserver v0.20.5
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.20.5
	k8s.io/client-go => k8s.io/client-go v0.20.5
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.20.5
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.20.5
	k8s.io/code-generator => k8s.io/code-generator v0.20.5
	k8s.io/component-base => k8s.io/component-base v0.20.5
	k8s.io/component-helpers => k8s.io/component-helpers v0.20.5
	k8s.io/controller-manager => k8s.io/controller-manager v0.20.5
	k8s.io/cri-api => k8s.io/cri-api v0.20.5
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.20.5
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.20.5
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.20.5
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.20.5
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.20.5
	k8s.io/kubectl => k8s.io/kubectl v0.20.5
	k8s.io/kubelet => k8s.io/kubelet v0.20.5
	k8s.io/kubernetes => k8s.io/kubernetes v1.20.5
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.20.5
	k8s.io/metrics => k8s.io/metrics v0.20.5
	k8s.io/mount-utils => k8s.io/mount-utils v0.20.5
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.20.5
)
