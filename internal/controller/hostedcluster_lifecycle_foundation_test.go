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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpuprovisioningv1alpha1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	provisioningv1alpha1 "github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/api/v1alpha1"
	"github.com/rh-ecosystem-edge/dpf-hcp-bridge-operator/internal/controller/hostedcluster"
)

var _ = Describe("HostedCluster Lifecycle - Foundation & Secret Management", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	var (
		ctx              context.Context
		testNamespace    string
		dpuClusterName   string
		bridgeName       string
		pullSecretName   string
		sshKeySecretName string
		ocpReleaseImage  string
		blueFieldImage   string
		baseDomain       string
		etcdStorageClass string
		clusterType      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = "default"
		dpuClusterName = "test-dpucluster-foundation"
		bridgeName = "test-bridge-foundation-" + time.Now().Format("20060102150405")
		pullSecretName = "test-pull-secret-foundation"
		sshKeySecretName = "test-ssh-key-foundation"
		ocpReleaseImage = "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64"
		blueFieldImage = "quay.io/example/bluefield:4.17.0"
		baseDomain = "test-cluster.example.com"
		etcdStorageClass = "standard"
		clusterType = "bf3"

		// Note: Using default namespace for tests, no need to create

		// Ensure clusters namespace exists (created by operator startup)
		clustersNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "clusters",
			},
		}
		err := k8sClient.Create(ctx, clustersNs)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Fail("Failed to create clusters namespace: " + err.Error())
		}

		// Ensure dpf-hcp-bridge-system namespace exists (for ConfigMap)
		operatorNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dpf-hcp-bridge-system",
			},
		}
		err = k8sClient.Create(ctx, operatorNs)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Fail("Failed to create dpf-hcp-bridge-system namespace: " + err.Error())
		}

		// Create ocp-bluefield-images ConfigMap for image resolution
		// Note: The key must be the OCP version (extracted from the release image),
		// not the full image URL (ConfigMap keys cannot contain colons or slashes)
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ocp-bluefield-images",
				Namespace: "dpf-hcp-bridge-system",
			},
			Data: map[string]string{
				"4.17.0": blueFieldImage, // Key is extracted version, not full URL
			},
		}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		// Create DPUCluster
		dpuCluster := &dpuprovisioningv1alpha1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuClusterName,
				Namespace: testNamespace,
			},
			Spec: dpuprovisioningv1alpha1.DPUClusterSpec{
				Type: clusterType,
			},
		}
		Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())

		// Set DPUCluster phase to Ready
		dpuCluster.Status.Phase = dpuprovisioningv1alpha1.PhaseReady
		Expect(k8sClient.Status().Update(ctx, dpuCluster)).To(Succeed())

		// Create pull-secret
		pullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pullSecretName,
				Namespace: testNamespace,
			},
			Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				".dockerconfigjson": []byte(`{"auths":{"quay.io":{"auth":"dGVzdDp0ZXN0MTIz"}}}`),
			},
		}
		Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

		// Create ssh-key
		sshKey := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sshKeySecretName,
				Namespace: testNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"id_rsa.pub": []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ..."),
			},
		}
		Expect(k8sClient.Create(ctx, sshKey)).To(Succeed())
	})

	AfterEach(func() {
		// Clean up DPFHCPBridge
		bridge := &provisioningv1alpha1.DPFHCPBridge{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridgeName,
				Namespace: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, bridge)

		// Clean up DPUCluster
		dpuCluster := &dpuprovisioningv1alpha1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuClusterName,
				Namespace: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, dpuCluster)

		// Clean up user secrets
		pullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pullSecretName,
				Namespace: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, pullSecret)

		sshKey := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sshKeySecretName,
				Namespace: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, sshKey)

		// Clean up secrets in clusters namespace
		pullSecretTarget := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridgeName + "-pull-secret",
				Namespace: "clusters",
			},
		}
		_ = k8sClient.Delete(ctx, pullSecretTarget)

		sshKeyTarget := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridgeName + "-ssh-key",
				Namespace: "clusters",
			},
		}
		_ = k8sClient.Delete(ctx, sshKeyTarget)

		etcdKeyTarget := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridgeName + "-etcd-encryption-key",
				Namespace: "clusters",
			},
		}
		_ = k8sClient.Delete(ctx, etcdKeyTarget)

		// Clean up ConfigMap
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ocp-bluefield-images",
				Namespace: "dpf-hcp-bridge-system",
			},
		}
		_ = k8sClient.Delete(ctx, configMap)
	})

	Context("Finalizer Management", func() {
		It("should add finalizer to DPFHCPBridge on creation", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.HighlyAvailable,
					VirtualIP:                      "192.168.1.100",
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify finalizer is added
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(bridge, FinalizerName)
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Secret Copying to Clusters Namespace", func() {
		It("should copy pull-secret with correct type and labels", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify pull-secret is copied
			pullSecretTarget := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      bridgeName + "-pull-secret",
					Namespace: "clusters",
				}, pullSecretTarget)
			}, timeout, interval).Should(Succeed())

			// Verify secret type
			Expect(pullSecretTarget.Type).To(Equal(corev1.SecretTypeDockerConfigJson))

			// Verify labels
			Expect(pullSecretTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeName, bridgeName))
			Expect(pullSecretTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeNamespace, testNamespace))
			Expect(pullSecretTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelSafeToDelete, "true"))

			// Verify data is copied
			Expect(pullSecretTarget.Data).To(HaveKey(".dockerconfigjson"))
		})

		It("should copy ssh-key with correct type and labels", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify ssh-key is copied
			sshKeyTarget := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      bridgeName + "-ssh-key",
					Namespace: "clusters",
				}, sshKeyTarget)
			}, timeout, interval).Should(Succeed())

			// Verify secret type
			Expect(sshKeyTarget.Type).To(Equal(corev1.SecretTypeOpaque))

			// Verify labels
			Expect(sshKeyTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeName, bridgeName))
			Expect(sshKeyTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeNamespace, testNamespace))
			Expect(sshKeyTarget.Labels).To(HaveKeyWithValue(hostedcluster.LabelSafeToDelete, "true"))

			// Verify data is copied
			Expect(sshKeyTarget.Data).To(HaveKey("id_rsa.pub"))
		})
	})

	Context("ETCD Encryption Key Generation", func() {
		It("should generate ETCD encryption key with correct format", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify ETCD key is generated
			etcdKeySecret := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      bridgeName + "-etcd-encryption-key",
					Namespace: "clusters",
				}, etcdKeySecret)
			}, timeout, interval).Should(Succeed())

			// Verify secret type
			Expect(etcdKeySecret.Type).To(Equal(corev1.SecretTypeOpaque))

			// Verify labels
			Expect(etcdKeySecret.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeName, bridgeName))
			Expect(etcdKeySecret.Labels).To(HaveKeyWithValue(hostedcluster.LabelBridgeNamespace, testNamespace))
			Expect(etcdKeySecret.Labels).To(HaveKeyWithValue(hostedcluster.LabelSafeToDelete, "true"))

			// Verify key length (32 bytes)
			Expect(etcdKeySecret.Data).To(HaveKey(hyperv1.AESCBCKeySecretKey))
			Expect(etcdKeySecret.Data[hyperv1.AESCBCKeySecretKey]).To(HaveLen(32))
		})
	})

	Context("Idempotency", func() {
		It("should not create duplicate secrets on multiple reconciliations", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Wait for initial reconciliation
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      bridgeName + "-etcd-encryption-key",
					Namespace: "clusters",
				}, &corev1.Secret{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Get initial ETCD key for comparison
			initialEtcdKey := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      bridgeName + "-etcd-encryption-key",
				Namespace: "clusters",
			}, initialEtcdKey)).To(Succeed())
			initialKeyBytes := initialEtcdKey.Data[hyperv1.AESCBCKeySecretKey]

			// Trigger another reconciliation by updating a label
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)).To(Succeed())
			bridge.Labels = map[string]string{"test-trigger": "reconcile"}
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			// Wait a bit for potential reconciliation
			time.Sleep(2 * time.Second)

			// Verify ETCD key hasn't changed (idempotency)
			currentEtcdKey := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      bridgeName + "-etcd-encryption-key",
				Namespace: "clusters",
			}, currentEtcdKey)).To(Succeed())
			Expect(currentEtcdKey.Data[hyperv1.AESCBCKeySecretKey]).To(Equal(initialKeyBytes))

			// Verify only one of each secret exists
			secretList := &corev1.SecretList{}
			Expect(k8sClient.List(ctx, secretList, &client.ListOptions{
				Namespace: "clusters",
			})).To(Succeed())

			pullSecretCount := 0
			sshKeyCount := 0
			etcdKeyCount := 0

			for _, secret := range secretList.Items {
				if secret.Labels[hostedcluster.LabelBridgeName] == bridgeName {
					if secret.Name == bridgeName+"-pull-secret" {
						pullSecretCount++
					}
					if secret.Name == bridgeName+"-ssh-key" {
						sshKeyCount++
					}
					if secret.Name == bridgeName+"-etcd-encryption-key" {
						etcdKeyCount++
					}
				}
			}

			Expect(pullSecretCount).To(Equal(1))
			Expect(sshKeyCount).To(Equal(1))
			Expect(etcdKeyCount).To(Equal(1))
		})
	})

	Context("Phase Computation", func() {
		It("should set phase to Pending after successful secret creation", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify phase is Pending after validations and secret creation
			Eventually(func() provisioningv1alpha1.DPFHCPBridgePhase {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)
				if err != nil {
					return ""
				}
				return bridge.Status.Phase
			}, timeout, interval).Should(Equal(provisioningv1alpha1.PhasePending))
		})

		It("should set phase to Deleting when CR is being deleted", func() {
			// Create DPFHCPBridge
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: pullSecretName},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Wait for initial reconciliation
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)
				return err == nil && bridge.Status.Phase == provisioningv1alpha1.PhasePending
			}, timeout, interval).Should(BeTrue())

			// Delete the CR
			Expect(k8sClient.Delete(ctx, bridge)).To(Succeed())

			// Verify phase is Deleting (this verifies updatePhaseFromConditions logic)
			// Note: The CR will be deleted quickly since we don't have real cleanup yet,
			// so we check the phase during deletion
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)
				if err != nil {
					// CR deleted, which is expected
					return true
				}
				// If CR still exists, phase should be Deleting
				return bridge.Status.Phase == provisioningv1alpha1.PhaseDeleting
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Error Handling", func() {
		It("should handle missing user secret gracefully", func() {
			// Create DPFHCPBridge with non-existent pull-secret
			bridge := &provisioningv1alpha1.DPFHCPBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bridgeName,
					Namespace: testNamespace,
				},
				Spec: provisioningv1alpha1.DPFHCPBridgeSpec{
					DPUClusterRef: provisioningv1alpha1.DPUClusterReference{
						Name:      dpuClusterName,
						Namespace: testNamespace,
					},
					BaseDomain:                     baseDomain,
					OCPReleaseImage:                ocpReleaseImage,
					SSHKeySecretRef:                corev1.LocalObjectReference{Name: sshKeySecretName},
					PullSecretRef:                  corev1.LocalObjectReference{Name: "non-existent-secret"},
					EtcdStorageClass:               etcdStorageClass,
					ControlPlaneAvailabilityPolicy: hyperv1.SingleReplica,
				},
			}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())

			// Verify secret is NOT created in clusters namespace due to missing source
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      bridgeName + "-pull-secret",
					Namespace: "clusters",
				}, &corev1.Secret{})
				return apierrors.IsNotFound(err)
			}, time.Second*5, interval).Should(BeTrue())

			// The reconciliation should fail but not crash
			// CR should still exist
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bridgeName, Namespace: testNamespace}, bridge)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
