package controller

import (
	"slices"
	"reflect"
	
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "github.com/shlokc9/akuity-tenant-controller/api/v1alpha1"
)

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
		udp := corev1.ProtocolUDP
		tcp := corev1.ProtocolTCP
		port := intstr.FromInt(53)
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: ExternalEgressCIDR,
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Protocol: &udp,
					Port: &port,
				},
				{
					Protocol: &tcp,
					Port: &port,
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
