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

import (
	"reflect"
)

// ResourceNeedsUpdate is a generic function that compares the Spec field of Kubernetes resources.
// It returns true if the existing resource spec differs from the desired resource spec.
// This is useful for drift detection and correction in controllers.
func ResourceNeedsUpdate[T any](existing, desired *T) bool {
	// Guard against nil inputs to prevent panic from .Elem()
	// If either is nil, they differ (unless both are nil)
	if existing == nil || desired == nil {
		return existing != desired
	}

	existingVal := reflect.ValueOf(existing).Elem()
	desiredVal := reflect.ValueOf(desired).Elem()

	existingSpec := existingVal.FieldByName("Spec")
	desiredSpec := desiredVal.FieldByName("Spec")
	if existingSpec.IsValid() && desiredSpec.IsValid() {
		return !reflect.DeepEqual(existingSpec.Interface(), desiredSpec.Interface())
	}

	return false
}
