module github.com/submariner-io/admiral

go 1.13

retract v0.10.0 // Tag was moved

require (
	github.com/go-logr/logr v0.3.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/submariner-io/shipyard v0.12.0-m0
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/oauth2 v0.0.0-20210427180440-81ed05c6b58c // indirect
	golang.org/x/term v0.0.0-20210429154555-c04ba851c2a4 // indirect
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6
	google.golang.org/appengine v1.6.7 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.19.10
	k8s.io/apimachinery v0.19.10
	k8s.io/client-go v0.19.10
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20210421082810-95288971da7e // indirect
	k8s.io/utils v0.0.0-20210305010621-2afb4311ab10 // indirect
)

// Pinned to kubernetes-1.19.10
replace (
	k8s.io/api => k8s.io/api v0.19.10
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.19.10
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.10
	k8s.io/client-go => k8s.io/client-go v0.19.10
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.19.10
	k8s.io/component-base => k8s.io/component-base v0.19.10
)
