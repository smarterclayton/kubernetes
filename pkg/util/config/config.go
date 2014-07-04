/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or cied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"sync"
)

type Merger interface {
	// Invoked when a change from a source is received.  May also function as an incremental
	// merger if you wish to consume changes incrementally.  Must be reentrant when more than
	// one source is defined.
	Merge(source string, update interface{}) error
}

// MergeFunc is a function that can be used to merge in a Mux
type MergeFunc func(source string, update interface{}) error

func (f MergeFunc) Merge(source string, update interface{}) error {
	return f(source, update)
}

// Mux is a class for merging configuration from multiple sources.  Changes are
// pushed via channels and send to the merge function.
type Mux struct {
	// Invoked when an update is sent to a source.
	merger Merger

	// Sources and their lock.
	sourceLock sync.RWMutex
	// Maps source names to channels
	sources map[string]chan interface{}
}

// NewMux creates a new mux that can merge changes from multiple sources.
func NewMux(merger Merger) *Mux {
	mux := &Mux{
		sources: make(map[string]chan interface{}),
		merger:  merger,
	}
	return mux
}

// SourceChannel returns a channel where a configuration source
// can send updates of new configurations. Multiple calls with the same
// source will return the same channel. This allows change and state based sources
// to use the same channel. Different source names however will be treated as a
// union.
func (m *Mux) SourceChannel(source string) chan interface{} {
	if len(source) == 0 {
		panic("SourceChannel given an empty name")
	}
	m.sourceLock.Lock()
	defer m.sourceLock.Unlock()
	channel, exists := m.sources[source]
	if exists {
		return channel
	}
	newChannel := make(chan interface{})
	m.sources[source] = newChannel
	go m.listen(source, newChannel)
	return newChannel
}

func (m *Mux) listen(source string, listenChannel <-chan interface{}) {
	for update := range listenChannel {
		m.merger.Merge(source, update)
	}
}

type Accessor interface {
	// Object returns a representation of the current merge state.
	// Must be reentrant when more than one source is defined.
	Object() interface{}
}

// AccessorFunc returns the current state of a merge
type AccessorFunc func() interface{}

func (f AccessorFunc) Object() interface{} {
	return f()
}

type Listener interface {
	// OnUpdate is invoked when a change is made to an object.
	OnUpdate(instance interface{})
}

// ListenerFunc receives a representation of the change or object.
type ListenerFunc func(instance interface{})

func (f ListenerFunc) OnUpdate(instance interface{}) {
	f(instance)
}

type Watcher struct {
	// Listeners for changes and their lock.
	listenerLock sync.RWMutex
	listeners    []Listener
}

// Register a set of listeners that support the Listener interface and
// notify them on changes.
func NewWatcher() *Watcher {
	return &Watcher{}
}

// Register Listener to receive updates of changes.
func (m *Watcher) Add(listener Listener) {
	m.listenerLock.Lock()
	m.listeners = append(m.listeners, listener)
	m.listenerLock.Unlock()
}

// Notify all listeners
func (m *Watcher) Notify(instance interface{}) {
	m.listenerLock.RLock()
	listeners := m.listeners
	m.listenerLock.RUnlock()
	for _, listener := range listeners {
		listener.OnUpdate(instance)
	}
}
