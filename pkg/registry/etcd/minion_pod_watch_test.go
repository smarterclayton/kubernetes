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
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

func TestConvertContainerListToPods(t *testing.T) {
	w := watch.NewFake()
	ch := make(chan watch.Event)
	go convertContainerListToPods(w, ch, "test")

	manifest := api.ContainerManifest{
		Version: "v1beta2",
		ID:      "foo",
	}
	w.Add(&api.ContainerManifestList{
		JSONBase: api.JSONBase{
			ResourceVersion: 1,
		},
		Items: []api.ContainerManifest{manifest},
	})

	actual := <-ch
	expected := &api.Pod{
		JSONBase: api.JSONBase{
			ID:                "foo",
			ResourceVersion:   1,
			CreationTimestamp: util.Time{},
		},
		DesiredState: api.PodState{
			Manifest: manifest,
			Host:     "test",
		},
		CurrentState: api.PodState{
			Host: "test",
		},
	}
	if actual.Type != watch.Added {
		t.Errorf("unexpected event type %#v", actual)
	}
	if !reflect.DeepEqual(actual.Object, expected) {
		t.Errorf("Expected %#v, Got %#v", expected, actual.Object)
	}
}

func TestConvertContainerListToPodsIncremental(t *testing.T) {
	testCases := map[string]struct {
		Sequence []watch.Event
		Expected []watch.Event
	}{
		"adds new": {
			Sequence: []watch.Event{
				{
					Type: watch.Added,
					Object: &api.ContainerManifestList{
						JSONBase: api.JSONBase{ResourceVersion: 1},
						Items: []api.ContainerManifest{
							{
								Version: "v1beta2",
								ID:      "foo",
							},
						},
					},
				},
			},
			Expected: []watch.Event{
				{
					Type: watch.Added,
					Object: &api.Pod{
						JSONBase: api.JSONBase{
							ID:                "foo",
							ResourceVersion:   1,
							CreationTimestamp: util.Time{},
						},
						DesiredState: api.PodState{
							Manifest: api.ContainerManifest{
								Version: "v1beta2",
								ID:      "foo",
							},
							Host: "test",
						},
						CurrentState: api.PodState{
							Host: "test",
						},
					},
				},
			},
		},
		"delete missing": {
			Sequence: []watch.Event{
				{
					Type: watch.Deleted,
					Object: &api.ContainerManifestList{
						JSONBase: api.JSONBase{ResourceVersion: 1},
						Items: []api.ContainerManifest{
							{
								Version: "v1beta2",
								ID:      "foo",
							},
						},
					},
				},
			},
			Expected: []watch.Event{},
		},
		"delete existing": {
			Sequence: []watch.Event{
				{
					Type: watch.Added,
					Object: &api.ContainerManifestList{
						JSONBase: api.JSONBase{ResourceVersion: 1},
						Items: []api.ContainerManifest{
							{
								Version: "v1beta2",
								ID:      "foo",
							},
						},
					},
				},
				{
					Type: watch.Deleted,
					Object: &api.ContainerManifestList{
						JSONBase: api.JSONBase{ResourceVersion: 1},
						Items: []api.ContainerManifest{
							{
								Version: "v1beta2",
								ID:      "foo",
							},
						},
					},
				},
			},
			Expected: []watch.Event{
				{
					Type: watch.Added,
					Object: &api.Pod{
						JSONBase: api.JSONBase{
							ID:                "foo",
							ResourceVersion:   1,
							CreationTimestamp: util.Time{},
						},
						DesiredState: api.PodState{
							Manifest: api.ContainerManifest{
								Version: "v1beta2",
								ID:      "foo",
							},
							Host: "test",
						},
						CurrentState: api.PodState{
							Host: "test",
						},
					},
				},
			},
		},
	}

	for k, testCase := range testCases {
		ch := make(chan watch.Event)
		w := watch.NewFake()
		go convertContainerListToPods(w, ch, "test")

		done := make(chan struct{})
		go func() {
			for _, actual := range testCase.Sequence {
				w.Action(actual.Type, actual.Object)
			}
			w.Stop()
			close(done)
		}()

		// if the watch should be closed
		if len(testCase.Expected) == 0 {
			<-done
			select {
			case _, open := <-ch:
				if open {
					t.Errorf("%s: unexpected open channel", k)
				}
			default:
			}
			continue
		}

		for _, expected := range testCase.Expected {
			actual := <-ch

			if actual.Type != expected.Type {
				t.Errorf("%s: unexpected event type %#v", k, actual)
			}
			if !reflect.DeepEqual(actual.Object, expected.Object) {
				t.Errorf("%s: Expected %#v, Got %#v", k, expected.Object, actual.Object)
			}
		}
		<-done
		w.Stop()
	}
}
