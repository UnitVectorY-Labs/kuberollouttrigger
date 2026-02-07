package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Restarter handles Kubernetes Deployment rollout restarts.
type Restarter struct {
	clientset kubernetes.Interface
	logger    *slog.Logger
}

// NewRestarter creates a new Restarter using the given kubeconfig path.
// If kubeconfigPath is empty, in-cluster config is used.
func NewRestarter(kubeconfigPath string, logger *slog.Logger) (*Restarter, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &Restarter{
		clientset: clientset,
		logger:    logger,
	}, nil
}

// NewRestarterWithClient creates a Restarter with an injected Kubernetes clientset (for testing).
func NewRestarterWithClient(clientset kubernetes.Interface, logger *slog.Logger) *Restarter {
	return &Restarter{
		clientset: clientset,
		logger:    logger,
	}
}

// MatchingDeployment describes a Deployment that matches an image reference.
type MatchingDeployment struct {
	Namespace      string
	Name           string
	ContainerNames []string
}

// FindMatchingDeployments lists all Deployments across accessible namespaces
// and returns those with containers matching the given image reference.
func (r *Restarter) FindMatchingDeployments(ctx context.Context, imageRef string) ([]MatchingDeployment, error) {
	deployments, err := r.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var matches []MatchingDeployment
	for _, d := range deployments.Items {
		var containerNames []string
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Image == imageRef {
				containerNames = append(containerNames, c.Name)
			}
		}
		if len(containerNames) > 0 {
			matches = append(matches, MatchingDeployment{
				Namespace:      d.Namespace,
				Name:           d.Name,
				ContainerNames: containerNames,
			})
		}
	}

	return matches, nil
}

// RestartDeployment triggers a rollout restart for the specified Deployment
// by patching the pod template annotation with the current timestamp.
func (r *Restarter) RestartDeployment(ctx context.Context, namespace, name string) error {
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`,
		time.Now().UTC().Format(time.RFC3339),
	)

	_, err := r.clientset.AppsV1().Deployments(namespace).Patch(
		ctx,
		name,
		types.StrategicMergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch deployment %s/%s: %w", namespace, name, err)
	}

	r.logger.Info("triggered rollout restart",
		"namespace", namespace,
		"deployment", name,
	)
	return nil
}
