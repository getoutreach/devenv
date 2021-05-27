module github.com/getoutreach/devenv

go 1.16

require (
	github.com/AlecAivazis/survey/v2 v2.2.12
	github.com/aws/aws-sdk-go-v2 v1.6.0
	github.com/aws/aws-sdk-go-v2/config v1.1.4
	github.com/aws/aws-sdk-go-v2/internal/ini v1.0.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.4.0
	github.com/cenkalti/backoff/v4 v4.1.0
	github.com/containerd/containerd v1.4.4 // indirect
	github.com/docker/docker v20.10.6+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/getoutreach/gobox v1.0.1
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.1
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/hashicorp/vault/api v1.1.0
	github.com/jaredallard/localizer v1.8.2
	github.com/jetstack/cert-manager v1.2.0
	github.com/jonboulle/clockwork v0.1.1-0.20190114141812-62fb9bc030d1 // indirect
	github.com/klauspost/compress v1.11.13 // indirect
	github.com/manifoldco/promptui v0.8.0
	github.com/minio/minio-go/v7 v7.0.10
	github.com/mitchellh/go-wordwrap v1.0.1
	github.com/novln/docker-parser v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/schollz/progressbar/v3 v3.8.1
	github.com/sirupsen/logrus v1.8.1
	github.com/urfave/cli/v2 v2.3.0
	github.com/versent/saml2aws/v2 v2.30.0
	github.com/vmware-tanzu/velero v1.6.0
	google.golang.org/grpc v1.36.1
	gopkg.in/yaml.v2 v2.4.0

	// Kubernetes dependencies
	k8s.io/api v0.20.5
	k8s.io/apiextensions-apiserver v0.20.5
	k8s.io/apimachinery v0.20.5
	k8s.io/cli-runtime v0.20.5
	k8s.io/client-go v0.20.5
	k8s.io/component-base v0.20.5
	k8s.io/kubectl v0.20.5
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
