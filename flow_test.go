package ctxflow

import (
	"errors"
	"sync"
	"testing"
	"time"

	"runtime"

	"golang.org/x/net/context"
)

var errSome = errors.New("someError")

func returnError(context.Context) error { return errSome }

func TestSerial(t *testing.T) {
	ctx := context.Background()

	// empty
	if err := SerialFunc()(ctx); err != nil {
		t.Error(err)
	}

	count := 0
	createCheckFunc := func(i int) FlowFunc {
		return func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			if count != i {
				t.Errorf("counter must be %d, acutal %d", i, count)
			}
			count++
			return nil
		}
	}
	var fs []FlowFunc
	for i := 0; i < 10; i++ {
		fs = append(fs, createCheckFunc(i))
	}
	if err := SerialFunc(fs...)(ctx); err != nil {
		t.Error(err)
	}

	// timeout
	count = 0
	tctx, _ := context.WithTimeout(ctx, 20*time.Millisecond)
	if err := SerialFunc(fs...)(tctx); err != context.DeadlineExceeded {
		t.Errorf("must be DeadlineExceeded, given %v", err)
	}

	// cancel
	count = 0
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(20 * time.Microsecond)
		cancel()
	}()
	if err := SerialFunc(fs...)(cctx); err != context.Canceled {
		t.Errorf("must be Canceled, given %v", err)
	}

	// check error
	count = 0
	var fsErr []FlowFunc
	fsErr = append(append(append(fsErr, fs[:3]...), returnError), fs[3:]...)
	if err := SerialFunc(fsErr...)(context.Background()); err != errSome {
		t.Errorf("must be someError, given %v", err)
	}
	if count != 3 {
		t.Errorf("count must be 3 actual %d", count)
	}

	// with nil func
	count = 0
	var fsNil []FlowFunc
	fsNil = append(append(append(fsNil, fs[:3]...), nil), fs[3:]...)
	if err := SerialFunc(fsNil...)(context.Background()); err != nil {
		t.Error(err)
	}
}

type concurrentChecker struct {
	l        sync.Mutex
	count    int
	macCount int
}

func (c *concurrentChecker) Inc() {
	c.l.Lock()
	c.count++
	if c.count > c.macCount {
		c.macCount = c.count
	}
	c.l.Unlock()
}

func (c *concurrentChecker) Dec() {
	c.l.Lock()
	c.count--
	c.l.Unlock()
}

func (c *concurrentChecker) Reset() {
	c.l.Lock()
	c.count = 0
	c.macCount = 0
	c.l.Unlock()
}

func (c *concurrentChecker) F(ctx context.Context) error {
	c.Inc()
	defer c.Dec()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return nil
}

func TestParallel(t *testing.T) {
	runtime.GOMAXPROCS(4) // force parallel
	ctx, cancel := context.WithCancel(context.Background())
	beforeGoroutines := runtime.NumGoroutine()

	// empty
	if err := ParallelFunc()(ctx); err != nil {
		t.Error(err)
	}

	c := concurrentChecker{}
	var fs []FlowFunc
	for i := 0; i < 100; i++ {
		fs = append(fs, c.F)
	}

	if err := ParallelFunc(fs...)(ctx); err != nil {
		t.Error(err)
	}

	if c.macCount != 100 {
		t.Errorf("must run 100 concurrent, actual %d", c.macCount)
	}

	c.Reset()

	if err := ParallelMaxWorkersFunc(10, fs...)(ctx); err != nil {
		t.Error(err)
	}

	if c.macCount != 10 {
		t.Errorf("must run 10 concurrent, actual %d", c.macCount)
	}

	c.Reset()

	tctx, _ := context.WithTimeout(ctx, 1*time.Millisecond)
	if err := ParallelMaxWorkersFunc(10, fs...)(tctx); err != context.DeadlineExceeded {
		t.Errorf("must be DeadlineExceeded, given %v", err)
	}

	c.Reset()

	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()
	if err := ParallelMaxWorkersFunc(10, fs...)(cctx); err != context.Canceled {
		t.Errorf("must be Canceled, given %v", err)
	}

	// with error
	c.Reset()
	var fsErr []FlowFunc
	fsErr = append(append(append(fsErr, fs[:3]...), returnError), fs[3:]...)
	if err := ParallelFunc(fsErr...)(ctx); err != errSome {
		t.Errorf("must be someError, given %v", err)
	}

	// with nil func
	c.Reset()
	var fsNil []FlowFunc
	fsNil = append(append(append(fsNil, fs[:3]...), nil), fs[3:]...)
	if err := ParallelFunc(fsNil...)(context.Background()); err != nil {
		t.Error(err)
	}

	cancel()

	// wait for all goroutines to stop
	time.Sleep(10 * time.Millisecond)

	// check goroutine leak
	afterGoroutines := runtime.NumGoroutine()
	if beforeGoroutines != afterGoroutines {
		t.Errorf("goroutine leaking %d -> %d", beforeGoroutines, afterGoroutines)
	}
}
