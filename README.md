Initialization Process

  1. The main.go file bootstraps the operator:
    - Line 25-28: init() registers both standard Kubernetes types and custom PodRestart types with the runtime scheme
    - Lines 31-44: Command-line flags are parsed for configuration options
    - Line 46: Zap logger is configured
    - Lines 48-59: Creates a new manager that handles:
        - Kubernetes client connections
      - Cache synchronization
      - Webhooks
      - Leader election (if enabled)
      - Health/readiness checks
    - Lines 61-68: Sets up the PodRestartReconciler with the manager, injecting:
        - The Kubernetes client
      - Runtime scheme
      - Logger
    - Lines 70-77: Adds health/readiness check endpoints
    - Lines 79-83: Starts the manager, which:
        - Starts cache synchronization
      - Starts all registered controllers
      - Blocks until graceful shutdown
  2. The SetupWithManager method (line 211-215) registers the controller with the manager:
    - Specifies that this controller watches PodRestart resources
    - Configures controller options like worker counts, predicates, etc.

  Complete Flow When Sample CR is Deployed

  When the sample.yaml CR is deployed:

  1. CR Creation:
    - User applies the sample.yaml with kubectl apply -f sample.yaml
    - Kubernetes API server validates and stores the CR
    - Controller manager's cache receives the new CR event
  2. Event Processing:
    - Controller-runtime framework detects the new/updated CR
    - Creates a reconcile request with the CR's namespace/name
    - Adds request to reconciler's work queue
    - A worker goroutine picks up the request
  3. Reconciliation Start (line 37):
    - Reconcile method is called with the CR's namespace/name
    - Logger captures context for this reconciliation cycle (line 38-39)
    - CR instance is fetched from the API server (lines 42-50)
  4. Pod Selection (lines 52-68):
    - Label selector from CR spec (app: my-application) is converted to Kubernetes selector
    - Pods matching this selector in the same namespace are listed
    - In this case, it would find all pods with the label app: my-application
  5. Kubernetes Client Setup (lines 70-78):
    - Gets an in-cluster config for direct API access
    - Creates a clientset specifically for log streaming
  6. Pod Evaluation Loop (lines 80-143):
    - For each matching pod that's in the Running phase:
    - Calls shouldRestartPod to check for error conditions (line 86)
  7. Error Detection (lines 149-203):
    - For each container in the pod:
        - Gets logs from the last 5 minutes
      - Searches for configured error patterns like "OutOfMemoryError"
      - If a match is found, returns true with reason
    - Metric conditions are acknowledged but not implemented
  8. Restart Decision (back to lines 87-142):
    - If a restart is needed, checks if minimum time between restarts has elapsed
    - If minTimeBetweenRestarts (5m) hasn't passed since last restart, skips this pod
    - Otherwise, deletes the pod to trigger a restart by Kubernetes
    - Updates CR status with:
        - Current time as LastRestartTime
      - Increments RestartCount
      - Adds a condition with details about the restart
  9. Reconciliation End:
    - Method returns with RequeueAfter: 30 * time.Second (line 145)
    - This schedules the next reconciliation in 30 seconds
    - Manager continues processing other requests

  The entire process repeats every 30 seconds, checking all selected pods for error conditions and restarting them if needed, while respecting the minimum time between restarts.