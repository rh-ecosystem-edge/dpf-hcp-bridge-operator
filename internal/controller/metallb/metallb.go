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

package metallb

import (
	"context"
	"fmt"

	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	provisioningv1alpha1 "github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/api/v1alpha1"
	"github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/internal/common"
)

// MetalLBManager handles MetalLB resource management for DPFHCPBridge
type MetalLBManager struct {
	client   client.Client
	recorder record.EventRecorder
}

// NewMetalLBManager creates a new MetalLB manager
func NewMetalLBManager(client client.Client, recorder record.EventRecorder) *MetalLBManager {
	return &MetalLBManager{
		client:   client,
		recorder: recorder,
	}
}

// ConfigureMetalLB orchestrates MetalLB resource configuration for a DPFHCPBridge
// It creates and maintains IPAddressPool and L2Advertisement resources when LoadBalancer exposure is needed.
func (m *MetalLBManager) ConfigureMetalLB(ctx context.Context, bridge *provisioningv1alpha1.DPFHCPBridge) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("bridge", client.ObjectKeyFromObject(bridge))

	// Check if MetalLB configuration is needed
	if !bridge.ShouldExposeThroughLoadBalancer() {
		log.V(1).Info("ShouldExposeThroughLoadBalancer is false, skipping MetalLB configuration")
		return ctrl.Result{}, nil
	}

	log.Info("Starting MetalLB configuration")

	// Configure IPAddressPool
	log.V(1).Info("Configuring IPAddressPool")
	if err := m.ensureIPAddressPool(ctx, bridge); err != nil {
		log.Error(err, "Failed to configure IPAddressPool")

		if condErr := m.setCondition(ctx, bridge, metav1.ConditionFalse, "CreatingIPAddressPool",
			fmt.Sprintf("Failed to create/update IPAddressPool: %v", err)); condErr != nil {
			log.Error(condErr, "Failed to update MetalLBConfigured condition")
		}

		return ctrl.Result{}, err
	}
	log.Info("IPAddressPool configured successfully", "name", bridge.Name, "namespace", common.OpenshiftOperatorsNamespace)

	// Configure L2Advertisement
	log.V(1).Info("Configuring L2Advertisement")
	if err := m.ensureL2Advertisement(ctx, bridge); err != nil {
		log.Error(err, "Failed to configure L2Advertisement")

		if condErr := m.setCondition(ctx, bridge, metav1.ConditionFalse, "L2AdvertisementFailed",
			fmt.Sprintf("Failed to create/update L2Advertisement: %v", err)); condErr != nil {
			log.Error(condErr, "Failed to update MetalLBConfigured condition")
		}

		return ctrl.Result{}, err
	}
	log.Info("L2Advertisement configured successfully", "name", fmt.Sprintf("advertise-%s", bridge.Name), "namespace", common.OpenshiftOperatorsNamespace)

	// Update condition to True - both resources successfully configured
	if err := m.setCondition(ctx, bridge, metav1.ConditionTrue, "MetalLBReady",
		"MetalLB configured successfully"); err != nil {
		log.Error(err, "Failed to update MetalLBConfigured condition")
		return ctrl.Result{}, err
	}

	log.Info("MetalLB configuration complete")
	return ctrl.Result{}, nil
}

// setCondition updates the MetalLBConfigured condition on the DPFHCPBridge CR
// and emits events when the condition changes to avoid event spam
func (m *MetalLBManager) setCondition(ctx context.Context, bridge *provisioningv1alpha1.DPFHCPBridge, status metav1.ConditionStatus, reason, message string) error {
	log := logf.FromContext(ctx)

	condition := metav1.Condition{
		Type:               provisioningv1alpha1.MetalLBConfigured,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: bridge.Generation,
	}

	// Only update if condition actually changed
	if changed := meta.SetStatusCondition(&bridge.Status.Conditions, condition); changed {
		log.V(1).Info("Updating MetalLBConfigured condition",
			"status", status,
			"reason", reason,
			"message", message)

		// Emit event only when condition status/reason changed (avoid spam)
		eventType := "Normal"
		eventReason := "MetalLBConfigured"
		if status == metav1.ConditionFalse {
			eventType = "Warning"
			eventReason = "MetalLBConfigurationFailed"
		}
		m.recorder.Event(bridge, eventType, eventReason, message)

		if err := m.client.Status().Update(ctx, bridge); err != nil {
			return fmt.Errorf("updating MetalLBConfigured condition: %w", err)
		}
	}

	return nil
}

