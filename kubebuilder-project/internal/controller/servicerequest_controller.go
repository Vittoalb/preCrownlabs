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
	kubevirtv1 "kubevirt.io/api/core/v1"
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

	// Recupera la VirtualMachine associata
	vm := &kubevirtv1.VirtualMachine{}
	err = r.Get(ctx, req.NamespacedName, vm)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("VirtualMachine non trovata, potrebbe essere stata eliminata", "Name", req.Name, "Namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Errore nel recupero della VirtualMachine", "Name", req.Name, "Namespace", req.Namespace)
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
	log.Info("Porta assegnata alla VM", "AssignedPort", assignedPort)

	// Crea il servizio Kubernetes
	service := &corev1.Service{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      "service-fedora-nginx",
			Namespace: "ns1",
			Annotations: map[string]string{
				"metallb.universe.tf/address-pool":    "my-ip-pool",
				"metallb.universe.tf/allow-shared-ip": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"kubevirt.io/domain": "fedora-nginx",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(assignedPort), // Porta assegnata dal controller
					TargetPort: intstr.FromInt(22),  // Porta interna della VM
				},
			},
		},
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

	// Aggiorna lo stato della VM
	vm.Annotations["externalIP"] = "assigned-external-ip" // Sostituisci con l'IP assegnato
	vm.Annotations["assignedPort"] = fmt.Sprintf("%d", assignedPort)

	err = r.Update(ctx, vm)
	if err != nil {
		log.Error(err, "Errore nell'aggiornamento delle annotazioni della VirtualMachine", "Name", vm.Name, "Namespace", vm.Namespace)
		return ctrl.Result{}, err
	}
	log.Info("Annotazioni della VirtualMachine aggiornate con successo", "Name", vm.Name, "Namespace", vm.Namespace)

	return ctrl.Result{}, nil
}

// SetupWithManager configura il controller
func (r *ServiceRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ServiceRequest{}).
		Complete(r)
}
