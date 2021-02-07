package cache

import (
	"io/ioutil"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

type fakeWatch struct {
	lock sync.Mutex
	ch   chan watch.Event
}

func newFakeWatch() watch.Interface {
	return &fakeWatch{
		ch: make(chan watch.Event),
	}
}

func (w *fakeWatch) Stop() {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.ch != nil {
		close(w.ch)
		w.ch = nil
	}
}

func (w *fakeWatch) ResultChan() <-chan watch.Event {
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.ch
}

var (
	lock               sync.Mutex
	benchmarkDataItems *v1.PodList
)

func benchmarkData(t testing.TB) *v1.PodList {
	lock.Lock()
	defer lock.Unlock()
	if benchmarkDataItems != nil {
		return benchmarkDataItems
	}
	// 27.384Mb, 1249 pods, averages 21,925b per pod
	data, err := ioutil.ReadFile("testdata/pods.json")
	if err != nil {
		t.Fatal(err)
	}
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	benchmarkDataItems = obj.(*v1.PodList)
	if len(benchmarkDataItems.Items) > 1000 {
		benchmarkDataItems.Items = benchmarkDataItems.Items[:1000]
	}
	return benchmarkDataItems
}

func BenchmarkInformerFill(b *testing.B) {
	obj := benchmarkData(b)
	lw := func() cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return obj, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return newFakeWatch(), nil
			},
			DisableChunking: true,
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		func() {
			informer := cache.NewSharedIndexInformer(lw, &v1.Pod{}, 0, nil)
			informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc:    func(obj interface{}) {},
				UpdateFunc: func(old, obj interface{}) {},
				DeleteFunc: func(obj interface{}) {},
			})
			doneCh := make(chan struct{})
			stopCh := make(chan struct{})
			go func() {
				defer close(doneCh)
				informer.Run(stopCh)
			}()
			for !informer.HasSynced() {
				time.Sleep(time.Millisecond)
			}
			close(stopCh)
			<-doneCh
		}()
	}
}

func BenchmarkMemdbInformerFill(b *testing.B) {
	obj := benchmarkData(b)
	lw := func() cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return obj, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return newFakeWatch(), nil
			},
			DisableChunking: true,
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		func() {
			indexer, err := NewMemdbStore()
			if err != nil {
				b.Fatal(err)
			}
			informer := cache.NewSharedInformerForIndexer(lw, &v1.Pod{}, 0, indexer)
			informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc:    func(obj interface{}) {},
				UpdateFunc: func(old, obj interface{}) {},
				DeleteFunc: func(obj interface{}) {},
			})
			doneCh := make(chan struct{})
			stopCh := make(chan struct{})
			go func() {
				defer close(doneCh)
				informer.Run(stopCh)
			}()
			for !informer.HasSynced() {
				time.Sleep(time.Millisecond)
			}
			close(stopCh)
			<-doneCh
		}()
	}
}

var preserve interface{}

func TestMemdbInformerFill(t *testing.T) {
	// inuse_objects: 305k, 121Mb
	// alloc_objects: 625k, 199Mb
	obj := benchmarkData(t)
	lw := func() cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return obj, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return newFakeWatch(), nil
			},
			DisableChunking: true,
		}
	}()

	// inuse_objects: 8100, 538kb
	// alloc_objects: 79k (7k Get), 4.6Mb (300kb Get)
	indexer, err := NewMemdbStore()
	if err != nil {
		t.Fatal(err)
	}
	informer := cache.NewSharedInformerForIndexer(lw, &v1.Pod{}, 0, indexer)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) {},
		UpdateFunc: func(old, obj interface{}) {},
		DeleteFunc: func(obj interface{}) {},
	})
	doneCh := make(chan struct{})
	stopCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		informer.Run(stopCh)
	}()
	for !informer.HasSynced() {
		time.Sleep(time.Millisecond)
	}
	close(stopCh)
	<-doneCh
	preserve = informer
}

func Test_memdbIndexer_Add(t *testing.T) {
	tests := []struct {
		name    string
		obj     interface{}
		wantErr bool
	}{
		{obj: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "name"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer, err := NewMemdbStore()
			if err != nil {
				t.Fatal(err)
			}
			if err := indexer.Add(tt.obj); (err != nil) != tt.wantErr {
				t.Fatalf("memdbIndexer.Add() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			obj, ok, err := indexer.Get(tt.obj)
			if err != nil || !ok || obj != tt.obj {
				t.Fatalf("unexpected get: %v, %t, %#v", err, ok, obj)
			}
		})
	}
}
