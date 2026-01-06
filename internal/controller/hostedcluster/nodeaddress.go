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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DetectNodeAddress auto-detects the management cluster node address for NodePort publishing
// Priority: ExternalDNS > ExternalIP > InternalIP
// This matches the HyperShift CLI pattern (GetAPIServerAddressByNode)
func DetectNodeAddress(ctx context.Context, c client.Client) (string, error) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return "", fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("no nodes found in cluster")
	}

	// Use first node and check addresses in priority order
	node := nodes.Items[0]

	// Priority order: ExternalDNS > ExternalIP > InternalIP
	addressTypes := []corev1.NodeAddressType{
		corev1.NodeExternalDNS,
		corev1.NodeExternalIP,
		corev1.NodeInternalIP,
	}

	for _, addrType := range addressTypes {
		for _, addr := range node.Status.Addresses {
			if addr.Type == addrType {
				return addr.Address, nil
			}
		}
	}

	return "", fmt.Errorf("no valid address found on node %s", node.Name)
}
