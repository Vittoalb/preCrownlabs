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

type ServiceRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var usedPorts = make(map[int]bool) // Mappa per tracciare le porte già utilizzate
const finalizerName = "servicerequest.networking.example.com/finalizer"

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

	// FINALIZER: Cleanup prima della cancellazione
	if serviceRequest.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(serviceRequest, finalizerName) {
			controllerutil.AddFinalizer(serviceRequest, finalizerName)
			if err := r.Update(ctx, serviceRequest); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(serviceRequest, finalizerName) {
			log.Info("Pulizia risorse prima della rimozione", "Name", serviceRequest.Name)

			// Cancella il servizio Kubernetes associato
			service := &corev1.Service{}
			svcErr := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("service-%s", serviceRequest.Name), Namespace: serviceRequest.Spec.Namespace}, service)
			if svcErr == nil {
				_ = r.Delete(ctx, service)
			}

			// Libera le porte
			for _, svc := range serviceRequest.Status.AssignedPorts {
				log.Info("Rilascio porta", "Port", svc.AssignedPort)
				delete(usedPorts, svc.AssignedPort)
			}

			// Rimuove il finalizer
			controllerutil.RemoveFinalizer(serviceRequest, finalizerName)
			if err := r.Update(ctx, serviceRequest); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Verifica esistenza risorse
	existingVM := &kubevirtv1.VirtualMachine{}
	err = r.Get(ctx, client.ObjectKey{Name: serviceRequest.Spec.VMName, Namespace: serviceRequest.Spec.Namespace}, existingVM)
	vmExists := err == nil

	// Aggiungi OwnerReference al tuo ServiceRequest (fa sì che la VM possieda la ServiceRequest)
	if vmExists {
		controllerutil.SetControllerReference(existingVM, serviceRequest, r.Scheme)
	}

	// Verifica se il Service esiste realmente
	existingService := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("service-%s", serviceRequest.Name), Namespace: serviceRequest.Spec.Namespace}, existingService)
	serviceExists := err == nil

	// Se status è "Created" ma le risorse NON esistono realmente, azzera lo stato
	if serviceRequest.Status.Status == "Created" && (!vmExists || !serviceExists) {
		log.Info("VM o Service mancante, reset dello stato per ricreare le risorse", "VM esiste", vmExists, "Service esiste", serviceExists)
		serviceRequest.Status.Status = ""
		serviceRequest.Status.AssignedPorts = nil
		err = r.Status().Update(ctx, serviceRequest)
		if err != nil {
			log.Error(err, "Errore nel reset dello stato")
			return ctrl.Result{}, err
		}
		// Requeue per rieseguire il reconcile
		return ctrl.Result{Requeue: true}, nil
	}

	// Se tutto esiste e status è "Created", esci
	if serviceRequest.Status.Status == "Created" {
		log.Info("La VM e il Service esistono già", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)
		return ctrl.Result{}, nil
	}

	// Assegna porte
	assignedPorts := []networkingv1alpha1.Service{}
	basePort := 30000 // Porta iniziale per l'assegnazione dinamica

	// Verifica se ci sono porte già utilizzate
	for _, svc := range serviceRequest.Spec.Services {
		var assignedPort int

		if svc.Port == 0 {
			assignedPort = basePort
			for usedPorts[assignedPort] {
				assignedPort++
			}
		} else {
			assignedPort = svc.Port
		}

		usedPorts[assignedPort] = true
		assignedPorts = append(assignedPorts, networkingv1alpha1.Service{
			Name:         svc.Name,
			TargetPort:   svc.TargetPort,
			AssignedPort: assignedPort,
		})
		log.Info("Porta assegnata", "Service", svc.Name, "AssignedPort", assignedPort)
	}

	// Crea VM
	runStrategy := kubevirtv1.RunStrategyAlways
	if serviceRequest.Spec.VMName == "" {
		return ctrl.Result{}, fmt.Errorf("spec.vmName è richiesto ma è vuoto")
	}

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      serviceRequest.Spec.VMName,
			Namespace: serviceRequest.Spec.Namespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			RunStrategy: &runStrategy,
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kubevirt.io/domain": serviceRequest.Spec.VMName},
				},
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "containerdisk", DiskDevice: kubevirtv1.DiskDevice{Disk: &kubevirtv1.DiskTarget{Bus: "virtio"}}},
								{Name: "cloudinitdisk", DiskDevice: kubevirtv1.DiskDevice{Disk: &kubevirtv1.DiskTarget{Bus: "virtio"}}},
							},
							Interfaces: []kubevirtv1.Interface{
								{Name: "default", InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{Masquerade: &kubevirtv1.InterfaceMasquerade{}}},
							},
						},
						Resources: kubevirtv1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1024Mi"),
							},
						},
					},
					Networks: []kubevirtv1.Network{
						{Name: "default", NetworkSource: kubevirtv1.NetworkSource{Pod: &kubevirtv1.PodNetwork{}}},
					},
					Volumes: []kubevirtv1.Volume{
						{Name: "containerdisk", VolumeSource: kubevirtv1.VolumeSource{
							ContainerDisk: &kubevirtv1.ContainerDiskSource{Image: "kubevirt/fedora-cloud-container-disk-demo"},
						}},
						{Name: "cloudinitdisk", VolumeSource: kubevirtv1.VolumeSource{
							CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
								UserData: `#cloud-config
package_update: true
packages:
  - nginx
  - openssh-server
  - openssh-clients
ssh_pwauth: true
disable_root: false
users:
  - name: fedora
    groups: sudo
    shell: /bin/bash
    sudo: ["ALL=(ALL) NOPASSWD:ALL"]
    lock_passwd: false
chpasswd:
  list: |
    fedora:fedora
  expire: False
runcmd:
  - echo "Ciao mondo" > /usr/share/nginx/html/index.html
  - systemctl enable sshd
  - systemctl start sshd
  - systemctl enable nginx
  - systemctl start nginx`,
							},
						}},
					},
				},
			},
		},
	}

	if !vmExists {
		if err = r.Create(ctx, vm); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Crea Service
	service := &corev1.Service{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      fmt.Sprintf("service-%s", serviceRequest.Name),
			Namespace: serviceRequest.Spec.Namespace,
			Annotations: map[string]string{
				"metallb.universe.tf/address-pool":    "my-ip-pool",
				"metallb.universe.tf/allow-shared-ip": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{"kubevirt.io/domain": serviceRequest.Spec.VMName},
			Ports:    []corev1.ServicePort{},
		},
	}

	for _, svc := range assignedPorts {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name:       svc.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(svc.AssignedPort),
			TargetPort: intstr.FromInt(svc.TargetPort),
		})
	}

	// Aggiungi OwnerReference al Service (ServiceRequest è il padre)
	controllerutil.SetControllerReference(serviceRequest, service, r.Scheme)

	if err = r.Create(ctx, service); err != nil {
		return ctrl.Result{}, err
	}

	// Aggiorna stato
	serviceRequest.Status.Status = "Created"
	serviceRequest.Status.AssignedPorts = assignedPorts
	if err = r.Status().Update(ctx, serviceRequest); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

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
