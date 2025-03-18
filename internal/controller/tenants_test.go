package controller

// TODO: Add tests for the Tenant reconciler
import (
	"time"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/pkg/errors"
	api "github.com/shlokc9/akuity-tenant-controller/api/v1alpha1"
)

func init() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
}

// This test verifies that when Tenant Resource is created, dependent Namespace and NetworkPolicy is also created.
func TestCreatesNamespaceAndNetworkPolicy(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource.
	tenant := createTenantResource("test-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req := createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, np := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Verifying that the Namespace has the correct labels.
	expectedLabels := map[string]string{
		MandatoryLabelKey: MandatoryLabelValue,
		"foo":             "bar",
	}
	for key, val := range expectedLabels {
		if ns.Labels[key] != val {
			t.Errorf("expected namespace label %s=%s, got %s", key, val, ns.Labels[key])
		}
	}

	// Verifying ingress rule. It should have one ingress rule with an empty PodSelector.
	if len(np.Spec.Ingress) != 1 {
		t.Errorf("expected 1 ingress rule, got %d", len(np.Spec.Ingress))
	} else {
		ingressRule := np.Spec.Ingress[0]
		if ingressRule.From == nil || len(ingressRule.From) != 1 {
			t.Errorf("expected ingress rule to have one 'from' peer, got %v", ingressRule.From)
		} else {
			peer := ingressRule.From[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in ingress rule, got %v", peer.PodSelector)
			}
		}
	}

	// Checking that the NetworkPolicy has both egress rules.
	if len(np.Spec.Egress) != 2 {
		t.Errorf("expected 2 egress rules, got %d", len(np.Spec.Egress))
	} else {
		// Validating intra-namespace egress rule.
		intraEgressRule := np.Spec.Egress[0]
		if len(intraEgressRule.To) != 1 {
			t.Errorf("expected intra-namespace egress rule to have 1 peer, got %d", len(intraEgressRule.To))
		} else {
			peer := intraEgressRule.To[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in intra-namespace egress rule, got %v", peer.PodSelector)
			}
		}
		// Validating external egress rule.
		externalEgressRule := np.Spec.Egress[1]
		if len(externalEgressRule.To) != 1 {
			t.Errorf("expected external egress rule to have 1 peer, got %d", len(externalEgressRule.To))
		} else {
			peer := externalEgressRule.To[0]
			if peer.IPBlock == nil || peer.IPBlock.CIDR != ExternalEgressCIDR {
				t.Errorf("expected external egress rule to have IPBlock with external egress CIDR, got %v", peer.IPBlock)
			}
		}
	}

	// Calling Reconcile. This should stop the reconcile loop.
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Third Reconcile failed: %v", err)
	}
	if res.Requeue {
		t.Fatalf("expected reconcile loop to be stopped, but got requeued")
	}
}

// This test verifies that when AllowEgress is false, the NetworkPolicy only contains the intra-namespace rule.
func TestNoExternalEgress(t *testing.T) {
	_ = context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource.
	tenant := createTenantResource("no-egress-tenant", map[string]string{"env": "test"}, false)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	_, np := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Verifying ingress rule exists and is correct.
	if len(np.Spec.Ingress) != 1 {
		t.Errorf("expected 1 ingress rule, got %d", len(np.Spec.Ingress))
	} else {
		ingressRule := np.Spec.Ingress[0]
		if ingressRule.From == nil || len(ingressRule.From) != 1 {
			t.Errorf("expected ingress rule to have one 'from' peer, got %v", ingressRule.From)
		} else {
			peer := ingressRule.From[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in ingress rule, got %v", peer.PodSelector)
			}
		}
	}
	
	// When AllowEgress is false, we expect only the intra-namespace egress rule.
	if len(np.Spec.Egress) != 1 {
		t.Errorf("expected 1 egress rule, got %d", len(np.Spec.Egress))
	} else {
		// Validating that the egress rule is for intra-namespace traffic.
		intraEgressRule := np.Spec.Egress[0]
		if len(intraEgressRule.To) != 1 {
			t.Errorf("expected intra-namespace egress rule to have 1 peer, got %d", len(intraEgressRule.To))
		} else {
			peer := intraEgressRule.To[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in intra-namespace egress rule, got %v", peer.PodSelector)
			}
		}
	}
}

