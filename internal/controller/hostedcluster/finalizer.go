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
// 1. Deletes HostedCluster CR in clusters namespace
// 2. Waits for HostedCluster to be fully deleted (polls until NotFound)
// 3. Deletes NodePool CR explicitly
// 4. Deletes copied/generated secrets (pull-secret, ssh-key, etcd-encryption-key)
// 5. Updates status with cleanup progress
// 6. Returns without removing finalizer if cleanup fails or times out
//
// The finalizer is removed by the caller ONLY when this function returns success (no error)
//
// Timeout handling:
// - Requeues every 10 seconds while waiting for HostedCluster deletion
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

		// Return error to stop requeuing
		return ctrl.Result{}, fmt.Errorf("hostedCluster deletion timeout exceeded after %v", elapsedTime)
	}

	// Step 1: Delete HostedCluster and wait for it to be fully removed
	hcDeleted, err := fm.deleteHostedCluster(ctx, cr)
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

	// Step 2: Delete NodePool
	log.Info("HostedCluster deleted, deleting NodePool")
	if err := fm.deleteNodePool(ctx, cr); err != nil {
		log.Error(err, "Failed to delete NodePool")
		return ctrl.Result{}, err
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

// deleteHostedCluster deletes the HostedCluster and returns true when it's fully deleted
func (fm *FinalizerManager) deleteHostedCluster(ctx context.Context, cr *provisioningv1alpha1.DPFHCPBridge) (bool, error) {
	log := logf.FromContext(ctx)

	hc := &hyperv1.HostedCluster{}
	hcKey := types.NamespacedName{
		Name:      cr.Name,
		Namespace: ClustersNamespace,
	}

	err := fm.Get(ctx, hcKey, hc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// HostedCluster is fully deleted
			log.Info("HostedCluster deleted successfully",
				"hostedCluster", cr.Name,
				"namespace", ClustersNamespace)
			return true, nil
		}
		// Handle "no matches for kind" error (CRD not installed) as if resource doesn't exist
		if meta.IsNoMatchError(err) {
			log.V(1).Info("HostedCluster CRD not installed, treating as deleted",
				"hostedCluster", cr.Name,
				"namespace", ClustersNamespace)
			return true, nil
		}
		return false, fmt.Errorf("failed to get HostedCluster: %w", err)
	}

	// HostedCluster still exists
	if hc.DeletionTimestamp == nil {
		// HostedCluster not yet marked for deletion, delete it now
		log.Info("Deleting HostedCluster",
			"hostedCluster", cr.Name,
			"namespace", ClustersNamespace)

		if err := fm.Delete(ctx, hc); err != nil {
			if apierrors.IsNotFound(err) {
				// Already deleted (race condition)
				return true, nil
			}
			return false, fmt.Errorf("failed to delete HostedCluster: %w", err)
		}
		log.Info("HostedCluster deletion initiated",
			"hostedCluster", cr.Name,
			"namespace", ClustersNamespace)
	} else {
		// HostedCluster is marked for deletion but still exists (HyperShift finalizers running)
		elapsedDeletion := time.Since(hc.DeletionTimestamp.Time)
		log.V(1).Info("HostedCluster deletion in progress (HyperShift finalizers running)",
			"hostedCluster", cr.Name,
			"namespace", ClustersNamespace,
			"deletionElapsed", elapsedDeletion)
	}

	// HostedCluster still exists, need to wait
	return false, nil
}

// deleteNodePool deletes the NodePool CR
func (fm *FinalizerManager) deleteNodePool(ctx context.Context, cr *provisioningv1alpha1.DPFHCPBridge) error {
	log := logf.FromContext(ctx)

	np := &hyperv1.NodePool{}
	npKey := types.NamespacedName{
		Name:      cr.Name,
		Namespace: ClustersNamespace,
	}

	err := fm.Get(ctx, npKey, np)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// NodePool already deleted
			log.V(1).Info("NodePool already deleted",
				"nodePool", cr.Name,
				"namespace", ClustersNamespace)
			return nil
		}
		// Handle "no matches for kind" error (CRD not installed) as if resource doesn't exist
		if meta.IsNoMatchError(err) {
			log.V(1).Info("NodePool CRD not installed, treating as deleted",
				"nodePool", cr.Name,
				"namespace", ClustersNamespace)
			return nil
		}
		return fmt.Errorf("failed to get NodePool: %w", err)
	}

	// Delete NodePool
	log.Info("Deleting NodePool",
		"nodePool", cr.Name,
		"namespace", ClustersNamespace)

	if err := fm.Delete(ctx, np); err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted (race condition)
			return nil
		}
		return fmt.Errorf("failed to delete NodePool: %w", err)
	}

	log.Info("NodePool deleted successfully",
		"nodePool", cr.Name,
		"namespace", ClustersNamespace)

	return nil
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
		if err := fm.deleteSecret(ctx, secretName); err != nil {
			log.Error(err, "Failed to delete secret",
				"secret", secretName,
				"namespace", ClustersNamespace)
			return err
		}
	}

	log.Info("All secrets deleted successfully",
		"count", len(secretNames),
		"namespace", ClustersNamespace)

	return nil
}

// deleteSecret deletes a single secret
func (fm *FinalizerManager) deleteSecret(ctx context.Context, secretName string) error {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: ClustersNamespace,
	}

	err := fm.Get(ctx, secretKey, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Secret already deleted
			log.V(1).Info("Secret already deleted",
				"secret", secretName,
				"namespace", ClustersNamespace)
			return nil
		}
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// Delete secret
	log.V(1).Info("Deleting secret",
		"secret", secretName,
		"namespace", ClustersNamespace)

	if err := fm.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted (race condition)
			return nil
		}
		return fmt.Errorf("failed to delete secret %s: %w", secretName, err)
	}

	log.Info("Secret deleted successfully",
		"secret", secretName,
		"namespace", ClustersNamespace)

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
