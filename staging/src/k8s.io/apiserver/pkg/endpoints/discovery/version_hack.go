/*
Copyright 2017 The Kubernetes Authors.

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

package discovery

import (
	"regexp"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// HACK: support the case when we can add core or other legacy scheme resources through CRDs (KCP scenario)
var ContributedResources map[schema.GroupVersion]APIResourceLister = map[schema.GroupVersion]APIResourceLister{}

func withContributedResources(groupVersion schema.GroupVersion, apiResourceLister APIResourceLister) APIResourceLister {
	return APIResourceListerFunc(func() []metav1.APIResource {
		result := apiResourceLister.ListAPIResources()
		if additionalResources := ContributedResources[groupVersion]; additionalResources != nil {
			result = append(result, additionalResources.ListAPIResources()...)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Name < result[j].Name
		})

		return result
	})
}

// IsAPIContributed returns `true` is the path corresponds to a resource that
// has been contribued to a legacy scheme group from a CRD.
func IsAPIContributed(path string) bool {
	for gv, resourceLister := range ContributedResources {
		prefix := gv.Group
		if prefix != "" {
			prefix = "/apis/" + prefix + "/" + gv.Version + "/"
		} else {
			prefix = "/api/" + gv.Version + "/"
		}
		if !strings.HasPrefix(path, prefix) {
			continue
		}

		for _, resource := range resourceLister.ListAPIResources() {
			if strings.HasPrefix(path, prefix+resource.Name) {
				return true
			}
			if resource.Namespaced {
				if matched, _ := regexp.MatchString(prefix+"namespaces/[^/][^/]*/"+resource.Name+"(/[^/].*)?", path); matched {
					return true
				}
			}
		}
	}
	return false
}
