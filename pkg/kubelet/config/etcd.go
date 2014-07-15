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

// Reads the pod configuration from etcd using the Kubernetes etcd schema
package config

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/tools"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
	"github.com/golang/glog"
)

type ConfigSourceEtcd struct {
	key    string
	client tools.EtcdClient
	ch     chan<- interface{}

	waitDuration time.Duration
}

func NewConfigSourceEtcd(key string, client tools.EtcdClient, period time.Duration, ch chan<- interface{}) *ConfigSourceEtcd {
	config := &ConfigSourceEtcd{
		key:    key,
		client: client,
		ch:     ch,

		waitDuration: period,
	}
	glog.Infof("Watching etcd for %s", key)
	go config.run()
	return config
}

// run loops forever looking for changes to a key in etcd.  If a watch exceeds the
// max waitDuration, we will sleep for another interval beyond that before trying
// again.
func (s *ConfigSourceEtcd) run() {
	index := uint64(0)
	util.Forever(func() {
		for {
			lastIndex, err := s.extractFromResponse(index)
			if err != nil {
				if !tools.IsEtcdNotFound(err) {
					glog.Errorf("Unable to extract from the response: %s", err)
				}
				return
			}
			index = lastIndex + 1
		}
	}, s.waitDuration)
}

// extractFromResponse fetches the key (or waits for a change to a key) and then returns
// the index read.  It will watch no longer than waitDuration and then return
func (s *ConfigSourceEtcd) extractFromResponse(fromIndex uint64) (lastIndex uint64, err error) {
	var response *etcd.Response

	if fromIndex == 0 {
		response, err = s.client.Get(s.key, true, false)
	} else {
		stop := make(chan bool)
		go func() {
			select {
			case <-time.After(s.waitDuration):
			}
			stop <- true
		}()
		response, err = s.client.Watch(s.key, fromIndex, false, nil, stop)
	}
	if err != nil {
		return 0, err
	}

	pods, err := responseToPods(response)
	if err != nil {
		glog.Infof("Response was in error: %#v", response)
		return 0, fmt.Errorf("error parsing response: %#v", err)
	}

	glog.Infof("Got state from etcd: %+v", pods)
	s.ch <- kubelet.PodUpdate{pods, kubelet.SET}

	return response.Node.ModifiedIndex, nil
}

// ResponseToManifests takes an etcd Response object, and turns it into a structured list of containers.
// It returns a list of containers, or an error if one occurs.
func responseToPods(response *etcd.Response) ([]kubelet.Pod, error) {
	if response.Node == nil || len(response.Node.Value) == 0 {
		return nil, fmt.Errorf("no nodes field: %v", response)
	}
	var manifests []api.ContainerManifest
	if err := extractYAMLData([]byte(response.Node.Value), &manifests); err != nil {
		return []kubelet.Pod{}, err
	}
	pods := []kubelet.Pod{}
	for _, manifest := range manifests {
		pods = append(pods, kubelet.Pod{Name: manifest.ID, Manifest: manifest})
	}
	return pods, nil
}
