package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Service rappresenta un servizio con una porta assegnata
type Service struct {
	Name         string `json:"name"`
	TargetPort   int    `json:"targetPort"`
	AssignedPort int    `json:"assignedPort,omitempty"`
}

// ServiceRequestSpec definisce i campi di input per la richiesta
type ServiceRequestSpec struct {
	Namespace string    `json:"namespace"`
	VMName    string    `json:"vmName"`
	Services  []Service `json:"services"`
}

// ServiceRequestStatus tiene traccia dello stato della richiesta
type ServiceRequestStatus struct {
	Status        string    `json:"status,omitempty"`
	AssignedIP    string    `json:"assignedIP,omitempty"`
	AssignedPorts []Service `json:"assignedPorts,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ServiceRequest è la definizione della risorsa custom
type ServiceRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceRequestSpec   `json:"spec,omitempty"`
	Status ServiceRequestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServiceRequestList contiene una lista di ServiceRequest
type ServiceRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceRequest `json:"items"`
}