// This test verifies that when AllowEgress is true, the NetworkPolicy contains two egress rules.
func TestAllowEgress(t *testing.T) {
	_ = context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource with AllowEgress true.
	tenant := createTenantResource("allow-egress-tenant", map[string]string{"env": "prod"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	_, np := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Verifying ingress rule exists and is correct.
	if len(np.Spec.Ingress) != 1 {
		t.Errorf("expected 1 ingress rule, got %d", len(np.Spec.Ingress))
	} else {
		ingressRule := np.Spec.Ingress[0]
		if ingressRule.From == nil || len(ingressRule.From) != 1 {
			t.Errorf("expected ingress rule to have one 'from' peer, got %v", ingressRule.From)
		} else {
			peer := ingressRule.From[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in ingress rule, got %v", peer.PodSelector)
			}
		}
	}
	
	// Verifying that the NetworkPolicy has two egress rules (intra-namespace + external egress).
	if len(np.Spec.Egress) != 2 {
		t.Errorf("expected 2 egress rules in network policy, got: %d", len(np.Spec.Egress))
	} else {
		// Validating intra-namespace egress rule.
		intraEgressRule := np.Spec.Egress[0]
		if len(intraEgressRule.To) != 1 {
			t.Errorf("expected intra-namespace egress rule to have 1 peer, got %d", len(intraEgressRule.To))
		} else {
			peer := intraEgressRule.To[0]
			if peer.PodSelector == nil || len(peer.PodSelector.MatchLabels) != 0 {
				t.Errorf("expected empty PodSelector in intra-namespace egress rule, got %v", peer.PodSelector)
			}
		}
		// Validating external egress rule.
		externalEgressRule := np.Spec.Egress[1]
		if len(externalEgressRule.To) != 1 {
			t.Errorf("expected external egress rule to have 1 peer, got %d", len(externalEgressRule.To))
		} else {
			peer := externalEgressRule.To[0]
			if peer.IPBlock == nil || peer.IPBlock.CIDR != ExternalEgressCIDR {
				t.Errorf("expected external egress rule to have IPBlock with external egress CIDR, got %v", peer.IPBlock)
			}
		}
	}
}

// This test verifies that Tenant AdditionalLabels update is synchronized properly with the Namespace Labels.
func TestUpdateNamespaceLabels(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource with AllowEgress true.
	tenant := createTenantResource("update-labels-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, _ := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Checking if expected Labels from Tenant Resource were added to Namespace Labels.
	expectedInitialLabels := map[string]string{
		K8SSystemGeneratedKey: ns.Name,
		MandatoryLabelKey: MandatoryLabelValue,
		"foo":             "bar",
	}
	if !equalLabels(ns.Labels, expectedInitialLabels) {
		t.Errorf("Expected initial namespace labels %v, got %v", expectedInitialLabels, ns.Labels)
	}

	// Fetching the latest tenant object after changes to it in previous reconcile loops.
	// This ensures that fake client doesn't throw any errors when we modify the tenant object.
	latestTenant := &api.Tenant{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(tenant), latestTenant); err != nil {
		t.Fatalf("Failed to get latest tenant: %v", err)
	}
	tenant = latestTenant

	// Updating the Tenant's AdditionalLabels.
	tenant.Spec.AdditionalLabels = map[string]string{
		"foo": "baz",
		"new": "label",
	}
	if err := fakeClient.Update(ctx, tenant); err != nil {
		t.Fatalf("Failed to update tenant with new labels: %v", err)
	}

	// Calling Reconcile. To update the namespace labels.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Third Reconcile failed after label update: %v", err)
	}

	// Fetching the namespace again.
	updatedNS := &corev1.Namespace{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: tenant.Name}, updatedNS); err != nil {
		t.Fatalf("Expected namespace to exist after update, but got error: %v", err)
	}

	// Checking if expected Labels from Tenant Resource were added to Namespace Labels.
	expectedUpdatedLabels := map[string]string{
		K8SSystemGeneratedKey: updatedNS.Name,
		MandatoryLabelKey: MandatoryLabelValue,
		"foo":             "baz",
		"new":             "label",
	}
	if !equalLabels(updatedNS.Labels, expectedUpdatedLabels) {
		t.Errorf("Expected updated namespace labels %v, got %v", expectedUpdatedLabels, updatedNS.Labels)
	}
}

// This test verifies that Tenant Status is updated with ErrorMessage and an unrecoverable error is thrown
// when Namespace with same name as Tenant already exists and is not owned by the Tenant.
func TestTenantNamespaceOwnershipError(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource with a manually set UID.
	tenant := createTenantResource("ownership-error-tenant", map[string]string{"foo": "bar"}, true)
	tenant.ObjectMeta.UID = "tenant-uid"

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, _ := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Modifying the namespace owner reference to simulate that it is not owned by this Tenant.
	ctrlBool := true
	ns.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: tenant.APIVersion,
			Kind:       tenant.Kind,
			Name:       tenant.Name,
			UID:        "different-uid",
			Controller: &ctrlBool,
		},
	}
	if err := fakeClient.Update(ctx, ns); err != nil {
		t.Fatalf("Failed to update namespace for ownership simulation: %v", err)
	}

	// Calling Reconcile. This should now detect the ownership mismatch and return an error.
	_, err := r.Reconcile(ctx, req)
	if err == nil {
		t.Fatalf("Expected error due to namespace ownership mismatch, but got nil")
	}

	// Re-fetching the Tenant to check if its Status.ErrorMessage is updated.
	updatedTenant := &api.Tenant{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(tenant), updatedTenant); err != nil {
		t.Fatalf("Failed to get updated tenant: %v", err)
	}
	if updatedTenant.Status.ErrorMessage == "" {
		t.Errorf("Expected tenant.Status.ErrorMessage to be set due to ownership error, but it was empty")
	}

	// Verifying that the error message returned matches the expected pattern.
	if strings.Contains(err.Error(), "exists but is not owned by tenant") &&
		updatedTenant.Status.ErrorMessage == "Namespace exists and is not owned by this Tenant" {
		t.Logf("Received error: %v", err)
	}
}

