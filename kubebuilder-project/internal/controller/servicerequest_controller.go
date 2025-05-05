package controller

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	networkingv1alpha1 "github.com/your-repo/service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// Controlla se la VM è già stata creata
	if serviceRequest.Status.Status == "Created" {
		log.Info("La VM è già stata creata", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)
		return ctrl.Result{}, nil
	}

	// Assegna porte dinamiche per i servizi
	rand.Seed(time.Now().UnixNano())
	assignedPorts := []networkingv1alpha1.Service{}
	basePort := 30000 // Porta iniziale per l'assegnazione dinamica

	for i, service := range serviceRequest.Spec.Services {
		assignedPort := basePort + i
		assignedPorts = append(assignedPorts, networkingv1alpha1.Service{
			Name:         service.Name,
			TargetPort:   service.TargetPort,
			AssignedPort: assignedPort,
		})
		log.Info("Porta assegnata", "Service", service.Name, "AssignedPort", assignedPort)
	}

	// Crea la VM
	runStrategy := kubevirtv1.RunStrategyAlways
	if serviceRequest.Spec.VMName == "" {
		log.Error(nil, "Spec.VMName è vuoto. Impossibile creare la VM.")
		return ctrl.Result{}, fmt.Errorf("spec.vmName è richiesto ma è vuoto")
	}

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: ctrl.ObjectMeta{
			//GenerateName: fmt.Sprintf("vm-%s-", serviceRequest.Name), // Usa generateName per creare un nome univoco
			Name:      serviceRequest.Spec.VMName,
			Namespace: serviceRequest.Spec.Namespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			RunStrategy: &runStrategy, // Modifica qui
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{
									Name: "containerdisk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{
											Bus: "virtio",
										},
									},
								},
								{
									Name: "cloudinitdisk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{
											Bus: "virtio",
										},
									},
								},
							},
							Interfaces: []kubevirtv1.Interface{
								{
									Name: "default",
									InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{
										Masquerade: &kubevirtv1.InterfaceMasquerade{},
									},
								},
							},
						},
						Resources: kubevirtv1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1024Mi"), // Modifica qui
							},
						},
					},
					Networks: []kubevirtv1.Network{
						{
							Name: "default",
							NetworkSource: kubevirtv1.NetworkSource{
								Pod: &kubevirtv1.PodNetwork{},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "containerdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "kubevirt/fedora-cloud-container-disk-demo",
								},
							},
						},
						{
							Name: "cloudinitdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
									UserData: `#cloud-config
	password: fedora
	chpasswd: { expire: False }
	ssh_pwauth: True
	packages:
	  - nginx
	runcmd:
	  - echo "Ciao mondo" > /usr/share/nginx/html/index.html
	  - systemctl enable nginx
	  - systemctl start nginx
	`,
								},
							},
						},
					},
				},
			},
		},
	}

	log.Info("Creazione della VM", "Name", vm.ObjectMeta.Name, "Namespace", vm.ObjectMeta.Namespace)

	existingVM := &kubevirtv1.VirtualMachine{}
	err = r.Get(ctx, client.ObjectKey{Name: vm.Name, Namespace: vm.Namespace}, existingVM)
	if err == nil {
		log.Info("La VM esiste già, salto la creazione", "VMName", vm.Name, "Namespace", vm.Namespace)
	} else if errors.IsNotFound(err) {
		err = r.Create(ctx, vm)
		if err != nil {
			log.Error(err, "Errore nella creazione della VM", "VMName", vm.Name, "Namespace", vm.Namespace)
			return ctrl.Result{}, err
		}
		log.Info("VM creata con successo", "VMName", vm.Name, "Namespace", vm.Namespace)
	} else {
		log.Error(err, "Errore nel recuperare la VM esistente")
		return ctrl.Result{}, err
	}

	// Crea il servizio Kubernetes con MetalLB
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
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"kubevirt.io/domain": serviceRequest.Spec.VMName,
			},
			Ports: []corev1.ServicePort{},
		},
	}

	for _, assignedPort := range assignedPorts {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name:       assignedPort.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(assignedPort.AssignedPort),
			TargetPort: intstr.FromInt(assignedPort.TargetPort),
		})
	}

	err = r.Create(ctx, service)
	if err != nil {
		log.Error(err, "Errore nella creazione del servizio Kubernetes", "ServiceName", service.Name, "Namespace", service.Namespace)
		return ctrl.Result{}, err
	}
	log.Info("Servizio Kubernetes creato con successo", "ServiceName", service.Name, "Namespace", service.Namespace)

	// Aggiorna lo stato della ServiceRequest
	serviceRequest.Status.Status = "Created"
	serviceRequest.Status.AssignedPorts = assignedPorts
	err = r.Status().Update(ctx, serviceRequest)
	if err != nil {
		log.Error(err, "Errore nell'aggiornamento dello stato della ServiceRequest", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)
		return ctrl.Result{}, err
	}
	log.Info("Stato della ServiceRequest aggiornato con successo", "Name", serviceRequest.Name, "Namespace", serviceRequest.Spec.Namespace)

	return ctrl.Result{}, nil
}

// SetupWithManager configura il controller
func (r *ServiceRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ServiceRequest{}).
		Complete(r)
}
