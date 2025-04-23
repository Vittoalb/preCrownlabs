package controller

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	networkingv1alpha1 "github.com/your-repo/service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile implementa la logica del controller
func (r *ServiceRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.Log.WithName("ServiceRequestController")

	// Recupera la risorsa ServiceRequest
	serviceRequest := &networkingv1alpha1.ServiceRequest{}
	err := r.Get(ctx, req.NamespacedName, serviceRequest)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("ServiceRequest non trovato, potrebbe essere stato eliminato", "Name", req.Name, "Namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Errore nel recupero della ServiceRequest", "Name", req.Name, "Namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	// Controlla se il servizio è già stato creato
	if serviceRequest.Status.Status == "Created" {
		log.Info("Il servizio è già stato creato", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)
		return ctrl.Result{}, nil
	}

	// Assegna una porta casuale
	rand.Seed(time.Now().UnixNano())
	assignedPort := rand.Intn(10000) + 30000 // Porta tra 30000 e 40000
	log.Info("Porta assegnata al servizio", "AssignedPort", assignedPort)

	// Crea il servizio Kubernetes
	service := &corev1.Service{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      fmt.Sprintf("service-%s", serviceRequest.Name),
			Namespace: serviceRequest.Spec.Namespace,
			Annotations: map[string]string{
				"metallb.universe.tf/address-pool": "my-ip-pool",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(assignedPort),
					TargetPort: intstr.FromInt(serviceRequest.Spec.TargetPort), // Usa il valore specificato
				},
			},
			Selector: map[string]string{
				"app":       serviceRequest.Spec.App,
				"component": serviceRequest.Spec.Component,
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	// Aggiungi l'annotazione per l'IP sharing se allowSharedIP è true
	if serviceRequest.Spec.AllowSharedIP {
		service.Annotations["metallb.universe.tf/allow-shared-ip"] = "true"
	}

	err = r.Create(ctx, service)
	if err != nil {
		log.Error(err, "Errore nella creazione del servizio Kubernetes", "ServiceName", service.Name, "Namespace", service.Namespace)
		return ctrl.Result{}, err
	}
	log.Info("Servizio Kubernetes creato con successo", "ServiceName", service.Name, "Namespace", service.Namespace)

	// Aggiorna lo stato della ServiceRequest
	serviceRequest.Status.AssignedPort = assignedPort
	serviceRequest.Status.Status = "Created"
	err = r.Status().Update(ctx, serviceRequest)
	if err != nil {
		log.Error(err, "Errore nell'aggiornamento dello stato della ServiceRequest", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)
		return ctrl.Result{}, err
	}
	log.Info("Stato della ServiceRequest aggiornato con successo", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace, "AssignedPort", assignedPort)

	return ctrl.Result{}, nil
}

// SetupWithManager configura il controller
func (r *ServiceRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ServiceRequest{}).
		Complete(r)
}
