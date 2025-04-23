package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceRequestSpec definisce i campi di input per la richiesta
type ServiceRequestSpec struct {
	Namespace     string `json:"namespace"`               // Namespace in cui creare il servizio
	App           string `json:"app"`                     // Nome dell'applicazione
	Component     string `json:"component"`               // Componente dell'applicazione
	TargetPort    int    `json:"targetPort"`              // Porta target specificata dall'utente
	AllowSharedIP bool   `json:"allowSharedIP,omitempty"` // Nuovo campo per abilitare l'IP sharing
}

// ServiceRequestStatus tiene traccia dello stato della richiesta
type ServiceRequestStatus struct {
	AssignedPort int    `json:"assignedPort,omitempty"` // Porta assegnata dinamicamente
	Status       string `json:"status,omitempty"`       // Stato della richiesta (es. "Created")
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
