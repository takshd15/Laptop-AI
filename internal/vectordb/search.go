package vectordb

import "container/heap"

// scoreHeap is a min-heap of SearchResults ordered by Score ascending.
// The root (index 0) is always the LOWEST score among the current candidates.
//
// Why a min-heap for top-K?
//   A sort-all approach allocates n result slots and costs O(n log n).
//   The min-heap approach keeps only k slots and costs O(n log k):
//     - heap size < k  → push unconditionally
//     - new score > root (current worst) → evict root, push new
//   For n=100,000 records and k=5: ~230,000 ops vs ~1,700,000 for sort.
type scoreHeap []SearchResult

func (h scoreHeap) Len() int            { return len(h) }
func (h scoreHeap) Less(i, j int) bool  { return h[i].Score < h[j].Score } // min at root
func (h scoreHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *scoreHeap) Push(x interface{}) { *h = append(*h, x.(SearchResult)) }
func (h *scoreHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// topKSearch scans one partition and returns its local top-K results.
// This is called once per goroutine in parallelTopKSearch.
func topKSearch(partition []Record, query []float32, queryNorm float32, k int) []SearchResult {
	h := make(scoreHeap, 0, k)
	heap.Init(&h)

	for _, rec := range partition {
		if rec.Norm == 0 || len(rec.Vector) != len(query) {
			continue
		}
		score := cosineSimilarity(query, queryNorm, rec.Vector, rec.Norm)

		if h.Len() < k {
			heap.Push(&h, SearchResult{Record: rec, Score: score})
		} else if score > h[0].Score {
			heap.Pop(&h)
			heap.Push(&h, SearchResult{Record: rec, Score: score})
		}
	}

	out := make([]SearchResult, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(SearchResult)
	}
	return out
}

// parallelTopKSearch launches one goroutine per partition and merges their results.
//
// Search flow:
//
//	query vector
//	    ↓
//	[partition 0] → goroutine → local top-K
//	[partition 1] → goroutine → local top-K     ← all run concurrently
//	[partition 2] → goroutine → local top-K
//	    ↓
//	merge: global top-K from all local results
//
// Each partition is one segment's worth of records (aligned in Open/flush).
// Total cost: O(n/p * log k) parallel + O(p*k * log k) merge,
// where p = number of partitions, n = total records.
func parallelTopKSearch(partitions [][]Record, query []float32, k int) []SearchResult {
	if k <= 0 || len(query) == 0 || len(partitions) == 0 {
		return nil
	}

	queryNorm := l2Norm(query)
	if queryNorm == 0 {
		return nil
	}

	ch := make(chan []SearchResult, len(partitions))

	for _, part := range partitions {
		p := part // capture — each goroutine gets its own slice reference
		go func() {
			ch <- topKSearch(p, query, queryNorm, k)
		}()
	}

	// Collect local top-K from every goroutine.
	// The channel is buffered to len(partitions), so goroutines never block.
	partial := make([]SearchResult, 0, len(partitions)*k)
	for range partitions {
		partial = append(partial, <-ch...)
	}

	return mergeTopK(partial, k)
}

// mergeTopK finds the global top-K from a set of already-scored partial results.
// Runs one final heap pass — O(m log k) where m = len(results).
func mergeTopK(results []SearchResult, k int) []SearchResult {
	if k <= 0 || len(results) == 0 {
		return nil
	}

	h := make(scoreHeap, 0, k)
	heap.Init(&h)

	for _, r := range results {
		if h.Len() < k {
			heap.Push(&h, r)
		} else if r.Score > h[0].Score {
			heap.Pop(&h)
			heap.Push(&h, r)
		}
	}

	out := make([]SearchResult, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(SearchResult)
	}
	return out
}
