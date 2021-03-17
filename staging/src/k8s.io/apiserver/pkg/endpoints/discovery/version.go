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
	"net/http"
	"regexp"
	"strings"

	restful "github.com/emicklei/go-restful"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
)

type APIResourceLister interface {
	ListAPIResources() []metav1.APIResource
}

type APIResourceListerFunc func() []metav1.APIResource

func (f APIResourceListerFunc) ListAPIResources() []metav1.APIResource {
	return f()
}

// APIVersionHandler creates a webservice serving the supported resources for the version
// E.g., such a web service will be registered at /apis/extensions/v1beta1.
type APIVersionHandler struct {
	serializer runtime.NegotiatedSerializer

	groupVersion      schema.GroupVersion
	apiResourceLister APIResourceLister
}

// HACK: support the case when we can add core or other legacy scheme resources through CRDs (KCP scenario)
var ContributedResources map[schema.GroupVersion]APIResourceLister = map[schema.GroupVersion]APIResourceLister{}

func withContributedResources(groupVersion schema.GroupVersion, apiResourceLister APIResourceLister) APIResourceLister {
	return APIResourceListerFunc(func() []metav1.APIResource {
		result := apiResourceLister.ListAPIResources()
		if additionalResources := ContributedResources[groupVersion]; additionalResources != nil {
			result = append(result, additionalResources.ListAPIResources()...)
		}
		return result
	})
} 

func IsAPIContributed(path string) bool {
	for gv, resourceLister := range ContributedResources {
		prefix := gv.Group
		if prefix != "" {
			prefix = "/apis/" + prefix + "/" + gv.Version + "/"
		} else { 
			prefix = "/api/" + gv.Version + "/"
		}
		if !strings.HasPrefix(path, prefix) {
			return false
		}

		for _, resource := range resourceLister.ListAPIResources() {
			if strings.HasPrefix(path, prefix + resource.Name) {
				return true
			}
			if resource.Namespaced {
				if matched, _ := regexp.MatchString(prefix + "namespaces/[^/][^/]*/" + resource.Name + "(/[^/].*)?", path); matched {
					return true
				}
			} 
		} 
	}
	return false
}

func NewAPIVersionHandler(serializer runtime.NegotiatedSerializer, groupVersion schema.GroupVersion, apiResourceLister APIResourceLister) *APIVersionHandler {
	if keepUnversioned(groupVersion.Group) {
		// Because in release 1.1, /apis/extensions returns response with empty
		// APIVersion, we use stripVersionNegotiatedSerializer to keep the
		// response backwards compatible.
		serializer = stripVersionNegotiatedSerializer{serializer}
	}

	return &APIVersionHandler{
		serializer:        serializer,
		groupVersion:      groupVersion,
		apiResourceLister: withContributedResources(groupVersion, apiResourceLister),
	}
}

func (s *APIVersionHandler) AddToWebService(ws *restful.WebService) {
	mediaTypes, _ := negotiation.MediaTypesForSerializer(s.serializer)
	ws.Route(ws.GET("/").To(s.handle).
		Doc("get available resources").
		Operation("getAPIResources").
		Produces(mediaTypes...).
		Consumes(mediaTypes...).
		Writes(metav1.APIResourceList{}))
}

// handle returns a handler which will return the api.VersionAndVersion of the group.
func (s *APIVersionHandler) handle(req *restful.Request, resp *restful.Response) {
	s.ServeHTTP(resp.ResponseWriter, req.Request)
}

func (s *APIVersionHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	responsewriters.WriteObjectNegotiated(s.serializer, negotiation.DefaultEndpointRestrictions, schema.GroupVersion{}, w, req, http.StatusOK,
		&metav1.APIResourceList{GroupVersion: s.groupVersion.String(), APIResources: s.apiResourceLister.ListAPIResources()})
}
