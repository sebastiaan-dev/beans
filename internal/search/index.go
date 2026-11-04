// Package search provides full-text search functionality for beans using Bleve.
package search

import (
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/hmans/beans/pkg/bean"
)

// Index wraps a Bleve in-memory index for searching beans.
type Index struct {
	index bleve.Index
}

// beanDocument is the structure stored in the Bleve index.
type beanDocument struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// NewIndex creates a new in-memory Bleve index.
func NewIndex() (*Index, error) {
	idx, err := newMemIndex()
	if err != nil {
		return nil, err
	}
	return &Index{index: idx}, nil
}

// newMemIndex creates an in-memory scorch index. An empty path makes scorch
// keep all segments in memory. Scorch is much faster and lighter than
// bleve.NewMemOnly (upsidedown on a gtreap store), whose cost grows
// superlinearly with the amount of indexed text.
func newMemIndex() (bleve.Index, error) {
	return bleve.NewUsing("", sharedIndexMapping, scorch.Name, scorch.Name, nil)
}

// sharedIndexMapping is immutable after construction and safe to share.
var sharedIndexMapping = buildIndexMapping()

// buildIndexMapping creates the Bleve index mapping for bean documents.
func buildIndexMapping() mapping.IndexMapping {
	// Create a text field mapping with the standard analyzer
	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Analyzer = "standard"
	// Only matching is needed: don't store field contents or doc values.
	// Term vectors stay enabled because phrase queries need positions.
	textFieldMapping.Store = false
	textFieldMapping.DocValues = false

	// Create a keyword field mapping for ID (stored but not analyzed)
	keywordFieldMapping := bleve.NewKeywordFieldMapping()

	// Create the document mapping
	beanMapping := bleve.NewDocumentMapping()
	beanMapping.AddFieldMappingsAt("id", keywordFieldMapping)
	beanMapping.AddFieldMappingsAt("slug", textFieldMapping)
	beanMapping.AddFieldMappingsAt("title", textFieldMapping)
	beanMapping.AddFieldMappingsAt("body", textFieldMapping)

	// Create the index mapping with BM25 scoring for better relevance ranking
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = beanMapping
	indexMapping.DefaultAnalyzer = "standard"
	indexMapping.IndexDynamic = false
	indexMapping.StoreDynamic = false

	// Use BM25 scoring algorithm (available in Bleve v2.5.0+)
	// BM25 provides better relevance ranking than TF-IDF, especially for:
	// - Handling term frequency saturation (repeated terms don't over-boost)
	// - Normalizing for document length (short docs aren't unfairly penalized)
	indexMapping.ScoringModel = "bm25"

	return indexMapping
}

// Close closes the index.
func (idx *Index) Close() error {
	return idx.index.Close()
}

// IndexBean adds or updates a bean in the search index.
func (idx *Index) IndexBean(b *bean.Bean) error {
	doc := beanDocument{
		ID:    b.ID,
		Slug:  b.Slug,
		Title: b.Title,
		Body:  b.Body,
	}
	return idx.index.Index(b.ID, doc)
}

// DeleteBean removes a bean from the search index.
func (idx *Index) DeleteBean(id string) error {
	return idx.index.Delete(id)
}

// DefaultSearchLimit is the default maximum number of search results.
const DefaultSearchLimit = 1000

// Search executes a search query and returns matching bean IDs.
// The limit parameter controls the maximum number of results (0 uses DefaultSearchLimit,
// a negative limit returns all matches).
func (idx *Index) Search(queryStr string, limit int) ([]string, error) {
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	if limit < 0 {
		count, err := idx.index.DocCount()
		if err != nil {
			return nil, err
		}
		limit = int(count)
	}
	return searchIndex(idx.index, queryStr, limit)
}

