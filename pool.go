package van

import (
	"reflect"
	"sync/atomic"
)

const (
	poolSize     = 64
	poolItemSize = 16
)

type argPool struct {
	freeList chan *[]reflect.Value
	itemSize int

	// metrics for testing
	allocated int32
	inuse     int32
}

func newPool() *argPool {
	p := &argPool{
		freeList: make(chan *[]reflect.Value, poolSize),
		itemSize: poolItemSize,
	}
	return p
}

func (p *argPool) get(cap int) (*[]reflect.Value, []reflect.Value) {
	if cap > p.itemSize {
		return nil, make([]reflect.Value, cap)
	}

	select {
	case bufptr := <-p.freeList:
		atomic.AddInt32(&p.inuse, 1)
		return bufptr, (*bufptr)[:cap]
	default:
		buf := make([]reflect.Value, 0, p.itemSize)
		atomic.AddInt32(&p.allocated, 1)
		atomic.AddInt32(&p.inuse, 1)
		return &buf, buf[:cap]
	}
}

func (p *argPool) put(bufptr *[]reflect.Value) {
	if bufptr != nil {
		p.zero(*bufptr)

		select {
		case p.freeList <- bufptr:
			atomic.AddInt32(&p.inuse, -1)
		default:
			atomic.AddInt32(&p.allocated, -1)
			atomic.AddInt32(&p.inuse, -1)
		}
	}
}

func (p *argPool) zero(buf []reflect.Value) {
	// Replace each value in the buffer with an empty reflect.Value
	//  a. to make sure we don't accidentely provide an wrong dependency
	//  b. to make sure dependencies are garbage collected
	for i := range buf {
		buf[i] = reflect.Value{}
	}
}
