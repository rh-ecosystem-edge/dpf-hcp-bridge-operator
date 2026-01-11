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
	"time"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	provisioningv1alpha1 "github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/api/v1alpha1"
)

const (
	// DeletionTimeout is the maximum time to wait for HostedCluster deletion (30 minutes)
	DeletionTimeout = 30 * time.Minute

	// DeletionRequeueInterval is the interval between deletion status checks (10 seconds)
	DeletionRequeueInterval = 10 * time.Second
)

// FinalizerManager handles finalizer-based cleanup for DPFHCPBridge resources
type FinalizerManager struct {
	client.Client
}

// NewFinalizerManager creates a new FinalizerManager
func NewFinalizerManager(c client.Client) *FinalizerManager {
	return &FinalizerManager{Client: c}
}

// HandleFinalizerCleanup performs cleanup when DPFHCPBridge is being deleted
// This function:
// 1. Deletes HostedCluster CR in the same namespace as DPFHCPBridge
// 2. Waits for HostedCluster to be fully deleted (polls until NotFound)
// 3. Deletes NodePool CR in the same namespace as DPFHCPBridge
// 4. Waits for NodePool to be fully deleted (polls until NotFound)
// 5. Deletes copied/generated secrets (pull-secret, ssh-key, etcd-encryption-key)
// 6. Updates status with cleanup progress
// 7. Returns without removing finalizer if cleanup fails or times out
//
// The finalizer is removed by the caller ONLY when this function returns success (no error)
//
// Timeout handling:
// - Requeues every 10 seconds while waiting for HostedCluster/NodePool deletion
// - Times out after 30 minutes
// - On timeout: Sets hostedClusterCleanup = "Failed", keeps finalizer, stops requeuing
func (fm *FinalizerManager) HandleFinalizerCleanup(ctx context.Context, cr *provisioningv1alpha1.DPFHCPBridge) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues(
		"feature", "hostedcluster-finalizer",
		"dpfhcpbridge", fmt.Sprintf("%s/%s", cr.Namespace, cr.Name),
	)

	log.Info("Starting finalizer cleanup",
		"phase", cr.Status.Phase)

	// Set cleanup condition to InProgress using meta package
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               provisioningv1alpha1.HostedClusterCleanup,
		Status:             metav1.ConditionFalse,
		Reason:             "CleanupInProgress",
		Message:            "Deleting HostedCluster and associated resources",
		LastTransitionTime: metav1.Now(),
	})
	if err := fm.Status().Update(ctx, cr); err != nil {
		log.Error(err, "Failed to update cleanup condition to InProgress")
		return ctrl.Result{}, err
	}

	// Calculate elapsed time since deletion started
	deletionTimestamp := cr.DeletionTimestamp
	if deletionTimestamp == nil {
		log.Error(nil, "DeletionTimestamp is nil but cleanup was triggered")
		return ctrl.Result{}, fmt.Errorf("deletionTimestamp is nil")
	}

	elapsedTime := time.Since(deletionTimestamp.Time)
	if elapsedTime > DeletionTimeout {
		// Timeout exceeded - fail cleanup and keep finalizer
		log.Error(nil, "HostedCluster deletion timeout exceeded",
			"timeout", DeletionTimeout,
			"elapsed", elapsedTime)

		// Set cleanup condition to Failed
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
			Type:               provisioningv1alpha1.HostedClusterCleanup,
			Status:             metav1.ConditionFalse,
			Reason:             "CleanupTimeout",
			Message:            fmt.Sprintf("HostedCluster deletion timeout exceeded after %v", elapsedTime),
			LastTransitionTime: metav1.Now(),
		})
		if err := fm.Status().Update(ctx, cr); err != nil {
			log.Error(err, "Failed to update cleanup condition to Failed")
		}

		// Return nil error to stop automatic requeuing
		// The finalizer remains, keeping the CR in Terminating state
		// Manual intervention is required to investigate and remove the finalizer
		return ctrl.Result{}, nil
	}

	// Step 1: Delete HostedCluster and wait for it to be fully removed
	hcDeleted, err := fm.deleteResource(ctx, cr, &hyperv1.HostedCluster{}, "HostedCluster")
	if err != nil {
		log.Error(err, "Failed to delete HostedCluster")
		return ctrl.Result{}, err
	}

	if !hcDeleted {
		// HostedCluster still exists, requeue to check again
		remainingTime := DeletionTimeout - elapsedTime
		log.Info("Waiting for HostedCluster deletion",
			"elapsed", elapsedTime,
			"remaining", remainingTime,
			"requeueAfter", DeletionRequeueInterval)
		return ctrl.Result{RequeueAfter: DeletionRequeueInterval}, nil
	}

	// Step 2: Delete NodePool and wait for it to be fully removed
	log.Info("HostedCluster deleted, deleting NodePool")
	npDeleted, err := fm.deleteResource(ctx, cr, &hyperv1.NodePool{}, "NodePool")
	if err != nil {
		log.Error(err, "Failed to delete NodePool")
		return ctrl.Result{}, err
	}

	if !npDeleted {
		// NodePool still exists, requeue to check again
		remainingTime := DeletionTimeout - elapsedTime
		log.Info("Waiting for NodePool deletion",
			"elapsed", elapsedTime,
			"remaining", remainingTime,
			"requeueAfter", DeletionRequeueInterval)
		return ctrl.Result{RequeueAfter: DeletionRequeueInterval}, nil
	}

	// Step 3: Delete secrets
	log.Info("NodePool deleted, deleting secrets")
	if err := fm.deleteSecrets(ctx, cr); err != nil {
		log.Error(err, "Failed to delete secrets")
		return ctrl.Result{}, err
	}

	// All cleanup complete - set condition to Succeeded
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               provisioningv1alpha1.HostedClusterCleanup,
		Status:             metav1.ConditionTrue,
		Reason:             "CleanupSucceeded",
		Message:            "HostedCluster and associated resources deleted successfully",
		LastTransitionTime: metav1.Now(),
	})
	if err := fm.Status().Update(ctx, cr); err != nil {
		log.Error(err, "Failed to update cleanup condition to Succeeded")
		return ctrl.Result{}, err
	}

	log.Info("Finalizer cleanup completed successfully")
	return ctrl.Result{}, nil
}

