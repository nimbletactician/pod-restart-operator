// controller.go
package controllers

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/example/pod-restart-operator/api/v1alpha1"
)

// PodRestartReconciler reconciles a PodRestart object
type PodRestartReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=operator.example.com,resources=podrestarts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.example.com,resources=podrestarts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.example.com,resources=podrestarts/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,resources=pods/log,verbs=get

func (r *PodRestartReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PodRestart", "name", req.NamespacedName)

	// Fetch the PodRestart instance
	podRestart := &operatorv1alpha1.PodRestart{}
	if err := r.Get(ctx, req.NamespacedName, podRestart); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		return ctrl.Result{}, err
	}

	// List pods matching the label selector
	podList := &corev1.PodList{}
	labelSelector, err := metav1.LabelSelectorAsSelector(&podRestart.Spec.PodSelector)
	if err != nil {
		logger.Error(err, "Invalid label selector")
		return ctrl.Result{}, err
	}

	listOpts := []client.ListOption{
		client.InNamespace(req.Namespace),
		client.MatchingLabelsSelector{Selector: labelSelector},
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		logger.Error(err, "Failed to list pods")
		return ctrl.Result{}, err
	}

	// Get the Kubernetes clientset for logs
	config, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check each pod for error conditions
	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		shouldRestart, reason := r.shouldRestartPod(ctx, clientset, pod, podRestart)
		if shouldRestart {
			// Check if minimum time between restarts has elapsed
			if podRestart.Spec.MinTimeBetweenRestarts != nil && podRestart.Status.LastRestartTime != nil {
				sinceLastRestart := time.Since(podRestart.Status.LastRestartTime.Time)
				minTime := podRestart.Spec.MinTimeBetweenRestarts.Duration
				if sinceLastRestart < minTime {
					logger.Info("Skipping restart due to minimum time between restarts not elapsed",
						"pod", pod.Name,
						"timeSinceLastRestart", sinceLastRestart,
						"minimumTime", minTime)
					continue
				}
			}

			// Restart the pod by deleting it (the controller will recreate it)
			logger.Info("Restarting pod due to error condition",
				"pod", pod.Name,
				"reason", reason)

			if err := r.Delete(ctx, &pod); err != nil {
				logger.Error(err, "Failed to delete pod for restart", "pod", pod.Name)
				continue
			}

			// Update the PodRestart status
			patch := client.MergeFrom(podRestart.DeepCopy())
			now := metav1.Now()
			podRestart.Status.LastRestartTime = &now
			podRestart.Status.RestartCount++

			// Add a condition
			condition := metav1.Condition{
				Type:               "PodRestarted",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "ErrorDetected",
				Message:            fmt.Sprintf("Pod %s restarted due to: %s", pod.Name, reason),
			}

			// Update or add the condition
			found := false
			for i, c := range podRestart.Status.Conditions {
				if c.Type == condition.Type {
					podRestart.Status.Conditions[i] = condition
					found = true
					break
				}
			}
			if !found {
				podRestart.Status.Conditions = append(podRestart.Status.Conditions, condition)
			}

			if err := r.Status().Patch(ctx, podRestart, patch); err != nil {
				logger.Error(err, "Failed to update PodRestart status")
			}
		}
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// shouldRestartPod checks if a pod should be restarted based on log patterns or metrics
func (r *PodRestartReconciler) shouldRestartPod(ctx context.Context, clientset *kubernetes.Clientset, pod corev1.Pod, pr *operatorv1alpha1.PodRestart) (bool, string) {
	// Check log patterns if specified
	if len(pr.Spec.ErrorPatterns) > 0 {
		for _, container := range pod.Spec.Containers {
			podLogOpts := corev1.PodLogOptions{
				Container: container.Name,
				// Limit to recent logs (last 5 minutes)
				SinceSeconds: ptr(int64(300)),
			}

			req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
			podLogs, err := req.Stream(ctx)
			if err != nil {
				r.Log.Error(err, "Failed to get pod logs",
					"pod", pod.Name,
					"container", container.Name)
				continue
			}
			defer podLogs.Close()

			// Read logs and check for patterns
			buf := make([]byte, 2048)
			for {
				n, err := podLogs.Read(buf)
				if err != nil {
					break
				}

				logChunk := string(buf[:n])
				for _, pattern := range pr.Spec.ErrorPatterns {
					matched, err := regexp.MatchString(pattern, logChunk)
					if err != nil {
						r.Log.Error(err, "Error matching pattern",
							"pattern", pattern)
						continue
					}

					if matched {
						return true, fmt.Sprintf("Found error pattern '%s' in logs", pattern)
					}
				}
			}
		}
	}

	// Metric conditions would be checked here
	// This is a simplified example - in a real implementation, you would
	// connect to a metrics provider (Prometheus, etc.) and check the conditions
	if len(pr.Spec.MetricConditions) > 0 {
		// This would be replaced with actual metric checking logic
		r.Log.Info("Metric condition checking is not implemented in this example")
	}

	return false, ""
}

// Helper for creating pointers to int64
func ptr(i int64) *int64 {
	return &i
}

// SetupWithManager sets up the controller with the Manager
func (r *PodRestartReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.PodRestart{}).
		Complete(r)
}
