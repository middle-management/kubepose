package kubepose

import (
	"fmt"
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// hasHPA reports whether the service requested HPA generation via annotations.
func hasHPA(service types.ServiceConfig) bool {
	_, hasMin := service.Annotations[HPAMinReplicasAnnotationKey]
	_, hasMax := service.Annotations[HPAMaxReplicasAnnotationKey]
	return hasMin || hasMax
}

// createHorizontalPodAutoscaler emits an HPA targeting the given workload kind
// (Deployment or StatefulSet).
func (t Transformer) createHorizontalPodAutoscaler(service types.ServiceConfig, targetKind string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	serviceName := getServiceName(service)

	maxReplicas, err := positiveIntAnnotation(service, HPAMaxReplicasAnnotationKey, 0)
	if err != nil {
		return nil, err
	}
	if maxReplicas == 0 {
		return nil, fmt.Errorf("hpa requires %s to be set to a positive integer", HPAMaxReplicasAnnotationKey)
	}

	minReplicas, err := positiveIntAnnotation(service, HPAMinReplicasAnnotationKey, 1)
	if err != nil {
		return nil, err
	}
	if minReplicas > maxReplicas {
		return nil, fmt.Errorf("hpa minReplicas (%d) is greater than maxReplicas (%d)", minReplicas, maxReplicas)
	}

	var metrics []autoscalingv2.MetricSpec
	if v, ok := service.Annotations[HPATargetCPUUtilizationAnnotationKey]; ok {
		cpu, err := strconv.Atoi(v)
		if err != nil || cpu <= 0 {
			return nil, fmt.Errorf("invalid %s %q: must be a positive integer percentage", HPATargetCPUUtilizationAnnotationKey, v)
		}
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: ptr.To(int32(cpu)),
				},
			},
		})
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       targetKind,
				Name:       serviceName,
			},
			MinReplicas: ptr.To(int32(minReplicas)),
			MaxReplicas: int32(maxReplicas),
			Metrics:     metrics,
		},
	}, nil
}

func positiveIntAnnotation(service types.ServiceConfig, key string, defaultValue int) (int, error) {
	v, ok := service.Annotations[key]
	if !ok {
		return defaultValue, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive integer", key, v)
	}
	return n, nil
}