// newQuery builds a Bleve query from query string syntax, which supports:
//   - Simple terms: "authentication"
//   - Boolean operators: "user AND password"
//   - Wildcards: "auth*"
//   - Phrases: "\"user login\""
//   - Field-specific: "title:login"
func newQuery(queryStr string) *query.QueryStringQuery {
	return bleve.NewQueryStringQuery(queryStr)
}

func searchIndex(index bleve.Index, queryStr string, limit int) ([]string, error) {
	searchRequest := bleve.NewSearchRequest(newQuery(queryStr))
	searchRequest.Size = limit
	searchRequest.Fields = []string{"id"} // Only return ID field

	result, err := index.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		ids = append(ids, hit.ID)
	}

	return ids, nil
}

// indexBatchSize bounds the number of documents per batch in IndexBeans.
// One huge batch builds one huge in-memory segment, which needs far more
// peak memory than several smaller ones.
const indexBatchSize = 256

// IndexBeans indexes multiple beans in batches for efficiency.
func (idx *Index) IndexBeans(beans []*bean.Bean) error {
	for start := 0; start < len(beans); start += indexBatchSize {
		end := min(start+indexBatchSize, len(beans))
		if err := indexBeans(idx.index, beans[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func indexBeans(index bleve.Index, beans []*bean.Bean) error {
	batch := index.NewBatch()
	for _, b := range beans {
		doc := beanDocument{
			ID:    b.ID,
			Slug:  b.Slug,
			Title: b.Title,
			Body:  b.Body,
		}
		if err := batch.Index(b.ID, doc); err != nil {
			return err
		}
	}
	return index.Batch(batch)
}

// matchChunkSize is the number of beans indexed together by Match. Small
// chunks keep memory bounded regardless of store size; chunks are processed
// in parallel.
const matchChunkSize = 64

// maxMatchWorkers caps parallelism in Match. Building segments allocates
// heavily, so more workers mostly add memory pressure, not speed.
const maxMatchWorkers = 4

// Match returns the IDs of all beans matching the query, without building a
// persistent index. The beans are processed in independent chunks, each
// indexed into a short-lived in-memory index, so memory stays bounded and the
// work spreads across several CPUs. Since query string matching does not depend on
// corpus statistics, the result is the same set an index over all beans would
// match. Results are returned in input order.
//
// Use this for one-shot searches; long-running processes that search
// repeatedly should maintain an Index instead.
func Match(beans []*bean.Bean, queryStr string) ([]string, error) {
	// Validate the query once up front so syntax errors are reported even
	// when there is nothing to search.
	if err := newQuery(queryStr).Validate(); err != nil {
		return nil, fmt.Errorf("invalid search query: %w", err)
	}

	numChunks := (len(beans) + matchChunkSize - 1) / matchChunkSize
	results := make([][]string, numChunks)
	errs := make([]error, numChunks)

	chunks := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), maxMatchWorkers, numChunks) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range chunks {
				start := i * matchChunkSize
				end := min(start+matchChunkSize, len(beans))
				results[i], errs[i] = matchChunk(beans[start:end], queryStr)
			}
		}()
	}
	for i := range numChunks {
		chunks <- i
	}
	close(chunks)
	wg.Wait()

	order := make(map[string]int, len(beans))
	for i, b := range beans {
		order[b.ID] = i
	}
	var ids []string
	for i := range numChunks {
		if errs[i] != nil {
			return nil, errs[i]
		}
		// Hits within a chunk come in relevance order; restore input order
		chunkIDs := results[i]
		slices.SortFunc(chunkIDs, func(a, b string) int { return order[a] - order[b] })
		ids = append(ids, chunkIDs...)
	}
	return ids, nil
}

func matchChunk(beans []*bean.Bean, queryStr string) ([]string, error) {
	index, err := newMemIndex()
	if err != nil {
		return nil, err
	}
	defer index.Close()

	if err := indexBeans(index, beans); err != nil {
		return nil, err
	}
	return searchIndex(index, queryStr, len(beans))
}
