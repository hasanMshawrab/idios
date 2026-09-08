package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func TestContainerKindFollowsSpecList(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "p1"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init-db", Image: "busybox:1.36"},
				{Name: "proxy", Image: "envoy:1.30", RestartPolicy: &always},
			},
			Containers:          []corev1.Container{{Name: "api", Image: "web:1"}},
			EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Image: "busybox:1.36"}}},
		},
	}
	var got []struct{ Name, Kind string }
	for _, c := range mapContainers(pod, clock.Format(testNow)) {
		got = append(got, struct{ Name, Kind string }{c.Name, c.Kind})
	}
	want := []struct{ Name, Kind string }{
		{"init-db", store.ContainerKindInit}, {"proxy", store.ContainerKindSidecar},
		{"api", store.ContainerKindApp}, {"debug", store.ContainerKindEphemeral},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestContainerStateFromStatus(t *testing.T) {
	now := clock.Format(testNow)
	started := metav1.Date(2026, 8, 27, 11, 50, 0, 0, time.UTC)
	finished := metav1.Date(2026, 8, 27, 11, 55, 0, 0, time.UTC)
	base := store.Container{PodUID: "p1", Name: "api", Kind: store.ContainerKindApp, Image: "web:1.4.2", ImageTag: ptr("1.4.2"), UpdatedAt: now}
	cases := []struct {
		name   string
		status *corev1.ContainerStatus
		want   store.Container
	}{
		{"no status yet is waiting without reason", nil, func() store.Container {
			c := base
			c.State = store.StateWaiting
			return c
		}()},
		{"running", &corev1.ContainerStatus{
			Name: "api", Ready: true, RestartCount: 2, ImageID: "web@sha256:aa", ContainerID: "containerd://c2",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c2")
			c.State, c.Ready, c.RestartCount, c.RunningSince = store.StateRunning, true, 2, ptr(k8sTime(started))
			return c
		}()},
		{"waiting with last termination", &corev1.ContainerStatus{
			Name: "api", RestartCount: 1, ImageID: "web@sha256:aa", ContainerID: "containerd://c1",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 10s"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 137, Signal: 9, Reason: "OOMKilled", StartedAt: started, FinishedAt: finished, ContainerID: "containerd://c1"}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c1")
			c.State, c.Reason, c.RestartCount = store.StateWaiting, ptr("CrashLoopBackOff"), 1
			c.LastTerminatedReason, c.LastTerminatedExitCode, c.LastTerminatedSignal = ptr("OOMKilled"), ptr[int64](137), ptr[int64](9)
			c.LastTerminatedAt = ptr(k8sTime(finished))
			return c
		}()},
		{"terminated", &corev1.ContainerStatus{
			Name: "api", ImageID: "web@sha256:aa", ContainerID: "containerd://c0",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1, Reason: "Error", Message: "panic: config missing", StartedAt: started, FinishedAt: finished}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c0")
			c.State, c.Reason, c.ExitCode, c.Signal = store.StateTerminated, ptr("Error"), ptr[int64](1), ptr[int64](0)
			c.Message = ptr("panic: config missing")
			return c
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{UID: "p1"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "web:1.4.2"}}},
			}
			if c.status != nil {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{*c.status}
			}
			got := mapContainers(pod, now)
			if d := cmp.Diff([]store.Container{c.want}, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestImageTagParsing(t *testing.T) {
	cases := []struct {
		image string
		want  *string
	}{
		{"web:1.4.2", ptr("1.4.2")},
		{"registry.example.com:5000/team/web:1.4.2", ptr("1.4.2")},
		{"registry.example.com:5000/team/web", nil},
		{"web@sha256:0123456789abcdef", nil},
		{"web:1.4.2@sha256:0123456789abcdef", ptr("1.4.2")},
		{"busybox", nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, imageTag(c.image)); d != "" {
			t.Errorf("imageTag(%q): %s", c.image, d)
		}
	}
}

func TestResourceQuantitiesParsed(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "p1"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "api", Image: "web:1",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			},
		}}},
	}
	got := mapContainers(pod, clock.Format(testNow))[0]
	want := store.Container{
		PodUID: "p1", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", ImageTag: ptr("1"),
		CPURequest: ptr("500m"), MemRequest: ptr("512Mi"), MemLimit: ptr("1Gi"),
		CPURequestMillis: ptr[int64](500), MemRequestBytes: ptr[int64](512 << 20), MemLimitBytes: ptr[int64](1 << 30),
		State: store.StateWaiting, UpdatedAt: clock.Format(testNow),
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

// The kubelet caps the termination message it reads per container; ingest
// caps it again, on a rune boundary, so the value that reaches an incident's
// last_message cannot grow past that whatever the API server hands over.
func TestTerminatedMessageCappedAtIngest(t *testing.T) {
	wide := string([]byte{0xe4, 0xb8, 0xad})
	cases := []struct {
		name    string
		message string
		want    *string
	}{
		{"absent", "", nil},
		{"under the cap", "panic: config missing", ptr("panic: config missing")},
		{"exactly at the cap", strings.Repeat("a", 4096), ptr(strings.Repeat("a", 4096))},
		{"over the cap", strings.Repeat("a", 5000), ptr(strings.Repeat("a", 4096))},
		{"a rune straddling the cap", strings.Repeat("a", 4095) + wide, ptr(strings.Repeat("a", 4095))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{UID: "p1"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "web:1"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "api",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 2, Reason: "Error", Message: c.message}},
				}}},
			}
			if d := cmp.Diff(c.want, mapContainers(pod, clock.Format(testNow))[0].Message); d != "" {
				t.Fatal(d)
			}
		})
	}
}
