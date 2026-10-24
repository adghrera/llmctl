package rag

import (
	"os"
	"path/filepath"
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
