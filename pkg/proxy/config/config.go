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
	"sync"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/config"
	"github.com/golang/glog"
)

type Operation int

const (
	SET Operation = iota
	ADD
	REMOVE
)

// Defines an operation sent on the channel. You can add or remove single services by
// sending an array of size one and Op == ADD|REMOVE. For setting the state of the system
// to a given state for this source configuration, set Services as desired and Op to SET,
// which will reset the system state to that specified in this operation for this source
// channel. To remove all services, set Services to empty array and Op to SET
type ServiceUpdate struct {
	Services []api.Service
	Op       Operation
}

// Defines an operation sent on the channel. You can add or remove single endpoints by
// sending an array of size one and Op == ADD|REMOVE. For setting the state of the system
// to a given state for this source configuration, set Endpoints as desired and Op to SET,
// which will reset the system state to that specified in this operation for this source
// channel. To remove all endpoints, set Endpoints to empty array and Op to SET
type EndpointsUpdate struct {
	Endpoints []api.Endpoints
	Op        Operation
}

type ServiceConfigHandler interface {
	// Sent when a configuration has been changed by one of the sources. This is the
	// union of all the configuration sources.
	OnUpdate(services []api.Service)
}

type EndpointsConfigHandler interface {
	// OnUpdate gets called when endpoints configuration is changed for a given
	// service on any of the configuration sources. An example is when a new
	// service comes up, or when containers come up or down for an existing service.
	OnUpdate(endpoints []api.Endpoints)
}

type EndpointsConfig struct {
	mux     *config.Mux
	watcher *config.Watcher
	store   *endpointsStore
}

func NewEndpointsConfig() *EndpointsConfig {
	updates := make(chan struct{})
	store := &endpointsStore{updates: updates, endpoints: make(map[string]map[string]api.Endpoints)}
	mux := config.NewMux(store)
	watcher := config.NewWatcher()
	go watchForUpdates(watcher, store, updates)
	return &EndpointsConfig{mux, watcher, store}
}

func (c *EndpointsConfig) RegisterHandler(handler EndpointsConfigHandler) {
	c.watcher.Add(config.ListenerFunc(func(instance interface{}) {
		handler.OnUpdate(instance.([]api.Endpoints))
	}))
}

func (c *EndpointsConfig) SourceChannel(source string) chan EndpointsUpdate {
	ch := c.mux.SourceChannel(source)
	endpointsCh := make(chan EndpointsUpdate)
	go func() {
		for update := range endpointsCh {
			ch <- update
		}
		close(ch)
	}()
	return endpointsCh
}

func (c *EndpointsConfig) Config() map[string]map[string]api.Endpoints {
	return c.store.Object().(map[string]map[string]api.Endpoints)
}

type endpointsStore struct {
	endpointLock sync.RWMutex
	endpoints    map[string]map[string]api.Endpoints
	updates      chan<- struct{}
}

func (s *endpointsStore) Merge(source string, change interface{}) error {
	s.endpointLock.Lock()
	endpoints := s.endpoints[source]
	if endpoints == nil {
		endpoints = make(map[string]api.Endpoints)
	}
	update := change.(EndpointsUpdate)
	switch update.Op {
	case ADD:
		glog.Infof("Adding new endpoint from source %s : %v", source, update.Endpoints)
		for _, value := range update.Endpoints {
			endpoints[value.Name] = value
		}
	case REMOVE:
		glog.Infof("Removing an endpoint %v", update)
		for _, value := range update.Endpoints {
			delete(endpoints, value.Name)
		}
	case SET:
		glog.Infof("Setting endpoints %v", update)
		// Clear the old map entries by just creating a new map
		endpoints = make(map[string]api.Endpoints)
		for _, value := range update.Endpoints {
			endpoints[value.Name] = value
		}
	default:
		glog.Infof("Received invalid update type: %v", update)
	}
	s.endpoints[source] = endpoints
	s.endpointLock.Unlock()
	if s.updates != nil {
		s.updates <- struct{}{}
	}
	return nil
}

func (s *endpointsStore) Object() interface{} {
	s.endpointLock.RLock()
	endpoints := make([]api.Endpoints, 0)
	for _, sourceEndpoints := range s.endpoints {
		for _, value := range sourceEndpoints {
			endpoints = append(endpoints, value)
		}
	}
	s.endpointLock.RUnlock()
	return endpoints
}

type ServiceConfig struct {
	mux     *config.Mux
	watcher *config.Watcher
	store   *serviceStore
}

func NewServiceConfig() *ServiceConfig {
	updates := make(chan struct{})
	store := &serviceStore{updates: updates, services: make(map[string]map[string]api.Service)}
	mux := config.NewMux(store)
	watcher := config.NewWatcher()
	go watchForUpdates(watcher, store, updates)
	return &ServiceConfig{mux, watcher, store}
}

func (c *ServiceConfig) RegisterHandler(handler ServiceConfigHandler) {
	c.watcher.Add(config.ListenerFunc(func(instance interface{}) {
		handler.OnUpdate(instance.([]api.Service))
	}))
}

func (c *ServiceConfig) SourceChannel(source string) chan ServiceUpdate {
	ch := c.mux.SourceChannel(source)
	serviceCh := make(chan ServiceUpdate)
	go func() {
		for update := range serviceCh {
			ch <- update
		}
		close(ch)
	}()
	return serviceCh
}

func (c *ServiceConfig) Config() map[string]map[string]api.Service {
	return c.store.Object().(map[string]map[string]api.Service)
}

type serviceStore struct {
	serviceLock sync.RWMutex
	services    map[string]map[string]api.Service
	updates     chan<- struct{}
}

func (s *serviceStore) Merge(source string, change interface{}) error {
	s.serviceLock.Lock()
	services := s.services[source]
	if services == nil {
		services = make(map[string]api.Service)
	}
	update := change.(ServiceUpdate)
	switch update.Op {
	case ADD:
		glog.Infof("Adding new service from source %s : %v", source, update.Services)
		for _, value := range update.Services {
			services[value.ID] = value
		}
	case REMOVE:
		glog.Infof("Removing a service %v", update)
		for _, value := range update.Services {
			delete(services, value.ID)
		}
	case SET:
		glog.Infof("Setting services %v", update)
		// Clear the old map entries by just creating a new map
		services = make(map[string]api.Service)
		for _, value := range update.Services {
			services[value.ID] = value
		}
	default:
		glog.Infof("Received invalid update type: %v", update)
	}
	s.services[source] = services
	s.serviceLock.Unlock()
	if s.updates != nil {
		s.updates <- struct{}{}
	}
	return nil
}

func (s *serviceStore) Object() interface{} {
	s.serviceLock.RLock()
	services := make([]api.Service, 0)
	for _, sourceServices := range s.services {
		for _, value := range sourceServices {
			services = append(services, value)
		}
	}
	s.serviceLock.RUnlock()
	return services
}

// watchForUpdates invokes watcher.Notify() with the latest version of an object
// when changes occur.
func watchForUpdates(watcher *config.Watcher, accessor config.Accessor, updates <-chan struct{}) {
	for _ = range updates {
		watcher.Notify(accessor.Object())
	}
}
