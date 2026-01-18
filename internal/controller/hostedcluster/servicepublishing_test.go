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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

var _ = Describe("Service Publishing Strategy Builder", func() {
	Context("LoadBalancer Mode", func() {
		It("should return 4 service publishing strategies", func() {
			strategy := BuildServicePublishingStrategy(true, "")

			Expect(strategy).To(HaveLen(4))
		})

		It("should use LoadBalancer for APIServer", func() {
			strategy := BuildServicePublishingStrategy(true, "")

			apiServerStrategy := findServiceStrategyByType(strategy, hyperv1.APIServer)
			Expect(apiServerStrategy).ToNot(BeNil())
			Expect(apiServerStrategy.Type).To(Equal(hyperv1.LoadBalancer))
		})

		It("should use Route for OAuthServer", func() {
			strategy := BuildServicePublishingStrategy(true, "")

			oauthStrategy := findServiceStrategyByType(strategy, hyperv1.OAuthServer)
			Expect(oauthStrategy).ToNot(BeNil())
			Expect(oauthStrategy.Type).To(Equal(hyperv1.Route))
		})

		It("should use Route for Konnectivity", func() {
			strategy := BuildServicePublishingStrategy(true, "")

			konnectivityStrategy := findServiceStrategyByType(strategy, hyperv1.Konnectivity)
			Expect(konnectivityStrategy).ToNot(BeNil())
			Expect(konnectivityStrategy.Type).To(Equal(hyperv1.Route))
		})

		It("should use Route for Ignition", func() {
			strategy := BuildServicePublishingStrategy(true, "")

			ignitionStrategy := findServiceStrategyByType(strategy, hyperv1.Ignition)
			Expect(ignitionStrategy).ToNot(BeNil())
			Expect(ignitionStrategy.Type).To(Equal(hyperv1.Route))
		})
	})

	Context("NodePort Mode", func() {
		nodeAddress := "192.168.1.100"

		It("should return 5 service publishing strategies including OIDC", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			Expect(strategy).To(HaveLen(5))
		})

		It("should use NodePort for APIServer with correct address", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			apiServerStrategy := findServiceStrategyByType(strategy, hyperv1.APIServer)
			Expect(apiServerStrategy).ToNot(BeNil())
			Expect(apiServerStrategy.Type).To(Equal(hyperv1.NodePort))
			Expect(apiServerStrategy.NodePort).ToNot(BeNil())
			Expect(apiServerStrategy.NodePort.Address).To(Equal(nodeAddress))
		})

		It("should use NodePort for OAuthServer", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			oauthStrategy := findServiceStrategyByType(strategy, hyperv1.OAuthServer)
			Expect(oauthStrategy).ToNot(BeNil())
			Expect(oauthStrategy.Type).To(Equal(hyperv1.NodePort))
			Expect(oauthStrategy.NodePort.Address).To(Equal(nodeAddress))
		})

		It("should use NodePort for OIDC", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			oidcStrategy := findServiceStrategyByType(strategy, hyperv1.OIDC)
			Expect(oidcStrategy).ToNot(BeNil())
			Expect(oidcStrategy.Type).To(Equal(hyperv1.NodePort))
			Expect(oidcStrategy.NodePort.Address).To(Equal(nodeAddress))
		})

		It("should use NodePort for Konnectivity", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			konnectivityStrategy := findServiceStrategyByType(strategy, hyperv1.Konnectivity)
			Expect(konnectivityStrategy).ToNot(BeNil())
			Expect(konnectivityStrategy.Type).To(Equal(hyperv1.NodePort))
			Expect(konnectivityStrategy.NodePort.Address).To(Equal(nodeAddress))
		})

		It("should use NodePort for Ignition", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			ignitionStrategy := findServiceStrategyByType(strategy, hyperv1.Ignition)
			Expect(ignitionStrategy).ToNot(BeNil())
			Expect(ignitionStrategy.Type).To(Equal(hyperv1.NodePort))
			Expect(ignitionStrategy.NodePort.Address).To(Equal(nodeAddress))
		})

		It("should sort services alphabetically", func() {
			strategy := BuildServicePublishingStrategy(false, nodeAddress)

			// Verify services are in alphabetical order
			for i := 0; i < len(strategy)-1; i++ {
				currentService := string(strategy[i].Service)
				nextService := string(strategy[i+1].Service)
				Expect(currentService < nextService).To(BeTrue(),
					"Expected %s to be before %s", currentService, nextService)
			}
		})
	})
})

// Helper function to find strategy for a specific service
func findServiceStrategyByType(strategies []hyperv1.ServicePublishingStrategyMapping, service hyperv1.ServiceType) *hyperv1.ServicePublishingStrategy {
	for _, s := range strategies {
		if s.Service == service {
			return &s.ServicePublishingStrategy
		}
	}
	return nil
}
