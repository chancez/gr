package review

import (
	"container/list"
	"context"
	"strings"
	"sync"

	"github.com/chancez/gr/internal/git"
)

// DefaultCacheLines bounds the diff cache by the total number of diff lines it holds.
const DefaultCacheLines = 300_000

// diffCache keeps recently used diffs, bounded by their total line count rather than their
// number, so a huge diff costs what it weighs. A diff between two fixed revisions never changes, so
// nothing is ever invalidated; entries only age out.
type diffCache struct {
	mu    sync.Mutex
	max   int
	size  int
	order *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key    string
	files  []git.FileDiff
	weight int
}

func newDiffCache(maxLines int) *diffCache {
	return &diffCache{max: maxLines, order: list.New(), items: map[string]*list.Element{}}
}

// weight is a diff's cost in the cache: its lines, and one per file.
func weight(files []git.FileDiff) int {
	n := 0
	for _, f := range files {
		n++
		for _, h := range f.Hunks {
			n += len(h.Lines)
		}
	}
	return n
}

func (c *diffCache) get(key string) ([]git.FileDiff, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).files, true
}

// put adds a diff, evicting the least recently used until the cache fits. The newest entry stays
// even when it alone is over the bound, so a diff too big to cache is still not read twice in a
// row.
func (c *diffCache) put(key string, files []git.FileDiff) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return
	}
	e := &cacheEntry{key: key, files: files, weight: weight(files)}
	c.items[key] = c.order.PushFront(e)
	c.size += e.weight
	for c.size > c.max && c.order.Len() > 1 {
		oldest := c.order.Back()
		old := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.items, old.key)
		c.size -= old.weight
	}
}

func cacheKey(from, to string, paths []string) string {
	return from + "\x00" + to + "\x00" + strings.Join(paths, "\x00")
}

// Diffs is where the service reads diffs and file lists. A *git.Repo is one, and NewCachedDiffs
// wraps one in caches, so the code using it reads the same way whether or not it is cached.
type Diffs interface {
	Diff(ctx context.Context, from, to string, paths ...string) ([]git.FileDiff, error)
	DiffFiles(ctx context.Context, from, to string) ([]git.FileDiff, error)
	CommitFiles(ctx context.Context, base, head string) (map[string][]git.FileDiff, error)
}

// NewCachedDiffs caches next's diffs in a cache of at most maxLines diff lines, and its file lists.
// Both are keyed by revisions, whose diffs never change, so nothing is invalidated.
func NewCachedDiffs(next Diffs, maxLines int) Diffs {
	return &cachedDiffs{next: next, cache: newDiffCache(maxLines)}
}

type cachedDiffs struct {
	next  Diffs
	cache *diffCache
	lists fileLists
}

func (c *cachedDiffs) Diff(ctx context.Context, from, to string, paths ...string) ([]git.FileDiff, error) {
	key := cacheKey(from, to, paths)
	if files, ok := c.cache.get(key); ok {
		return files, nil
	}
	files, err := c.next.Diff(ctx, from, to, paths...)
	if err != nil {
		return nil, err
	}
	c.cache.put(key, files)
	return files, nil
}

func (c *cachedDiffs) DiffFiles(ctx context.Context, from, to string) ([]git.FileDiff, error) {
	key := "diff:" + from + ".." + to
	if lists, ok := c.lists.get(key); ok {
		return lists[""], nil
	}
	files, err := c.next.DiffFiles(ctx, from, to)
	if err != nil {
		return nil, err
	}
	c.lists.put(key, map[string][]git.FileDiff{"": files})
	return files, nil
}

func (c *cachedDiffs) CommitFiles(ctx context.Context, base, head string) (map[string][]git.FileDiff, error) {
	key := "log:" + base + ".." + head
	if lists, ok := c.lists.get(key); ok {
		return lists, nil
	}
	lists, err := c.next.CommitFiles(ctx, base, head)
	if err != nil {
		return nil, err
	}
	c.lists.put(key, lists)
	return lists, nil
}

// fileLists caches the per-commit file lists of ranges, which are small, keeping a few ranges.
type fileLists struct {
	mu     sync.Mutex
	keys   []string
	ranges map[string]map[string][]git.FileDiff
}

const keptFileLists = 4

func (l *fileLists) get(key string) (map[string][]git.FileDiff, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.ranges[key]
	return v, ok
}

func (l *fileLists) put(key string, v map[string][]git.FileDiff) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ranges == nil {
		l.ranges = map[string]map[string][]git.FileDiff{}
	}
	if _, ok := l.ranges[key]; !ok {
		l.keys = append(l.keys, key)
	}
	l.ranges[key] = v
	for len(l.keys) > keptFileLists {
		delete(l.ranges, l.keys[0])
		l.keys = l.keys[1:]
	}
}
