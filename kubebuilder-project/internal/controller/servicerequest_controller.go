package controller

import (
	"context"
	"fmt"

	networkingv1alpha1 "github.com/your-repo/service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubevirtv1 "kubevirt.io/api/core/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ServiceRequestReconciler reconciles ServiceRequest resources
// -----------------------------------------------------------------------------

type ServiceRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	usedPorts     = make(map[int]bool)                                // porte in uso
	finalizerName = "servicerequest.networking.example.com/finalizer" // finalizer costante
)

// ----------------------------------------------------------------------------
// Reconcile
// ----------------------------------------------------------------------------

func (r *ServiceRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.Log.WithName("ServiceRequestController")

	// 1. Carica la risorsa -----------------------------------------------------------------
	sr := &networkingv1alpha1.ServiceRequest{}
	if err := r.Get(ctx, req.NamespacedName, sr); err != nil {
		if errors.IsNotFound(err) {
			log.Info("ServiceRequest non esiste più", "name", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Finalizer -------------------------------------------------------------------------
	if sr.ObjectMeta.DeletionTimestamp.IsZero() {
		// non in cancellazione → assicura finalizer
		if !controllerutil.ContainsFinalizer(sr, finalizerName) {
			controllerutil.AddFinalizer(sr, finalizerName)
			if err := r.Update(ctx, sr); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// in cancellazione → cleanup
		if controllerutil.ContainsFinalizer(sr, finalizerName) {
			log.Info("Cleanup risorse associate prima della rimozione", "name", sr.Name)

			// Elimina Service LB
			svc := &corev1.Service{}
			if err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("service-%s", sr.Name), Namespace: sr.Spec.Namespace}, svc); err == nil {
				_ = r.Delete(ctx, svc)
			}

			// Libera porte
			for _, p := range sr.Status.AssignedPorts {
				delete(usedPorts, p.AssignedPort)
			}

			// Rimuovi finalizer
			controllerutil.RemoveFinalizer(sr, finalizerName)
			if err := r.Update(ctx, sr); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil // stop reconciliation quando è in delete
	}

	// 3. Controllo esistenza risorse --------------------------------------------------------
	vm := &kubevirtv1.VirtualMachine{}
	vmKey := client.ObjectKey{Name: sr.Spec.VMName, Namespace: sr.Spec.Namespace}
	vmExists := r.Get(ctx, vmKey, vm) == nil

	svc := &corev1.Service{}
	svcKey := client.ObjectKey{Name: fmt.Sprintf("service-%s", sr.Name), Namespace: sr.Spec.Namespace}
	svcExists := r.Get(ctx, svcKey, svc) == nil

	// 3.a: se VM NON esiste ma Service sì  → cleanup & requeue
	if !vmExists && svcExists {
		log.Info("VM rimossa manualmente: elimino Service e rilascio porte", "service", svc.Name)
		if err := r.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		for _, p := range sr.Status.AssignedPorts {
			delete(usedPorts, p.AssignedPort)
		}
		// reset stato e riesegui reconcile
		sr.Status.Status = ""
		sr.Status.AssignedPorts = nil
		_ = r.Status().Update(ctx, sr)
		return ctrl.Result{Requeue: true}, nil
	}

	// 3.b: se VM e Service esistono già e stato == Created → nulla da fare
	if sr.Status.Status == "Created" && vmExists && svcExists {
		return ctrl.Result{}, nil
	}

	// 4. Assegna porte ---------------------------------------------------------------------
	assigned := []networkingv1alpha1.Service{}
	base := 30000
	for _, s := range sr.Spec.Services {
		port := s.Port
		if port == 0 {
			port = base
			for usedPorts[port] {
				port++
			}
		}
		usedPorts[port] = true
		assigned = append(assigned, networkingv1alpha1.Service{
			Name:         s.Name,
			TargetPort:   s.TargetPort,
			AssignedPort: port,
		})
	}

	// 5. Crea VM se manca ------------------------------------------------------------------
	runStrategy := kubevirtv1.RunStrategyAlways
	if !vmExists {
		vm = &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sr.Spec.VMName,
				Namespace: sr.Spec.Namespace,
				Labels:    map[string]string{"kubevirt.io/domain": sr.Spec.VMName},
			},
			Spec: kubevirtv1.VirtualMachineSpec{
				RunStrategy: &runStrategy,
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"kubevirt.io/domain": sr.Spec.VMName},
					},
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Domain: kubevirtv1.DomainSpec{
							Devices: kubevirtv1.Devices{
								Interfaces: []kubevirtv1.Interface{{
									Name: "default", InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{Masquerade: &kubevirtv1.InterfaceMasquerade{}}}},
							},
							Resources: kubevirtv1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1024Mi")},
							},
						},
						Networks: []kubevirtv1.Network{{Name: "default", NetworkSource: kubevirtv1.NetworkSource{Pod: &kubevirtv1.PodNetwork{}}}},
					},
				},
			},
		}
		if err := r.Create(ctx, vm); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 5.a: ServiceRequest diventa FIGLIA della VM per GC automatico -------------------------
	if err := controllerutil.SetControllerReference(vm, sr, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Update(ctx, sr); err != nil {
		return ctrl.Result{}, err
	}

	// 6. Crea/aggiorna Service --------------------------------------------------------------
	if !svcExists {
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("service-%s", sr.Name),
				Namespace: sr.Spec.Namespace,
				Annotations: map[string]string{
					"metallb.universe.tf/address-pool":    "my-ip-pool",
					"metallb.universe.tf/allow-shared-ip": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeLoadBalancer,
				Selector: map[string]string{"kubevirt.io/domain": sr.Spec.VMName},
			},
		}
		for _, p := range assigned {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       p.Name,
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(p.AssignedPort),
				TargetPort: intstr.FromInt(p.TargetPort),
			})
		}
		// SR è la owner del Service per cleanup via finalizer
		if err := controllerutil.SetControllerReference(sr, svc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, svc); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 7. Aggiorna Status --------------------------------------------------------------------
	sr.Status.Status = "Created"
	sr.Status.AssignedPorts = assigned
	if err := r.Status().Update(ctx, sr); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ----------------------------------------------------------------------------
// Setup with Manager
// ----------------------------------------------------------------------------

func (r *ServiceRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ServiceRequest{}).
		Complete(r)
}

/* disapplica tutto
kubectl delete servicerequest --all -n default
kubectl delete services --all -n ns1
kubectl delete virtualmachines --all -n ns1
*/
