/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or sied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Reads the pod configuration from an HTTP GET response
package config

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"gopkg.in/v1/yaml"
)

type ConfigSourceURL struct {
	url string
	ch  chan<- interface{}
}

func NewConfigSourceURL(url string, period time.Duration, ch chan<- interface{}) *ConfigSourceURL {
	config := &ConfigSourceURL{
		url: url,
		ch:  ch,
	}
	glog.Infof("Watching URL %s", url)
	go util.Forever(config.run, period)
	return config
}

func (s *ConfigSourceURL) run() {
	if err := s.extractFromURL(); err != nil {
		glog.Errorf("Failed to read URL: %s", err)
	}
}

func (s *ConfigSourceURL) extractFromURL() error {
	request, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("zero-length data received from %v", s.url)
	}

	// First try as if it's a single manifest
	var pod kubelet.Pod
	singleErr := yaml.Unmarshal(data, &pod.Manifest)
	// TODO: replace with validation
	if singleErr == nil && pod.Manifest.Version == "" {
		// If data is a []ContainerManifest, trying to put it into a ContainerManifest
		// will not give an error but also won't set any of the fields.
		// Our docs say that the version field is mandatory, so using that to judge wether
		// this was actually successful.
		singleErr = fmt.Errorf("got blank version field")
	}

	if singleErr == nil {
		pod.Name = fmt.Sprintf("url-")
		s.ch <- kubelet.PodUpdate{[]kubelet.Pod{pod}, kubelet.SET}
		return nil
	}

	// That didn't work, so try an array of manifests.
	var manifests []api.ContainerManifest
	multiErr := yaml.Unmarshal(data, &manifests)
	// We're not sure if the person reading the logs is going to care about the single or
	// multiple manifest unmarshalling attempt, so we need to put both in the logs, as is
	// done at the end. Hence not returning early here.
	if multiErr == nil && len(manifests) > 0 && manifests[0].Version == "" {
		multiErr = fmt.Errorf("got blank version field")
	}
	if multiErr == nil {
		pods := []kubelet.Pod{}
		for i := range manifests {
			pod := kubelet.Pod{Manifest: manifests[i]}
			pod.Name = fmt.Sprintf("url--%d", i+1)
			pods = append(pods, pod)
		}
		s.ch <- kubelet.PodUpdate{pods, kubelet.SET}
		return nil
	}

	return fmt.Errorf("%v: received '%v', but couldn't parse as a "+
		"single manifest (%v: %+v) or as multiple manifests (%v: %+v).\n",
		s.url, string(data), singleErr, pod.Manifest, multiErr, manifests)
}
