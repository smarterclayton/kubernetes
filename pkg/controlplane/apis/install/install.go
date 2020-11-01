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

package install

import (
	"k8s.io/kubernetes/pkg/api/controlplanescheme"
	auditregistrationinstall "k8s.io/kubernetes/pkg/apis/auditregistration/install"
	authenticationinstall "k8s.io/kubernetes/pkg/apis/authentication/install"
	authorizationinstall "k8s.io/kubernetes/pkg/apis/authorization/install"
	certificatesinstall "k8s.io/kubernetes/pkg/apis/certificates/install"
	coordinationinstall "k8s.io/kubernetes/pkg/apis/coordination/install"
	controlplaneinstall "k8s.io/kubernetes/pkg/apis/core/install/controlplane"
	eventsinstall "k8s.io/kubernetes/pkg/apis/events/install"
	flowcontrolinstall "k8s.io/kubernetes/pkg/apis/flowcontrol/install"
	rbacinstall "k8s.io/kubernetes/pkg/apis/rbac/install"
)

func init() {
	controlplaneinstall.Install(controlplanescheme.Scheme)
	auditregistrationinstall.Install(controlplanescheme.Scheme)
	authenticationinstall.Install(controlplanescheme.Scheme)
	authorizationinstall.Install(controlplanescheme.Scheme)
	certificatesinstall.Install(controlplanescheme.Scheme)
	coordinationinstall.Install(controlplanescheme.Scheme)
	rbacinstall.Install(controlplanescheme.Scheme)
	flowcontrolinstall.Install(controlplanescheme.Scheme)
	eventsinstall.Install(controlplanescheme.Scheme)
}
