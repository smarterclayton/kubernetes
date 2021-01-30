package sizes

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"
	"text/tabwriter"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

var remember = make([]interface{}, 0, 2048)

func TestSize(t *testing.T) {
	var test int
	emptyAlloc := estimateAllocations(func() {
		test = 1
	})
	if emptyAlloc != 0 || test != 1 {
		t.Fatalf("estimateSize returned non-zero allocation for no-op: %d", emptyAlloc)
	}

	// creating the decoder causes allocations
	deserializer := scheme.Codecs.UniversalDeserializer()
	info, _ := runtime.SerializerInfoForMediaType(scheme.Codecs.SupportedMediaTypes(), runtime.ContentTypeProtobuf)
	protoSerializer := info.Serializer

	testCases := []sizeTestCase{
		{heapAlloc: 1024, fn: func() interface{} { return &v1.Pod{} }},
		{heapAlloc: 480, fn: func() interface{} { return &v1.PodSpec{} }},
		{heapAlloc: 240, fn: func() interface{} { return &v1.PodStatus{} }},
		{heapAlloc: 768, fn: func() interface{} { return &v1.Node{} }},
		{heapAlloc: 112, fn: func() interface{} { return &v1.NodeSpec{} }},
		{heapAlloc: 352, fn: func() interface{} { return &v1.NodeStatus{} }},
		{heapAlloc: 256, fn: func() interface{} { return &metav1.ObjectMeta{} }},
		{heapAlloc: 32, fn: func() interface{} { return &metav1.TypeMeta{} }},

		{
			name: "empty",
			fn: func() interface{} {
				return map[string]string{}
			},
		},
		{
			name: "small",
			fn: func() interface{} {
				return map[string]string{
					"medium":          "value",
					"long-label-name": "1",
				}
			},
		},
		{
			name: "alloc 8",
			fn: func() interface{} {
				m := make(map[string]string, 8)
				return m
			},
		},
		{
			name: "alloc 8, set 2",
			fn: func() interface{} {
				m := make(map[string]string, 8)
				m["medium"] = "value"
				m["long-label-name"] = "1"
				return m
			},
		},
		{
			name: "alloc 64 entries",
			fn: func() interface{} {
				m := make(map[string]string, 64)
				return m
			},
		},
		{
			name: "alloc 8, set 2, strings",
			fn: func() interface{} {
				m := make(map[string]string, 8)
				m[s("medium")] = s("value")
				m[s("long-label-name")] = s("1")
				return m
			},
		},
		{
			name: "with basic fields",
			fn: func() interface{} {
				return &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      s("0000000000"),
						Namespace: s("namespace"),
					},
				}
			},
		},
		{
			name: "with basic fields and small labels",
			fn: func() interface{} {
				return &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      s("0000000000"),
						Namespace: s("namespace"),
						Labels: map[string]string{
							"medium":          "value",
							"long-label-name": "1",
						},
					},
				}
			},
		},
		{
			op: "allocate",
			fn: func() interface{} {
				return exampleRC()
			},
		},
		{
			op: "copy",
			fnfn: func() (uint64, allocatorFunc) {
				obj := exampleRC()
				return 0, func() interface{} {
					return obj.DeepCopy()
				}
			},
		},
		{
			op: "JSON decode",
			fnfn: func() (uint64, allocatorFunc) {
				b, err := ioutil.ReadFile(filepath.Join("..", "replication_controller_example.json"))
				if err != nil {
					t.Fatal(err)
				}
				// go-jsoniter lazily inits decoder logic, so call this first
				deserializer.Decode(b, nil, nil)

				return uint64(len(b)), func() interface{} {
					obj, _, err := deserializer.Decode(b, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					remember = append(remember, obj)
					return obj
				}
			},
		},
		{
			op:       "JSON decode",
			typeName: "*v1.ReplicationController",
			name:     "unstructured",
			fnfn: func() (uint64, allocatorFunc) {
				b, err := ioutil.ReadFile(filepath.Join("..", "replication_controller_example.json"))
				if err != nil {
					t.Fatal(err)
				}

				return uint64(len(b)), func() interface{} {
					obj, _, err := unstructured.UnstructuredJSONScheme.Decode(b, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					return obj
				}
			},
		},
		{
			op: "proto decode",
			fnfn: func() (uint64, allocatorFunc) {
				b, err := ioutil.ReadFile(filepath.Join("..", "replication_controller_example.json"))
				if err != nil {
					t.Fatal(err)
				}
				// go-jsoniter lazily inits decoder logic, so call this first
				obj, _, err := deserializer.Decode(b, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				//t.Logf("%# v", pretty.Formatter(obj))

				var buf bytes.Buffer
				if err := protoSerializer.Encode(obj, &buf); err != nil {
					t.Fatal(err)
				}
				b = buf.Bytes()

				return uint64(len(b)), func() interface{} {
					obj, _, err := deserializer.Decode(b, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					return obj
				}
			},
		},
	}

	for _, test := range testCases {
		t.Run(fmt.Sprintf("%s_%s", test.op, test.name), func(t *testing.T) {
			testAllocation(t, test, nil)
		})
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 1, 1, ' ', 0)
	fmt.Fprintf(w, "OP\tTYPE\tVARIANT\tSIZE\tSOURCE\tRATIO\n")
	for _, test := range testCases {
		testAllocation(t, test, w)
	}
	w.Flush()
	t.Logf("\n%s", buf.String())
}

type sizeTestCase struct {
	name      string
	op        string
	typeName  string
	fnfn      func() (uint64, allocatorFunc)
	fn        allocatorFunc
	heapAlloc uint64
}

func testAllocation(t *testing.T, test sizeTestCase, w *tabwriter.Writer) {
	t.Helper()

	var sourceSize uint64
	fn := test.fn
	if fn == nil {
		sourceSize, fn = test.fnfn()
	}

	var obj interface{}
	heapAlloc := estimateAllocations(func() { obj = fn() })

	op := "new"
	if len(test.op) > 0 {
		op = test.op
	}

	if w != nil {
		fmt.Fprintf(w, "%s\t", op)
		if len(test.typeName) > 0 {
			fmt.Fprintf(w, "%s\t", test.typeName)
		} else {
			fmt.Fprintf(w, "%T\t", obj)
		}
		fmt.Fprintf(w, "%s\t%6d", test.name, heapAlloc)
		if sourceSize > 0 {
			fmt.Fprintf(w, "\t%5d\t%.1f%%", sourceSize, percent(heapAlloc, sourceSize))
		} else {
			fmt.Fprint(w, "\t\t")
		}
		fmt.Fprintln(w)
		return
	}

	switch {
	case test.heapAlloc == 0:
		if heapAlloc == 0 {
			t.Fatalf("%s %T: expected non-zero, got %d", op, obj, heapAlloc)
		}
	case test.heapAlloc > 0:
		if test.heapAlloc != heapAlloc {
			t.Fatalf("%s %T: expected %d, got %d", op, obj, test.heapAlloc, heapAlloc)
		}
	}
	t.Logf("allocated %d", heapAlloc)
}

func percent(a uint64, b uint64) float64 {
	return float64(a) / float64(b) * 100
}

func TestObjectSize(t *testing.T) {
	var obj interface{}
	estimate := func(fn func() interface{}) uint64 {
		size := estimateAllocations(func() {
			obj = fn()
		})
		return size
	}

	for _, testCase := range []struct {
		obj           reflect.Value
		ignoreStrings bool
		include       bool
		expect        uint64
	}{
		{expect: 3708, obj: reflect.ValueOf(exampleRC())},
		// off by 22 bytes from the test object above, probably map estimate
		{expect: 3222, ignoreStrings: true, obj: reflect.ValueOf(exampleRC())},
		{
			expect: estimate(func() interface{} { return map[string]string{} }),
			obj:    reflect.ValueOf(map[string]string{}),
		},
		{
			expect: estimate(func() interface{} { return map[string]string{"a": "1"} }),
			obj:    reflect.ValueOf(map[string]string{"a": "1"}),
		},
		{
			expect: estimate(func() interface{} { m := map[string]string{}; return &m }),
			obj:    reflect.ValueOf(&map[string]string{}),
		},
		{
			expect: 4, /* 4 bytes */
			obj:    reflect.ValueOf("blah"),
		},
		{
			expect:  20, /* 4 bytes */
			obj:     reflect.ValueOf("blah"),
			include: true,
		},
		{
			expect: 20, /* 4 bytes plus string header of pointer and length */
			obj:    reflect.ValueOf(sp("blah")),
		},
	} {
		opt := sizeOptions{
			TypeSize: map[reflect.Type]uint64{
				reflect.TypeOf(time.Time{}): uint64(reflect.TypeOf(time.Time{}).Size()),
			},
		}
		opt.IgnoreStrings = testCase.ignoreStrings
		pointers := make(map[uintptr]struct{})
		if actual := sizeValue(testCase.obj, testCase.include, opt, pointers); actual != testCase.expect {
			t.Errorf("expected %d, got %d", testCase.expect, actual)
		}
	}
	// hold on to the object to prevent compiler optimization
	if obj == nil {
		t.Fatal("expected object")
	}
}

func exampleRC() *v1.ReplicationController {
	return &v1.ReplicationController{
		TypeMeta: metav1.TypeMeta{
			Kind:       s("ReplicationController"),
			APIVersion: s("v1"),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       s("elasticsearch-logging-controller"),
			GenerateName:               s(""),
			Namespace:                  s("default"),
			SelfLink:                   s("/api/v1/namespaces/default/replicationcontrollers/elasticsearch-logging-controller"),
			UID:                        types.UID(s("aa76f162-e8e5-11e4-8fde-42010af09327")),
			ResourceVersion:            s("98"),
			Generation:                 0,
			CreationTimestamp:          metav1.Time{Time: time.Unix(1, 0)},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: nil,
			Labels: map[string]string{
				s("kubernetes.io/cluster-service"): s("true"),
				s("name"):                          s("elasticsearch-logging"),
			},
			Annotations:     map[string]string{},
			OwnerReferences: nil,
			Finalizers:      nil,
			ClusterName:     s(""),
			ManagedFields:   nil,
		},
		Spec: v1.ReplicationControllerSpec{
			Replicas:        int32p(1),
			MinReadySeconds: 0,
			Selector: map[string]string{
				s("name"): s("elasticsearch-logging"),
			},
			Template: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:                       s(""),
					GenerateName:               s(""),
					Namespace:                  s(""),
					SelfLink:                   s(""),
					UID:                        types.UID(s("")),
					ResourceVersion:            s(""),
					Generation:                 0,
					CreationTimestamp:          metav1.Time{},
					DeletionTimestamp:          nil,
					DeletionGracePeriodSeconds: nil,
					Labels: map[string]string{
						s("kubernetes.io/cluster-service"): s("true"),
						s("name"):                          s("elasticsearch-logging"),
					},
					Annotations:     map[string]string{},
					OwnerReferences: nil,
					Finalizers:      nil,
					ClusterName:     s(""),
					ManagedFields:   nil,
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: s("es-persistent-storage"),
							VolumeSource: v1.VolumeSource{
								HostPath:              (*v1.HostPathVolumeSource)(nil),
								EmptyDir:              &v1.EmptyDirVolumeSource{},
								GCEPersistentDisk:     (*v1.GCEPersistentDiskVolumeSource)(nil),
								AWSElasticBlockStore:  (*v1.AWSElasticBlockStoreVolumeSource)(nil),
								GitRepo:               (*v1.GitRepoVolumeSource)(nil),
								Secret:                (*v1.SecretVolumeSource)(nil),
								NFS:                   (*v1.NFSVolumeSource)(nil),
								ISCSI:                 (*v1.ISCSIVolumeSource)(nil),
								Glusterfs:             (*v1.GlusterfsVolumeSource)(nil),
								PersistentVolumeClaim: (*v1.PersistentVolumeClaimVolumeSource)(nil),
								RBD:                   (*v1.RBDVolumeSource)(nil),
								FlexVolume:            (*v1.FlexVolumeSource)(nil),
								Cinder:                (*v1.CinderVolumeSource)(nil),
								CephFS:                (*v1.CephFSVolumeSource)(nil),
								Flocker:               (*v1.FlockerVolumeSource)(nil),
								DownwardAPI:           (*v1.DownwardAPIVolumeSource)(nil),
								FC:                    (*v1.FCVolumeSource)(nil),
								AzureFile:             (*v1.AzureFileVolumeSource)(nil),
								ConfigMap:             (*v1.ConfigMapVolumeSource)(nil),
								VsphereVolume:         (*v1.VsphereVirtualDiskVolumeSource)(nil),
								Quobyte:               (*v1.QuobyteVolumeSource)(nil),
								AzureDisk:             (*v1.AzureDiskVolumeSource)(nil),
								PhotonPersistentDisk:  (*v1.PhotonPersistentDiskVolumeSource)(nil),
								Projected:             (*v1.ProjectedVolumeSource)(nil),
								PortworxVolume:        (*v1.PortworxVolumeSource)(nil),
								ScaleIO:               (*v1.ScaleIOVolumeSource)(nil),
								StorageOS:             (*v1.StorageOSVolumeSource)(nil),
								CSI:                   (*v1.CSIVolumeSource)(nil),
								Ephemeral:             (*v1.EphemeralVolumeSource)(nil),
							},
						},
					},
					InitContainers: nil,
					Containers: []v1.Container{
						{
							Name:       s("elasticsearch-logging"),
							Image:      s("k8s.gcr.io/elasticsearch:1.0"),
							Command:    nil,
							Args:       nil,
							WorkingDir: s(""),
							Ports: []v1.ContainerPort{
								{Name: s("db"), HostPort: 0, ContainerPort: 9200, Protocol: v1.Protocol(s("TCP")), HostIP: s("")},
								{Name: s("transport"), HostPort: 0, ContainerPort: 9300, Protocol: v1.Protocol(s("TCP")), HostIP: s("")},
							},
							EnvFrom:   nil,
							Env:       nil,
							Resources: v1.ResourceRequirements{},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:             s("es-persistent-storage"),
									ReadOnly:         false,
									MountPath:        s("/data"),
									SubPath:          s(""),
									MountPropagation: (*v1.MountPropagationMode)(nil),
									SubPathExpr:      s(""),
								},
							},
							VolumeDevices:            nil,
							LivenessProbe:            (*v1.Probe)(nil),
							ReadinessProbe:           (*v1.Probe)(nil),
							StartupProbe:             (*v1.Probe)(nil),
							Lifecycle:                (*v1.Lifecycle)(nil),
							TerminationMessagePath:   s("/dev/termination-log"),
							TerminationMessagePolicy: v1.TerminationMessagePolicy(s("")),
							ImagePullPolicy:          v1.PullPolicy(s("IfNotPresent")),
							SecurityContext:          (*v1.SecurityContext)(nil),
							Stdin:                    false,
							StdinOnce:                false,
							TTY:                      false,
						},
					},
					EphemeralContainers:           nil,
					RestartPolicy:                 "Always",
					TerminationGracePeriodSeconds: (*int64)(nil),
					ActiveDeadlineSeconds:         (*int64)(nil),
					DNSPolicy:                     v1.DNSPolicy(s("ClusterFirst")),
					NodeSelector:                  map[string]string{},
					ServiceAccountName:            s(""),
					DeprecatedServiceAccount:      s(""),
					AutomountServiceAccountToken:  (*bool)(nil),
					NodeName:                      s(""),
					HostNetwork:                   false,
					HostPID:                       false,
					HostIPC:                       false,
					ShareProcessNamespace:         (*bool)(nil),
					SecurityContext:               (*v1.PodSecurityContext)(nil),
					ImagePullSecrets:              nil,
					Hostname:                      s(""),
					Subdomain:                     s(""),
					Affinity:                      (*v1.Affinity)(nil),
					SchedulerName:                 s(""),
					Tolerations:                   nil,
					HostAliases:                   nil,
					PriorityClassName:             s(""),
					Priority:                      (*int32)(nil),
					DNSConfig:                     (*v1.PodDNSConfig)(nil),
					ReadinessGates:                nil,
					RuntimeClassName:              (*string)(nil),
					EnableServiceLinks:            (*bool)(nil),
					PreemptionPolicy:              (*v1.PreemptionPolicy)(nil),
					Overhead:                      v1.ResourceList{},
					TopologySpreadConstraints:     nil,
					SetHostnameAsFQDN:             (*bool)(nil),
				},
			},
		},
		Status: v1.ReplicationControllerStatus{
			Replicas:             1,
			FullyLabeledReplicas: 0,
			ReadyReplicas:        0,
			AvailableReplicas:    0,
			ObservedGeneration:   0,
			Conditions:           nil,
		},
	}
}

func sp(s string) *string {
	return &s
}

func s(s string) string {
	copied := []byte(s)
	return string(copied)
}

func int32p(i int32) *int32 {
	return &i
}
