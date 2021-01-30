package sizes

import (
	"log"
	"math"
	"reflect"
	"runtime"
)

func ptrSize() int {
	return 32 << uintptr(^uintptr(0)>>63)
}

type allocatorFunc func() interface{}

func estimateAllocations(fn func()) uint64 {
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	fn()
	runtime.ReadMemStats(&m2)
	return m2.HeapAlloc - m1.HeapAlloc
}

type sizeOptions struct {
	TypeSize      map[reflect.Type]uint64
	IgnoreStrings bool
}

func sizeObject(obj interface{}, opt sizeOptions) uint64 {
	v := reflect.ValueOf(obj)
	pointersSeen := make(map[uintptr]struct{})
	return sizeValue(v, v.Kind() != reflect.Ptr, opt, pointersSeen)
}

var wordSize = uint64(reflect.ValueOf(int(1)).Type().Size())

func sizeValue(v reflect.Value, includeSelf bool, opt sizeOptions, pointersSeen map[uintptr]struct{}) uint64 {
	t := v.Type()
	if s, ok := opt.TypeSize[t]; ok {
		return s
	}

	var size uint64
	if includeSelf {
		size += uint64(t.Size())
	}

	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.Chan:
		size += uint64(t.Elem().Size()) * uint64(v.Cap())
		for i := 0; i < v.Len(); i++ {
			size += sizeValue(v.Index(i), false, opt, pointersSeen)
		}
	case reflect.Map:
		// This data is just an estimate of the internal map structure and will only
		// be approximately accurate. Since we cannot get access to the number of buckets,
		// we estimate the minimum bucket size needed to fit the current map length according
		// into the current Go power-of-2 bucket allocation scheme.
		overhead := uint64(48)
		bucketSize :=
			// overflow ptr in bucket struct plus hash for 8 items
			wordSize + 22 +
				// size of 8 keys in a bucket
				uint64(8*t.Key().Size()) +
				// size of 8 values in a bucket
				uint64(8*t.Elem().Size())
		buckets := uint64(math.Pow(2, math.Ceil(math.Log2(float64(v.Len()+1)/3.25))))

		var s uint64
		for iter := v.MapRange(); iter.Next(); {
			s += sizeValue(iter.Key(), false, opt, pointersSeen)
			s += sizeValue(iter.Value(), false, opt, pointersSeen)
		}
		//log.Printf("map %s had elements sized %d, estimated overhead %d, buckets %d x %d", t, s, overhead, buckets, bucketSize)
		size += s + overhead + buckets*bucketSize
	case reflect.Ptr:
		if !v.IsNil() {
			ptr := v.Pointer()
			if _, ok := pointersSeen[ptr]; ok {
				//log.Printf("skipping already seen ptr %d", ptr)
				break
			}
			pointersSeen[ptr] = struct{}{}

			size += sizeValue(v.Elem(), true, opt, pointersSeen)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			//log.Printf("assess %s.%s", v.Type(), f.Name)
			size += sizeValue(v.Field(i), false, opt, pointersSeen)
			// if s > 0 {
			// 	log.Printf("assessed %s.%s %d", t, f.Name, s)
			// }
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Bool, reflect.Complex64, reflect.Complex128, reflect.Float32, reflect.Float64,
		reflect.Func:
	case reflect.String:
		if !opt.IgnoreStrings {
			size += uint64(v.Len())
		}
	default:
		log.Printf("unknown kind: %s", v.Kind().String())
	}
	return size
}
