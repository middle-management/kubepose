package kubepose

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (t Transformer) createPodSpec(service types.ServiceConfig) corev1.PodSpec {
	return corev1.PodSpec{
		RestartPolicy:             getRestartPolicy(service),
		SecurityContext:           getSecurityContext(service),
		ServiceAccountName:        service.Annotations[ServiceAccountNameAnnotationKey],
		TopologySpreadConstraints: getTopologySpreadConstraints(service),
		HostAliases:               convertExtraHosts(service.ExtraHosts),
	}
}

func getRestartPolicy(service types.ServiceConfig) corev1.RestartPolicy {
	if service.Deploy != nil && service.Deploy.RestartPolicy != nil {
		switch service.Deploy.RestartPolicy.Condition {
		case "on-failure":
			return corev1.RestartPolicyOnFailure
		case "never":
			return corev1.RestartPolicyNever
		}
	}

	// TODO restart: on-failure[:max-retries] should probably fail...

	switch strings.ToLower(service.Restart) {
	case "always":
		return corev1.RestartPolicyAlways
	case "no":
		return corev1.RestartPolicyNever
	case "unless-stopped", "on-failure":
		return corev1.RestartPolicyOnFailure
	}

	if service.Annotations[ContainerTypeAnnotationKey] == "init" {
		// init containers default to on-failure policy
		return corev1.RestartPolicyOnFailure
	}

	// compose default is "no" but that is not valid in k8s deployments etc
	return corev1.RestartPolicyAlways
}

func getSecurityContext(service types.ServiceConfig) *corev1.PodSecurityContext {
	var runAsUser, runAsGroup, fsGroup *int64
	var supplementalGroups []int64

	if service.User != "" {
		parts := strings.Split(service.User, ":")

		if uid, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			runAsUser = &uid
		} else {
			fmt.Printf("Warning: skipping named user %q - only numeric IDs are supported\n", parts[0])
		}

		if len(parts) > 1 {
			if gid, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				runAsGroup = &gid
				fsGroup = &gid
			} else {
				fmt.Printf("Warning: skipping named group %q - only numeric IDs are supported\n", parts[1])
			}
		}
	}

	for _, g := range service.GroupAdd {
		if gid, err := strconv.ParseInt(g, 10, 64); err == nil {
			supplementalGroups = append(supplementalGroups, gid)
		} else {
			fmt.Printf("Warning: skipping named group %q - only numeric IDs are supported\n", g)
		}
	}

	if runAsUser == nil && runAsGroup == nil && fsGroup == nil && len(supplementalGroups) == 0 {
		return nil
	}

	return &corev1.PodSecurityContext{
		RunAsUser:          runAsUser,
		RunAsGroup:         runAsGroup,
		FSGroup:            fsGroup,
		SupplementalGroups: supplementalGroups,
	}
}

func getTopologySpreadConstraints(service types.ServiceConfig) []corev1.TopologySpreadConstraint {
	var constraints []corev1.TopologySpreadConstraint
	if service.Deploy == nil {
		return constraints
	}
	for _, preference := range service.Deploy.Placement.Preferences {
		if preference.Spread == "" {
			continue
		}
		constraints = append(constraints, corev1.TopologySpreadConstraint{
			MaxSkew:           int32(1),
			TopologyKey:       preference.Spread,
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: getMatchLabels(service),
			},
		})
	}
	return constraints
}

func convertExtraHosts(extraHosts types.HostsList) []corev1.HostAlias {
	if len(extraHosts) == 0 {
		return nil
	}

	// HostsList is map[hostname][]ip, we need to invert it to map[ip][]hostname
	ipToHostnames := make(map[string][]string)
	for hostname, ips := range extraHosts {
		for _, ip := range ips {
			ipToHostnames[ip] = append(ipToHostnames[ip], hostname)
		}
	}

	// Convert to HostAlias slice
	var hostAliases []corev1.HostAlias
	for ip, hostnames := range ipToHostnames {
		// Sort hostnames for consistent output
		sort.Strings(hostnames)
		hostAliases = append(hostAliases, corev1.HostAlias{
			IP:        ip,
			Hostnames: hostnames,
		})
	}

	// Sort by IP for consistent output
	sort.Slice(hostAliases, func(i, j int) bool {
		return hostAliases[i].IP < hostAliases[j].IP
	})

	return hostAliases
}

func getMatchLabels(service types.ServiceConfig) map[string]string {
	matchLabels := map[string]string{
		AppSelectorLabelKey: getServiceName(service),
	}
	if annotation, ok := service.Annotations[SelectorMatchLabelsAnnotationKey]; ok {
		newMatchLabels := make(map[string]string)
		err := json.Unmarshal([]byte(annotation), &newMatchLabels)
		if err != nil {
			logrus.Warnf("Error parsing selector match labels: %v\n", err)
		} else {
			matchLabels = newMatchLabels
		}
	}
	return matchLabels
}