// ensureIPAddressPool creates or updates the IPAddressPool resource
func (m *MetalLBManager) ensureIPAddressPool(ctx context.Context, bridge *provisioningv1alpha1.DPFHCPBridge) error {
	log := logf.FromContext(ctx)

	desired := m.buildIPAddressPool(bridge)

	// Check if IPAddressPool exists
	var existing metallbv1beta1.IPAddressPool
	err := m.client.Get(ctx, client.ObjectKey{
		Name:      bridge.Name,
		Namespace: common.OpenshiftOperatorsNamespace,
	}, &existing)

	if errors.IsNotFound(err) {
		// Create new IPAddressPool
		log.Info("Creating IPAddressPool", "name", desired.Name, "namespace", desired.Namespace)
		if err := m.client.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating IPAddressPool: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting IPAddressPool: %w", err)
	}

	// IPAddressPool exists - verify ownership before updating
	if !m.isOwnedByBridge(&existing, bridge) {
		return fmt.Errorf("IPAddressPool %s/%s exists but is not owned by DPFHCPBridge %s/%s (missing ownership labels)",
			existing.Namespace, existing.Name, bridge.Namespace, bridge.Name)
	}

	// Resource is owned by us - check if spec update is needed (drift correction)
	if common.ResourceNeedsUpdate(&existing, desired) {
		log.Info("Detected spec drift in IPAddressPool, correcting", "name", existing.Name)
		existing.Spec = desired.Spec

		if err := m.client.Update(ctx, &existing); err != nil {
			return fmt.Errorf("updating IPAddressPool: %w", err)
		}

		m.recorder.Event(bridge, "Normal", "MetalLBDriftCorrected",
			fmt.Sprintf("Corrected spec drift in IPAddressPool %s", existing.Name))
	} else {
		log.V(1).Info("IPAddressPool spec already matches desired state", "name", existing.Name)
	}

	return nil
}

// ensureL2Advertisement creates or updates the L2Advertisement resource
func (m *MetalLBManager) ensureL2Advertisement(ctx context.Context, bridge *provisioningv1alpha1.DPFHCPBridge) error {
	log := logf.FromContext(ctx)

	desired := m.buildL2Advertisement(bridge)

	// Check if L2Advertisement exists
	var existing metallbv1beta1.L2Advertisement
	err := m.client.Get(ctx, client.ObjectKey{
		Name:      fmt.Sprintf("advertise-%s", bridge.Name),
		Namespace: common.OpenshiftOperatorsNamespace,
	}, &existing)

	if errors.IsNotFound(err) {
		// Create new L2Advertisement
		log.Info("Creating L2Advertisement", "name", desired.Name, "namespace", desired.Namespace)
		if err := m.client.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating L2Advertisement: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting L2Advertisement: %w", err)
	}

	// L2Advertisement exists - verify ownership before updating
	if !m.isOwnedByBridge(&existing, bridge) {
		return fmt.Errorf("L2Advertisement %s/%s exists but is not owned by DPFHCPBridge %s/%s (missing ownership labels)",
			existing.Namespace, existing.Name, bridge.Namespace, bridge.Name)
	}

	// Resource is owned by us - check if spec update is needed (drift correction)
	if common.ResourceNeedsUpdate(&existing, desired) {
		log.Info("Detected spec drift in L2Advertisement, correcting", "name", existing.Name)
		existing.Spec = desired.Spec

		if err := m.client.Update(ctx, &existing); err != nil {
			return fmt.Errorf("updating L2Advertisement: %w", err)
		}

		m.recorder.Event(bridge, "Normal", "MetalLBDriftCorrected",
			fmt.Sprintf("Corrected spec drift in L2Advertisement %s", existing.Name))
	} else {
		log.V(1).Info("L2Advertisement spec already matches desired state", "name", existing.Name)
	}

	return nil
}

// buildIPAddressPool constructs the desired IPAddressPool from DPFHCPBridge spec
func (m *MetalLBManager) buildIPAddressPool(bridge *provisioningv1alpha1.DPFHCPBridge) *metallbv1beta1.IPAddressPool {
	return &metallbv1beta1.IPAddressPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bridge.Name,
			Namespace: common.OpenshiftOperatorsNamespace,
			Labels: map[string]string{
				common.LabelDPFHCPBridgeName:      bridge.Name,
				common.LabelDPFHCPBridgeNamespace: bridge.Namespace,
			},
		},
		Spec: metallbv1beta1.IPAddressPoolSpec{
			Addresses: []string{
				fmt.Sprintf("%s/32", bridge.Spec.VirtualIP),
			},
			AllocateTo: &metallbv1beta1.ServiceAllocation{
				Namespaces: []string{
					fmt.Sprintf("clusters-%s", bridge.Name),
				},
			},
			AutoAssign: ptr.To(true),
		},
	}
}

// buildL2Advertisement constructs the desired L2Advertisement from DPFHCPBridge spec
func (m *MetalLBManager) buildL2Advertisement(bridge *provisioningv1alpha1.DPFHCPBridge) *metallbv1beta1.L2Advertisement {
	return &metallbv1beta1.L2Advertisement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("advertise-%s", bridge.Name),
			Namespace: common.OpenshiftOperatorsNamespace,
			Labels: map[string]string{
				common.LabelDPFHCPBridgeName:      bridge.Name,
				common.LabelDPFHCPBridgeNamespace: bridge.Namespace,
			},
		},
		Spec: metallbv1beta1.L2AdvertisementSpec{
			IPAddressPools: []string{bridge.Name},
		},
	}
}

// isOwnedByBridge checks if a resource has the correct ownership labels for the given DPFHCPBridge.
// This prevents taking ownership of resources created by other operators or users.
func (m *MetalLBManager) isOwnedByBridge(obj client.Object, bridge *provisioningv1alpha1.DPFHCPBridge) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}

	bridgeName, hasBridgeName := labels[common.LabelDPFHCPBridgeName]
	bridgeNamespace, hasBridgeNamespace := labels[common.LabelDPFHCPBridgeNamespace]

	return hasBridgeName && hasBridgeNamespace &&
		bridgeName == bridge.Name &&
		bridgeNamespace == bridge.Namespace
}
