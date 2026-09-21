package gh

import "sync"

// PMap runs fn over items concurrently, preserving order, bounded to
// `concurrency` in flight at once. Note this bound is separate from the
// Client's own semaphore: this one caps how many rule-level jobs run,
// the client's caps how many HTTP requests are open across all of them.
func PMap[T, R any](items []T, concurrency int, fn func(T) R) []R {
	if len(items) == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}
	results := make([]R, len(items))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item T) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = fn(item)
		}(i, item)
	}
	wg.Wait()
	return results
}
