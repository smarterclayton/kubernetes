/*
Copyright 2014 Google Inc. All rights reserved.

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

package etcd

import (
	"reflect"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/golang/glog"
)

// minionPodWatch adapts an api.ContainerManifestList watcher into
// an api.Pod watcher
type minionPodWatch struct {
	watch watch.Interface
	ch    chan watch.Event
}

// NewMinionPodWatch adapts a watcher on an api.ContainerManifestList
// to an api.Pod watcher.  It keeps the previous value in memory to
// determine what has changed. Closing this watch will close the
// passed watch.
func NewMinionPodWatch(host string, w watch.Interface) watch.Interface {
	ch := make(chan watch.Event)
	watcher := &minionPodWatch{w, ch}
	go func() {
		defer util.HandleCrash()
		convertContainerListToPods(w, ch, host)
	}()
	return watcher
}

// Stop implements the watch.Interface interface
func (w *minionPodWatch) Stop() {
	w.watch.Stop()
}

// Stop implements the watch.Interface interface
func (w *minionPodWatch) ResultChan() <-chan watch.Event {
	return w.ch
}

// convertContainerListToPods adapts api.ContainerManifestList events into api.Pod
// events.  It will close the provided watch and channel when it exits.
func convertContainerListToPods(w watch.Interface, ch chan watch.Event, host string) {
	existing := make(map[string]*api.ContainerManifest)

	for event := range w.ResultChan() {
		manifests, ok := event.Object.(*api.ContainerManifestList)
		if !ok {
			glog.Errorf("Unexpected object type in watch stream: %#v", event)
			continue
		}
		switch event.Type {
		case watch.Added, watch.Modified:
			found := make(map[string]*api.ContainerManifest)
			for _, manifest := range manifests.Items {
				found[manifest.ID] = &manifest
				if old, ok := existing[manifest.ID]; ok {
					if reflect.DeepEqual(manifest, old) {
						continue
					}
					ch <- watch.Event{
						Type:   watch.Modified,
						Object: transformManifestToPod(&manifest, host, manifests.ResourceVersion),
					}
					continue
				}
				ch <- watch.Event{
					Type:   watch.Added,
					Object: transformManifestToPod(&manifest, host, manifests.ResourceVersion),
				}
			}
			for id, manifest := range existing {
				if _, ok := found[id]; !ok {
					ch <- watch.Event{
						Type:   watch.Deleted,
						Object: transformManifestToPod(manifest, host, manifests.ResourceVersion),
					}
				}
			}
			existing = found

		case watch.Deleted:
			if len(existing) == 0 {
				continue
			}
			for _, manifest := range manifests.Items {
				ch <- watch.Event{
					Type:   watch.Deleted,
					Object: transformManifestToPod(&manifest, host, manifests.ResourceVersion),
				}
			}
			existing = make(map[string]*api.ContainerManifest)

		}
	}

	w.Stop()
	close(ch)
}

// transformManifestToPod is a partial reversal of the transformation of Pod -> ContainerManifest
// performed when a pod is bound to a minion.
// TODO: remove when we bind pods directly to the kubelet
func transformManifestToPod(manifest *api.ContainerManifest, host string, resourceVersion uint64) *api.Pod {
	return &api.Pod{
		JSONBase: api.JSONBase{
			ID:              manifest.ID,
			ResourceVersion: resourceVersion,
		},
		DesiredState: api.PodState{
			Manifest: *manifest,
			Host:     host,
		},
		CurrentState: api.PodState{
			Host: host,
		},
	}
}

// transformManifestListToPodList helps transform the stored format for manifests
// into a list of pods
func transformManifestListToPodList(host string, manifests *api.ContainerManifestList) (*api.PodList, error) {
	pods := &api.PodList{JSONBase: api.JSONBase{ResourceVersion: manifests.ResourceVersion}}
	for i := range manifests.Items {
		pods.Items = append(pods.Items, *transformManifestToPod(&manifests.Items[i], host, manifests.ResourceVersion))
	}
	return pods, nil
}
