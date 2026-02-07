package k8s

import (
	"context"
	"log/slog"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func createTestDeployment(namespace, name string, images ...string) *appsv1.Deployment {
	containers := make([]corev1.Container, len(images))
	for i, img := range images {
		containers[i] = corev1.Container{
			Name:  "container-" + string(rune('a'+i)),
			Image: img,
		}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func TestFindMatchingDeployments_SingleMatch(t *testing.T) {
	client := fake.NewSimpleClientset(
		createTestDeployment("default", "my-app", "ghcr.io/test/myservice:dev"),
		createTestDeployment("default", "other-app", "ghcr.io/test/otherservice:dev"),
	)

	restarter := NewRestarterWithClient(client, testLogger())
	matches, err := restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/myservice:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Name != "my-app" {
		t.Errorf("expected my-app, got %s", matches[0].Name)
	}
	if matches[0].Namespace != "default" {
		t.Errorf("expected default namespace, got %s", matches[0].Namespace)
	}
}

func TestFindMatchingDeployments_MultipleMatches(t *testing.T) {
	client := fake.NewSimpleClientset(
		createTestDeployment("ns1", "app1", "ghcr.io/test/myservice:dev"),
		createTestDeployment("ns2", "app2", "ghcr.io/test/myservice:dev"),
		createTestDeployment("ns3", "app3", "ghcr.io/test/otherservice:dev"),
	)

	restarter := NewRestarterWithClient(client, testLogger())
	matches, err := restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/myservice:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestFindMatchingDeployments_NoMatch(t *testing.T) {
	client := fake.NewSimpleClientset(
		createTestDeployment("default", "my-app", "ghcr.io/test/myservice:prod"),
	)

	restarter := NewRestarterWithClient(client, testLogger())
	matches, err := restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/myservice:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestFindMatchingDeployments_MultipleContainers(t *testing.T) {
	client := fake.NewSimpleClientset(
		createTestDeployment("default", "multi-container",
			"ghcr.io/test/myservice:dev",
			"ghcr.io/test/sidecar:dev",
		),
	)

	restarter := NewRestarterWithClient(client, testLogger())

	// Match first container
	matches, err := restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/myservice:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if len(matches[0].ContainerNames) != 1 {
		t.Errorf("expected 1 container match, got %d", len(matches[0].ContainerNames))
	}

	// Match second container
	matches, err = restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/sidecar:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestFindMatchingDeployments_ExactMatch(t *testing.T) {
	client := fake.NewSimpleClientset(
		createTestDeployment("default", "app1", "ghcr.io/test/myservice:dev"),
		createTestDeployment("default", "app2", "ghcr.io/test/myservice:prod"),
		createTestDeployment("default", "app3", "ghcr.io/test/myservice-extended:dev"),
	)

	restarter := NewRestarterWithClient(client, testLogger())
	matches, err := restarter.FindMatchingDeployments(context.Background(), "ghcr.io/test/myservice:dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match (exact), got %d", len(matches))
	}
	if matches[0].Name != "app1" {
		t.Errorf("expected app1, got %s", matches[0].Name)
	}
}

func TestRestartDeployment(t *testing.T) {
	deploy := createTestDeployment("default", "my-app", "ghcr.io/test/myservice:dev")
	client := fake.NewSimpleClientset(deploy)

	restarter := NewRestarterWithClient(client, testLogger())
	err := restarter.RestartDeployment(context.Background(), "default", "my-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the deployment was patched
	updated, err := client.AppsV1().Deployments("default").Get(context.Background(), "my-app", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	restartedAt, ok := updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]
	if !ok {
		t.Error("expected restartedAt annotation to be set")
	}
	if restartedAt == "" {
		t.Error("expected restartedAt annotation to have a value")
	}
}

func TestRestartDeployment_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()

	restarter := NewRestarterWithClient(client, testLogger())
	err := restarter.RestartDeployment(context.Background(), "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent deployment")
	}
}
