package incident

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestContainerCategoryKeysOnReason(t *testing.T) {
	cases := []struct {
		state, reason, last string
		terminating         bool
		want                string
	}{
		{store.StateWaiting, "CrashLoopBackOff", "Error", false, store.CategoryCrash},
		{store.StateWaiting, "CrashLoopBackOff", "OOMKilled", false, store.CategoryOOM},
		{store.StateWaiting, "CrashLoopBackOff", "", false, store.CategoryCrash},
		{store.StateTerminated, "OOMKilled", "", false, store.CategoryOOM},
		{store.StateTerminated, "Error", "", false, store.CategoryCrash},
		{store.StateTerminated, "Error", "OOMKilled", false, store.CategoryCrash},
		{store.StateTerminated, "Completed", "", false, ""},
		{store.StateTerminated, "ContainerCannotRun", "", false, store.CategoryConfig},
		{store.StateTerminated, "StartError", "", false, store.CategoryConfig},
		{store.StateWaiting, "ErrImagePull", "", false, store.CategoryImagePull},
		{store.StateWaiting, "ImagePullBackOff", "", false, store.CategoryImagePull},
		{store.StateWaiting, "InvalidImageName", "", false, store.CategoryImagePull},
		{store.StateWaiting, "CreateContainerConfigError", "", false, store.CategoryConfig},
		{store.StateWaiting, "CreateContainerError", "", false, store.CategoryConfig},
		{store.StateWaiting, "ContainerCreating", "", false, ""},
		{store.StateWaiting, "PodInitializing", "OOMKilled", false, ""},
		{store.StateWaiting, "", "", false, ""},
		{store.StateRunning, "", "OOMKilled", false, ""},
		{store.StateRunning, "", "", false, ""},
		// Only the Error termination reads the terminating flag: a bad death
		// on the way out is its own kind, everything else means what it
		// means on a live pod.
		{store.StateTerminated, "Error", "", true, store.CategoryUncleanExit},
		{store.StateTerminated, "Error", "OOMKilled", true, store.CategoryUncleanExit},
		{store.StateTerminated, "OOMKilled", "", true, store.CategoryOOM},
		{store.StateTerminated, "Completed", "", true, ""},
		{store.StateTerminated, "StartError", "", true, store.CategoryConfig},
		{store.StateWaiting, "CrashLoopBackOff", "", true, store.CategoryCrash},
		{store.StateWaiting, "ImagePullBackOff", "", true, store.CategoryImagePull},
		{store.StateWaiting, "CreateContainerConfigError", "", true, store.CategoryConfig},
		{store.StateRunning, "", "", true, ""},
	}
	for _, c := range cases {
		if got := ContainerCategory(c.state, c.reason, c.last, c.terminating); got != c.want {
			t.Errorf("ContainerCategory(%q, %q, %q, %v) = %q, want %q", c.state, c.reason, c.last, c.terminating, got, c.want)
		}
	}
}

