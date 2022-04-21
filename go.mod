module github.com/submariner-io/admiral

go 1.13

retract v0.10.0 // Tag was moved

require (
	github.com/go-logr/logr v1.2.0
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.19.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/rs/zerolog v1.26.1
	github.com/submariner-io/shipyard v0.12.0-m3
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	k8s.io/api v0.23.5
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
	k8s.io/klog v1.0.0
	sigs.k8s.io/controller-runtime v0.11.2
)

// Pinned to kubernetes-1.21.11
replace (
	k8s.io/api => k8s.io/api v0.21.11
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.11
	k8s.io/client-go => k8s.io/client-go v0.21.11
)
