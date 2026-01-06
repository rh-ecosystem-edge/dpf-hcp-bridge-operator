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

package hostedcluster

import (
	"context"
	"fmt"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/api/util/ipnet"
	"github.com/openshift/hypershift/support/infraid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	provisioningv1alpha1 "github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/api/v1alpha1"
)

// HostedClusterManager manages HostedCluster resources
type HostedClusterManager struct {
	client.Client
}

// NewHostedClusterManager creates a new HostedClusterManager
func NewHostedClusterManager(c client.Client) *HostedClusterManager {
	return &HostedClusterManager{Client: c}
}

// CreateOrUpdateHostedCluster creates or updates the HostedCluster resource
// Returns ctrl.Result and error for reconciliation flow
//
// This function:
// - Checks if HostedCluster already exists with matching labels (idempotency)
// - Creates new HostedCluster if it doesn't exist
// - Handles name conflicts (HC exists with different owner labels)
// - Uses infraid.New() for consistent infraID generation
func (hm *HostedClusterManager) CreateOrUpdateHostedCluster(ctx context.Context, cr *provisioningv1alpha1.DPFHCPBridge) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	hcName := cr.Name
	hcNamespace := ClustersNamespace

	// Check if HostedCluster already exists
	existingHC := &hyperv1.HostedCluster{}
	hcKey := types.NamespacedName{Name: hcName, Namespace: hcNamespace}
	err := hm.Get(ctx, hcKey, existingHC)

	if err == nil {
		// HostedCluster exists - verify ownership via labels
		if existingHC.Labels[LabelBridgeName] == cr.Name &&
			existingHC.Labels[LabelBridgeNamespace] == cr.Namespace {
			log.V(1).Info("HostedCluster already exists with matching labels, adopting",
				"hostedCluster", hcName,
				"namespace", hcNamespace)
			// TODO Phase 3: Check if spec needs update
			return ctrl.Result{}, nil
		}

		// Name conflict - HC exists but owned by different DPFHCPBridge
		return ctrl.Result{}, fmt.Errorf("hostedCluster %s exists in %s but is owned by different DPFHCPBridge (labels mismatch)", hcName, hcNamespace)
	}

	if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to check existing HostedCluster: %w", err)
	}

	// HostedCluster doesn't exist - create it
	exposeThroughLB := cr.ShouldExposeThroughLoadBalancer()
	log.Info("Creating HostedCluster",
		"hostedCluster", hcName,
		"namespace", hcNamespace,
		"releaseImage", cr.Spec.OCPReleaseImage,
		"exposeThroughLoadBalancer", exposeThroughLB)

	// Detect node address if using NodePort mode
	var nodeAddress string
	if !exposeThroughLB {
		log.V(1).Info("Detecting node address for NodePort mode")
		addr, err := DetectNodeAddress(ctx, hm.Client)
		if err != nil {
			log.Error(err, "Failed to detect node address")
			return ctrl.Result{}, fmt.Errorf("failed to detect node address: %w", err)
		}
		nodeAddress = addr
		log.Info("Detected node address", "address", nodeAddress)
	}

	hc := hm.buildHostedCluster(cr, nodeAddress)

	if err := hm.Create(ctx, hc); err != nil {
		log.Error(err, "Failed to create HostedCluster",
			"hostedCluster", hcName,
			"namespace", hcNamespace)
		return ctrl.Result{}, fmt.Errorf("failed to create HostedCluster: %w", err)
	}

	log.Info("HostedCluster created successfully",
		"hostedCluster", hcName,
		"namespace", hcNamespace)

	return ctrl.Result{}, nil
}