func TestConditionAndPodReasonCategories(t *testing.T) {
	conds := []struct {
		ty, status, reason string
		want               string
	}{
		{"PodScheduled", "False", "Unschedulable", store.CategoryScheduling},
		{"PodScheduled", "False", "SchedulingGated", ""},
		{"PodScheduled", "True", "", ""},
		{"DisruptionTarget", "True", "TerminationByKubelet", store.CategoryNodePressure},
		{"DisruptionTarget", "True", "EvictionByEvictionAPI", ""},
		{"DisruptionTarget", "True", "PreemptionByScheduler", store.CategoryRescheduled},
		{"DisruptionTarget", "True", "DeletionByTaintManager", store.CategoryRescheduled},
		{"DisruptionTarget", "True", "DeletionByPodGC", store.CategoryRescheduled},
		{"DisruptionTarget", "False", "TerminationByKubelet", ""},
		{"Ready", "False", "", ""},
	}
	for _, c := range conds {
		if got := ConditionCategory(c.ty, c.status, c.reason); got != c.want {
			t.Errorf("ConditionCategory(%q, %q, %q) = %q, want %q", c.ty, c.status, c.reason, got, c.want)
		}
	}
	reasons := []struct{ reason, want string }{
		{"Evicted", store.CategoryNodePressure}, {"Preempting", ""}, {"NodeLost", ""}, {"", ""},
	}
	for _, c := range reasons {
		if got := PodReasonCategory(c.reason); got != c.want {
			t.Errorf("PodReasonCategory(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

func TestEventCategoryTable(t *testing.T) {
	cases := []struct {
		reason    string
		component string
		want      *string
	}{
		{"FailedScheduling", "default-scheduler", ptr(store.CategoryScheduling)},
		{"Failed", "kubelet", ptr(store.CategoryImagePull)},
		{"ErrImagePull", "kubelet", ptr(store.CategoryImagePull)},
		{"ImagePullBackOff", "kubelet", ptr(store.CategoryImagePull)},
		{"InvalidImageName", "kubelet", ptr(store.CategoryImagePull)},
		{"CreateContainerConfigError", "kubelet", ptr(store.CategoryConfig)},
		{"CreateContainerError", "kubelet", ptr(store.CategoryConfig)},
		{"Unhealthy", "kubelet", ptr(store.CategoryProbe)},
		{"OOMKilling", "kubelet", ptr(store.CategoryOOM)},
		{"Evicted", "kubelet", ptr(store.CategoryNodePressure)},
		{"Evicted", "", ptr(store.CategoryNodePressure)},
		{"Evicted", "node-drainer", ptr(store.CategoryRescheduled)},
		{"Evicted", "default-scheduler", ptr(store.CategoryRescheduled)},
		{"BackOff", "kubelet", nil},
		{"Killing", "kubelet", nil}, {"Preempted", "default-scheduler", nil},
		{"Preempting", "default-scheduler", nil}, {"Pulling", "kubelet", nil}, {"Pulled", "kubelet", nil},
		{"Scheduled", "default-scheduler", nil}, {"Started", "kubelet", nil}, {"Created", "kubelet", nil},
		{"", "", nil}, {"", "node-drainer", nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, EventCategory(c.reason, c.component)); d != "" {
			t.Errorf("EventCategory(%q, %q): %s", c.reason, c.component, d)
		}
	}
	// Evicted is the only reason whose meaning depends on who reported it;
	// every other reason means the same thing from any component.
	for _, reason := range []string{"FailedScheduling", "Failed", "ErrImagePull", "ImagePullBackOff",
		"InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "Unhealthy",
		"OOMKilling", "BackOff", "Killing", "Preempted", ""} {
		base := EventCategory(reason, "")
		for _, component := range []string{"kubelet", "default-scheduler", "node-drainer"} {
			if d := cmp.Diff(base, EventCategory(reason, component)); d != "" {
				t.Errorf("EventCategory(%q, %q) differs from the componentless mapping: %s", reason, component, d)
			}
		}
	}
}

// Categories key on the reason, for an event its source component, and
// whether the pod is terminating: phase lies (CrashLoopBackOff reports
// Running), exit codes are ambiguous (137 is OOM or a grace-period kill) and
// messages are prose. Only category.go may map reasons, and it may not touch
// those fields. stuck is the one category no reason produces: it is opened
// by elapsed time alone, and the reasons a stuck pod does report are the
// ones a starting pod reports too.
func TestCategoriesNeverReadPhaseExitCodeOrMessage(t *testing.T) {
	for _, reason := range []string{"ContainerCreating", "PodInitializing", "Pending", "SchedulingGated", ""} {
		for _, state := range []string{store.StateWaiting, store.StateRunning, store.StateTerminated} {
			for _, terminating := range []bool{false, true} {
				if got := ContainerCategory(state, reason, "", terminating); got == store.CategoryStuck {
					t.Errorf("ContainerCategory(%q, %q, \"\", %v) = %q", state, reason, terminating, got)
				}
			}
		}
		if got := ConditionCategory("PodScheduled", "False", reason); got == store.CategoryStuck {
			t.Errorf("ConditionCategory(\"PodScheduled\", \"False\", %q) = %q", reason, got)
		}
		if got := PodReasonCategory(reason); got == store.CategoryStuck {
			t.Errorf("PodReasonCategory(%q) = %q", reason, got)
		}
		if got := EventCategory(reason, "kubelet"); got != nil && *got == store.CategoryStuck {
			t.Errorf("EventCategory(%q) = %q", reason, *got)
		}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "category.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Phase", "ExitCode", "Message", "LastMessage", "StatusMessage", "LastTerminatedExitCode":
			t.Errorf("category.go:%d reads .%s", fset.Position(sel.Pos()).Line, sel.Sel.Name)
		}
		return true
	})

	for _, dir := range []string{".", "../ingest"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || (dir == "." && name == "category.go") {
				continue
			}
			pf, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range pf.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && strings.HasSuffix(fn.Name.Name, "Category") {
					t.Errorf("%s/%s defines %s; category mapping belongs in incident/category.go", dir, name, fn.Name.Name)
				}
			}
		}
	}
}
