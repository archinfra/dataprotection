package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
	"github.com/archinfra/dataprotection/controllers"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dpv1alpha1.AddToScheme(scheme))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "notification-gateway" {
		if err := runNotificationGateway(os.Args[2:]); err != nil {
			ctrl.Log.WithName("notification-gateway").Error(err, "unable to run notification gateway")
			os.Exit(1)
		}
		return
	}

	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "data-protection-operator.archinfra.io",
	})
	if err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.BackupAddonReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create BackupAddon controller")
		os.Exit(1)
	}
	if err = (&controllers.BackupSourceReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create BackupSource controller")
		os.Exit(1)
	}
	if err = (&controllers.BackupStorageReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create BackupStorage controller")
		os.Exit(1)
	}
	if err = (&controllers.RetentionPolicyReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create RetentionPolicy controller")
		os.Exit(1)
	}
	if err = (&controllers.SnapshotReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create Snapshot controller")
		os.Exit(1)
	}
	if err = (&controllers.NotificationEndpointReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create NotificationEndpoint controller")
		os.Exit(1)
	}
	if err = (&controllers.BackupPolicyReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create BackupPolicy controller")
		os.Exit(1)
	}
	if err = (&controllers.BackupJobReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), APIReader: mgr.GetAPIReader()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create BackupJob controller")
		os.Exit(1)
	}
	if err = (&controllers.RestoreJobReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), APIReader: mgr.GetAPIReader()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create RestoreJob controller")
		os.Exit(1)
	}
	if err = (&controllers.JobObserverReconciler{Client: mgr.GetClient(), APIReader: mgr.GetAPIReader()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create JobObserver controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.WithName("setup").Info("starting data protection operator v2")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.WithName("setup").Error(err, "problem running manager")
		os.Exit(1)
	}
}
