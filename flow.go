package ctxflow

import "golang.org/x/net/context"

// FlowFunc is a function that receives context and returns error
type FlowFunc func(ctx context.Context) error

// SerialFunc returns a FlowFunc that processes given FlowFuncs in serial
func SerialFunc(fs ...FlowFunc) FlowFunc {
	return func(ctx context.Context) error {
		for _, f := range fs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if f == nil {
				continue
			}
			if err := f(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

// ParallelFunc returns a FlowFunc that processes given FlowFuncs in parallel.
// If some error occurs in a FlowFunc, return the error and cancels other funcs.
func ParallelFunc(fs ...FlowFunc) FlowFunc {
	return parallel(-1, fs...)
}

// ParallelMaxWorkersFunc returns a FlowFunc that processes given FlowFuncs in parallel but limits gorutines to given workerCount.
// Panics if  workerCount < 1.
// If some error occurs in a FlowFunc, return the error and cancels other funcs.
func ParallelMaxWorkersFunc(workerCount int, fs ...FlowFunc) FlowFunc {
	if workerCount < 1 {
		panic("worker count must be grater than 0")
	}
	return parallel(workerCount, fs...)
}

func parallel(workerCount int, fs ...FlowFunc) FlowFunc {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var sem chan struct{}
		if workerCount > 0 {
			sem = make(chan struct{}, workerCount)
		}

		errCh := make(chan error, len(fs))
		doneCh := make(chan struct{}, len(fs))

		for _, f := range fs {
			go func(f FlowFunc) {
				if f == nil {
					goto end
				}

				if sem != nil {
					select {
					case sem <- struct{}{}:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}
				defer func() {
					if sem != nil {
						<-sem
					}
				}()

				if err := f(ctx); err != nil {
					errCh <- err
					return
				}
			end:
				doneCh <- struct{}{}
			}(f)
		}
		var err error
		for i := 0; i < len(fs); i++ {
			select {
			case iErr := <-errCh:
				if err != nil {
					err = iErr
				}
			case <-doneCh:
			}
		}
		if err != nil {
			return err
		}
		return nil
	}
}
