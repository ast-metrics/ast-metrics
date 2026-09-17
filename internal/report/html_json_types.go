package report

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"
)

// depRef represents a file dependency reference.
type depRef struct {
	Path  string `json:"path"`
	Short string `json:"short"`
}

// fileDepsEntry holds efferent and afferent file dependencies.
type fileDepsEntry struct {
	Efferent []depRef `json:"efferent"`
	Afferent []depRef `json:"afferent"`
}

// folderDepRef represents a folder dependency reference with an edge count.
type folderDepRef struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// folderDepsEntry holds efferent/afferent folder dependencies and file count.
type folderDepsEntry struct {
	Efferent  []folderDepRef `json:"efferent"`
	Afferent  []folderDepRef `json:"afferent"`
	FileCount int            `json:"fileCount"`
}

// folderDepsPayload is the complete folder dependencies payload.
type folderDepsPayload struct {
	Folders       map[string]folderDepsEntry `json:"folders"`
	FilesByFolder map[string][]string        `json:"filesByFolder"`
}

// StringDictionary maps FNV-64a hex hashes to original strings.
// It is used to deduplicate repeated file/folder paths across JSON payloads.
type StringDictionary struct {
	mu      sync.Mutex
	entries map[string]string
}

// NewStringDictionary creates an empty StringDictionary.
func NewStringDictionary() *StringDictionary {
	return &StringDictionary{entries: make(map[string]string)}
}

// Add registers a string and returns its 16-char hex hash.
func (d *StringDictionary) Add(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	key := fmt.Sprintf("%016x", h.Sum64())
	d.mu.Lock()
	d.entries[key] = s
	d.mu.Unlock()
	return key
}

// ToJSON serialises the dictionary to JSON.
func (d *StringDictionary) ToJSON() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, err := json.Marshal(d.entries)
	if err != nil {
		return "{}"
	}
	return string(data)
}
