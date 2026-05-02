package kubepose

import (
	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func (t Transformer) addContainersToSpec(podSpec *corev1.PodSpec, appServices, initServices []types.ServiceConfig) {
nextInitService:
	for _, svc := range initServices {
		for _, container := range podSpec.InitContainers {
			if container.Name == svc.Name {
				continue nextInitService
			}
		}
		podSpec.InitContainers = append(podSpec.InitContainers, t.createContainer(svc))
	}
nextService:
	for _, svc := range appServices {
		for _, container := range podSpec.Containers {
			if container.Name == svc.Name {
				continue nextService
			}
		}
		podSpec.Containers = append(podSpec.Containers, t.createContainer(svc))
	}
}

func (t Transformer) createPod(service types.ServiceConfig) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: t.createPodSpec(service),
	}
}

func (t Transformer) createDaemonSet(resources *Resources, service types.ServiceConfig) *appsv1.DaemonSet {
	serviceName := getServiceName(service)

	for _, ds := range resources.DaemonSets {
		if ds.ObjectMeta.Name == serviceName {
			return ds
		}
	}

	ds := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: getUpdateStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: getMatchLabels(service),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: mergeMaps(service.Annotations, t.Annotations),
					Labels: mergeMaps(service.Labels, map[string]string{
						AppSelectorLabelKey: serviceName,
					}),
				},
				Spec: t.createPodSpec(service),
			},
		},
	}
	resources.DaemonSets = append(resources.DaemonSets, ds)
	return ds
}

func (t Transformer) createDeployment(resources *Resources, service types.ServiceConfig) *appsv1.Deployment {
	serviceName := getServiceName(service)

	for _, d := range resources.Deployments {
		if d.ObjectMeta.Name == serviceName {
			return d
		}
	}

	var replicas *int32
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = ptr.To(int32(*service.Deploy.Replicas))
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Strategy: getDeploymentStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: getMatchLabels(service),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: mergeMaps(service.Annotations, t.Annotations),
					Labels: mergeMaps(service.Labels, map[string]string{
						AppSelectorLabelKey: serviceName,
					}),
				},
				Spec: t.createPodSpec(service),
			},
		},
	}
	resources.Deployments = append(resources.Deployments, d)
	return d
}

func getDeploymentStrategy(service types.ServiceConfig) appsv1.DeploymentStrategy {
	if service.Deploy != nil && service.Deploy.UpdateConfig != nil {
		updateConfig := service.Deploy.UpdateConfig

		var maxSurge *intstr.IntOrString
		if updateConfig.Parallelism != nil {
			maxSurge = &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(ptr.Deref(updateConfig.Parallelism, 0)),
			}
		}

		switch updateConfig.Order {
		case "stop-first":
			return appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			}
		case "start-first":
			return appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: maxSurge,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			}
		default: // default to RollingUpdate with some unavailability allowed
			return appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: maxSurge,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
		}
	}

	return appsv1.DeploymentStrategy{}
}

func getUpdateStrategy(service types.ServiceConfig) appsv1.DaemonSetUpdateStrategy {
	if service.Deploy != nil && service.Deploy.UpdateConfig != nil {
		updateConfig := service.Deploy.UpdateConfig

		var parallelism *intstr.IntOrString
		if updateConfig.Parallelism != nil {
			parallelism = &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(ptr.Deref(updateConfig.Parallelism, 0)),
			}
		}

		switch updateConfig.Order {
		case "start-first":
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: parallelism,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			}
		case "stop-first":
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxUnavailable: parallelism,
				},
			}
		default:
			// Default to allowing both surge and unavailability
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: parallelism,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
		}
	}

	return appsv1.DaemonSetUpdateStrategy{}
}
