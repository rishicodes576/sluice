package vectorstore

import (
	"math"
	"math/rand"
	"sort"
	"sync"
)

// HNSW is a from-scratch implementation of Hierarchical Navigable Small World
// graphs (Malkov & Yashunin, 2018) for approximate nearest-neighbour search.
//
// The index is a stack of proximity graphs: upper layers are sparse for fast
// long-range navigation, layer 0 is dense for accurate local search. A query
// greedily descends the layers to an entry point, then runs a bounded
// best-first search (controlled by ef) on layer 0. Insertions use the paper's
// neighbour-selection heuristic to keep the graph navigable while bounding
// out-degree at M (2*M on layer 0).
type HNSW struct {
	mu             sync.RWMutex
	dim            int
	m              int     // target out-degree per layer
	mMax0          int     // max out-degree on layer 0
	efConstruction int     // candidate list size during insertion
	ml             float64 // level-generation normalisation factor
	rng            *rand.Rand

	nodes      []*node
	byID       map[string]int
	entryPoint int
	maxLevel   int
}

type node struct {
	id        string
	vec       []float64
	neighbors [][]int // neighbors[layer] = node indices
	deleted   bool
}

// HNSWOption configures an HNSW index.
type HNSWOption func(*HNSW)

// WithM sets the target out-degree M.
func WithM(m int) HNSWOption { return func(h *HNSW) { h.m = m; h.mMax0 = 2 * m } }

// WithEfConstruction sets the construction-time candidate list size.
func WithEfConstruction(ef int) HNSWOption { return func(h *HNSW) { h.efConstruction = ef } }

// WithSeed makes level assignment deterministic (used in tests).
func WithSeed(seed int64) HNSWOption { return func(h *HNSW) { h.rng = rand.New(rand.NewSource(seed)) } }

// NewHNSW creates an index for vectors of the given dimension.
func NewHNSW(dim int, opts ...HNSWOption) *HNSW {
	h := &HNSW{
		dim: dim, m: 16, mMax0: 32, efConstruction: 200,
		rng: rand.New(rand.NewSource(1)), byID: make(map[string]int), entryPoint: -1,
	}
	for _, o := range opts {
		o(h)
	}
	h.ml = 1.0 / math.Log(float64(h.m))
	return h
}

// distance is the search metric: 1 - cosine similarity, so smaller is closer.
func (h *HNSW) distance(a, b []float64) float64 { return 1 - cosine(a, b) }

func (h *HNSW) randomLevel() int {
	return int(-math.Log(h.rng.Float64()+1e-12) * h.ml)
}

