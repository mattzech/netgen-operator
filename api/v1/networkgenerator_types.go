/*
Copyright 2023 Atlas.
*/

package v1

import (
	//"github.com/docker/docker/integration-cli/environment"
	//ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	v1alpha1 "github.com/aws-controllers-k8s/ec2-controller/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkGeneratorSpec defines the desired state of NetworkGenerator
type NetworkGeneratorSpec struct {
	//+kubebuilder:validation:Required
	CIDRBlocks []*string `json:"cidrBlocks,omitempty"`
	//+kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Environment string `json:"environment,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	//+kubebuilder:validation:Required
	Region string `json:"region,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ExistingAWSResources []ExistingAWSResources `json:"existingAWSResources,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	//+kubebuilder:validation:Required
	SubnetDesign string `json:"subnetDesign,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	//+kubebuilder:validation:Required
	VPCEndpoints []string `json:"vpcEndpoints,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Tags []*v1alpha1.Tag `json:"tags,omitempty"`
}

// NetworkGeneratorStatus defines the observed state of NetworkGenerator
type NetworkGeneratorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// To be used as AdoptedResources
type ExistingAWSResources struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Name string `json:"name,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ResourceID string `json:"resourceID,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Kind string `json:"kind,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	IPv4CIDR string `json:"ipv4CIDR,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NetworkGenerator is the Schema for the networkgenerators API
type NetworkGenerator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkGeneratorSpec   `json:"spec,omitempty"`
	Status NetworkGeneratorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NetworkGeneratorList contains a list of NetworkGenerator
type NetworkGeneratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkGenerator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkGenerator{}, &NetworkGeneratorList{})
}
