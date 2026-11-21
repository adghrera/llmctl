package rag

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! Python is great.")
	expected := []string{"hello", "world", "python", "is", "great"}
	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(expected), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token[%d] = %q, want %q", i, tok, expected[i])
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	c := []float64{0, 1, 0}
	if sim := cosineSimilarity(a, b); sim != 1.0 {
		t.Errorf("identical vectors: got %f, want 1.0", sim)
	}
	if sim := cosineSimilarity(a, c); sim != 0.0 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", sim)
	}
}

func TestSplitChunks(t *testing.T) {
	// Short text should be one chunk.
	chunks := splitChunks("Hello world.")
	if len(chunks) != 1 {
		t.Fatalf("short text: got %d chunks, want 1", len(chunks))
	}
	// Long text should produce multiple chunks.
	long := ""
	for i := 0; i < 50; i++ {
		long += "This is sentence number " + string(rune('A'+i%26)) + " about technology and programming. "
	}
	chunks = splitChunks(long)
	if len(chunks) < 2 {
		t.Fatalf("long text: got %d chunks, want >1", len(chunks))
	}
}

func TestVectorSearch(t *testing.T) {
	// Use a temp dir for the store.
	tmp := t.TempDir()
	os.Setenv("LLMCTL_HOME", tmp)
	defer os.Unsetenv("LLMCTL_HOME")

	store, err := Open()
	if err != nil {
		t.Fatal(err)
	}

	// Create KB.
	rec, err := store.Create("Test KB", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Add documents.
	_, err = store.AddDoc(rec.ID, "python.txt", "text/plain",
		[]byte("Python is a programming language used for machine learning and data science."))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AddDoc(rec.ID, "network.txt", "text/plain",
		[]byte("TCP/IP networking protocols enable communication between computers on the internet."))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AddDoc(rec.ID, "cooking.txt", "text/plain",
		[]byte("Pasta is cooked in boiling salted water. Add olive oil and parmesan cheese to taste."))
	if err != nil {
		t.Fatal(err)
	}

	// Search for programming-related query.
	results, err := store.Search(rec.ID, "machine learning programming", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'machine learning programming'")
	}
	if results[0].Filename != "python.txt" {
		t.Errorf("top result should be python.txt, got %s", results[0].Filename)
	}

	// Search for networking query.
	results, err = store.Search(rec.ID, "internet TCP communication", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'internet TCP communication'")
	}
	if results[0].Filename != "network.txt" {
		t.Errorf("top result should be network.txt, got %s", results[0].Filename)
	}

	// Verify files exist on disk.
	vocabPath := filepath.Join(store.Dir(rec.ID), "vocab.json")
	if _, err := os.Stat(vocabPath); os.IsNotExist(err) {
		t.Error("vocab.json not created")
	}
	vecPath := filepath.Join(store.Dir(rec.ID), "vectors.json")
	if _, err := os.Stat(vecPath); os.IsNotExist(err) {
		t.Error("vectors.json not created")
	}
}

// stubEmbedServer returns a fake OpenAI-compatible /v1/embeddings endpoint that
// hashes tokens into a small fixed-dimension bag-of-words vector.
func stubEmbedServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		type item struct {
			Embedding []float64 `json:"embedding"`
		}
		out := struct {
			Data []item `json:"data"`
		}{}
		for _, text := range req.Input {
			vec := make([]float64, 32)
			for _, tok := range strings.Fields(strings.ToLower(text)) {
				h := fnv.New32a()
				h.Write([]byte(tok))
				vec[h.Sum32()%32]++
			}
			out.Data = append(out.Data, item{Embedding: vec})
		}
		json.NewEncoder(w).Encode(out)
	}))
}

func TestEmbeddingSearch(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("LLMCTL_HOME", tmp)
	defer os.Unsetenv("LLMCTL_HOME")

	srv := stubEmbedServer()
	defer srv.Close()

	store, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Create("Embed KB", "")
	if err != nil {
		t.Fatal(err)
	}
	// Configure embeddings (base URL — the API appends /embeddings; here we
	// point at the stub root and include the path ourselves).
	rec, err = store.SetEmbed(rec.ID, &EmbedConfig{URL: srv.URL + "/embeddings", Model: "stub"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.KB.Embed == nil || rec.KB.Embed.Model != "stub" {
		t.Fatalf("embed config not persisted: %+v", rec.KB.Embed)
	}

	if _, err := store.AddDoc(rec.ID, "cats.txt", "text/plain",
		[]byte("Cats sleep sixteen hours a day. Kittens play with yarn.")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDoc(rec.ID, "cars.txt", "text/plain",
		[]byte("The engine needs oil changes. Brakes squeak on the highway.")); err != nil {
		t.Fatal(err)
	}

	// Embedding index must exist.
	if _, err := os.Stat(filepath.Join(store.Dir(rec.ID), "embeddings.json")); os.IsNotExist(err) {
		t.Fatal("embeddings.json not created")
	}

	// Query with a word from cats.txt must rank it first.
	results, err := store.Search(rec.ID, "kittens yarn", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Filename != "cats.txt" {
		t.Fatalf("expected cats.txt first, got %+v", results)
	}

	// Clearing embed config removes the index; search still works (TF-IDF).
	rec, err = store.SetEmbed(rec.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.KB.Embed != nil {
		t.Error("embed config should be cleared")
	}
	if _, err := os.Stat(filepath.Join(store.Dir(rec.ID), "embeddings.json")); !os.IsNotExist(err) {
		t.Error("embeddings.json should be removed when embed config cleared")
	}
	results, err = store.Search(rec.ID, "kittens yarn", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Filename != "cats.txt" {
		t.Fatalf("TF-IDF fallback broken: %+v", results)
	}
}

func TestEmbedConfigValidation(t *testing.T) {
	if (&EmbedConfig{URL: "x"}).valid() {
		t.Error("missing model should be invalid")
	}
	if (&EmbedConfig{Model: "x"}).valid() {
		t.Error("missing url should be invalid")
	}
	if !(&EmbedConfig{URL: "u", Model: "m"}).valid() {
		t.Error("url+model should be valid")
	}
}
