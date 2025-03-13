package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

// newReconciler initializes a new reconciler object with kubeClient and a scheme
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
	if err := r.ensureFinalizer(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	// Synchronizing the Namespace.
	if res, err := r.syncNamespace(ctx, tenant); err != nil || res.Requeue {
		return res, err
	}

	// Synchronizing the NetworkPolicy.
	if res, err := r.syncNetworkPolicy(ctx, tenant); err != nil || res.Requeue {
		return res, err
	}

	logger.Info("Reconciliation complete for Tenant", "tenant", tenant.Name)
	return ctrl.Result{}, nil
}

// handleFinalization cleans up dependent resources when the Tenant is deleted.
// We only delete the namespace because of Kubernetes' cascading deletion behaviour.
// Deleting a namespace cleans up all the resources in it including any NetworkPolicies.
func (r *reconciler) handleFinalization(ctx context.Context, tenant *api.Tenant) error {
	logger := log.FromContext(ctx)
	
	logger.Info("Tenant marked for deletion; running finalization")
	nsName := tenant.Name
	namespace := &corev1.Namespace{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Namespace already deleted", "namespace", nsName)
			return nil
		}
		return errors.Wrap(err, "failed to get namespace during finalization")
	}
	if err := r.client.Delete(ctx, namespace); err != nil {
		return errors.Wrap(err, "failed to delete namespace during finalization")
	}
	
	logger.Info("Namespace deletion initiated", "namespace", nsName)
	return nil
}

// ensureFinalizer makes sure that the Tenant has the required finalizer.
func (r *reconciler) ensureFinalizer(ctx context.Context, tenant *api.Tenant) error {
	logger := log.FromContext(ctx)
	
	if !containsString(tenant.Finalizers, TenantFinalizer) {
		tenant.Finalizers = append(tenant.Finalizers, TenantFinalizer)
		if err := r.client.Update(ctx, tenant); err != nil {
			logger.Error(err, "Failed to add finalizer to Tenant")
			return errors.Wrap(err, "failed to add finalizer")
		}
		logger.Info("Finalizer added to Tenant", "tenant", tenant.Name)
	}
	
	return nil
}

// syncNamespace ensures that the namespace exists, is owned by the Tenant, and its labels match the desired state.
func (r *reconciler) syncNamespace(ctx context.Context, tenant *api.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	
	nsName := tenant.Name
	namespace := &corev1.Namespace{}
	err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Namespace not found. Creating new namespace", "namespace", nsName)
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						MandatoryLabelKey: MandatoryLabelValue,
					},
				},
			}
			for key, value := range tenant.Spec.AdditionalLabels {
				newNS.Labels[key] = value
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

	// Verifying ownership.
	logger.Info("Verifying namespace ownership", "namespace", nsName, "tenant", tenant.Name)
	if !isNamespaceOwnedByTenant(namespace, tenant) {
		err := errors.Errorf("namespace %s exists but is not owned by tenant %s", nsName, tenant.Name)
		logger.Error(err, "Updating Tenant status with ownership error")
		tenant.Status.ErrorMessage = "Namespace exists and is not owned by this Tenant"
		if updateErr := r.client.Status().Update(ctx, tenant); updateErr != nil {
			logger.Error(updateErr, "Failed to update tenant status", "tenant", tenant.Name)
			return ctrl.Result{}, errors.Wrap(updateErr, "failed to update tenant status")
		}
		return ctrl.Result{}, err
	}

	// Synchronizing labels.
	desiredLabels := map[string]string{
		MandatoryLabelKey: MandatoryLabelValue,
	}
	for key, value := range tenant.Spec.AdditionalLabels {
		desiredLabels[key] = value
	}
	if !equalLabels(namespace.Labels, desiredLabels) {
		logger.Info("Namespace labels out of sync. Updating labels.", "namespace", nsName, "desiredLabels", desiredLabels)
		namespace.Labels = desiredLabels
		if err := r.client.Update(ctx, namespace); err != nil {
			logger.Error(err, "Failed to update namespace labels", "namespace", nsName)
			return ctrl.Result{}, errors.Wrap(err, "failed to update namespace labels")
		}
		logger.Info("Namespace labels updated", "namespace", nsName)
		return ctrl.Result{Requeue: true}, nil
	}
	
	return ctrl.Result{}, nil
}

// syncNetworkPolicy ensures that the NetworkPolicy exists and its spec matches the desired state.
func (r *reconciler) syncNetworkPolicy(ctx context.Context, tenant *api.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	
	nsName := tenant.Name
	desiredNP := desiredNetworkPolicy(tenant)
	existingNP := &networkingv1.NetworkPolicy{}
	err := r.client.Get(ctx, client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: nsName}, existingNP)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("NetworkPolicy not found. Creating new one", "networkPolicy", TenantNetworkPolicyName)
			if err := controllerutil.SetControllerReference(tenant, desiredNP, r.scheme); err != nil {
				logger.Error(err, "Failed to set owner reference for network policy", "networkPolicy", TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to set owner reference for network policy")
			}
			if err := r.client.Create(ctx, desiredNP); err != nil {
				logger.Error(err, "Failed to create network policy", "networkPolicy", TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to create network policy")
			}
			logger.Info("NetworkPolicy created successfully", "networkPolicy", TenantNetworkPolicyName)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get network policy", "networkPolicy", TenantNetworkPolicyName)
		return ctrl.Result{}, errors.Wrap(err, "failed to get network policy")
	} else {
		if !networkPolicyEqual(existingNP, desiredNP) {
			logger.Info("NetworkPolicy spec out of sync. Updating NetworkPolicy", "networkPolicy", TenantNetworkPolicyName)
			existingNP.Spec = desiredNP.Spec
			if err := r.client.Update(ctx, existingNP); err != nil {
				logger.Error(err, "Failed to update network policy", "networkPolicy", TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to update network policy")
			}
			logger.Info("NetworkPolicy updated successfully", "networkPolicy", TenantNetworkPolicyName)
			return ctrl.Result{Requeue: true}, nil
		}
	}
	
	return ctrl.Result{}, nil
}