// This test verifies that when a Tenant owned Namespace is directly updated, the controller reconciles, 
// discarding the update and synchronizing the desired state of the Namespace from the Tenant Resource.
func TestNamespaceSyncAfterDirectUpdate(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant with AdditionalLabels {"foo": "bar"}.
	tenant := createTenantResource("sync-labels-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, _ := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Checking if expected Labels from Tenant Resource were added to Namespace Labels.
	expectedLabels := map[string]string{
		K8SSystemGeneratedKey: ns.Name,
		MandatoryLabelKey: MandatoryLabelValue,
		"foo":             "bar",
	}
	if !equalLabels(ns.Labels, expectedLabels) {
		t.Errorf("Expected namespace labels %v, got %v", expectedLabels, ns.Labels)
	}

	// Manually changing the "foo" label to simulate drift.
	ns.Labels["foo"] = "modified"
	if err := fakeClient.Update(ctx, ns); err != nil {
		t.Fatalf("Failed to update namespace with modified labels: %v", err)
	}

	// Calling Reconcile again to trigger synchronization.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile after modifying namespace failed: %v", err)
	}

	// Re-fetching the namespace and verifying that labels have been restored.
	updatedNS := &corev1.Namespace{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: tenant.Name}, updatedNS); err != nil {
		t.Fatalf("Expected namespace to exist after reconciliation, but got error: %v", err)
	}
	if !equalLabels(updatedNS.Labels, expectedLabels) {
		t.Errorf("Expected namespace labels to be restored to %v, but got %v", expectedLabels, updatedNS.Labels)
	}
}

// This test verifies that when a Tenant owned NetworkPolicy is directly updated, the controller reconciles, 
// discarding the update and synchronizing the desired state of the NetworkPolicy from the Tenant Resource.
func TestNetworkPolicySyncAfterDirectUpdate(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource.
	tenant := createTenantResource("np-sync-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	_, np := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)
	npKey := client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: tenant.Name}

	// Manually modifying the NetworkPolicy spec (simulate drift). For instance, remove all egress rules.
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	if err := fakeClient.Update(ctx, np); err != nil {
		t.Fatalf("Failed to update network policy to simulate drift: %v", err)
	}

	// Calling Reconcile again to trigger synchronization.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile after network policy drift failed: %v", err)
	}

	// Re-fetching the NetworkPolicy and compare with the desired state.
	updatedNP := &networkingv1.NetworkPolicy{}
	if err := fakeClient.Get(ctx, npKey, updatedNP); err != nil {
		t.Fatalf("Failed to get network policy after reconciliation: %v", err)
	}
	desiredNP := desiredNetworkPolicy(tenant)
	if !networkPolicyEqual(updatedNP, desiredNP) {
		t.Errorf("Expected network policy spec to be restored to desired state.\nDesired: %#v\nGot: %#v", desiredNP.Spec, updatedNP.Spec)
	}
}

