// Package rag manages knowledge bases and documents for RAG (Retrieval-
// Augmented Generation). Knowledge bases live at
// $LLMCTL_HOME/knowledge/<kb_id>/ with a kb.json manifest and documents
// stored under docs/<doc_id>/.
//
// Vector search uses TF-IDF vectorization with cosine similarity, implemented
// entirely in stdlib Go. Documents are split into overlapping chunks; each
// chunk is vectorized. Search returns the most relevant chunks ranked by
// cosine similarity to the query vector.
package rag

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmctl/internal/paths"
)

const (
	ManifestName = "kb.json"
	DocsDir      = "docs"
	MaxDocSize   = 10 << 20 // 10 MiB

	// Chunking parameters.
	ChunkSize    = 500 // target characters per chunk
	ChunkOverlap = 100 // overlap between adjacent chunks
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Doc is one document inside a knowledge base.
type Doc struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	Content     string    `json:"content,omitempty"` // omitted in list responses
}

// KB is one knowledge base.
type KB struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DocCount    int       `json:"doc_count"`
}

// Record is a KB with its on-disk directory.
type Record struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	KB   KB     `json:"kb"`
	Docs []Doc  `json:"docs"`
}

// SearchResult is one chunk returned by search, with relevance score.
type SearchResult struct {
	DocID      string  `json:"doc_id"`
	Filename   string  `json:"filename"`
	ChunkText  string  `json:"chunk_text"`
	Score      float64 `json:"score"`
	ChunkIndex int     `json:"chunk_index"`
}

// chunk is an internal representation of a document passage.
type chunk struct {
	DocID      string
	Filename   string
	Text       string
	Index      int
	Vector     []float64
}

// Store manages knowledge bases on disk.
type Store struct {
	dir string
}

// Open opens (creating if needed) the rag store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for a KB id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a random short id.
func genID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "kb-000000"
	}
	return "kb-" + hex.EncodeToString(b)
}

// ---------- CRUD ----------

// List returns all knowledge bases sorted by updated_at descending.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []Record{}
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		r, err := s.load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].KB.UpdatedAt.After(out[j].KB.UpdatedAt)
	})
	return out, nil
}

// Get returns one KB by id (with doc content).
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid kb id")
	}
	return s.load(id)
}

// Create writes a new KB directory and returns the record.
func (s *Store) Create(name, description string) (*Record, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("kb missing name")
	}
	id := genID()
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("kb %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	docsDir := filepath.Join(final, DocsDir)
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	now := time.Now().UTC()
	kb := KB{
		ID:          id,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := writeJSON(filepath.Join(final, ManifestName), kb); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return &Record{ID: id, Dir: final, KB: kb, Docs: []Doc{}}, nil
}

// Update replaces an existing KB's metadata.
func (s *Store) Update(id, name, description string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid kb id")
	}
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("kb missing name")
	}
	rec.KB.Name = name
	rec.KB.Description = description
	rec.KB.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return nil, err
	}
	return s.load(id)
}

// Delete removes the KB directory and all documents.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// ---------- Documents ----------

// AddDoc adds a document to a KB, chunks it, and indexes vectors.
func (s *Store) AddDoc(kbID, filename, contentType string, content []byte) (*Doc, error) {
	if len(content) > MaxDocSize {
		return nil, fmt.Errorf("document too large (max %d MiB)", MaxDocSize/1024/1024)
	}
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	docID := genID()
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc := Doc{
		ID:          docID,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(content)),
		Source:      "upload",
		CreatedAt:   now,
		Content:     string(content),
	}
	if err := writeJSON(filepath.Join(docDir, "doc.json"), doc); err != nil {
		os.RemoveAll(docDir)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(docDir, "content.txt"), content, 0o644); err != nil {
		os.RemoveAll(docDir)
		return nil, err
	}
	// Re-index all documents in the KB.
	if err := s.reindex(rec.Dir); err != nil {
		os.RemoveAll(docDir)
		return nil, err
	}
	// Update doc count.
	rec.KB.DocCount++
	rec.KB.UpdatedAt = now
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DeleteDoc removes a document from a KB and re-indexes.
func (s *Store) DeleteDoc(kbID, docID string) error {
	rec, err := s.Get(kbID)
	if err != nil {
		return err
	}
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	if err := os.RemoveAll(docDir); err != nil {
		return err
	}
	// Re-index remaining docs.
	if err := s.reindex(rec.Dir); err != nil {
		return err
	}
	rec.KB.DocCount--
	if rec.KB.DocCount < 0 {
		rec.KB.DocCount = 0
	}
	rec.KB.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return err
	}
	return nil
}

