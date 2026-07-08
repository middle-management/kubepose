package kubepose

import (
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TODO support LoadBalancer, NodePort, ExternalName, ClusterIP types via annotations.
func (t Transformer) createService(service types.ServiceConfig) *corev1.Service {
	serviceName := getServiceName(service)
	ports := appendExposePorts(convertServicePorts(service.Ports), service.Expose)
	spec := corev1.ServiceSpec{
		Selector: map[string]string{
			AppSelectorLabelKey: serviceName,
		},
		Ports: ports,
	}
	if len(ports) == 0 {
		// Compose gives every service a DNS name whether or not it publishes
		// ports. A headless Service keeps that contract on Kubernetes.
		spec.ClusterIP = corev1.ClusterIPNone
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: spec,
	}
}

// appendExposePorts adds compose expose entries ("port" or "port/protocol")
// as Service ports, skipping targets already covered by published ports.
// Entries are validated in validateService, so parse failures are skipped.
func appendExposePorts(ports []corev1.ServicePort, expose []string) []corev1.ServicePort {
	seen := make(map[int]bool)
	for _, p := range ports {
		seen[p.TargetPort.IntValue()] = true
	}
	for _, e := range expose {
		target, protocol, _ := strings.Cut(e, "/")
		n, err := strconv.Atoi(target)
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		ports = append(ports, corev1.ServicePort{
			Name:       strconv.Itoa(n),
			Port:       int32(n),
			TargetPort: intstr.FromInt(n),
			Protocol:   convertProtocol(protocol),
		})
	}
	return ports
}

// mergeServicePorts folds another group member's ports into an existing
// Service, so port declarations survive regardless of which member was
// converted first. A headless placeholder becomes a normal ClusterIP Service
// once any member contributes ports.
func mergeServicePorts(existing *corev1.Service, ports []corev1.ServicePort) {
	seen := make(map[int32]bool)
	for _, p := range existing.Spec.Ports {
		seen[p.Port] = true
	}
	for _, p := range ports {
		if seen[p.Port] {
			continue
		}
		seen[p.Port] = true
		existing.Spec.Ports = append(existing.Spec.Ports, p)
	}
	if len(existing.Spec.Ports) > 0 {
		existing.Spec.ClusterIP = ""
	}
}

func convertServicePorts(ports []types.ServicePortConfig) []corev1.ServicePort {
	var servicePorts []corev1.ServicePort
	for _, port := range ports {
		published := int(port.Target)
		if port.Published != "" {
			published, _ = strconv.Atoi(port.Published)
		}
		servicePort := corev1.ServicePort{
			Name:       strconv.Itoa(published),
			Port:       int32(published),
			TargetPort: intstr.FromInt(int(port.Target)),
			Protocol:   convertProtocol(port.Protocol),
		}
		servicePorts = append(servicePorts, servicePort)
	}
	return servicePorts
}

func convertProtocol(protocol string) corev1.Protocol {
	switch strings.ToUpper(protocol) {
	case "TCP":
		return corev1.ProtocolTCP
	case "UDP":
		return corev1.ProtocolUDP
	default:
		return corev1.ProtocolTCP
	}
}

func (t Transformer) createIngress(service types.ServiceConfig) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix

	// Check if a specific ingress class is specified in annotations
	var ingressClassName *string
	if class, ok := service.Annotations[ServiceExposeIngressClassNameAnnotationKey]; ok {
		ingressClassName = &class
	}

	// Get host from labels or annotations
	var hosts []string
	if expose, ok := service.Annotations[ServiceExposeAnnotationKey]; ok && expose != "true" {
		for _, line := range strings.Split(expose, "\n") {
			for _, part := range strings.Split(line, ",") {
				host := strings.TrimSpace(part)
				if host != "" {
					hosts = append(hosts, host)
				}
			}
		}
	} else {
		// Default host
		hosts = append(hosts, service.Name)
	}
	if len(hosts) == 0 {
		return nil
	}

	// Find the first HTTP port
	var servicePort int32
	for _, port := range service.Ports {
		if port.Protocol == "" || strings.ToUpper(port.Protocol) == "TCP" {
			published := int32(port.Target)
			if port.Published != "" {
				if p, err := strconv.Atoi(port.Published); err == nil {
					published = int32(p)
				}
			}
			servicePort = published
			break
		}
	}
	if servicePort == 0 {
		for _, e := range service.Expose {
			target, protocol, _ := strings.Cut(e, "/")
			if protocol != "" && !strings.EqualFold(protocol, "tcp") {
				continue
			}
			if p, err := strconv.Atoi(target); err == nil {
				servicePort = int32(p)
				break
			}
		}
	}
	if servicePort == 0 {
		// A portless service has nothing an Ingress could route to.
		return nil
	}

	var rules []networkingv1.IngressRule
	for _, host := range hosts {
		rules = append(rules, networkingv1.IngressRule{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: service.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: servicePort,
									},
								},
							},
						},
					},
				},
			},
		})
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClassName,
			Rules:            rules,
		},
	}
}

func (t Transformer) createServiceAccount(name string, service types.ServiceConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      service.Labels,
		},
	}
}