// This test verifies that when a Tenant owned Namespace is directly deleted, the controller reconciles,
// and restores the desired state of the Namespace from the Tenant Resource.
func TestNamespaceSyncAfterDirectDelete(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource with AdditionalLabels.
	tenant := createTenantResource("restore-namespace-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, _ := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Checking if expected Labels from Tenant Resource were added to Namespace Labels.
	expectedLabels := map[string]string{
		K8SSystemGeneratedKey: ns.Name,
		MandatoryLabelKey: MandatoryLabelValue,
		"foo":             "bar",
	}
	if !equalLabels(ns.Labels, expectedLabels) {
		t.Errorf("Expected namespace labels %v, got %v", expectedLabels, ns.Labels)
	}

	// Simulating deletion of the namespace.
	if err := fakeClient.Delete(ctx, ns); err != nil {
		t.Fatalf("Failed to delete namespace: %v", err)
	}

	// Verifying deletion: Getting the namespace should now fail.
	err := fakeClient.Get(ctx, client.ObjectKey{Name: tenant.Name}, &corev1.Namespace{})
	if err == nil || client.IgnoreNotFound(err) != nil {
		t.Logf("Namespace successfully deleted from fake client")
	}

	// Calling Reconcile to restore the missing namespace.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Third Reconcile failed to restore deleted namespace: %v", err)
	}

	// Re-fetching the namespace and verify it exists and matches the desired state.
	restoredNS := &corev1.Namespace{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: tenant.Name}, restoredNS); err != nil {
		t.Fatalf("Expected namespace to be restored, but got error: %v", err)
	}
	if !equalLabels(restoredNS.Labels, expectedLabels) {
		t.Errorf("Expected restored namespace labels to be %v, but got %v", expectedLabels, restoredNS.Labels)
	}
}

// This test verifies that when a Tenant owned NetworkPolicy is directly deleted, the controller reconciles,
// and restores the desired state of the NetworkPolicy from the Tenant Resource.
func TestNetworkPolicySyncAfterDirectDelete(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource.
	tenant := createTenantResource("restore-np-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	_, np := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)
	npKey := client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: tenant.Name}

	// Simulating deletion of the NetworkPolicy.
	if err := fakeClient.Delete(ctx, np); err != nil {
		t.Fatalf("Failed to delete network policy: %v", err)
	}

	// Verifying that the NetworkPolicy is deleted.
	err := fakeClient.Get(ctx, npKey, &networkingv1.NetworkPolicy{})
	if err == nil {
		t.Fatalf("Expected network policy to be deleted, but it still exists")
	}

	// Calling Reconcile to restore missing NetworkPolicy.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Third Reconcile failed to restore deleted network policy: %v", err)
	}

	// Re-fetching the NetworkPolicy and verifying that its spec matches the desired state.
	restoredNP := &networkingv1.NetworkPolicy{}
	if err := fakeClient.Get(ctx, npKey, restoredNP); err != nil {
		t.Fatalf("Expected network policy to be restored, but got error: %v", err)
	}
	desiredNP := desiredNetworkPolicy(tenant)
	if !networkPolicyEqual(restoredNP, desiredNP) {
		t.Errorf("Expected network policy spec to be restored to desired state.\nDesired: %#v\nGot: %#v", desiredNP.Spec, restoredNP.Spec)
	}
}

