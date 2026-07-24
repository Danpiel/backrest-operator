package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BackrestClusterSpec defines desired Backrest host + agents.
type BackrestClusterSpec struct {
	Version    string                 `json:"version,omitempty"`
	Image      string                 `json:"image,omitempty"`
	Host       BackrestHostSpec       `json:"host,omitempty"`
	Agents     BackrestAgentsSpec     `json:"agents,omitempty"`
	Auth       BackrestAuthSpec       `json:"auth,omitempty"`
	Monitoring BackrestMonitoringSpec `json:"monitoring,omitempty"`
	MCP        BackrestMCPSpec        `json:"mcp,omitempty"`
}

type BackrestHostSpec struct {
	Replicas           *int32                 `json:"replicas,omitempty"`
	ServiceType        string                 `json:"serviceType,omitempty"`
	EnableServiceLinks *bool                  `json:"enableServiceLinks,omitempty"`
	Ingress            BackrestIngressSpec    `json:"ingress,omitempty"`
	Persistence        BackrestPersistenceSpec `json:"persistence,omitempty"`
	Resources          map[string]interface{} `json:"resources,omitempty"`
	NodeSelector       map[string]string      `json:"nodeSelector,omitempty"`
	Tolerations        []map[string]interface{} `json:"tolerations,omitempty"`
}

type BackrestIngressSpec struct {
	Enabled   bool                     `json:"enabled,omitempty"`
	ClassName string                   `json:"className,omitempty"`
	Host      string                   `json:"host,omitempty"`
	TLS       []map[string]interface{} `json:"tls,omitempty"`
	// Annotations applied to the managed Ingress (cert-manager, external-dns, Traefik, …).
	Annotations map[string]string `json:"annotations,omitempty"`
	// BackendServiceName overrides the default Backrest host Service (e.g. oauth2-proxy).
	BackendServiceName string `json:"backendServiceName,omitempty"`
	// BackendServicePort defaults to the Backrest UI port when empty.
	BackendServicePort int32 `json:"backendServicePort,omitempty"`
}

type BackrestPersistenceSpec struct {
	Size             string `json:"size,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
}

type BackrestAgentsSpec struct {
	Enabled      *bool                    `json:"enabled,omitempty"`
	Mode         string                   `json:"mode,omitempty"`
	Replicas     *int32                   `json:"replicas,omitempty"`
	NodeSelector map[string]string        `json:"nodeSelector,omitempty"`
	Tolerations  []map[string]interface{} `json:"tolerations,omitempty"`
	Resources    map[string]interface{}   `json:"resources,omitempty"`
	Multihost    BackrestMultihostSpec    `json:"multihost,omitempty"`
}

type BackrestMultihostSpec struct {
	ServerURL   string   `json:"serverURL,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type BackrestAuthSpec struct {
	Enabled        bool   `json:"enabled,omitempty"`
	ExistingSecret string `json:"existingSecret,omitempty"`
}

type BackrestMonitoringSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

type BackrestMCPSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

type BackrestClusterStatus struct {
	Phase           string      `json:"phase,omitempty"`
	HostReady       bool        `json:"hostReady,omitempty"`
	AgentsReady     int32       `json:"agentsReady,omitempty"`
	AgentsDesired   int32       `json:"agentsDesired,omitempty"`
	MultihostPaired int32       `json:"multihostPaired,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackrestCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              BackrestClusterSpec   `json:"spec,omitempty"`
	Status            BackrestClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackrestClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackrestCluster `json:"items"`
}
