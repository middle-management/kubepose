package kubepose_test

import (
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/middle-management/kubepose"
)

// TestConvertNegative covers invalid or edge-case service configurations.
// Some scenarios assert desired error behavior; others document current
// silent-fallback behavior so that regressions or future hardening work
// surface here first.
func TestConvertNegative(t *testing.T) {
	t.Parallel()

	t.Run("unknown healthcheck test type returns error not panic", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			HealthCheck: &types.HealthCheckConfig{
				Test: []string{"WHATEVER", "echo", "hi"},
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil {
			t.Fatal("expected error for unknown healthcheck test type, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported healthcheck test type") {
			t.Fatalf("expected unsupported-healthcheck error, got: %v", err)
		}
		if !strings.Contains(err.Error(), `"web"`) {
			t.Fatalf("expected error to mention service name, got: %v", err)
		}
	})

	t.Run("error from one service surfaces deterministically", func(t *testing.T) {
		t.Parallel()
		// Two services, one valid and one invalid. The invalid one should
		// be reported with its name; iteration order must be deterministic.
		project := &types.Project{
			Services: types.Services{
				"a-good": types.ServiceConfig{Name: "a-good", Image: "nginx"},
				"z-bad": types.ServiceConfig{
					Name:  "z-bad",
					Image: "nginx",
					HealthCheck: &types.HealthCheckConfig{
						Test: []string{"BOGUS"},
					},
				},
			},
		}
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil {
			t.Fatal("expected error from invalid service, got nil")
		}
		if !strings.Contains(err.Error(), `"z-bad"`) {
			t.Fatalf("expected error to mention z-bad, got: %v", err)
		}
	})

	t.Run("healthcheck NONE disables probes", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			HealthCheck: &types.HealthCheckConfig{
				Test: []string{"NONE"},
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resources.Deployments))
		}
		c := resources.Deployments[0].Spec.Template.Spec.Containers[0]
		if c.LivenessProbe != nil || c.ReadinessProbe != nil || c.StartupProbe != nil {
			t.Fatal("expected no probes when healthcheck test is NONE")
		}
	})

	t.Run("healthcheck disable=true produces no probes", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			HealthCheck: &types.HealthCheckConfig{
				Test:    []string{"CMD", "/healthz"},
				Disable: true,
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := resources.Deployments[0].Spec.Template.Spec.Containers[0]
		if c.LivenessProbe != nil || c.ReadinessProbe != nil || c.StartupProbe != nil {
			t.Fatal("expected no probes when healthcheck is disabled")
		}
	})

	t.Run("invalid published port is silently ignored", func(t *testing.T) {
		t.Parallel()
		// Documents existing behavior at convert.go convertServicePorts.
		// strconv errors are swallowed; published falls back to 0.
		// TODO: surface a real error in a future PR.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			Ports: []types.ServicePortConfig{
				{Target: 80, Published: "not-a-number"},
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources.Services) != 1 {
			t.Fatalf("expected 1 service, got %d", len(resources.Services))
		}
		ports := resources.Services[0].Spec.Ports
		if len(ports) != 1 || ports[0].Port != 0 {
			t.Fatalf("expected silent fallback to port 0, got %+v", ports)
		}
	})

	t.Run("invalid healthcheck port annotation is silently ignored", func(t *testing.T) {
		t.Parallel()
		// Documents existing behavior at convert.go getProbes.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			Ports: []types.ServicePortConfig{{Target: 8080, Protocol: "tcp"}},
			Annotations: map[string]string{
				kubepose.HealthcheckHttpGetPathAnnotationKey: "/healthz",
				kubepose.HealthcheckHttpGetPortAnnotationKey: "not-a-port",
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := resources.Deployments[0].Spec.Template.Spec.Containers[0]
		if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet == nil {
			t.Fatal("expected an HTTP liveness probe")
		}
		// Falls back to first declared port instead of erroring.
		if got := c.LivenessProbe.HTTPGet.Port.IntVal; got != 8080 {
			t.Fatalf("expected fallback to first port 8080, got %d", got)
		}
	})

	t.Run("invalid selector matchLabels JSON falls back to default", func(t *testing.T) {
		t.Parallel()
		// Documents convert.go getMatchLabels: bad JSON is logged as a
		// warning and the default app selector is used instead.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			Annotations: map[string]string{
				kubepose.SelectorMatchLabelsAnnotationKey: "{not valid json",
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := resources.Deployments[0].Spec.Selector.MatchLabels
		want := map[string]string{kubepose.AppSelectorLabelKey: "web"}
		if len(got) != len(want) || got[kubepose.AppSelectorLabelKey] != "web" {
			t.Fatalf("expected fallback selector %v, got %v", want, got)
		}
	})

	t.Run("named user is skipped with no security context", func(t *testing.T) {
		t.Parallel()
		// Documents that named users like "ubuntu" are dropped (only numeric
		// IDs are supported); a warning is printed but conversion succeeds.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			User:  "ubuntu",
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := resources.Deployments[0].Spec.Template.Spec
		if spec.SecurityContext != nil {
			t.Fatalf("expected no SecurityContext for non-numeric user, got %+v", spec.SecurityContext)
		}
	})

	t.Run("numeric user populates SecurityContext", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			User:  "1000:2000",
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sc := resources.Deployments[0].Spec.Template.Spec.SecurityContext
		if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser != 1000 {
			t.Fatalf("expected RunAsUser=1000, got %+v", sc)
		}
		if sc.RunAsGroup == nil || *sc.RunAsGroup != 2000 {
			t.Fatalf("expected RunAsGroup=2000, got %+v", sc)
		}
	})

	t.Run("unknown workload type returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			Annotations: map[string]string{
				kubepose.WorkloadTypeAnnotationKey: "ReplicaSet",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "unsupported kubepose.workload") {
			t.Fatalf("expected unsupported workload error, got: %v", err)
		}
	})

	t.Run("empty cronjob schedule returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "job",
			Image: "alpine",
			Annotations: map[string]string{
				kubepose.CronJobScheduleAnnotationKey: "",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("expected empty-schedule error, got: %v", err)
		}
	})

	t.Run("hpa on cronjob is rejected", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "job",
			Image: "alpine",
			Annotations: map[string]string{
				kubepose.CronJobScheduleAnnotationKey: "0 * * * *",
				kubepose.HPAMaxReplicasAnnotationKey:  "5",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("expected HPA-on-CronJob error, got: %v", err)
		}
	})

	t.Run("hpa without max replicas returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "api",
			Image: "nginx",
			Annotations: map[string]string{
				kubepose.HPAMinReplicasAnnotationKey: "2",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "maxReplicas") {
			t.Fatalf("expected missing-maxReplicas error, got: %v", err)
		}
	})

	t.Run("hpa min greater than max returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "api",
			Image: "nginx",
			Annotations: map[string]string{
				kubepose.HPAMinReplicasAnnotationKey: "10",
				kubepose.HPAMaxReplicasAnnotationKey: "2",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "greater than maxReplicas") {
			t.Fatalf("expected min>max error, got: %v", err)
		}
	})

	t.Run("hpa with non-numeric value returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "api",
			Image: "nginx",
			Annotations: map[string]string{
				kubepose.HPAMaxReplicasAnnotationKey: "lots",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "must be a positive integer") {
			t.Fatalf("expected positive-integer error, got: %v", err)
		}
	})

	t.Run("workload statefulset with hpa targets statefulset", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "db",
			Image: "postgres",
			Annotations: map[string]string{
				kubepose.WorkloadTypeAnnotationKey:   "StatefulSet",
				kubepose.HPAMaxReplicasAnnotationKey: "5",
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources.HorizontalPodAutoscalers) != 1 {
			t.Fatalf("expected 1 HPA, got %d", len(resources.HorizontalPodAutoscalers))
		}
		ref := resources.HorizontalPodAutoscalers[0].Spec.ScaleTargetRef
		if ref.Kind != "StatefulSet" || ref.Name != "db" {
			t.Fatalf("expected StatefulSet/db target, got %+v", ref)
		}
	})

	t.Run("CMD with empty command list still produces a probe", func(t *testing.T) {
		t.Parallel()
		// Documents current behavior: ["CMD"] with no following args
		// yields an exec probe with an empty Command slice (kubectl would
		// reject this on apply). A future PR could reject this upfront.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			HealthCheck: &types.HealthCheckConfig{
				Test: []string{"CMD"},
			},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := resources.Deployments[0].Spec.Template.Spec.Containers[0]
		if c.LivenessProbe == nil || c.LivenessProbe.Exec == nil {
			t.Fatal("expected an exec liveness probe")
		}
		if len(c.LivenessProbe.Exec.Command) != 0 {
			t.Fatalf("expected empty Command, got %v", c.LivenessProbe.Exec.Command)
		}
	})
}

func projectWith(svc types.ServiceConfig) *types.Project {
	return &types.Project{
		Services: types.Services{svc.Name: svc},
	}
}
