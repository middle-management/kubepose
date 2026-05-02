package kubepose

import (
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TODO TCP and GRPC health checks.
func getProbes(service types.ServiceConfig) (liveness *corev1.Probe, readiness *corev1.Probe, startup *corev1.Probe) {
	if service.HealthCheck != nil && service.HealthCheck.Disable {
		return nil, nil, nil
	}

	var probe *corev1.Probe

	// Convert test command
	if service.HealthCheck != nil && len(service.HealthCheck.Test) > 0 {
		var command []string
		switch service.HealthCheck.Test[0] {
		case "CMD-SHELL":
			command = []string{"/bin/sh", "-c", strings.Join(service.HealthCheck.Test[1:], " ")}
		case "CMD":
			command = service.HealthCheck.Test[1:]
			if len(command) == 1 {
				command = splitCommand(command[0])
			}
		case "NONE":
			return nil, nil, nil
		default:
			// Unsupported types are rejected upfront by validateService;
			// fall through here defensively so callers never panic.
			return nil, nil, nil
		}

		probe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: command,
				},
			},
		}
	}

	if probe != nil && service.HealthCheck != nil {
		// Convert timing parameters
		if service.HealthCheck.Interval != nil {
			probe.PeriodSeconds = int32(time.Duration(*service.HealthCheck.Interval).Seconds())
		}
		if service.HealthCheck.Timeout != nil {
			probe.TimeoutSeconds = int32(time.Duration(*service.HealthCheck.Timeout).Seconds())
		}
		if service.HealthCheck.Retries != nil {
			probe.FailureThreshold = int32(*service.HealthCheck.Retries)
		}

		// Handle startup probe if start_period and/or start_interval are specified
		if service.HealthCheck.StartInterval != nil {
			startup = probe.DeepCopy()
			startup.PeriodSeconds = int32(time.Duration(*service.HealthCheck.StartInterval).Seconds())
			startup.InitialDelaySeconds = 0
			startup.FailureThreshold = 0

			if service.HealthCheck.StartPeriod != nil {
				startPeriodSeconds := int32(time.Duration(*service.HealthCheck.StartPeriod).Seconds())
				startup.FailureThreshold = max(startPeriodSeconds/startup.PeriodSeconds, 1)
			}

		} else if service.HealthCheck.StartPeriod != nil {
			// If only start_period is specified, use it as initial delay for liveness probe
			probe.InitialDelaySeconds = int32(time.Duration(*service.HealthCheck.StartPeriod).Seconds())
		}

		// Use the same probe for both liveness and readiness
		liveness = probe.DeepCopy()
		readiness = probe.DeepCopy()
	}

	// Check for HTTP-specific health check annotations
	if path, ok := service.Annotations[HealthcheckHttpGetPathAnnotationKey]; ok {
		httpGetPort := getFirstPort(service)
		if port, ok := service.Annotations[HealthcheckHttpGetPortAnnotationKey]; ok {
			if p, err := strconv.Atoi(port); err == nil {
				httpGetPort = p
			}
		}

		httpProbe := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: path,
					Port: intstr.FromInt(httpGetPort),
				},
			},
		}

		// Copy timing parameters if they exist
		if probe != nil {
			httpProbe.PeriodSeconds = probe.PeriodSeconds
			httpProbe.TimeoutSeconds = probe.TimeoutSeconds
			httpProbe.InitialDelaySeconds = probe.InitialDelaySeconds
			httpProbe.FailureThreshold = probe.FailureThreshold
		}

		// Handle startup probe for HTTP health checks
		if service.HealthCheck != nil && service.HealthCheck.StartInterval != nil {
			startup = httpProbe.DeepCopy()
			startup.PeriodSeconds = int32(time.Duration(*service.HealthCheck.StartInterval).Seconds())
			startup.FailureThreshold = 0
			startup.InitialDelaySeconds = 0
			httpProbe.InitialDelaySeconds = 0

			if service.HealthCheck.StartPeriod != nil {
				startPeriodSeconds := int32(time.Duration(*service.HealthCheck.StartPeriod).Seconds())
				startup.FailureThreshold = max(startPeriodSeconds/startup.PeriodSeconds, 1)
			}
		}

		liveness = httpProbe
		readiness = httpProbe.DeepCopy()
	}

	return liveness, readiness, startup
}

func getFirstPort(service types.ServiceConfig) int {
	if len(service.Ports) > 0 {
		if published, err := strconv.Atoi(service.Ports[0].Published); err == nil {
			return published
		}
		return int(service.Ports[0].Target)
	}
	return 80 // default port if none specified
}

func splitCommand(cmd string) []string {
	var args []string
	var current string
	var inQuotes bool

	for _, r := range cmd {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case ' ':
			if !inQuotes {
				if current != "" {
					args = append(args, current)
					current = ""
				}
			} else {
				current += string(r)
			}
		default:
			current += string(r)
		}
	}

	if current != "" {
		args = append(args, current)
	}

	return args
}
