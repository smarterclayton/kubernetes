/*
Copyright 2020 The Kubernetes Authors.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// ControlPlaneGroupName is the name of the group when installed in the generic control plane
	ControlPlaneGroupName = "core"

	// SchemeGroupVersion is group version used to register these objects
	ControlPlaneSchemeGroupVersion = schema.GroupVersion{Group: ControlPlaneGroupName, Version: "v1"}

	// ControlPlaneSchemeBuilder object to register various known types for the control plane
	ControlPlaneSchemeBuilder = runtime.NewSchemeBuilder(addControlPlaneKnownTypes)

	// AddToControlPlaneScheme represents a func that can be used to apply all the registered
	// funcs in a scheme
	AddToControlPlaneScheme = ControlPlaneSchemeBuilder.AddToScheme
)

func addControlPlaneKnownTypes(scheme *runtime.Scheme) error {
	if err := scheme.AddIgnoredConversionType(&metav1.TypeMeta{}, &metav1.TypeMeta{}); err != nil {
		return err
	}
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Event{},
		&EventList{},
		&List{},
		&LimitRange{},
		&LimitRangeList{},
		&ResourceQuota{},
		&ResourceQuotaList{},
		&Namespace{},
		&NamespaceList{},
		&ServiceAccount{},
		&ServiceAccountList{},
		&Secret{},
		&SecretList{},
		&SerializedReference{},
		&RangeAllocation{},
		&ConfigMap{},
		&ConfigMapList{},
	)

	return nil
}
