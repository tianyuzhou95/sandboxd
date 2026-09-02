// Copyright (c) 2026 Ant Group Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nydus

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/inclusionAI/sandboxd/pkg/imagemanager/imageconfig"
	"github.com/sirupsen/logrus"
)

const (
	bootstrapCacheDirName    = ".bootstrap_cache"
	bootstrapCacheFileExt    = ".bootstrap"
	bootstrapCacheEnvExt     = ".env"
	bootstrapCacheProcessExt = ".process"
	bootstrapCacheOutputName = "bootstrap"
	defaultBootstrapCacheCap = 128
)

type bootstrapCache struct {
	mu       sync.Mutex
	roots    map[string]*bootstrapCacheRoot
	now      func() time.Time
	capacity int
}

type bootstrapCacheRoot struct {
	root        string
	now         func() time.Time
	capacity    int
	mu          sync.Mutex
	initialized bool
	entries     map[string]*list.Element
	lru         *list.List
	keyLocks    map[string]*bootstrapCacheLock
}

type bootstrapCacheEntry struct {
	key  string
	path string
}

type bootstrapCacheLock struct {
	mu   sync.Mutex
	refs int
}

type bootstrapCacheFile struct {
	key     string
	path    string
	modTime time.Time
}

func newBootstrapCache() *bootstrapCache {
	return newBootstrapCacheWithCapacity(defaultBootstrapCacheCap)
}

func newBootstrapCacheWithCapacity(capacity int) *bootstrapCache {
	return &bootstrapCache{
		roots:    make(map[string]*bootstrapCacheRoot),
		now:      time.Now,
		capacity: capacity,
	}
}

// Link returns (outputPath, env, image process, hit, error).
// env is nil when the cache entry predates env caching (caller should fetch from registry).
// env is non-nil (possibly empty) when the env sidecar was found.
func (c *bootstrapCache) Link(imageURL string, outputDir string) (string, []string, *imageconfig.Process, bool, error) {
	root := c.rootForOutput(outputDir)
	key := bootstrapCacheKey(imageURL)

	unlock := root.acquireKeyLock(key)
	defer unlock()

	if err := root.ensureInitialized(); err != nil {
		return "", nil, nil, false, err
	}

	outputPath := bootstrapOutputPath(outputDir)

	root.mu.Lock()
	elem := root.entries[key]
	if elem == nil {
		root.mu.Unlock()
		logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, "", outputPath, "")).Debug("nydus bootstrap cache miss")
		return "", nil, nil, false, nil
	}

	entry := elem.Value.(*bootstrapCacheEntry)
	if !fileExists(entry.path) {
		root.removeEntryLocked(elem)
		root.mu.Unlock()
		logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, entry.path, "", "")).Warn("nydus bootstrap cache entry disappeared, dropping it from cache")
		return "", nil, nil, false, nil
	}
	root.lru.MoveToFront(elem)
	cachePath := entry.path
	root.mu.Unlock()

	_ = touchFile(cachePath, root.now())
	if err := ensureHardLink(cachePath, outputPath); err != nil {
		return "", nil, nil, false, fmt.Errorf("failed to hardlink cached bootstrap %s to %s: %w", cachePath, outputPath, err)
	}

	// Read env sidecar — nil means old entry without env cache.
	env := readEnvSidecar(root.envPath(key))
	process := readProcessSidecar(root.processPath(key))

	logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, cachePath, outputPath, "")).
		WithField("env_cached", env != nil).
		WithField("process_cached", process != nil).
		Debug("reused cached nydus bootstrap")

	return outputPath, env, process, true, nil
}