// Add implements Store.
func (h *HNSW) Add(id string, vec []float64) error {
	if len(vec) != h.dim {
		return ErrDimMismatch
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	if i, ok := h.byID[id]; ok { // update in place, keep graph edges
		h.nodes[i].vec = vec
		h.nodes[i].deleted = false
		return nil
	}

	level := h.randomLevel()
	n := &node{id: id, vec: vec, neighbors: make([][]int, level+1)}
	cur := len(h.nodes)
	h.nodes = append(h.nodes, n)
	h.byID[id] = cur

	if h.entryPoint == -1 {
		h.entryPoint = cur
		h.maxLevel = level
		return nil
	}

	ep := h.entryPoint
	// Descend from the top down to the layer just above the new node's level,
	// greedily following the single nearest neighbour.
	for lc := h.maxLevel; lc > level; lc-- {
		ep = h.greedyClosest(vec, ep, lc)
	}
	// From the new node's top layer down to 0, find neighbours and link.
	for lc := min(level, h.maxLevel); lc >= 0; lc-- {
		cands := h.searchLayer(vec, []int{ep}, h.efConstruction, lc)
		selected := h.selectNeighbors(vec, cands, h.m)
		n.neighbors[lc] = selected
		mMax := h.m
		if lc == 0 {
			mMax = h.mMax0
		}
		for _, nb := range selected {
			h.nodes[nb].neighbors[lc] = append(h.nodes[nb].neighbors[lc], cur)
			if len(h.nodes[nb].neighbors[lc]) > mMax {
				h.nodes[nb].neighbors[lc] = h.selectNeighbors(h.nodes[nb].vec, h.nodes[nb].neighbors[lc], mMax)
			}
		}
		if len(cands) > 0 {
			ep = cands[0]
		}
	}
	if level > h.maxLevel {
		h.maxLevel = level
		h.entryPoint = cur
	}
	return nil
}

// greedyClosest walks a single layer, hopping to the closest neighbour until no
// neighbour improves on the current node.
func (h *HNSW) greedyClosest(q []float64, ep, layer int) int {
	best := ep
	bestD := h.distance(q, h.nodes[ep].vec)
	for {
		improved := false
		if layer < len(h.nodes[best].neighbors) {
			for _, nb := range h.nodes[best].neighbors[layer] {
				d := h.distance(q, h.nodes[nb].vec)
				if d < bestD {
					bestD, best, improved = d, nb, true
				}
			}
		}
		if !improved {
			return best
		}
	}
}

// searchLayer runs bounded best-first search on one layer and returns candidate
// node indices ordered nearest-first.
func (h *HNSW) searchLayer(q []float64, entry []int, ef, layer int) []int {
	visited := make(map[int]bool, ef*2)
	cand := &distHeap{min: true}
	res := &distHeap{min: false}
	for _, e := range entry {
		d := h.distance(q, h.nodes[e].vec)
		visited[e] = true
		cand.push(item{e, d})
		res.push(item{e, d})
	}
	for cand.len() > 0 {
		c := cand.pop()
		if res.len() >= ef && c.dist > res.peek().dist {
			break
		}
		if layer < len(h.nodes[c.node].neighbors) {
			for _, nb := range h.nodes[c.node].neighbors[layer] {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				d := h.distance(q, h.nodes[nb].vec)
				if res.len() < ef || d < res.peek().dist {
					cand.push(item{nb, d})
					res.push(item{nb, d})
					if res.len() > ef {
						res.pop()
					}
				}
			}
		}
	}
	out := res.drainSortedAsc()
	ids := make([]int, len(out))
	for i, it := range out {
		ids[i] = it.node
	}
	return ids
}

// selectNeighbors applies the paper's heuristic: keep a candidate only if it is
// closer to the query than to any already-selected neighbour, which spreads
// connections across directions and preserves navigability.
func (h *HNSW) selectNeighbors(q []float64, candidates []int, m int) []int {
	type cd struct {
		id int
		d  float64
	}
	cds := make([]cd, 0, len(candidates))
	for _, c := range candidates {
		cds = append(cds, cd{c, h.distance(q, h.nodes[c].vec)})
	}
	sort.Slice(cds, func(i, j int) bool { return cds[i].d < cds[j].d })
	selected := make([]int, 0, m)
	for _, c := range cds {
		if len(selected) >= m {
			break
		}
		good := true
		for _, s := range selected {
			if h.distance(h.nodes[c.id].vec, h.nodes[s].vec) < c.d {
				good = false
				break
			}
		}
		if good {
			selected = append(selected, c.id)
		}
	}
	// If the heuristic was too strict, top up with the next closest candidates.
	for _, c := range cds {
		if len(selected) >= m {
			break
		}
		if !contains(selected, c.id) {
			selected = append(selected, c.id)
		}
	}
	return selected
}

// Search implements Store.
func (h *HNSW) Search(vec []float64, k int) []Match {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.entryPoint == -1 {
		return nil
	}
	ep := h.entryPoint
	for lc := h.maxLevel; lc > 0; lc-- {
		ep = h.greedyClosest(vec, ep, lc)
	}
	ef := h.efConstruction
	if k > ef {
		ef = k
	}
	cands := h.searchLayer(vec, []int{ep}, ef, 0)
	out := make([]Match, 0, k)
	for _, c := range cands {
		if h.nodes[c].deleted {
			continue
		}
		out = append(out, Match{ID: h.nodes[c].id, Similarity: cosine(vec, h.nodes[c].vec)})
		if len(out) >= k {
			break
		}
	}
	return out
}

// Delete implements Store via soft-deletion: the node stays in the graph to
// preserve connectivity but is excluded from results.
func (h *HNSW) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i, ok := h.byID[id]; ok {
		h.nodes[i].deleted = true
		delete(h.byID, id)
	}
}

// Len implements Store.
func (h *HNSW) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byID)
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// item is a (node, distance) pair used by the search heaps.
type item struct {
	node int
	dist float64
}

// distHeap is a binary heap over items. When min is true it is a min-heap
// (nearest on top); otherwise a max-heap (farthest on top).
type distHeap struct {
	items []item
	min   bool
}

func (h *distHeap) len() int   { return len(h.items) }
func (h *distHeap) peek() item { return h.items[0] }
func (h *distHeap) less(i, j int) bool {
	if h.min {
		return h.items[i].dist < h.items[j].dist
	}
	return h.items[i].dist > h.items[j].dist
}

func (h *distHeap) push(it item) {
	h.items = append(h.items, it)
	i := len(h.items) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !h.less(i, p) {
			break
		}
		h.items[i], h.items[p] = h.items[p], h.items[i]
		i = p
	}
}

func (h *distHeap) pop() item {
	top := h.items[0]
	last := len(h.items) - 1
	h.items[0] = h.items[last]
	h.items = h.items[:last]
	i, n := 0, len(h.items)
	for {
		l, r, smallest := 2*i+1, 2*i+2, i
		if l < n && h.less(l, smallest) {
			smallest = l
		}
		if r < n && h.less(r, smallest) {
			smallest = r
		}
		if smallest == i {
			break
		}
		h.items[i], h.items[smallest] = h.items[smallest], h.items[i]
		i = smallest
	}
	return top
}

// drainSortedAsc empties the heap and returns items sorted by ascending
// distance (nearest first).
func (h *distHeap) drainSortedAsc() []item {
	out := make([]item, len(h.items))
	copy(out, h.items)
	sort.Slice(out, func(i, j int) bool { return out[i].dist < out[j].dist })
	h.items = h.items[:0]
	return out
}
