package main

import (
	"flag"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/submariner-io/admiral/pkg/federate/kubefed"
	"github.com/submariner-io/admiral/pkg/orchestrator/controller"
	submarinerv1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	"github.com/submariner-io/submariner/pkg/signals"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	masterURL  string
	kubeConfig string
)

func init() {
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	err := submarinerv1.AddToScheme(scheme.Scheme)
	if err != nil {
		klog.Exitf("Error adding submariner V1 to the scheme: %v", err)
	}

	klog.Info("Starting submariner orchestrator")

	kubeConfig, err := clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
	if err != nil {
		klog.Exitf("Error building kubeconfig: %v", err)
	}

	klog.Infof("Got kube config: %#v", kubeConfig)

	stopCh := signals.SetupSignalHandler()

	federator, err := kubefed.New(kubeConfig, stopCh)
	if err != nil {
		klog.Exitf("Error creating federator: %v", err)
	}

	controller := controller.New(federator)
	if err = controller.Start(); err != nil {
		klog.Exitf("Error starting orchestrator controller: %v", err)
	}

	<-stopCh

	controller.Stop()
	klog.Info("Submariner orchestrator shutting down")
}