// deleteResource is a generic function to delete a Kubernetes resource and wait for deletion
// Returns true when resource is fully deleted (NotFound), false if still exists
func (fm *FinalizerManager) deleteResource(
	ctx context.Context,
	cr *provisioningv1alpha1.DPFHCPBridge,
	obj client.Object,
	resourceKind string,
) (bool, error) {
	log := logf.FromContext(ctx)

	// Construct the resource key from CR name (HostedCluster and NodePool use same name as CR)
	key := types.NamespacedName{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}

	err := fm.Get(ctx, key, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Resource is fully deleted
			log.Info(fmt.Sprintf("%s deleted successfully", resourceKind),
				resourceKind, key.Name,
				"namespace", key.Namespace)
			return true, nil
		}
		// Handle "no matches for kind" error (CRD not installed) as if resource doesn't exist
		if meta.IsNoMatchError(err) {
			log.V(1).Info(fmt.Sprintf("%s CRD not installed, treating as deleted", resourceKind),
				resourceKind, key.Name,
				"namespace", key.Namespace)
			return true, nil
		}
		return false, fmt.Errorf("failed to get %s: %w", resourceKind, err)
	}

	// Resource still exists
	deletionTimestamp := obj.GetDeletionTimestamp()
	if deletionTimestamp == nil {
		// Resource not yet marked for deletion, delete it now
		log.Info(fmt.Sprintf("Deleting %s", resourceKind),
			resourceKind, key.Name,
			"namespace", key.Namespace)

		if err := fm.Delete(ctx, obj); err != nil {
			if apierrors.IsNotFound(err) {
				// Already deleted (race condition)
				return true, nil
			}
			return false, fmt.Errorf("failed to delete %s: %w", resourceKind, err)
		}
		log.Info(fmt.Sprintf("%s deletion initiated", resourceKind),
			resourceKind, key.Name,
			"namespace", key.Namespace)
	} else {
		// Resource is marked for deletion but still exists (finalizers running)
		elapsedDeletion := time.Since(deletionTimestamp.Time)
		log.V(1).Info(fmt.Sprintf("%s deletion in progress (finalizers running)", resourceKind),
			resourceKind, key.Name,
			"namespace", key.Namespace,
			"deletionElapsed", elapsedDeletion)
	}

	// Resource still exists, need to wait
	return false, nil
}

// deleteSecrets deletes all copied/generated secrets
func (fm *FinalizerManager) deleteSecrets(ctx context.Context, cr *provisioningv1alpha1.DPFHCPBridge) error {
	log := logf.FromContext(ctx)

	secretNames := []string{
		fmt.Sprintf("%s-pull-secret", cr.Name),
		fmt.Sprintf("%s-ssh-key", cr.Name),
		fmt.Sprintf("%s-etcd-encryption-key", cr.Name),
	}

	for _, secretName := range secretNames {
		if err := fm.deleteSecret(ctx, cr.Namespace, secretName); err != nil {
			log.Error(err, "Failed to delete secret",
				"secret", secretName,
				"namespace", cr.Namespace)
			return err
		}
	}

	log.Info("All secrets deleted successfully",
		"count", len(secretNames),
		"namespace", cr.Namespace)

	return nil
}

// deleteSecret deletes a single secret
func (fm *FinalizerManager) deleteSecret(ctx context.Context, namespace, secretName string) error {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	err := fm.Get(ctx, secretKey, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Secret already deleted
			log.V(1).Info("Secret already deleted",
				"secret", secretName,
				"namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// Delete secret
	log.V(1).Info("Deleting secret",
		"secret", secretName,
		"namespace", namespace)

	if err := fm.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted (race condition)
			return nil
		}
		return fmt.Errorf("failed to delete secret %s: %w", secretName, err)
	}

	log.Info("Secret deleted successfully",
		"secret", secretName,
		"namespace", namespace)

	return nil
}

// GetDeletionElapsedTime calculates the elapsed time since deletion started
func GetDeletionElapsedTime(deletionTimestamp *metav1.Time) time.Duration {
	if deletionTimestamp == nil {
		return 0
	}
	return time.Since(deletionTimestamp.Time)
}

// IsDeletionTimeoutExceeded checks if the deletion timeout has been exceeded
func IsDeletionTimeoutExceeded(deletionTimestamp *metav1.Time) bool {
	elapsed := GetDeletionElapsedTime(deletionTimestamp)
	return elapsed > DeletionTimeout
}