// GetDoc returns a document by kb id and doc id (with content).
func (s *Store) GetDoc(kbID, docID string) (*Doc, error) {
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	return s.loadDoc(docDir)
}

// ---------- Search ----------

// Search finds the most relevant chunks across all documents in a KB using
// TF-IDF cosine similarity. Returns results sorted by score descending.
func (s *Store) Search(kbID, query string, limit int) ([]SearchResult, error) {
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	// Load vocabulary and all chunk vectors.
	vocab, err := s.loadVocab(rec.Dir)
	if err != nil || len(vocab) == 0 {
		// No index yet — fall back to keyword match.
		return s.keywordFallback(rec, query, limit), nil
	}
	chunks, err := s.loadAllChunks(rec.Dir)
	if err != nil || len(chunks) == 0 {
		return s.keywordFallback(rec, query, limit), nil
	}
	// Build query vector using same vocabulary.
	qTokens := tokenize(query)
	qVec := tfidfVector(qTokens, vocab, chunks)
	// Score each chunk.
	type scored struct {
		chunk chunk
		score float64
	}
	var results []scored
	for _, c := range chunks {
		if len(c.Vector) == 0 {
			continue
		}
		sim := cosineSimilarity(qVec, c.Vector)
		if sim > 0 {
			results = append(results, scored{chunk: c, score: sim})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	// Deduplicate by document (keep best chunk per doc).
	seen := make(map[string]bool)
	out := []SearchResult{}
	for _, r := range results {
		if seen[r.chunk.DocID] {
			continue
		}
		seen[r.chunk.DocID] = true
		out = append(out, SearchResult{
			DocID:      r.chunk.DocID,
			Filename:   r.chunk.Filename,
			ChunkText:  r.chunk.Text,
			Score:      math.Round(r.score*10000) / 10000,
			ChunkIndex: r.chunk.Index,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// keywordFallback is used when no vector index exists yet.
func (s *Store) keywordFallback(rec *Record, query string, limit int) []SearchResult {
	qLower := strings.ToLower(query)
	out := []SearchResult{}
	for _, d := range rec.Docs {
		idx := strings.Index(strings.ToLower(d.Content), qLower)
		if idx < 0 && !strings.Contains(strings.ToLower(d.Filename), qLower) {
			continue
		}
		snippet := d.Content
		if len(snippet) > 300 {
			if idx > 0 {
				start := idx - 100
				if start < 0 {
					start = 0
				}
				end := start + 300
				if end > len(snippet) {
					end = len(snippet)
				}
				snippet = snippet[start:end]
			} else {
				snippet = snippet[:300]
			}
		}
		out = append(out, SearchResult{
			DocID:     d.ID,
			Filename:  d.Filename,
			ChunkText: snippet,
			Score:     1.0,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// ---------- Indexing ----------

// reindex rebuilds the TF-IDF index for a KB (vocab + per-chunk vectors).
func (s *Store) reindex(kbDir string) error {
	// Load all documents' content.
	docsDir := filepath.Join(kbDir, DocsDir)
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var allChunks []chunk
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		doc, err := s.loadDoc(filepath.Join(docsDir, e.Name()))
		if err != nil {
			continue
		}
		textChunks := splitChunks(doc.Content)
		for i, tc := range textChunks {
			allChunks = append(allChunks, chunk{
				DocID:    doc.ID,
				Filename: doc.Filename,
				Text:     tc,
				Index:    i,
			})
		}
	}
	if len(allChunks) == 0 {
		// Remove stale index.
		os.Remove(filepath.Join(kbDir, "vocab.json"))
		os.Remove(filepath.Join(kbDir, "vectors.json"))
		return nil
	}
	// Build vocabulary from all chunks.
	vocab := buildVocab(allChunks)
	// Compute TF-IDF vector for each chunk.
	for i := range allChunks {
		tokens := tokenize(allChunks[i].Text)
		allChunks[i].Vector = tfidfVector(tokens, vocab, allChunks)
	}
	// Persist vocabulary.
	if err := writeJSON(filepath.Join(kbDir, "vocab.json"), vocab); err != nil {
		return err
	}
	// Persist vectors (grouped by doc for efficient loading).
	type docVectors struct {
		DocID  string      `json:"doc_id"`
		Vectors [][]float64 `json:"vectors"`
	}
	byDoc := make(map[string]*docVectors)
	for _, c := range allChunks {
		dv, ok := byDoc[c.DocID]
		if !ok {
			dv = &docVectors{DocID: c.DocID}
			byDoc[c.DocID] = dv
		}
		dv.Vectors = append(dv.Vectors, c.Vector)
	}
	dvList := make([]docVectors, 0, len(byDoc))
	for _, dv := range byDoc {
		dvList = append(dvList, *dv)
	}
	return writeJSON(filepath.Join(kbDir, "vectors.json"), dvList)
}

// loadVocab loads the vocabulary for a KB.
func (s *Store) loadVocab(kbDir string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(kbDir, "vocab.json"))
	if err != nil {
		return nil, err
	}
	var vocab []string
	if err := json.Unmarshal(b, &vocab); err != nil {
		return nil, err
	}
	return vocab, nil
}

// loadAllChunks loads all chunk vectors for a KB.
func (s *Store) loadAllChunks(kbDir string) ([]chunk, error) {
	vb, err := os.ReadFile(filepath.Join(kbDir, "vectors.json"))
	if err != nil {
		return nil, err
	}
	type docVectors struct {
		DocID   string       `json:"doc_id"`
		Vectors [][]float64  `json:"vectors"`
	}
	var dvList []docVectors
	if err := json.Unmarshal(vb, &dvList); err != nil {
		return nil, err
	}
	// Build a map of docID -> chunk texts.
	docsDir := filepath.Join(kbDir, DocsDir)
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, err
	}
	docChunks := make(map[string][]string)
	docNames := make(map[string]string)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		doc, err := s.loadDoc(filepath.Join(docsDir, e.Name()))
		if err != nil {
			continue
		}
		docChunks[doc.ID] = splitChunks(doc.Content)
		docNames[doc.ID] = doc.Filename
	}
	var out []chunk
	for _, dv := range dvList {
		texts := docChunks[dv.DocID]
		name := docNames[dv.DocID]
		for i, vec := range dv.Vectors {
			text := ""
			if i < len(texts) {
				text = texts[i]
			}
			out = append(out, chunk{
				DocID:    dv.DocID,
				Filename: name,
				Text:     text,
				Index:    i,
				Vector:   vec,
			})
		}
	}
	return out, nil
}

// ---------- Chunking ----------

// splitChunks splits text into overlapping chunks at sentence boundaries.
func splitChunks(text string) []string {
	if len(text) <= ChunkSize {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}
	// Split into sentences.
	sentences := splitSentences(text)
	var chunks []string
	var current strings.Builder
	for _, sent := range sentences {
		if current.Len()+len(sent) > ChunkSize && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			// Start new chunk with overlap.
			current.Reset()
			// Add tail of previous chunk for overlap.
			prev := chunks[len(chunks)-1]
			if len(prev) > ChunkOverlap {
				overlap := prev[len(prev)-ChunkOverlap:]
				// Find word boundary.
				if idx := strings.Index(overlap, " "); idx > 0 {
					overlap = overlap[idx+1:]
				}
				current.WriteString(overlap)
			}
		}
		current.WriteString(sent)
	}
	if strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

// splitSentences splits text at sentence boundaries (period, exclamation, question mark).
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	for i, r := range text {
		current.WriteRune(r)
		if (r == '.' || r == '!' || r == '?' || r == '\n') && current.Len() > 0 {
			// Check if next char is space or end (sentence boundary).
			if i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' || text[i+1] == '\t' {
				sentences = append(sentences, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}
	return sentences
}

// ---------- Loading ----------

func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("kb %s: %w", id, err)
	}
	var kb KB
	if err := json.Unmarshal(mb, &kb); err != nil {
		return nil, fmt.Errorf("kb %s: bad manifest: %w", id, err)
	}
	if kb.ID == "" {
		kb.ID = id
	}
	docsDir := filepath.Join(dir, DocsDir)
	docs, err := s.loadDocs(docsDir)
	if err != nil {
		return nil, err
	}
	kb.DocCount = len(docs)
	return &Record{ID: kb.ID, Dir: dir, KB: kb, Docs: docs}, nil
}

func (s *Store) loadDocs(docsDir string) ([]Doc, error) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []Doc{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		doc, err := s.loadDoc(filepath.Join(docsDir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, *doc)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Store) loadDoc(dir string) (*Doc, error) {
	mb, err := os.ReadFile(filepath.Join(dir, "doc.json"))
	if err != nil {
		return nil, err
	}
	var d Doc
	if err := json.Unmarshal(mb, &d); err != nil {
		return nil, err
	}
	// Read content from separate file.
	contentPath := filepath.Join(dir, "content.txt")
	content, err := os.ReadFile(contentPath)
	if err != nil {
		d.Content = ""
	} else {
		d.Content = string(content)
	}
	return &d, nil
}

// ---------- TF-IDF ----------

// tokenize splits text into lowercase word tokens.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// buildVocab builds a sorted list of unique terms from all chunks.
func buildVocab(chunks []chunk) []string {
	vocabMap := make(map[string]bool)
	for _, c := range chunks {
		for _, t := range tokenize(c.Text) {
			vocabMap[t] = true
		}
	}
	vocab := make([]string, 0, len(vocabMap))
	for t := range vocabMap {
		vocab = append(vocab, t)
	}
	sort.Strings(vocab)
	return vocab
}

// tfidfVector computes the TF-IDF vector for tokens given a vocabulary and corpus.
func tfidfVector(tokens []string, vocab []string, corpus []chunk) []float64 {
	vec := make([]float64, len(vocab))
	if len(vocab) == 0 || len(tokens) == 0 {
		return vec
	}
	// Term frequency.
	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}
	totalTokens := float64(len(tokens))
	// IDF: pre-compute document frequency for each vocab term.
	numDocs := float64(len(corpus))
	docTokenSets := make([]map[string]bool, len(corpus))
	for i, c := range corpus {
		set := make(map[string]bool)
		for _, t := range tokenize(c.Text) {
			set[t] = true
		}
		docTokenSets[i] = set
	}
	for i, term := range vocab {
		tfVal, ok := tf[term]
		if !ok {
			continue
		}
		// Compute IDF for this term.
		docCount := 0
		for _, set := range docTokenSets {
			if set[term] {
				docCount++
			}
		}
		var idf float64
		if docCount > 0 {
			idf = math.Log(numDocs/float64(docCount)) + 1.0
		}
		tfNorm := float64(tfVal) / totalTokens
		vec[i] = tfNorm * idf
	}
	return vec
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ---------- Utilities ----------

func writeJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