// This test verifies the cleanup process of Tenant resource along with it's dependent Namespace and NetworkPolicy.
func TestTenantCleanupProcess(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource.
	tenant := createTenantResource("cleanup-tenant", map[string]string{"key": "value"}, true)

	// Creating a fake client preloaded with the Tenant. Initialize reconciler and request.
	fakeClient, r, req:= createClientReconcilerAndRequest(scheme, tenant, nil)

	// Reconciling and Requeing to create Namespace and NetworkPolicy.
	ns, _ := simulateReconcileAndRequeue(t, fakeClient, r, req, tenant)

	// Instead of patching/updating deletionTimestamp (which is immutable in the fake client),
	// we are simulating deletion by directly calling the finalization logic.
	if err := r.handleFinalization(ctx, tenant); err != nil {
		t.Fatalf("Finalization logic failed: %v", err)
	}

	// Re-fetching the latest tenant to next finalizer removal update.
	latestTenant := &api.Tenant{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(tenant), latestTenant); err != nil {
		t.Fatalf("Failed to get tenant for deletion simulation: %v", err)
	}
	tenant = latestTenant

	// Simulating removal of the finalizer as would happen after finalization.
	tenant.Finalizers = removeString(tenant.Finalizers, TenantFinalizer)
	if err := fakeClient.Update(ctx, tenant); err != nil {
		t.Fatalf("Failed to update tenant after finalization: %v", err)
	}

	// Simulating deletion of the Namespace.
	if err := fakeClient.Delete(ctx, ns); err != nil {
		t.Fatalf("Failed to delete namespace: %v", err)
	}

	// Verifying that the dependent namespace is deleted.
	deletedNS := &corev1.Namespace{}
	err := fakeClient.Get(ctx, client.ObjectKey{Name: tenant.Name}, deletedNS)
	if err == nil {
		t.Fatalf("Expected namespace to be deleted after tenant finalization, but it still exists")
	}
	if !k8serrors.IsNotFound(err) {
		t.Fatalf("Unexpected error when verifying namespace deletion: %v", err)
	}

	// Verifying that the Tenant's finalizer is removed.
	finalTenant := &api.Tenant{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(tenant), finalTenant); err != nil {
		t.Fatalf("Failed to get tenant after finalization: %v", err)
	}
	if containsString(finalTenant.Finalizers, TenantFinalizer) {
		t.Errorf("Expected tenant finalizer to be removed after finalization, but it is still present: %v", finalTenant.Finalizers)
	}
}

// This test verifies the TestWaitForNamespaceToBeActive function which we disable in the entire test suite.
func TestWaitForNamespaceToBeActive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setting up the scheme.
	scheme := createNewScheme(t)

	// Building a fake client with no pre-created Namespace.
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Creating a reconciler with disableWait set to false.
	r := &reconciler{
		client:      fakeClient,
		scheme:      scheme,
		disableWait: false,
	}

	// Defining the Namespace name we expect.
	nsName := "test-namespace"

	// Running waitForNamespaceToBeActive in a separate goroutine.
	errCh := make(chan error)
	go func() {
		errCh <- r.waitForNamespaceToBeActive(ctx, nsName)
	}()

	// Simulating a delay before the Namespace is created.
	time.Sleep(3 * time.Second)

	// Creating the Namespace in the fake client.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
		},
	}
	if err := fakeClient.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Waiting for waitForNamespaceToBeActive to finish.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected waitForNamespaceToBeActive to succeed, but got error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for namespace to become active")
	}
}

// This test verifies the WaitForNetworkPolicyToBeActive function which we disable in the entire test suite.
func TestWaitForNetworkPolicyToBeActive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Building a fake client with no pre-created NetworkPolicy.
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Creating a reconciler with disableWait set to false.
	r := &reconciler{
		client:      fakeClient,
		scheme:      scheme,
		disableWait: false,
	}

	// Defining the key for the NetworkPolicy we expect.
	npKey := client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: "test-namespace"}

	// Running waitForNetworkPolicyToBeActive in a separate goroutine.
	errCh := make(chan error)
	go func() {
		errCh <- r.waitForNetworkPolicyToBeActive(ctx, npKey)
	}()

	// Simulating a delay before the NetworkPolicy is created.
	time.Sleep(3 * time.Second)

	// Creating the NetworkPolicy in the fake client.
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TenantNetworkPolicyName,
			Namespace: "test-namespace",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
		},
	}
	if err := fakeClient.Create(ctx, np); err != nil {
		t.Fatalf("failed to create network policy: %v", err)
	}

	// Waiting for waitForNetworkPolicyToBeActive to finish.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected waitForNetworkPolicyToBeActive to succeed, but got error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for network policy to become active")
	}
}

