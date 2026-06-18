package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/owl-scheduler/k8s-custom-scheduler/pkg/apis"
	"github.com/owl-scheduler/k8s-custom-scheduler/pkg/scheduler"
	"github.com/owl-scheduler/k8s-custom-scheduler/pkg/scheduler/plugins"
)

var (
	configFile    = flag.String("config", "", "Path to the scheduler configuration file.")
	kubeconfig    = flag.String("kubeconfig", "", "Path to a kubeconfig file. Only required if running out-of-cluster.")
	masterURL     = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig.")
	leaderElect   = flag.Bool("leader-elect", false, "Enable leader election for high availability.")
	leaderElectID = flag.String("leader-elect-identity", "owl-scheduler", "Leader election identity.")
	bindAddress   = flag.String("bind-address", "0.0.0.0", "The IP address on which to listen for health and metrics.")
	healthPort    = flag.Int("health-port", 10259, "The port for the health endpoint.")
	metricsPort   = flag.Int("metrics-port", 10260, "The port for the metrics endpoint.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.InfoS("Starting owl-scheduler", "version", "v0.1.0")

	// Load configuration
	config := apis.DefaultConfig()
	if *configFile != "" {
		klog.InfoS("Loading configuration from file", "path", *configFile)
		klog.InfoS("Config file specified (file parsing not yet implemented)", "path", *configFile)
	}

	klog.InfoS("Scheduler configuration",
		"schedulerName", config.SchedulerName,
		"workerCount", config.WorkerCount,
		"maxRetryAttempts", config.MaxRetryAttempts,
		"leaderElect", *leaderElect,
	)

	// Build Kubernetes client config
	clientConfig, err := buildConfig(*kubeconfig, *masterURL)
	if err != nil {
		klog.ErrorS(err, "Failed to build Kubernetes client config")
		os.Exit(1)
	}

	// Create the Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}

	klog.InfoS("Kubernetes client initialized successfully")

	// Create the binder
	binder := scheduler.NewDefaultBinder(clientset, config)

	// Create filter and score plugins
	filterPlugins := []apis.FilterPlugin{
		&plugins.NodeResourcesFit{},
		&plugins.NodeNameFilter{},
		&plugins.NodeUnschedulableFilter{},
		&plugins.TaintTolerationFilter{},
	}
	scorePlugins := []apis.ScorePlugin{
		&plugins.LeastRequestedPriority{},
		&plugins.BalancedResourceAllocation{},
		&plugins.NodeAffinityScorer{},
	}

	sched := scheduler.NewScheduler(clientset, config, filterPlugins, scorePlugins, binder)

	// Handle leader election if enabled
	if *leaderElect {
		klog.InfoS("Leader election is enabled", "identity", *leaderElectID)
		klog.InfoS("Leader election setup not yet implemented, running without HA")
	}

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		klog.InfoS("Received signal, shutting down", "signal", sig)
		sched.Stop()
	}()

	// Run the scheduler (blocks until shutdown)
	if err := sched.Run(); err != nil {
		klog.ErrorS(err, "Scheduler exited with error")
		os.Exit(1)
	}

	klog.InfoS("owl-scheduler exited gracefully")
}

// buildConfig builds a Kubernetes rest.Config from the given kubeconfig path
// or falls back to in-cluster config.
func buildConfig(kubeconfig, masterURL string) (*rest.Config, error) {
	if kubeconfig != "" {
		klog.InfoS("Using kubeconfig", "path", kubeconfig)
		config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		return config, nil
	}

	klog.InfoS("No kubeconfig specified, using in-cluster config")
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build in-cluster config: %w", err)
	}
	return config, nil
}
