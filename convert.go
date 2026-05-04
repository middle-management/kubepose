package kubepose

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
)

type Transformer struct {
	Annotations map[string]string
	Labels      map[string]string
}

func (t Transformer) Convert(project *types.Project) (*Resources, error) {
	for _, name := range project.ServiceNames() {
		if err := validateService(project.Services[name]); err != nil {
			return nil, fmt.Errorf("service %q: %w", name, err)
		}
	}

	resources := &Resources{}

	secretMappings, err := t.processSecrets(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing secrets: %w", err)
	}

	configMappings, err := t.processConfigs(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing configs: %w", err)
	}

	volumeMappings, err := t.processVolumes(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing volumes: %w", err)
	}

	// Create a map to track created service accounts to avoid duplicates
	createdServiceAccounts := make(map[string]bool)

	// Process service accounts first
	for _, service := range project.Services {
		if saName, ok := service.Annotations[ServiceAccountNameAnnotationKey]; ok && saName != "" {
			if !createdServiceAccounts[saName] {
				resources.ServiceAccounts = append(resources.ServiceAccounts,
					t.createServiceAccount(saName, service))
				createdServiceAccounts[saName] = true
			}
		}
	}

	// Group services by kubepose.service.group
	groups := make(map[string][]types.ServiceConfig)
	for _, service := range project.Services {
		// Handle standalone pods (non-Always restart policy)
		if _, isCronJob := service.Annotations[CronJobScheduleAnnotationKey]; !isCronJob && getRestartPolicy(service) != corev1.RestartPolicyAlways && service.Annotations[ContainerTypeAnnotationKey] != "init" {
			pod := t.createPod(service)
			pod.Spec.Containers = []corev1.Container{t.createContainer(service)}
			t.updatePodSpecWithSecrets(&pod.Spec, service, secretMappings)
			t.updatePodSpecWithConfigs(&pod.Spec, service, configMappings)
			t.updatePodSpecWithVolumes(&pod.Spec, service, volumeMappings, resources)
			resources.Pods = append(resources.Pods, pod)
			continue
		}

		groupName := service.Annotations[ServiceGroupAnnotationKey]
		if groupName == "" {
			groupName = service.Name // Use service name as group if not specified
		}
		groups[groupName] = append(groups[groupName], service)
	}

	var groupNames []string
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	// Process groups in sorted order
	for _, groupName := range groupNames {
		services := groups[groupName]

		// Find main services (not init)
		var appServices, initServices []types.ServiceConfig
		for _, svc := range services {
			if svc.Annotations[ContainerTypeAnnotationKey] == "init" {
				initServices = append(initServices, svc)
			} else {
				appServices = append(appServices, svc)
			}
		}

		if len(appServices) == 0 {
			continue
		}

		// Sort services by name for consistent ordering
		sort.Slice(appServices, func(i, j int) bool {
			return appServices[i].Name < appServices[j].Name
		})
		sort.Slice(initServices, func(i, j int) bool {
			return initServices[i].Name < initServices[j].Name
		})

		for _, service := range appServices {
			var podSpec *corev1.PodSpec
			if _, ok := service.Annotations[CronJobScheduleAnnotationKey]; ok {
				cj := t.createCronJob(resources, service)
				podSpec = &cj.Spec.JobTemplate.Spec.Template.Spec
			} else if service.Deploy != nil && service.Deploy.Mode == "global" {
				ds := t.createDaemonSet(resources, service)
				podSpec = &ds.Spec.Template.Spec
			} else {
				deploy := t.createDeployment(resources, service)
				podSpec = &deploy.Spec.Template.Spec
			}
			t.addContainersToSpec(podSpec, appServices, initServices)
			for _, svc := range append(appServices, initServices...) {
				t.updatePodSpecWithSecrets(podSpec, svc, secretMappings)
				t.updatePodSpecWithConfigs(podSpec, svc, configMappings)
				t.updatePodSpecWithVolumes(podSpec, svc, volumeMappings, resources)
			}
			removeDuplicateVolumeMounts(podSpec.Containers)
			removeDuplicateVolumeMounts(podSpec.InitContainers)

			if len(service.Ports) > 0 {
				svc := t.createService(service)
				found := false
				for _, s := range resources.Services {
					if s.ObjectMeta.Name == svc.ObjectMeta.Name {
						found = true
						break
					}
				}
				if !found {
					resources.Services = append(resources.Services, svc)
					if _, ok := service.Annotations[ServiceExposeAnnotationKey]; ok {
						ingress := t.createIngress(service)
						if ingress == nil {
							continue
						}
						resources.Ingresses = append(resources.Ingresses, ingress)
					}
				}
			}
		}
	}

	return resources, nil
}

// validateService rejects service configurations that the converter cannot
// faithfully translate. Failing fast here keeps the rest of the conversion
// pipeline panic-free.
func validateService(service types.ServiceConfig) error {
	if service.HealthCheck != nil && len(service.HealthCheck.Test) > 0 {
		switch service.HealthCheck.Test[0] {
		case "CMD-SHELL", "CMD", "NONE":
		default:
			return fmt.Errorf("unsupported healthcheck test type %q (expected CMD, CMD-SHELL, or NONE)", service.HealthCheck.Test[0])
		}
	}
	if schedule, ok := service.Annotations[CronJobScheduleAnnotationKey]; ok && schedule == "" {
		return fmt.Errorf("%s must not be empty", CronJobScheduleAnnotationKey)
	}
	return nil
}

// getServiceName returns the kubernetes resource name for a compose service:
// the explicit group annotation if set, otherwise the service name itself.
func getServiceName(service types.ServiceConfig) string {
	if name := service.Annotations[ServiceGroupAnnotationKey]; name != "" {
		return name
	}
	return service.Name
}

func mergeMaps(maps ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}

var reEnvVars = regexp.MustCompile(`\$([a-zA-Z0-9.-_]+)`)

func escapeEnvs(input []string) []string {
	var args []string
	for _, arg := range input {
		args = append(args, reEnvVars.ReplaceAllString(arg, `$($1)`))
	}
	return args
}
