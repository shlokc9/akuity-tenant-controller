package controller

import (
	"fmt"
	"time"
	"strings"
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	
	ctrl "sigs.k8s.io/controller-runtime"
	log "sigs.k8s.io/controller-runtime/pkg/log"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/pkg/errors"
	api "github.com/shlokc9/akuity-tenant-controller/api/v1alpha1"
)

// handleFinalization cleans up dependent resources when the Tenant is deleted.
// We only delete the namespace because of Kubernetes' cascading deletion behaviour.
// Deleting a namespace cleans up all the resources in it including any NetworkPolicies.
func (r *reconciler) handleFinalization(ctx context.Context, tenant *api.Tenant) error {
	logger := log.FromContext(ctx)
	
	logger.Info("Tenant marked for deletion; running finalization")
	nsName := tenant.Name
	namespace := &corev1.Namespace{}
	if !isNamespaceOwnedByTenant(namespace, tenant) {
		return nil
	}
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
func (r *reconciler) ensureFinalizer(ctx context.Context, tenant *api.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	
	if !containsString(tenant.Finalizers, TenantFinalizer) {
		tenant.Finalizers = append(tenant.Finalizers, TenantFinalizer)
		if err := r.client.Update(ctx, tenant); err != nil {
			logger.Error(err, "Failed to add finalizer to Tenant")
			return ctrl.Result{}, errors.Wrap(err, "failed to add finalizer")
		}
		logger.Info("Finalizer added to Tenant", "tenant", tenant.Name)
	}
	
	return ctrl.Result{}, nil
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
						K8SSystemGeneratedKey: nsName,
						MandatoryLabelKey:     MandatoryLabelValue,
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
			if err := r.waitForNamespaceToBeActive(ctx, nsName); err != nil {
				logger.Error(err, "Namespace did not become active in time. Context timed out", "namespace", nsName)
				return ctrl.Result{}, errors.Wrap(err, "namespace did not become active")
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get namespace", "namespace", nsName)
		return ctrl.Result{}, errors.Wrap(err, "failed to get namespace")
	}

	// Verify ownership.
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

	// Synchronize labels.
	desiredLabels := map[string]string{
		K8SSystemGeneratedKey: nsName,
		MandatoryLabelKey:     MandatoryLabelValue,
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
		return ctrl.Result{}, nil
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
				if (k8serrors.IsForbidden(err) && strings.Contains(err.Error(), "being terminated")) ||
				 (k8serrors.IsNotFound(err) && strings.Contains(err.Error(), fmt.Sprintf("namespaces \"%s\" not found", nsName))) {
					logger.Info("Namespace was deleted externally. Waiting for Namespace to be reconciled.")
					return ctrl.Result{Requeue: true}, nil
				} else {
					logger.Error(err, "Failed to create network policy", "networkPolicy", TenantNetworkPolicyName)
					return ctrl.Result{}, errors.Wrap(err, "failed to create network policy")
				}
			}
			logger.Info("NetworkPolicy created successfully", "networkPolicy", TenantNetworkPolicyName)
			npKey := client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: nsName}
			if err := r.waitForNetworkPolicyToBeActive(ctx, npKey); err != nil {
				logger.Error(err, "NetworkPolicy did not appear in time. Context timed out", "networkPolicy", TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "network policy did not appear")
			}
			return ctrl.Result{}, nil
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
			return ctrl.Result{}, nil
		}
	}
	
	return ctrl.Result{}, nil
}

// waitForNamespaceToBeActive waits until the Namespace becomes Active.
func (r *reconciler) waitForNamespaceToBeActive(ctx context.Context, nsName string) error {
	if r.disableWait {
		return nil
	}
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second,
		true, func(ctx context.Context) (bool, error) {
	   ns := &corev1.Namespace{}
	   if err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, ns); err != nil {
		   if k8serrors.IsNotFound(err) {
				// The Namespace hasn't been created yet.
				return false, nil
		   }
		   // Some other error occurred.
		   return false, err
	   }
	   // Check if the namespace is Active.
	   return ns.Status.Phase == corev1.NamespaceActive, nil
   })
}

// waitForNetworkPolicyToBeActive waits until the NetworkPolicy is Created.
func (r *reconciler) waitForNetworkPolicyToBeActive(ctx context.Context, npKey client.ObjectKey) error {
	if r.disableWait {
		return nil
	}
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second,
		 true, func(ctx context.Context) (bool, error) {
		np := &networkingv1.NetworkPolicy{}
		if err := r.client.Get(ctx, npKey, np); err != nil {
			if k8serrors.IsNotFound(err) {
				// The NetworkPolicy hasn't been created yet.
				return false, nil
			}
			// Some other error occurred.
			return false, err
		}
		// NetworkPolicy exists.
		return true, nil
	})
}