func (c *bootstrapCache) Store(imageURL string, outputDir string, bootstrapPath string, env []string, process *imageconfig.Process) error {
	if bootstrapPath == "" {
		return fmt.Errorf("bootstrap path is empty")
	}

	root := c.rootForOutput(outputDir)
	key := bootstrapCacheKey(imageURL)

	unlock := root.acquireKeyLock(key)
	defer unlock()

	if err := root.ensureInitialized(); err != nil {
		return err
	}

	cachePath := root.cachePath(key)
	if err := ensureHardLink(bootstrapPath, cachePath); err != nil {
		return fmt.Errorf("failed to cache bootstrap %s at %s: %w", bootstrapPath, cachePath, err)
	}

	// Write env sidecar (even if env is empty, so we know it was cached).
	if err := writeEnvSidecar(root.envPath(key), env); err != nil {
		logrus.WithError(err).Warnf("failed to write env sidecar for %s", imageURL)
	}
	if err := writeProcessSidecar(root.processPath(key), process); err != nil {
		logrus.WithError(err).Warnf("failed to write process sidecar for %s", imageURL)
	}

	evictedPaths := root.recordAccess(key, cachePath)
	_ = touchFile(cachePath, root.now())

	logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, cachePath, "", bootstrapPath)).
		WithField("evicted_entries", len(evictedPaths)).
		Debug("stored nydus bootstrap in cache")

	for _, path := range evictedPaths {
		logrus.WithFields(bootstrapCacheLogFields("", "", root.root, path, "", "")).Info("evicted nydus bootstrap cache entry")
		_ = os.Remove(path)
		// Also remove env sidecar for evicted entries.
		removeBootstrapCacheSidecars(path)
	}
	return nil
}

func (c *bootstrapCache) rootForOutput(outputDir string) *bootstrapCacheRoot {
	rootDir := filepath.Join(filepath.Dir(filepath.Clean(outputDir)), bootstrapCacheDirName)

	c.mu.Lock()
	defer c.mu.Unlock()

	root := c.roots[rootDir]
	if root == nil {
		root = &bootstrapCacheRoot{
			root:     rootDir,
			now:      c.now,
			capacity: c.capacity,
			entries:  make(map[string]*list.Element),
			lru:      list.New(),
			keyLocks: make(map[string]*bootstrapCacheLock),
		}
		c.roots[rootDir] = root
	}
	return root
}

func (r *bootstrapCacheRoot) ensureInitialized() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.initialized {
		return nil
	}

	if err := os.MkdirAll(r.root, 0755); err != nil {
		return fmt.Errorf("failed to create bootstrap cache dir %s: %w", r.root, err)
	}

	entries, err := os.ReadDir(r.root)
	if err != nil {
		return fmt.Errorf("failed to read bootstrap cache dir %s: %w", r.root, err)
	}

	files := make([]bootstrapCacheFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), bootstrapCacheFileExt) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		key := strings.TrimSuffix(entry.Name(), bootstrapCacheFileExt)
		files = append(files, bootstrapCacheFile{
			key:     key,
			path:    filepath.Join(r.root, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, file := range files {
		elem := r.lru.PushBack(&bootstrapCacheEntry{key: file.key, path: file.path})
		r.entries[file.key] = elem
	}

	evictedPaths := r.evictLocked()
	for _, path := range evictedPaths {
		_ = os.Remove(path)
		removeBootstrapCacheSidecars(path)
	}

	r.initialized = true
	logrus.WithFields(bootstrapCacheLogFields("", "", r.root, "", "", "")).
		WithField("cache_entries", len(r.entries)).
		WithField("evicted_entries", len(evictedPaths)).
		WithField("capacity", r.capacity).
		Debug("initialized nydus bootstrap cache")
	return nil
}

func (r *bootstrapCacheRoot) recordAccess(key string, cachePath string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if elem := r.entries[key]; elem != nil {
		entry := elem.Value.(*bootstrapCacheEntry)
		entry.path = cachePath
		r.lru.MoveToFront(elem)
	} else {
		elem = r.lru.PushFront(&bootstrapCacheEntry{key: key, path: cachePath})
		r.entries[key] = elem
	}

	return r.evictLocked()
}

func (r *bootstrapCacheRoot) evictLocked() []string {
	if r.capacity <= 0 {
		return nil
	}

	evicted := make([]string, 0)
	for len(r.entries) > r.capacity {
		elem := r.lru.Back()
		if elem == nil {
			break
		}
		entry := elem.Value.(*bootstrapCacheEntry)
		evicted = append(evicted, entry.path)
		r.removeEntryLocked(elem)
	}
	return evicted
}

func (r *bootstrapCacheRoot) removeEntryLocked(elem *list.Element) {
	if elem == nil {
		return
	}
	entry := elem.Value.(*bootstrapCacheEntry)
	delete(r.entries, entry.key)
	r.lru.Remove(elem)
}

func (r *bootstrapCacheRoot) acquireKeyLock(key string) func() {
	r.mu.Lock()
	lock := r.keyLocks[key]
	if lock == nil {
		lock = &bootstrapCacheLock{}
		r.keyLocks[key] = lock
	}
	lock.refs++
	r.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		r.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(r.keyLocks, key)
		}
		r.mu.Unlock()
	}
}