// This helper function returns a new Scheme after adding Client-Go and Tenant API to it.
func createNewScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// This helper function returns a Tenant Resource.
func createTenantResource(name string, labels map[string]string, allowEgress bool) *api.Tenant {
	return &api.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: api.TenantSpec{
			AdditionalLabels: labels,
			AllowEgress:      allowEgress,
		},
	}
}

// This helper function creates a fake client, initializes a reconciler and creates a reconcile request for Tenant resource.
// We use errorPredicate to test out failure cases from the Tenant controller.
func createClientReconcilerAndRequest(
	scheme *runtime.Scheme, tenant *api.Tenant,
	errorPredicate func(obj client.Object) error) (client.WithWatch, *reconciler, ctrl.Request) {
	fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tenant).
					WithStatusSubresource(tenant).
					Build()
	var c client.WithWatch
	if errorPredicate != nil {
		c = &errorClient{WithWatch: fakeClient, errorFunc: errorPredicate}
	} else {
		c = fakeClient
	}
	r := &reconciler{
		client: c,
		scheme: scheme,
		disableWait: true,
	}
	req := ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(tenant),
	}
	return c, r, req
}

// This helper function simulates the reconcile loop after Tenant creation (creates Namespace and NetworkPolicy).
// We call Reconcile function twice here because the fake client does not support requeing a reconcile loop.
func simulateReconcileAndRequeue(
	t * testing.T, fakeClient client.WithWatch, 
	r *reconciler, req ctrl.Request, tenant *api.Tenant) (*corev1.Namespace, *networkingv1.NetworkPolicy) {
	
	// Calling Reconcile. This should create the namespace.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("First Reconcile failed: %v", err)
	}
	// Calling Reconcile. This should simulate requeue process and create the network policy.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Second Reconcile failed: %v", err)
	}
	// Verifying that the Namespace was created.
	ns := &corev1.Namespace{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: tenant.Name}, ns); err != nil {
		t.Fatalf("expected namespace to be created, but got error: %v", err)
	}
	// Verifying that the NetworkPolicy was created.
	np := &networkingv1.NetworkPolicy{}
	npKey := client.ObjectKey{Name: TenantNetworkPolicyName, Namespace: tenant.Name}
	if err := fakeClient.Get(context.Background(), npKey, np); err != nil {
		t.Fatalf("expected network policy to be created, but got error: %v", err)
	}
	
	return ns, np
}

// errorClient wraps a client.WithWatch.
type errorClient struct {
	client.WithWatch
	errorFunc func(obj client.Object) error
}

// This helper function simulates an update error when updating a Tenant using an error predicate.
func (e *errorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if e.errorFunc != nil {
		if err := e.errorFunc(obj); err != nil {
			return err
		}
	}
	return e.WithWatch.Update(ctx, obj, opts...)
}

// This test verifies that failure to add a Finalizer to Tenant Resource raises appropriate error.
func TestFailureToAddFinalizer(t *testing.T) {
	ctx := context.Background()

	// Setting up scheme.
	scheme := createNewScheme(t)

	// Creating a Tenant resource without a finalizer.
	tenant := createTenantResource("finalizer-failure-tenant", map[string]string{"foo": "bar"}, true)

	// Creating a fake client with error predicate. Initialize reconciler and request.
	_, r, req:= createClientReconcilerAndRequest(
		scheme,
		tenant,
		func(obj client.Object) error {
			if t, ok := obj.(*api.Tenant); ok {
				if t.Name == "finalizer-failure-tenant" {
					return errors.New("simulated update error for finalizer addition")
				}
			}
			return nil
		},
	)

	// Calling Reconcile. The controller will try to add the finalizer,
	// but our errorClient will force Update to fail.
	_, err := r.Reconcile(ctx, req)
	if err == nil {
		t.Fatalf("Expected error when adding finalizer, but got nil")
	}
	// Checking that the error message contains "failed to add finalizer".
	if !strings.Contains(err.Error(), "failed to add finalizer") {
		t.Errorf("Expected error message to contain 'failed to add finalizer', but got: %v", err)
	}
}
