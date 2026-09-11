package kubepose

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func (t Transformer) createContainer(service types.ServiceConfig) corev1.Container {
	livenessProbe, readinessProbe, startupProbe := getProbes(service)

	// An init-typed service becomes a native sidecar: an init container with
	// restartPolicy Always. validateService guarantees restart: always here.
	// https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/
	var containerRestartPolicy *corev1.ContainerRestartPolicy
	if service.Annotations[ContainerTypeAnnotationKey] == "init" {
		containerRestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
	}
	return corev1.Container{
		Name:            service.Name,
		Image:           service.Image,
		Command:         service.Entrypoint,
		WorkingDir:      service.WorkingDir,
		Stdin:           service.StdinOpen,
		TTY:             service.Tty,
		Args:            escapeEnvs(service.Command),
		Ports:           convertPorts(service.Ports),
		Env:             convertEnvironment(service.Environment),
		Resources:       getResourceRequirements(service),
		ImagePullPolicy: getImagePullPolicy(service),
		LivenessProbe:   livenessProbe,
		ReadinessProbe:  readinessProbe,
		StartupProbe:    startupProbe,
		RestartPolicy:   containerRestartPolicy,
		Lifecycle:       getLifecycle(service),
	}
}

func preStartContainerName(service types.ServiceConfig, index int) string {
	return fmt.Sprintf("%s-pre-start-%d", service.Name, index)
}

// createPreStartContainers converts a service's pre_start lifecycle hooks into
// init containers. Kubernetes runs init containers sequentially and each must
// exit 0 before the next starts, matching the compose pre_start contract.
func (t Transformer) createPreStartContainers(service types.ServiceConfig) []corev1.Container {
	var containers []corev1.Container
	for i, hook := range service.PreStart {
		image := hook.Image
		if image == "" {
			image = service.Image
		}

		env := make(types.MappingWithEquals, len(service.Environment)+len(hook.Environment))
		for k, v := range service.Environment {
			env[k] = v
		}
		for k, v := range hook.Environment {
			env[k] = v
		}

		containers = append(containers, corev1.Container{
			Name:            preStartContainerName(service, i),
			Image:           image,
			Command:         escapeEnvs(hook.Command),
			WorkingDir:      hook.WorkingDir,
			Env:             convertEnvironment(env),
			ImagePullPolicy: getImagePullPolicy(service),
			SecurityContext: getHookSecurityContext(hook),
		})
	}
	return containers
}

// inheritPreStartVolumeMounts copies the parent service container's volume
// mounts onto its pre_start init containers. The secret/config/volume plumbing
// attaches mounts by matching container name to service name, which never
// matches hook containers, so this must run after those updates.
func inheritPreStartVolumeMounts(spec *corev1.PodSpec, service types.ServiceConfig) {
	if len(service.PreStart) == 0 {
		return
	}

	var parentMounts []corev1.VolumeMount
	for _, container := range spec.Containers {
		if container.Name == service.Name {
			parentMounts = container.VolumeMounts
			break
		}
	}
	if parentMounts == nil {
		for _, container := range spec.InitContainers {
			if container.Name == service.Name {
				parentMounts = container.VolumeMounts
				break
			}
		}
	}
	if len(parentMounts) == 0 {
		return
	}

	for i := range spec.InitContainers {
		for hi := range service.PreStart {
			if spec.InitContainers[i].Name == preStartContainerName(service, hi) {
				spec.InitContainers[i].VolumeMounts = append(
					spec.InitContainers[i].VolumeMounts,
					parentMounts...,
				)
			}
		}
	}
}

func convertPorts(ports []types.ServicePortConfig) []corev1.ContainerPort {
	var containerPorts []corev1.ContainerPort
	for _, port := range ports {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			ContainerPort: int32(port.Target),
			Protocol:      convertProtocol(port.Protocol),
		})
	}
	return containerPorts
}

func convertEnvironment(env map[string]*string) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	for key, value := range env {
		envVar := corev1.EnvVar{
			Name: key,
		}
		if value != nil {
			envVar.Value = *value
		}
		envVars = append(envVars, envVar)
	}
	sort.Slice(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

func getResourceRequirements(service types.ServiceConfig) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}

	if service.Deploy != nil {
		if service.Deploy.Resources.Limits != nil {
			resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Limits.NanoCPUs.Value()*1000))),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Limits.MemoryBytes/1024/1024)),
			}
		}
		if service.Deploy.Resources.Reservations != nil {
			resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Reservations.NanoCPUs.Value()*1000))),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Reservations.MemoryBytes/1024/1024)),
			}
		}
	}

	return resources
}

