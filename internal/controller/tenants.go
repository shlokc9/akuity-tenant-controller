package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/pkg/errors"
	api "github.com/shlokc9/akuity-tenant-controller/api/v1alpha1"
)

// reconciler reconciles Tenant resources.
type reconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

// SetupReconcilerWithManager initializes a reconciler for Tenant resources and
// registers it with the provided Manager.
func SetupReconcilerWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.Tenant{}).
		Owns(&corev1.Namespace{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(newReconciler(mgr.GetClient(), mgr.GetScheme()))
}

// newReconciler initializes a new reconciler object with kubeClient and a scheme.
func newReconciler(kubeClient client.Client, scheme *runtime.Scheme) *reconciler {
	return &reconciler{
		client: kubeClient,
		scheme: scheme,
	}
}

// Reconcile is the main reconciliation loop which aims to move the current
// state of the cluster closer to the desired state.
func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: Add reconciliation logic here

	// Initializing a logger for controller reconciliation.
	logger := log.FromContext(ctx).WithValues("tenant", req.Name)
	logger.Info("Starting reconciliation for Tenant")

	// Fetch the Tenant resource.
	logger.Info("Fetching the Tenant")
	tenant := &api.Tenant{}
	if err := r.client.Get(ctx, req.NamespacedName, tenant); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Tenant resource not found; it may have been deleted")
		} else {
			logger.Error(err, "Failed to get Tenant resource")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Running finalization if Tenant is marked for deletion.
	if tenant.DeletionTimestamp != nil {
		if err := r.handleFinalization(ctx, tenant); err != nil {
			logger.Error(err, "Finalization failed")
			return ctrl.Result{}, errors.Wrap(err, "failed to finalize Tenant")
		}
		// Removing finalizer.
		tenant.Finalizers = removeString(tenant.Finalizers, TenantFinalizer)
		if err := r.client.Update(ctx, tenant); err != nil {
			logger.Error(err, "Failed to remove finalizer from Tenant")
			return ctrl.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}
		logger.Info("Finalization complete. Tenant deleted")
		return ctrl.Result{}, nil
	}

	// Ensuring the Tenant has our finalizer.
	if res, err := r.ensureFinalizer(ctx, tenant); err != nil {
		return res, err
	}

	// Synchronizing the Namespace.
	if res, err := r.syncNamespace(ctx, tenant); err != nil {
		return res, err
	}

	// Synchronizing the NetworkPolicy.
	if res, err := r.syncNetworkPolicy(ctx, tenant); err != nil {
		return res, err
	}

	logger.Info("Reconciliation complete for Tenant", "tenant", tenant.Name)
	return ctrl.Result{}, nil
}
