package worker

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// ProcessArray asynchronously processes an array, spinning up n (n being number of CPUs) goroutine worker
// instances. ProcessArray blocks until the workers have all finished
//nolint:funlen
func ProcessArray(ctx context.Context, data []interface{},
	fn func(context.Context, interface{}) (interface{}, error)) ([]interface{}, error) {
	wg := sync.WaitGroup{}

	maxProcs := runtime.GOMAXPROCS(0)
	if maxProcs == 0 {
		maxProcs = 1
	}

	workerCtx, cancel := context.WithCancel(ctx)

	dataChan := make(chan interface{})
	errChan := make(chan error)
	resultsChan := make(chan interface{})

	numItems := len(data)
	wg.Add(numItems)

	for i := 0; i != maxProcs; i++ {
		go func() {
			processor := func() {
				for {
					select {
					case <-workerCtx.Done():
						return
					case req := <-dataChan:
						res, err := fn(workerCtx, req)
						if res != nil {
							resultsChan <- res
						}
						if err != nil {
							// otherwise, publish the error
							errChan <- err
						}
						wg.Done()
					}
				}
			}

			// call the processor
			processor()
		}()
	}

	// handle responses from the workers
	results := make([]interface{}, 0)
	errors := make([]error, 0)
	go func(results *[]interface{}, errors *[]error) {
		for {
			select {
			case <-ctx.Done():
				return
			case resp := <-resultsChan:
				*results = append(*results, resp)
			case err := <-errChan:
				*errors = append(*errors, err)
			}
		}
	}(&results, &errors)

	for _, req := range data {
		dataChan <- req
	}

	// wait for them all to have finished
	wg.Wait()

	// cancel the worker context
	cancel()

	if len(errors) > 0 {
		return results, fmt.Errorf("errors occurred: %v", errors)
	}

	return results, nil
}
