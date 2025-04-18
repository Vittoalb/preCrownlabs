package net

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	netv1alpha1 "cloudprog-2025\operators\project\api\net\v1alpha1\publicserviceinstance_types.go"
)

// PublicServiceInstanceReconciler reconciles a PublicServiceInstance object
type PublicServiceInstanceReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=net.super.dev,resources=publicserviceinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=net.super.dev,resources=publicserviceinstances/status,verbs=get;update;patch

func (r *PublicServiceInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    instance := &netv1alpha1.PublicServiceInstance{}
    err := r.Get(ctx, req.NamespacedName, instance)
    if err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // Check if the Pod already exists
    pod := &corev1.Pod{}
    err = r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServiceName, Namespace: instance.Namespace}, pod)
    if err != nil && errors.IsNotFound(err) {
        // Create a new Pod
        pod = &corev1.Pod{
            ObjectMeta: ctrl.ObjectMeta{
                Name:      instance.Spec.ServiceName,
                Namespace: instance.Namespace,
            },
            Spec: corev1.PodSpec{
                Containers: []corev1.Container{
                    {
                        Name:  instance.Spec.ServiceName,
                        Image: instance.Spec.Image,
                        Ports: []corev1.ContainerPort{
                            {
                                ContainerPort: instance.Spec.TargetPort,
                            },
                        },
                    },
                },
            },
        }
        if err := r.Create(ctx, pod); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Check if the Service already exists
    service := &corev1.Service{}
    err = r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServiceName, Namespace: instance.Namespace}, service)
    if err != nil && errors.IsNotFound(err) {
        // Assign an external IP from MetalLB's pool
        externalIP := "172.18.0.240" // Example: You can implement logic to pick an available IP dynamically

        // Create a new Service
        service = &corev1.Service{
            ObjectMeta: ctrl.ObjectMeta{
                Name:      instance.Spec.ServiceName,
                Namespace: instance.Namespace,
            },
            Spec: corev1.ServiceSpec{
                Selector: map[string]string{
                    "app": instance.Spec.ServiceName,
                },
                Ports: []corev1.ServicePort{
                    {
                        Port:       instance.Spec.Port,
                        TargetPort: intstr.FromInt(int(instance.Spec.TargetPort)),
                    },
                },
                Type:      corev1.ServiceTypeLoadBalancer,
                ExternalIPs: []string{externalIP},
            },
        }
        if err := r.Create(ctx, service); err != nil {
            return ctrl.Result{}, err
        }

        // Update the status with the assigned external IP
        instance.Status.ExternalIP = externalIP
        if err := r.Status().Update(ctx, instance); err != nil {
            return ctrl.Result{}, err
        }
    }

    return ctrl.Result{}, nil
}

func (r *PublicServiceInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&netv1alpha1.PublicServiceInstance{}).
        Complete(r)
}