/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

// Operator-specific constants that may change if CR is renamed
const (
	// DPFHCPBridgeName is the resource kind name used in logs, metrics, events, etc.
	// If the CR is renamed, update this constant once and it propagates everywhere.
	DPFHCPBridgeName = "dpfhcpbridge"
)

// Label keys for cross-namespace resource ownership tracking
const (
	// LabelDPFHCPBridgeName is the label key for the DPFHCPBridge name
	// Used to track resources owned by a specific DPFHCPBridge across namespaces
	LabelDPFHCPBridgeName = "dpfhcpbridge.dpu.hcp.io/name"

	// LabelDPFHCPBridgeNamespace is the label key for the DPFHCPBridge namespace
	// Used to track resources owned by a specific DPFHCPBridge across namespaces
	LabelDPFHCPBridgeNamespace = "dpfhcpbridge.dpu.hcp.io/namespace"
)

// Namespace constants
const (
	// OpenshiftOperatorsNamespace is the namespace for OpenShift operator resources
	// Used for MetalLB resources and other operator-managed resources
	OpenshiftOperatorsNamespace = "openshift-operators"
)
