package kubepose_test

import (
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/middle-management/kubepose"
	corev1 "k8s.io/api/core/v1"
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

	t.Run("invalid published port returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			Ports: []types.ServicePortConfig{
				{Target: 80, Published: "not-a-number"},
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "invalid published port") {
			t.Fatalf("expected invalid published port error, got: %v", err)
		}
	})

	t.Run("invalid expose entry returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:   "web",
			Image:  "nginx",
			Expose: []string{"not-a-port"},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "invalid expose entry") {
			t.Fatalf("expected invalid expose entry error, got: %v", err)
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

	t.Run("named user returns error", func(t *testing.T) {
		t.Parallel()
		// Named users resolve against the image's /etc/passwd locally but
		// have no Kubernetes securityContext equivalent, so the deployed
		// container would silently run as a different user.
		for _, user := range []string{"ubuntu", "1000:staff"} {
			project := projectWith(types.ServiceConfig{
				Name:  "web",
				Image: "nginx",
				User:  user,
			})
			_, err := kubepose.Transformer{}.Convert(project)
			if err == nil || !strings.Contains(err.Error(), "only numeric user/group IDs") {
				t.Fatalf("user %q: expected numeric-IDs error, got: %v", user, err)
			}
		}
	})

	t.Run("named group_add returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:     "web",
			Image:    "nginx",
			GroupAdd: []string{"docker"},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "only numeric group IDs") {
			t.Fatalf("expected numeric group IDs error, got: %v", err)
		}
	})

	t.Run("named pre_start hook user returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			PreStart: []types.ServiceHook{
				{Command: []string{"echo", "hi"}, User: "root"},
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "only numeric user/group IDs") {
			t.Fatalf("expected numeric-IDs error for hook user, got: %v", err)
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

	t.Run("CMD with empty command list returns error", func(t *testing.T) {
		t.Parallel()
		// ["CMD"] with no following args would yield an exec probe with an
		// empty Command slice, which Kubernetes rejects on apply.
		project := projectWith(types.ServiceConfig{
			Name:  "web",
			Image: "nginx",
			HealthCheck: &types.HealthCheckConfig{
				Test: []string{"CMD"},
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "requires a command") {
			t.Fatalf("expected empty-command healthcheck error, got: %v", err)
		}
	})

	t.Run("group with only init services returns error", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name: "proxy", Image: "envoy",
			Restart: "always",
			Annotations: map[string]string{
				kubepose.ServiceGroupAnnotationKey:  "myapp",
				kubepose.ContainerTypeAnnotationKey: "init",
			},
		})
		_, err := kubepose.Transformer{}.Convert(project)
		if err == nil || !strings.Contains(err.Error(), "only") || !strings.Contains(err.Error(), "init") {
			t.Fatalf("expected init-only group error, got: %v", err)
		}
	})

	t.Run("portless service gets a headless Service for DNS parity", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:  "db",
			Image: "postgres",
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources.Services) != 1 {
			t.Fatalf("expected 1 service, got %d", len(resources.Services))
		}
		svc := resources.Services[0]
		if svc.Spec.ClusterIP != corev1.ClusterIPNone {
			t.Fatalf("expected headless service (ClusterIP None), got %q", svc.Spec.ClusterIP)
		}
		if len(svc.Spec.Ports) != 0 {
			t.Fatalf("expected no ports, got %+v", svc.Spec.Ports)
		}
	})

	t.Run("expose entries become Service ports", func(t *testing.T) {
		t.Parallel()
		project := projectWith(types.ServiceConfig{
			Name:   "db",
			Image:  "postgres",
			Expose: []string{"5432", "9000/udp"},
		})
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		svc := resources.Services[0]
		if svc.Spec.ClusterIP == corev1.ClusterIPNone {
			t.Fatal("expected a regular ClusterIP service when expose ports exist")
		}
		if len(svc.Spec.Ports) != 2 {
			t.Fatalf("expected 2 ports, got %+v", svc.Spec.Ports)
		}
		if svc.Spec.Ports[0].Port != 5432 || svc.Spec.Ports[0].Protocol != corev1.ProtocolTCP {
			t.Fatalf("expected 5432/TCP first, got %+v", svc.Spec.Ports[0])
		}
		if svc.Spec.Ports[1].Port != 9000 || svc.Spec.Ports[1].Protocol != corev1.ProtocolUDP {
			t.Fatalf("expected 9000/UDP second, got %+v", svc.Spec.Ports[1])
		}
	})

	t.Run("init container type without explicit restart always returns error", func(t *testing.T) {
		t.Parallel()
		// Unset restart is rejected too: compose's default is no restart
		// (run-once), so it must not silently become a sidecar.
		for _, restart := range []string{"", "no", "on-failure", "unless-stopped"} {
			project := projectWith(types.ServiceConfig{
				Name:    "setup",
				Image:   "alpine",
				Restart: restart,
				Annotations: map[string]string{
					kubepose.ContainerTypeAnnotationKey: "init",
				},
			})
			_, err := kubepose.Transformer{}.Convert(project)
			if err == nil || !strings.Contains(err.Error(), "pre_start") {
				t.Fatalf("restart %q: expected error pointing at pre_start hooks, got: %v", restart, err)
			}
		}
	})

	t.Run("init container type becomes a native sidecar", func(t *testing.T) {
		t.Parallel()
		group := map[string]string{kubepose.ServiceGroupAnnotationKey: "myapp"}
		project := &types.Project{
			Services: types.Services{
				"web": types.ServiceConfig{
					Name: "web", Image: "nginx",
					Annotations: group,
				},
				"proxy": types.ServiceConfig{
					Name: "proxy", Image: "envoy",
					Restart: "always",
					Annotations: map[string]string{
						kubepose.ServiceGroupAnnotationKey:  "myapp",
						kubepose.ContainerTypeAnnotationKey: "init",
					},
				},
			},
		}
		resources, err := kubepose.Transformer{}.Convert(project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resources.Deployments))
		}
		spec := resources.Deployments[0].Spec.Template.Spec
		if len(spec.InitContainers) != 1 || spec.InitContainers[0].Name != "proxy" {
			t.Fatalf("expected proxy as the only init container, got %+v", spec.InitContainers)
		}
		rp := spec.InitContainers[0].RestartPolicy
		if rp == nil || *rp != corev1.ContainerRestartPolicyAlways {
			t.Fatalf("expected container restartPolicy Always on sidecar, got %v", rp)
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
}