func (r *bootstrapCacheRoot) cachePath(key string) string {
	return filepath.Join(r.root, key+bootstrapCacheFileExt)
}

func (r *bootstrapCacheRoot) envPath(key string) string {
	return filepath.Join(r.root, key+bootstrapCacheEnvExt)
}

func (r *bootstrapCacheRoot) processPath(key string) string {
	return filepath.Join(r.root, key+bootstrapCacheProcessExt)
}

func bootstrapCacheKey(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return hex.EncodeToString(sum[:])
}

func bootstrapOutputPath(outputDir string) string {
	return filepath.Join(outputDir, bootstrapCacheOutputName)
}

func bootstrapCacheLogFields(imageURL string, key string, cacheRoot string, cachePath string, outputPath string, bootstrapPath string) logrus.Fields {
	fields := logrus.Fields{}
	if imageURL != "" {
		fields["image_url"] = imageURL
	}
	if key != "" {
		fields["cache_key"] = key
	}
	if cacheRoot != "" {
		fields["cache_root"] = cacheRoot
	}
	if cachePath != "" {
		fields["cache_path"] = cachePath
	}
	if outputPath != "" {
		fields["output_path"] = outputPath
	}
	if bootstrapPath != "" {
		fields["bootstrap_path"] = bootstrapPath
	}
	return fields
}

func ensureHardLink(sourcePath string, targetPath string) error {
	if sameFile(sourcePath, targetPath) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create bootstrap parent dir for %s: %w", targetPath, err)
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to replace bootstrap target %s: %w", targetPath, err)
	}
	if err := os.Link(sourcePath, targetPath); err != nil {
		return err
	}
	return nil
}

func touchFile(path string, now time.Time) error {
	return os.Chtimes(path, now, now)
}

func sameFile(pathA string, pathB string) bool {
	if pathA == "" || pathB == "" {
		return false
	}
	infoA, err := os.Stat(pathA)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(pathB)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// writeEnvSidecar atomically writes env vars as a JSON array to the sidecar path.
// Uses write-to-temp-then-rename to avoid leaving a corrupted file on crash.
// A nil env is written as an empty JSON array ([]). The presence of the
// file itself signals that env was cached.
func writeEnvSidecar(path string, env []string) error {
	if env == nil {
		env = []string{}
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readEnvSidecar reads a cached env sidecar file.
// Returns nil if the file does not exist (old cache entry without env).
// Returns a non-nil slice (possibly empty) if the file was read.
func readEnvSidecar(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var env []string
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	return env
}

func writeProcessSidecar(path string, process *imageconfig.Process) error {
	if process == nil {
		return fmt.Errorf("image process is unresolved")
	}
	data, err := json.Marshal(process)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readProcessSidecar(path string) *imageconfig.Process {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	process := &imageconfig.Process{}
	if err := json.Unmarshal(data, process); err != nil {
		return nil
	}
	return process
}

func removeBootstrapCacheSidecars(bootstrapPath string) {
	base := strings.TrimSuffix(bootstrapPath, bootstrapCacheFileExt)
	_ = os.Remove(base + bootstrapCacheEnvExt)
	_ = os.Remove(base + bootstrapCacheProcessExt)
}