// buildHostedCluster constructs the HostedCluster spec from DPFHCPBridge fields
// nodeAddress is only used when exposeThroughLoadBalancer=false (NodePort mode)
func (hm *HostedClusterManager) buildHostedCluster(cr *provisioningv1alpha1.DPFHCPBridge, nodeAddress string) *hyperv1.HostedCluster {
	// Build etcd storage spec
	// Only set StorageClassName if explicitly provided (matches HyperShift CLI behavior)
	// If not set, Kubernetes will use the default StorageClass
	etcdStorage := &hyperv1.PersistentVolumeEtcdStorageSpec{
		Size: ptr.To(resource.MustParse("8Gi")),
	}
	if cr.Spec.EtcdStorageClass != "" {
		etcdStorage.StorageClassName = ptr.To(cr.Spec.EtcdStorageClass)
	}

	hc := &hyperv1.HostedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: ClustersNamespace,
			Labels: map[string]string{
				LabelBridgeName:      cr.Name,
				LabelBridgeNamespace: cr.Namespace,
			},
		},
		Spec: hyperv1.HostedClusterSpec{
			// Release image
			Release: hyperv1.Release{
				Image: cr.Spec.OCPReleaseImage,
			},

			// Pull secret reference (copied to clusters namespace)
			PullSecret: corev1.LocalObjectReference{
				Name: fmt.Sprintf("%s-pull-secret", cr.Name),
			},

			// SSH key reference (copied to clusters namespace)
			SSHKey: corev1.LocalObjectReference{
				Name: fmt.Sprintf("%s-ssh-key", cr.Name),
			},

			// DNS configuration
			DNS: hyperv1.DNSSpec{
				BaseDomain: cr.Spec.BaseDomain,
			},

			// ETCD configuration with managed storage
			Etcd: hyperv1.EtcdSpec{
				ManagementType: hyperv1.Managed,
				Managed: &hyperv1.ManagedEtcdSpec{
					Storage: hyperv1.ManagedEtcdStorageSpec{
						Type:             hyperv1.PersistentVolumeEtcdStorage,
						PersistentVolume: etcdStorage,
					},
				},
			},

			// Networking configuration with Other network type
			// Default CIDRs as per feature spec
			Networking: hyperv1.ClusterNetworking{
				NetworkType: hyperv1.Other,
				ServiceNetwork: []hyperv1.ServiceNetworkEntry{
					{CIDR: *ipnet.MustParseCIDR("172.31.0.0/16")},
				},
				ClusterNetwork: []hyperv1.ClusterNetworkEntry{
					{CIDR: *ipnet.MustParseCIDR("10.132.0.0/14")},
				},
				MachineNetwork: []hyperv1.MachineNetworkEntry{},
			},

			// Platform: None (for DPU environments)
			Platform: hyperv1.PlatformSpec{
				Type: hyperv1.NonePlatform,
			},

			// Availability policy from DPFHCPBridge spec
			ControllerAvailabilityPolicy: cr.Spec.ControlPlaneAvailabilityPolicy,

			// InfraID: Generate deterministically from cluster name
			InfraID: infraid.New(cr.Name),

			// Secret encryption with AESCBC
			SecretEncryption: &hyperv1.SecretEncryptionSpec{
				Type: hyperv1.AESCBC,
				AESCBC: &hyperv1.AESCBCSpec{
					ActiveKey: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-etcd-encryption-key", cr.Name),
					},
				},
			},

			// Service publishing strategy (LoadBalancer or NodePort mode)
			Services: BuildServicePublishingStrategy(cr.ShouldExposeThroughLoadBalancer(), nodeAddress),

			// Capabilities: Disable optional cluster capabilities
			// These capabilities are disabled to reduce resource consumption in DPU environments
			Capabilities: &hyperv1.Capabilities{
				Disabled: []hyperv1.OptionalCapability{
					"ImageRegistry",
					"Insights",
					"Console",
					"openshift-samples",
					"Ingress",
					"NodeTuning",
				},
			},

			// NodeSelector: Schedule control plane pods only on master nodes
			// This prevents control plane pods from running on DPU worker nodes
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
		},
	}

	return hc
}