func getImagePullPolicy(service types.ServiceConfig) corev1.PullPolicy {
	if service.PullPolicy == "" {
		return corev1.PullIfNotPresent // default behavior
	}

	switch strings.ToLower(service.PullPolicy) {
	case "always":
		return corev1.PullAlways
	case "never":
		return corev1.PullNever
	case "if_not_present", "missing":
		return corev1.PullIfNotPresent
	default:
		return corev1.PullIfNotPresent
	}
}

func removeDuplicateVolumeMounts(containers []corev1.Container) {
	for i := range containers {
		seen := make(map[string]bool)
		var unique []corev1.VolumeMount
		for _, mount := range containers[i].VolumeMounts {
			key := mount.Name + ":" + mount.MountPath
			if !seen[key] {
				seen[key] = true
				unique = append(unique, mount)
			}
		}
		containers[i].VolumeMounts = unique
	}
}

// getLifecycle converts a service's post_start and pre_stop hooks into the
// container's Kubernetes lifecycle handlers.
//
// Unlike pre_start, which becomes an init container, these hooks run inside the
// already-created service container - the same thing compose does with a
// `docker exec` into the running container. The compose spec reflects that:
// post_start and pre_stop take only command, user, privileged, working_dir and
// environment (image and per_replica are pre_start-only), and validateService
// rejects the two of those that cannot be honored from inside a container.
func getLifecycle(service types.ServiceConfig) *corev1.Lifecycle {
	postStart := hookExecAction(service.PostStart)
	preStop := hookExecAction(service.PreStop)
	if postStart == nil && preStop == nil {
		return nil
	}

	lifecycle := &corev1.Lifecycle{}
	if postStart != nil {
		lifecycle.PostStart = &corev1.LifecycleHandler{Exec: postStart}
	}
	if preStop != nil {
		lifecycle.PreStop = &corev1.LifecycleHandler{Exec: preStop}
	}
	return lifecycle
}

// hookExecAction renders compose lifecycle hooks into the single exec handler
// Kubernetes allows per lifecycle event.
//
// A lone hook that carries nothing but a command maps one to one: Kubernetes
// execs the argument list directly, exactly as compose does, so the image needs
// no shell. Anything else - several hooks, a working_dir, or hook environment -
// is rendered as a /bin/sh script, since only a shell can chain commands, change
// directory, or set variables for the command it runs. The arguments are quoted
// so the shell passes them through literally, which keeps the two paths
// equivalent: neither expands a `$VAR` that compose would have exec'd verbatim.
func hookExecAction(hooks []types.ServiceHook) *corev1.ExecAction {
	if len(hooks) == 0 {
		return nil
	}

	if len(hooks) == 1 && !hookNeedsShell(hooks[0]) {
		return &corev1.ExecAction{Command: hooks[0].Command}
	}

	scripts := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		scripts = append(scripts, hookScript(hook))
	}
	// Joined with && because compose runs the hooks in declared order and
	// stops at the first one that fails.
	return &corev1.ExecAction{
		Command: []string{"/bin/sh", "-c", strings.Join(scripts, " && ")},
	}
}

func hookNeedsShell(hook types.ServiceHook) bool {
	return hook.WorkingDir != "" || len(hookEnvironment(hook)) > 0
}

// hookScript renders one hook as a shell command. working_dir and environment
// are scoped to the hook with a subshell so they do not leak into the hooks
// that run after it.
func hookScript(hook types.ServiceHook) string {
	args := make([]string, 0, len(hook.Command))
	for _, arg := range hook.Command {
		args = append(args, shellQuote(arg))
	}

	script := strings.Join(append(hookEnvironment(hook), strings.Join(args, " ")), " ")
	if hook.WorkingDir != "" {
		script = "cd " + shellQuote(hook.WorkingDir) + " && " + script
	}
	if hookNeedsShell(hook) {
		return "(" + script + ")"
	}
	return script
}

// hookEnvironment returns the hook's environment as sorted shell assignments.
// A key with no value means "inherit the value from the surrounding
// environment", which the container environment already hands to the exec'd
// process, so it is left alone rather than clobbered with an empty string.
func hookEnvironment(hook types.ServiceHook) []string {
	keys := make([]string, 0, len(hook.Environment))
	for key, value := range hook.Environment {
		if value == nil {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	assignments := make([]string, 0, len(keys))
	for _, key := range keys {
		assignments = append(assignments, key+"="+shellQuote(*hook.Environment[key]))
	}
	return assignments
}

// reShellSafeWord matches words a shell passes through untouched. "=" is
// deliberately absent: an unquoted foo=bar in command position would be read
// as a variable assignment rather than as the argument compose passes.
var reShellSafeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+:,./-]+$`)

// shellQuote renders s as a shell word that expands to s and nothing else,
// quoting only when the word needs it so the generated script stays readable.
func shellQuote(s string) string {
	if reShellSafeWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
