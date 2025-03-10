package controller

import (
	"context"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
		Complete(newReconciler(mgr.GetClient(), mgr.GetScheme()))
}

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

	// Fetching the Tenant resource.
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

	// Using the Tenant's name as the namespace name.
	nsName := tenant.Name

	// Trying to fetch the corresponding namespace.
	namespace := &corev1.Namespace{}
	err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Namespace not found. Creating new namespace", "namespace", nsName)
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						"akuity.io/tenant": "true",
					},
				},
			}
			logger.Info("Setting the Tenant as the owner of the namespace", "namespace", nsName)
			if err := controllerutil.SetControllerReference(tenant, newNS, r.scheme); err != nil {
				logger.Error(err, "Failed to set owner reference for namespace", "namespace", nsName)
				return ctrl.Result{}, errors.Wrap(err, "failed to set owner reference")
			}
			if err := r.client.Create(ctx, newNS); err != nil {
				logger.Error(err, "Failed to create namespace", "namespace", nsName)
				return ctrl.Result{}, errors.Wrap(err, "failed to create namespace")
			}
			logger.Info("Namespace created successfully", "namespace", nsName)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get namespace", "namespace", nsName)
		return ctrl.Result{}, errors.Wrap(err, "failed to get namespace")
	}

	// Verifying if existing namespace is owned by the current Tenant.
	logger.Info("Verifying if namespace is owned by the Tenant", "namespace", nsName, "tenant", tenant.Name)
	if !isNamespaceOwnedByTenant(namespace, tenant) {
		err := errors.Errorf("namespace %s exists but is not owned by tenant %s", nsName, tenant.Name)
		logger.Error(err, "Updating the TenantStatus with error message")
		tenant.Status.ErrorMessage = "Namespace exists and is not owned by this Tenant"
		if updateErr := r.client.Status().Update(ctx, tenant); updateErr != nil {
			logger.Error(updateErr, "Failed to update tenant status", "tenant", tenant.Name)
			return ctrl.Result{}, errors.Wrap(updateErr, "failed to update tenant status")
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// isNamespaceOwnedByTenant checks if the provided namespace is owned by the given Tenant.
func isNamespaceOwnedByTenant(ns *corev1.Namespace, tenant *api.Tenant) bool {
	for _, ownerRef := range ns.OwnerReferences {
		if ownerRef.UID == tenant.UID && ownerRef.Controller != nil && *ownerRef.Controller {
			return true
		}
	}
	return false
}
