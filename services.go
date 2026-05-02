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
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				AppSelectorLabelKey: serviceName,
			},
			Ports: convertServicePorts(service.Ports),
		},
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