func projectWith(svc types.ServiceConfig) *types.Project {
	return &types.Project{
		Services: types.Services{svc.Name: svc},
	}
}

func TestConvertHpaValidation(t *testing.T) {
	t.Parallel()

	withCpuReservation := func(svc types.ServiceConfig) types.ServiceConfig {
		if svc.Deploy == nil {
			svc.Deploy = &types.DeployConfig{}
		}
		svc.Deploy.Resources.Reservations = &types.Resource{NanoCPUs: 2}
		return svc
	}

	cases := []struct {
		name    string
		service types.ServiceConfig
		wantErr string
	}{
		{
			name: "minReplicas without maxReplicas names only the set key",
			service: types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{kubepose.HpaMinReplicasAnnotationKey: "2"},
			},
			wantErr: kubepose.HpaMinReplicasAnnotationKey + " has no effect without",
		},
		{
			name: "cpu without maxReplicas names only the set key",
			service: types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{kubepose.HpaCpuAnnotationKey: "70"},
			},
			wantErr: kubepose.HpaCpuAnnotationKey + " has no effect without",
		},
		{
			name: "non-numeric maxReplicas",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "lots"},
			}),
			wantErr: "must be a positive integer",
		},
		{
			name: "maxReplicas overflowing int32 is rejected, not truncated",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				// int32-truncates to 3; must fail validation instead of
				// emitting a plausible-looking wrong HPA.
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "4294967299"},
			}),
			wantErr: "must be a positive integer",
		},
		{
			name: "minReplicas above maxReplicas",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{
					kubepose.HpaMinReplicasAnnotationKey: "5",
					kubepose.HpaMaxReplicasAnnotationKey: "3",
				},
			}),
			wantErr: "must not exceed",
		},
		{
			name: "deploy.replicas above maxReplicas as implicit floor",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Deploy:      &types.DeployConfig{Replicas: intPtr(8)},
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "3"},
			}),
			wantErr: "must not exceed",
		},
		{
			name: "zero cpu target",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{
					kubepose.HpaMaxReplicasAnnotationKey: "3",
					kubepose.HpaCpuAnnotationKey:         "0",
				},
			}),
			wantErr: "positive integer percentage",
		},
		{
			name: "combined with cronjob schedule",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{
					kubepose.HpaMaxReplicasAnnotationKey:  "3",
					kubepose.CronJobScheduleAnnotationKey: "0 * * * *",
				},
			}),
			wantErr: "cannot be combined with",
		},
		{
			name: "combined with global mode",
			service: types.ServiceConfig{
				Name: "web", Image: "nginx",
				Deploy: &types.DeployConfig{
					Mode:      "global",
					Resources: types.Resources{Reservations: &types.Resource{NanoCPUs: 1}},
				},
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "3"},
			},
			wantErr: "deploy.mode: global",
		},
		{
			name: "missing cpu reservation",
			service: types.ServiceConfig{
				Name: "web", Image: "nginx",
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "3"},
			},
			wantErr: "requires deploy.resources.reservations.cpus",
		},
		{
			name: "standalone-pod service (restart: no) is rejected, not a silent no-op",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Restart:     "no",
				Annotations: map[string]string{kubepose.HpaMaxReplicasAnnotationKey: "3"},
			}),
			wantErr: "requires restart: always",
		},
		{
			name: "init container service is rejected, not a silent no-op",
			service: withCpuReservation(types.ServiceConfig{
				Name: "web", Image: "nginx",
				Restart: "always", // valid sidecar, so the HPA check is what fires
				Annotations: map[string]string{
					kubepose.HpaMaxReplicasAnnotationKey: "3",
					kubepose.ContainerTypeAnnotationKey:  "init",
				},
			}),
			wantErr: "has no effect on an init container",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := kubepose.Transformer{}.Convert(projectWith(tc.service))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func intPtr(i int) *int { return &i }
