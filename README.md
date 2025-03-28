# Pod Restart Operator - Controller Walkthrough

## Structure Definition (Lines 24-29)
```go
type PodRestartReconciler struct {
    client.Client
    Scheme *runtime.Scheme
    Log    logr.Logger
}
```
- Defines the controller structure with embedded Kubernetes client
- Stores the runtime scheme for API object serialization
- Maintains a logger for operational visibility

## Reconcile Method (Lines 37-146)
The core loop of the controller that handles each PodRestart custom resource.

### Initial Setup (Lines 38-39)
```go
logger := log.FromContext(ctx)
logger.Info("Reconciling PodRestart", "name", req.NamespacedName)
```
- Retrieves a logger from the context
- Logs the start of reconciliation with the resource name

### Fetching the PodRestart CR (Lines 42-50)
```go
podRestart := &operatorv1alpha1.PodRestart{}
if err := r.Get(ctx, req.NamespacedName, podRestart); err != nil {
    if errors.IsNotFound(err) {
        // Request object not found, could have been deleted after reconcile request
        return ctrl.Result{}, nil
    }
    // Error reading the object - requeue the request
    return ctrl.Result{}, err
}
```
- Creates an empty PodRestart object
- Attempts to fetch the CR from the Kubernetes API
- Handles "not found" case (likely deleted) gracefully
- Returns any other errors, triggering a requeue

### Pod Selection (Lines 52-68)
```go
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
```
- Creates an empty list to store matching pods
- Converts the CR's label selector into a Kubernetes selector
- Sets up list options to:
  - Find pods in the same namespace as the CR
  - Match the specified label selector
- Queries the Kubernetes API for matching pods

### Kubernetes Client Setup (Lines 70-78)
```go
config, err := rest.InClusterConfig()
if err != nil {
    return ctrl.Result{}, err
}
clientset, err := kubernetes.NewForConfig(config)
if err != nil {
    return ctrl.Result{}, err
}
```
- Gets the in-cluster configuration for Kubernetes API access
- Creates a clientset specifically for retrieving pod logs

### Pod Evaluation Loop (Lines 80-143)
For each pod that matches the selector, evaluate if it needs to be restarted.

#### Initial Check (Lines 82-84)
```go
if pod.Status.Phase != corev1.PodRunning {
    continue
}
```
- Skips pods that aren't in the "Running" phase

#### Restart Decision Process (Lines 86-142)
```go
shouldRestart, reason := r.shouldRestartPod(ctx, clientset, pod, podRestart)
if shouldRestart {
    // [Further processing...]
}
```
- Calls `shouldRestartPod` to check if the pod meets restart criteria
- If restart is needed, handles timing and executes the restart

#### Minimum Time Between Restarts (Lines 89-98)
```go
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
```
- Checks if a minimum time between restarts is specified and last restart time exists
- Calculates time since the last restart
- If not enough time has passed, skips this pod with a log message

#### Pod Restart Execution (Lines 101-109)
```go
logger.Info("Restarting pod due to error condition",
    "pod", pod.Name,
    "reason", reason)

if err := r.Delete(ctx, &pod); err != nil {
    logger.Error(err, "Failed to delete pod for restart", "pod", pod.Name)
    continue
}
```
- Logs the restart action with details
- Deletes the pod, which triggers Kubernetes to recreate it (via the deployment controller)
- Handles any deletion errors and continues to next pod

#### Status Update (Lines 112-141)
```go
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
```
- Creates a patch from the original CR state
- Updates status fields:
  - Sets last restart time to now
  - Increments the restart count
- Creates a condition entry with restart details
- Either updates an existing condition or adds a new one
- Patches the status subresource with the new information

### Reconciliation Completion (Line 145)
```go
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
```
- Schedules the next reconciliation after 30 seconds
- Returns no error

## shouldRestartPod Method (Lines 148-203)
Determines if a pod needs to be restarted based on error patterns in logs or metrics.

### Log Pattern Checking (Lines 151-192)
```go
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
```
- Checks if any error patterns are defined in the CR
- For each container in the pod:
  - Sets up pod log options to retrieve the last 5 minutes of logs
  - Gets a log stream from the Kubernetes API
  - Reads logs in chunks of 2048 bytes
  - For each log chunk, checks all configured error patterns
  - If a pattern matches, returns true with the reason
- If no patterns are found, continues to metric checks

### Metric Condition Placeholder (Lines 194-200)
```go
if len(pr.Spec.MetricConditions) > 0 {
    // This would be replaced with actual metric checking logic
    r.Log.Info("Metric condition checking is not implemented in this example")
}
```
- Placeholder for metric-based conditions
- Notes that implementation is not included in this example

### Default Return (Line 202)
```go
return false, ""
```
- Returns false with no reason if no restart conditions are met

## Helper Function (Lines 205-208)
```go
func ptr(i int64) *int64 {
    return &i
}
```
- Utility function to create pointers to int64 values
- Used for the SinceSeconds parameter in log options

## Controller Setup (Lines 210-215)
```go
func (r *PodRestartReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&operatorv1alpha1.PodRestart{}).
        Complete(r)
}
```
- Registers the reconciler with the controller manager
- Specifies that this controller watches PodRestart resources
- Completes the controller setup
