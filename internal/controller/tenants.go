package controller

import (
	"context"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	logger := log.FromContext(ctx).WithValues(Tenant, req.Name)
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

	// Running finalization logic if the Tenant is marked for deletion.
	if tenant.ObjectMeta.DeletionTimestamp != nil {
		logger.Info("Tenant is marked for deletion; starting finalization")
		if err := r.finalizeTenant(ctx, tenant); err != nil {
			logger.Error(err, "Failed to finalize Tenant")
			return ctrl.Result{}, errors.Wrap(err, "failed to finalize Tenant")
		}
		// Removing the finalizer and update the Tenant.
		tenant.Finalizers = removeString(tenant.Finalizers, TenantFinalizer)
		if err := r.client.Update(ctx, tenant); err != nil {
			logger.Error(err, "Failed to remove finalizer from Tenant")
			return ctrl.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}
		logger.Info("Finalization complete. Tenant deleted")
		return ctrl.Result{}, nil
	}

	// Ensuring the Tenant has our finalizer if not deleting.
	if !containsString(tenant.Finalizers, TenantFinalizer) {
		tenant.Finalizers = append(tenant.Finalizers, TenantFinalizer)
		if err := r.client.Update(ctx, tenant); err != nil {
			logger.Error(err, "Failed to add finalizer to Tenant")
			return ctrl.Result{}, errors.Wrap(err, "failed to add finalizer")
		}
	}

	// Using the Tenant's name as the namespace name.
	nsName := tenant.Name

	// Trying to fetch the corresponding namespace.
	namespace := &corev1.Namespace{}
	err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Namespace not found. Creating new namespace", Namespace, nsName)
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Labels: map[string]string{
						MandatoryLabelKey: MandatoryLabelValue,
					},
				},
			}
			for key, value := range tenant.Spec.AdditionalLabels {
				newNS.ObjectMeta.Labels[key] = value
			}
			logger.Info("Setting the Tenant as the owner of the namespace", Namespace, nsName)
			if err := controllerutil.SetControllerReference(tenant, newNS, r.scheme); err != nil {
				logger.Error(err, "Failed to set owner reference for namespace", Namespace, nsName)
				return ctrl.Result{}, errors.Wrap(err, "failed to set owner reference")
			}
			if err := r.client.Create(ctx, newNS); err != nil {
				logger.Error(err, "Failed to create namespace", Namespace, nsName)
				return ctrl.Result{}, errors.Wrap(err, "failed to create namespace")
			}
			logger.Info("Namespace created successfully", Namespace, nsName)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get namespace", Namespace, nsName)
		return ctrl.Result{}, errors.Wrap(err, "failed to get namespace")
	}

	// Verifying if existing namespace is owned by the current Tenant.
	logger.Info("Verifying if namespace is owned by the Tenant", Namespace, nsName, Tenant, tenant.Name)
	if !isNamespaceOwnedByTenant(namespace, tenant) {
		err := errors.Errorf("namespace %s exists but is not owned by tenant %s", nsName, tenant.Name)
		logger.Error(err, "Updating the TenantStatus with error message")
		tenant.Status.ErrorMessage = "Namespace exists and is not owned by this Tenant"
		if updateErr := r.client.Status().Update(ctx, tenant); updateErr != nil {
			logger.Error(updateErr, "Failed to update tenant status", Tenant, tenant.Name)
			return ctrl.Result{}, errors.Wrap(updateErr, "failed to update tenant status")
		}
		return ctrl.Result{}, err
	}

	// Building the desired labels: the mandatory label plus any additional labels from the Tenant spec.
	desiredLabels := map[string]string{
		MandatoryLabelKey: MandatoryLabelValue,
	}
	for key, value := range tenant.Spec.AdditionalLabels {
		desiredLabels[key] = value
	}

	// Checking if the current namespace labels match the desired labels.
	if !equalLabels(namespace.Labels, desiredLabels) {
		logger.Info("Namespace labels do not match desired state. Updating labels.", Namespace, nsName, DesiredLabels, desiredLabels)
		namespace.Labels = desiredLabels
		if err := r.client.Update(ctx, namespace); err != nil {
			logger.Error(err, "Failed to update namespace labels", Namespace, nsName)
			return ctrl.Result{}, errors.Wrap(err, "failed to update namespace labels")
		}
		logger.Info("Namespace labels updated", Namespace, nsName)
		return ctrl.Result{Requeue: true}, nil
	}

	// Building the desired NetworkPolicy based on the Tenant spec.
	desiredNP := desiredNetworkPolicy(tenant)	
	existingNP := &networkingv1.NetworkPolicy{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: nsName}, existingNP); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("NetworkPolicy not found. Creating new NP", NetworkPolicy, TenantNetworkPolicyName)
			// Setting the Tenant as the owner of the NetworkPolicy.
			if err := controllerutil.SetControllerReference(tenant, desiredNP, r.scheme); err != nil {
				logger.Error(err, "Failed to set owner reference for network policy", NetworkPolicy, TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to set owner reference for network policy")
			}
			if err := r.client.Create(ctx, desiredNP); err != nil {
				logger.Error(err, "Failed to create network policy", NetworkPolicy, TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to create network policy")
			}
			logger.Info("NetworkPolicy created successfully", NetworkPolicy, TenantNetworkPolicyName)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get network policy", NetworkPolicy, TenantNetworkPolicyName)
		return ctrl.Result{}, errors.Wrap(err, "failed to get network policy")
	} else {
		// Comparing the existing NetworkPolicy spec with the desired one.
		if !networkPolicyEqual(existingNP, desiredNP) {
			logger.Info("NetworkPolicy spec does not match desired state. Updating the existing NP", NetworkPolicy, TenantNetworkPolicyName)
			existingNP.Spec = desiredNP.Spec
			if err := r.client.Update(ctx, existingNP); err != nil {
				logger.Error(err, "Failed to update network policy", NetworkPolicy, TenantNetworkPolicyName)
				return ctrl.Result{}, errors.Wrap(err, "failed to update network policy")
			}
			logger.Info("NetworkPolicy updated successfully", NetworkPolicy, TenantNetworkPolicyName)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	logger.Info("Reconciliation complete for Tenant", Tenant, tenant.Name)
	return ctrl.Result{}, nil
}

// finalizeTenant handles cleanup when a Tenant is being deleted.
func (r *reconciler) finalizeTenant(ctx context.Context, tenant *api.Tenant) error {
	logger := log.FromContext(ctx).WithValues(Tenant, tenant.Name)
	nsName := tenant.Name
	namespace := &corev1.Namespace{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: nsName}, namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Namespace already deleted", Namespace, nsName)
			return nil
		}
		return errors.Wrap(err, "failed to get namespace during finalization")
	}
	// Deleting the namespace.
	if err := r.client.Delete(ctx, namespace); err != nil {
		return errors.Wrap(err, "failed to delete namespace during finalization")
	}
	logger.Info("Namespace deletion initiated", Namespace, nsName)
	return nil
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

// equalLabels compares two maps of labels.
// If they differ, the namespace’s labels are updated
// to exactly match the desired set
func equalLabels(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, val := range a {
		if bVal, ok := b[key]; !ok || bVal != val {
			return false
		}
	}
	return true
}

// desiredNetworkPolicy constructs the desired NetworkPolicy for the given Tenant.
func desiredNetworkPolicy(tenant *api.Tenant) *networkingv1.NetworkPolicy {
	ns := tenant.Name
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TenantNetworkPolicyName,
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// This policy applies to all pods in the Tenant's namespace.
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{IngressPolicyType, EgressPolicyType},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							// An empty PodSelector here allows all traffic from pods within the same namespace.
							PodSelector: &metav1.LabelSelector{},
						},
					},
				},
			},
			Egress: buildEgressRules(tenant.Spec.AllowEgress),
		},
	}
	return np
}

// buildEgressRules returns a slice of egress rules based on whether external egress is allowed.
func buildEgressRules(allowEgress bool) []networkingv1.NetworkPolicyEgressRule {
	rules := []networkingv1.NetworkPolicyEgressRule{
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					// An empty PodSelector here allows all traffic to pods within the same namespace.
					PodSelector: &metav1.LabelSelector{},
				},
			},
		},
	}
	if allowEgress {
		// Allow egress traffic to external destinations outside cluster.
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: ExternalEgressCIDR,
					},
				},
			},
		})
	}
	return rules
}

// networkPolicyEqual compares if two NetworkPolicy objects are equal based on their spec.
func networkPolicyEqual(a, b *networkingv1.NetworkPolicy) bool {
	return reflect.DeepEqual(a.Spec, b.Spec)
}

// containsString checks if a slice contains a given string.
func containsString(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	newSlice := []string{}
	for _, item := range slice {
		if item != s {
			newSlice = append(newSlice, item)
		}
	}
	return newSlice
}
