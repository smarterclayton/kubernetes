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

package config

import (
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
)

func expectError(t *testing.T, err error) {
	if err == nil {
		t.Errorf("Expected error, Got %#v", err)
	}
}

func expectNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expected no error, Got %#v", err)
	}
}

func expectEmptyChannel(t *testing.T, ch <-chan interface{}) {
	select {
	case update := <-ch:
		t.Errorf("Expected no update in channel, Got %#v", update)
	default:
	}
}

type sortedPods []kubelet.Pod

func (s sortedPods) Len() int {
	return len(s)
}
func (s sortedPods) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s sortedPods) Less(i, j int) bool {
	if s[i].Namespace < s[j].Namespace {
		return true
	}
	return s[i].Name < s[j].Name
}

func CreatePodUpdate(op kubelet.PodOperation, pods ...kubelet.Pod) kubelet.PodUpdate {
	newPods := make([]kubelet.Pod, len(pods))
	for i := range pods {
		newPods[i] = pods[i]
	}
	return kubelet.PodUpdate{newPods, op}
}

func createPodConfigTester() (chan<- interface{}, <-chan kubelet.PodUpdate, *PodConfig) {
	config := NewPodConfig(true)
	channel := config.Channel("test")
	ch := config.Updates()
	return channel, ch, config
}

func createFullPodConfigTester() (chan<- interface{}, <-chan kubelet.PodUpdate, *PodConfig) {
	config := NewPodConfig(false)
	channel := config.Channel("test")
	ch := config.Updates()
	return channel, ch, config
}

func expectPodUpdate(t *testing.T, ch <-chan kubelet.PodUpdate, expected ...kubelet.PodUpdate) {
	for i := range expected {
		update := <-ch
		if !reflect.DeepEqual(expected[i], update) {
			t.Fatalf("Expected %#v, Got %#v", expected[i], update)
		}
	}
	expectNoPodUpdate(t, ch)
}
func expectNoPodUpdate(t *testing.T, ch <-chan kubelet.PodUpdate) {
	select {
	case update := <-ch:
		t.Errorf("Expected no update in channel, Got %#v", update)
	default:
	}
}

func TestNewPodAdded(t *testing.T) {
	channel, ch, config := createPodConfigTester()

	// see an update
	podUpdate := CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo"})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo", Namespace: "test"}))

	config.Sync()
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.SET, kubelet.Pod{Name: "foo", Namespace: "test"}))
}

func TestNewPodAddedWithoutIncremental(t *testing.T) {
	channel, ch, config := createFullPodConfigTester()

	// see an update
	podUpdate := CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo"})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.SET, kubelet.Pod{Name: "foo", Namespace: "test"}))

	config.Sync()
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.SET, kubelet.Pod{Name: "foo", Namespace: "test"}))
}

func TestNewPodAddedUpdatedRemoved(t *testing.T) {
	channel, ch, _ := createPodConfigTester()

	// should register an add
	podUpdate := CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo"})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo", Namespace: "test"}))

	// should ignore ADDs that are identical
	expectNoPodUpdate(t, ch)

	// an kubelet.ADD should be converted to kubelet.UPDATE
	podUpdate = CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo", Manifest: api.ContainerManifest{Version: "test2"}})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.UPDATE, kubelet.Pod{Name: "foo", Namespace: "test", Manifest: api.ContainerManifest{Version: "test2"}}))

	podUpdate = CreatePodUpdate(kubelet.REMOVE, kubelet.Pod{Name: "foo"})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.REMOVE, kubelet.Pod{Name: "foo", Namespace: "test", Manifest: api.ContainerManifest{Version: "test2"}}))
}

func TestNewPodAddedUpdatedSet(t *testing.T) {
	channel, ch, _ := createPodConfigTester()

	// should register an add
	podUpdate := CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo"}, kubelet.Pod{Name: "foo2"}, kubelet.Pod{Name: "foo3"})
	channel <- podUpdate
	expectPodUpdate(t, ch, CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo", Namespace: "test"}, kubelet.Pod{Name: "foo2", Namespace: "test"}, kubelet.Pod{Name: "foo3", Namespace: "test"}))

	// should ignore ADDs that are identical
	expectNoPodUpdate(t, ch)

	// should be converted to an kubelet.ADD, kubelet.REMOVE, and kubelet.UPDATE
	podUpdate = CreatePodUpdate(kubelet.SET, kubelet.Pod{Name: "foo2", Manifest: api.ContainerManifest{Version: "test"}}, kubelet.Pod{Name: "foo3"}, kubelet.Pod{Name: "foo4", Namespace: "test"})
	channel <- podUpdate
	expectPodUpdate(t, ch,
		CreatePodUpdate(kubelet.REMOVE, kubelet.Pod{Name: "foo", Namespace: "test"}),
		CreatePodUpdate(kubelet.ADD, kubelet.Pod{Name: "foo4", Namespace: "test"}),
		CreatePodUpdate(kubelet.UPDATE, kubelet.Pod{Name: "foo2", Namespace: "test", Manifest: api.ContainerManifest{Version: "test"}}))
}
