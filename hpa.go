package kubepose

import (
	"fmt"
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// hpaDefaultCpuUtilization is the target average CPU utilization (percent of
// the container's CPU request) used when kubepose.hpa.cpu is not set.
const hpaDefaultCpuUtilization = 80

// hasHorizontalPodAutoscaler reports whether the service opts into an HPA.
// kubepose.hpa.maxReplicas is the enabling annotation; the others refine it.
func hasHorizontalPodAutoscaler(service types.ServiceConfig) bool {
	_, ok := service.Annotations[HpaMaxReplicasAnnotationKey]
	return ok
}

// createHorizontalPodAutoscaler emits an autoscaling/v2 HPA targeting the
// service's Deployment, scaling on average CPU utilization.
//
// The Deployment's spec.replicas must be left unset when an HPA manages it
// (see createDeployment): a pinned value would reset the HPA's chosen scale
// on every apply. minReplicas defaults to deploy.replicas so an existing
// `deploy.replicas: N` service keeps its floor when the HPA is enabled.
func (t Transformer) createHorizontalPodAutoscaler(resources *Resources, service types.ServiceConfig) *autoscalingv2.HorizontalPodAutoscaler {
	serviceName := getServiceName(service)

	for _, hpa := range resources.HorizontalPodAutoscalers {
		if hpa.ObjectMeta.Name == serviceName {
			return hpa
		}
	}

	// Values are validated in validateService; parse errors cannot occur here.
	maxReplicas, _ := strconv.Atoi(service.Annotations[HpaMaxReplicasAnnotationKey])

	minReplicas := 1
	if value, ok := service.Annotations[HpaMinReplicasAnnotationKey]; ok {
		minReplicas, _ = strconv.Atoi(value)
	} else if service.Deploy != nil && service.Deploy.Replicas != nil {
		minReplicas = *service.Deploy.Replicas
	}

	cpuUtilization := hpaDefaultCpuUtilization
	if value, ok := service.Annotations[HpaCpuUtilizationAnnotationKey]; ok {
		cpuUtilization, _ = strconv.Atoi(value)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
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
				Kind:       "Deployment",
				Name:       serviceName,
			},
			MinReplicas: ptr.To(int32(minReplicas)),
			MaxReplicas: int32(maxReplicas),
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptr.To(int32(cpuUtilization)),
						},
					},
				},
			},
		},
	}
	resources.HorizontalPodAutoscalers = append(resources.HorizontalPodAutoscalers, hpa)
	return hpa
}

// validateHpaAnnotations rejects HPA annotation combinations the converter
// cannot faithfully translate. Called from validateService.
func validateHpaAnnotations(service types.ServiceConfig) error {
	maxValue, hasMax := service.Annotations[HpaMaxReplicasAnnotationKey]
	minValue, hasMin := service.Annotations[HpaMinReplicasAnnotationKey]
	cpuValue, hasCpu := service.Annotations[HpaCpuUtilizationAnnotationKey]

	if !hasMax {
		if hasMin || hasCpu {
			return fmt.Errorf("%s and %s have no effect without %s",
				HpaMinReplicasAnnotationKey, HpaCpuUtilizationAnnotationKey, HpaMaxReplicasAnnotationKey)
		}
		return nil
	}

	maxReplicas, err := strconv.Atoi(maxValue)
	if err != nil || maxReplicas < 1 {
		return fmt.Errorf("%s must be a positive integer, got %q", HpaMaxReplicasAnnotationKey, maxValue)
	}

	minReplicas := 1
	if hasMin {
		minReplicas, err = strconv.Atoi(minValue)
		if err != nil || minReplicas < 1 {
			return fmt.Errorf("%s must be a positive integer, got %q", HpaMinReplicasAnnotationKey, minValue)
		}
	} else if service.Deploy != nil && service.Deploy.Replicas != nil {
		minReplicas = *service.Deploy.Replicas
	}
	if minReplicas > maxReplicas {
		return fmt.Errorf("%s (%d) must not exceed %s (%d)",
			HpaMinReplicasAnnotationKey, minReplicas, HpaMaxReplicasAnnotationKey, maxReplicas)
	}

	if hasCpu {
		cpuUtilization, err := strconv.Atoi(cpuValue)
		if err != nil || cpuUtilization < 1 {
			return fmt.Errorf("%s must be a positive integer percentage, got %q", HpaCpuUtilizationAnnotationKey, cpuValue)
		}
	}

	// An HPA only targets Deployments; the other workload kinds have no scale
	// subresource in kubepose's output.
	if _, isCronJob := service.Annotations[CronJobScheduleAnnotationKey]; isCronJob {
		return fmt.Errorf("%s cannot be combined with %s", HpaMaxReplicasAnnotationKey, CronJobScheduleAnnotationKey)
	}
	if service.Deploy != nil && service.Deploy.Mode == "global" {
		return fmt.Errorf("%s cannot be used with deploy.mode: global", HpaMaxReplicasAnnotationKey)
	}

	// The CPU-utilization metric is a percentage of the container's CPU
	// request; without a reservation the HPA can never compute utilization.
	if service.Deploy == nil ||
		service.Deploy.Resources.Reservations == nil ||
		service.Deploy.Resources.Reservations.NanoCPUs.Value() <= 0 {
		return fmt.Errorf("%s requires deploy.resources.reservations.cpus (the HPA CPU target is a percentage of the request)", HpaMaxReplicasAnnotationKey)
	}

	return nil
}
