// types.go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodRestartSpec defines the desired state of PodRestart
type PodRestartSpec struct {
	// PodSelector is a label selector to target pods
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// ErrorPatterns is a list of regex patterns to match against pod logs
	ErrorPatterns []string `json:"errorPatterns,omitempty"`

	// MetricConditions defines metric-based conditions that trigger restarts
	MetricConditions []MetricCondition `json:"metricConditions,omitempty"`

	// MinTimeBetweenRestarts is the minimum time to wait between pod restarts
	// +kubebuilder:validation:Format=duration
	MinTimeBetweenRestarts *metav1.Duration `json:"minTimeBetweenRestarts,omitempty"`
}

// MetricCondition defines a metric-based condition for pod restart
type MetricCondition struct {
	// Name of the metric
	Name string `json:"name"`

	// Threshold value for the metric
	Threshold string `json:"threshold"`

	// Operator is the comparison operator (>, <, >=, <=, ==)
	Operator string `json:"operator"`
}

// PodRestartStatus defines the observed state of PodRestart
type PodRestartStatus struct {
	// LastRestartTime is the last time a pod was restarted
	LastRestartTime *metav1.Time `json:"lastRestartTime,omitempty"`

	// RestartCount is the number of restarts performed
	RestartCount int `json:"restartCount"`

	// Conditions represent the latest available observations of the PodRestart state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RestartCount",type=integer,JSONPath=`.status.restartCount`
// +kubebuilder:printcolumn:name="LastRestart",type=date,JSONPath=`.status.lastRestartTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PodRestart is the Schema for the podrestarts API
type PodRestart struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodRestartSpec   `json:"spec,omitempty"`
	Status PodRestartStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PodRestartList contains a list of PodRestart
type PodRestartList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodRestart `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodRestart{}, &PodRestartList{})
}
