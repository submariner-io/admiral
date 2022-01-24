module github.com/submariner-io/admiral

go 1.13

retract v0.10.0 // Tag was moved

require (
	github.com/go-logr/logr v0.4.0
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.18.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.0
	github.com/rs/zerolog v1.26.1
	github.com/submariner-io/shipyard v0.12.0-m3
	golang.org/x/term v0.0.0-20210429154555-c04ba851c2a4 // indirect
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6
	google.golang.org/appengine v1.6.7 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.19.16
	k8s.io/apimachinery v0.19.16
	k8s.io/client-go v0.19.16
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20210421082810-95288971da7e // indirect
	k8s.io/utils v0.0.0-20210305010621-2afb4311ab10 // indirect
	sigs.k8s.io/controller-runtime v0.6.1
)
