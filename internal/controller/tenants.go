package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	api "github.com/akuity/tenant-controller/api/v1alpha1"
)

// reconciler reconciles Tenant resources.
type reconciler struct {
	client client.Client
}

// SetupReconcilerWithManager initializes a reconciler for Tenant resources and
// registers it with the provided Manager.
func SetupReconcilerWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.Tenant{}).
		Complete(newReconciler(mgr.GetClient()))
}

func newReconciler(kubeClient client.Client) *reconciler {
	return &reconciler{
		client: kubeClient,
	}
}

// Reconcile is the main reconciliation loop which aims to move the current
// state of the cluster closer to the desired state.
func (r *reconciler) Reconcile(context.Context, ctrl.Request) (ctrl.Result, error) {
	// TODO: Add reconciliation logic here
	return ctrl.Result{}, nil
}
