When your sample CR is deployed:

  1. Registration and Initialization: The operator is built using the Kubernetes controller-runtime framework. It registers the custom resource (CR) and sets up a controller
   that watches for PodRestart resources.
  2. Reconciliation Loop: The controller's Reconcile method runs every 30 seconds. For each PodRestart CR:
    - It finds pods matching the label selector (app: my-application)
    - For each pod, it checks for error conditions
  3. Error Detection:
    - It streams logs from each container in matching pods
    - It applies regex patterns like "OutOfMemoryError" to detect issues
    - If a pattern matches, it triggers a restart
  4. Pod Restart Mechanism:
    - The operator deletes the problematic pod
    - Kubernetes automatically recreates the pod via its parent controller (Deployment/StatefulSet)
    - A minimum time between restarts is enforced (5m in your sample)
  5. Status Updates:
    - Updates LastRestartTime and increments RestartCount
    - Records information about the restart in status conditions

  The metric conditions part is mentioned but not fully implemented in the code.


The reconciler is the core of the pod-restart-operator. Here's a deeper analysis:

  1. RBAC Annotations (lines 31-35):
  These annotations generate RBAC rules that grant the operator permissions to:
    - Manage PodRestart resources and their status
    - Get, list, watch, and delete Pods
    - Access Pod logs
  2. Reconciliation Flow (lines 37-146):
    - The Reconcile method is the heart of the controller
    - It's triggered whenever a PodRestart CR is created/updated/deleted or periodically (every 30s)
    - It follows the Kubernetes "level-triggered" pattern - it should drive the system to the desired state regardless of how many times it's called
  3. Pod Selection (lines 52-68):
    - Converts the label selector from the PodRestart CR to a Kubernetes selector
    - Uses the controller-runtime client to list pods matching the selector
    - Filters pods to only those in the same namespace as the PodRestart CR
  4. Kubernetes Client Initialization (lines 70-78):
    - Creates a Kubernetes clientset for advanced operations (like streaming logs)
    - Uses the in-cluster configuration (assumes running inside Kubernetes)
  5. Pod Analysis (lines 80-143):
    - Skips pods that aren't in Running phase
    - Calls shouldRestartPod to check if restart is needed
    - Enforces minimum time between restarts
    - Deletes pods that need to be restarted
    - Updates the PodRestart status using strategic merge patch
  6. Error Detection (lines 149-203):
    - Streams logs from each container in the pod
    - Checks logs against configured regex patterns
    - Uses a buffer-based approach to read log streams
    - Has placeholder for metric-based conditions
  7. Controller Setup (lines 210-215):
    - Configures the controller to watch PodRestart resources
    - Uses controller-runtime's builder pattern for configuration

  The reconciler follows the Kubernetes "operator pattern" by constantly monitoring and reconciling the actual state with the desired state described in the CR.