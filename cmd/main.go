package main

import (
	"context"
	"log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	api "github.com/shlokc9/akuity-tenant-controller/api/v1alpha1"
	"github.com/shlokc9/akuity-tenant-controller/internal/controller"
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))

	ctx := context.Background()

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("error loading REST config: %s", err)
	}

	scheme := runtime.NewScheme()
	if err = corev1.AddToScheme(scheme); err != nil {
		log.Fatalf("error adding Kubernetes core API to scheme: %s", err)
	}
	if err = api.AddToScheme(scheme); err != nil {
		log.Fatalf("error adding Akuity network API to scheme: %s", err)
	}

	mgr, err := ctrl.NewManager(
		restCfg,
		ctrl.Options{
			Scheme: scheme,
		},
	)
	if err != nil {
		log.Fatalf("error setting up controller manager: %s", err)
	}

	if err = controller.SetupReconcilerWithManager(mgr); err != nil {
		log.Fatalf("error setting up Promotions reconciler: %s", err)
	}

	if err = mgr.Start(ctx); err != nil {
		log.Fatal(err)
	}
}
