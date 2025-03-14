package controller

const (
	// TenantFinalizer is used to ensure cleanup when a Tenant resource is deleted.
	TenantFinalizer = "tenant.finalizers.akuity.io"

	// Mandatory label key and value for namespaces created via a Tenant.
	K8SSystemGeneratedKey = "kubernetes.io/metadata.name"
	MandatoryLabelKey   = "akuity.io/tenant"
	MandatoryLabelValue = "true"

	// TenantNetworkPolicyName is the name given to the NetworkPolicy created for a Tenant.
	TenantNetworkPolicyName = "tenant-network-policy"

	// ExternalEgressCIDR is the CIDR block used to allow external traffic when enabled.
	ExternalEgressCIDR = "0.0.0.0/0"

	// Error Handling and Logging constants
	Tenant = "tenant"
	Namespace = "namespace"
	NetworkPolicy = "networkPolicy"
	DesiredLabels = "desiredLabels"

	// NetworkPolicy types
	IngressPolicyType = "Ingress"
	EgressPolicyType = "Egress"
)