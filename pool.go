package van

import (
	"reflect"
	"sync"
	"sync/atomic"
)

type valuePool struct {
	pool     sync.Pool
	sliceCap int

	// metrics for testing
	allocated int32
	inuse     int32
}

func newPool(sliceCap int) *valuePool {
	p := &valuePool{
		sliceCap: sliceCap,
		pool:     sync.Pool{},
	}
	p.pool.New = func() interface{} {
		b := make([]reflect.Value, 0, sliceCap)
		atomic.AddInt32(&p.allocated, 1)
		return &b
	}
	return p
}

func (p *valuePool) get(cap int) (*[]reflect.Value, []reflect.Value) {
	if cap > p.sliceCap {
		buf := make([]reflect.Value, cap)
		return nil, buf
	}

	bufptr := p.pool.Get().(*[]reflect.Value)
	atomic.AddInt32(&p.inuse, 1)
	return bufptr, (*bufptr)[:cap]
}

func (p *valuePool) zero(buf []reflect.Value) {
	// Replace each value in the buffer with an empty reflect.Value
	//  a. to make sure we don't accidentely provide an wrong dependency
	//  b. to make sure dependencies are garbage collected
	for i := range buf {
		buf[i] = reflect.Value{}
	}
}

func (p *valuePool) put(bufptr *[]reflect.Value) {
	if bufptr != nil {
		p.zero(*bufptr)
		p.pool.Put(bufptr)
		atomic.AddInt32(&p.inuse, -1)
	}
}
