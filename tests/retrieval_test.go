// Retrieval quality evaluation for laptop-ai.
//
// Uses a small four-document corpus with 30 labelled questions.
// A vocabulary-based TF (term-frequency) embedder provides deterministic,
// semantically meaningful vectors without requiring a running Ollama instance.
//
// Run with:
//   go test -v -run=TestRetrievalQuality ./tests/
//
// Example output:
//
//   --- RETRIEVAL QUALITY REPORT ---
//   Top-1 correct: 28/30  (93%)
//   Top-3 correct: 30/30 (100%)
//   Misses (top-1):
//     q="what is a seed round?" expected=startup.md got=biology.md
//   PASS
package integration_test

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/chunker"
	"github.com/takshd15/laptop-ai/internal/extractor"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/vectordb"
)

// — TF embedder ——————————————————————————————————————————————————————————————

// tfEmbedder produces L2-normalised term-frequency vectors over a fixed vocabulary.
// Building the vocabulary from the document corpus ensures questions and documents
// share the same embedding space.
type tfEmbedder struct {
	vocab []string
	index map[string]int
}

// stopWords filters out function words that appear across all domains equally,
// which would dilute the discriminating signal from domain-specific terms.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "in": true,
	"of": true, "and": true, "or": true, "to": true, "for": true,
	"what": true, "how": true, "does": true, "did": true, "why": true,
	"with": true, "as": true, "at": true, "by": true, "from": true,
	"it": true, "its": true, "on": true, "be": true, "are": true,
	"was": true, "do": true, "can": true, "this": true, "that": true,
	"i": true, "my": true, "our": true, "we": true, "you": true,
	"has": true, "have": true, "had": true, "not": true, "no": true,
	"which": true, "when": true, "where": true, "who": true, "will": true,
}

// tokenize splits text into lowercase alphabetic tokens, removing stop words.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	var tokens []string
	var buf strings.Builder
	flush := func() {
		w := buf.String()
		buf.Reset()
		if len(w) > 2 && !stopWords[w] {
			tokens = append(tokens, w)
		}
	}
	for _, r := range lower {
		if r >= 'a' && r <= 'z' {
			buf.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// buildTFEmbedder creates a vocabulary from the given corpus texts and returns
// an embedder that projects any text into the shared TF space.
func buildTFEmbedder(corpusTexts []string) *tfEmbedder {
	index := make(map[string]int)
	var vocab []string
	for _, text := range corpusTexts {
		for _, tok := range tokenize(text) {
			if _, ok := index[tok]; !ok {
				index[tok] = len(vocab)
				vocab = append(vocab, tok)
			}
		}
	}
	return &tfEmbedder{vocab: vocab, index: index}
}

func (e *tfEmbedder) Embed(text string) ([]float32, error) {
	vec := make([]float32, len(e.vocab))
	for _, tok := range tokenize(text) {
		if i, ok := e.index[tok]; ok {
			vec[i]++
		}
	}
	// L2 normalise
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum > 0 {
		norm := float32(math.Sqrt(sum))
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec, nil
}

// — test corpus ——————————————————————————————————————————————————————————————

type document struct {
	name    string
	content string
}

var corpus = []document{
	{
		name: "biology.md",
		content: `
Basal ganglia are subcortical nuclei in the brain responsible for motor control and procedural learning.
Neurons project from the basal ganglia to the cortex and thalamus forming key motor circuits.
The striatum receives dopaminergic input from the substantia nigra pars compacta.
Parkinson disease results from degeneration of dopaminergic neurons in the substantia nigra.
Huntington disease involves progressive neuronal death in the striatum caused by abnormal protein aggregates.
Synaptic plasticity allows neurons to strengthen or weaken synaptic connections through activity-dependent changes.
Long-term potentiation in the hippocampus underlies learning and episodic memory consolidation.
The cerebellum works alongside the basal ganglia to coordinate fine motor movements and balance.
Glial cells including astrocytes and microglia support protect and nourish neurons throughout the brain.
Dopamine released by the substantia nigra modulates reward processing and voluntary motor circuits.
`,
	},
	{
		name: "algebra.md",
		content: `
Abstract algebra is the study of algebraic structures such as groups rings and fields.
A group is a set equipped with a binary operation satisfying closure associativity identity and invertibility.
The identity element of a group leaves every other element unchanged under the group operation.
An inverse element combined with any element produces the identity under the group operation.
A ring extends a group by adding a second binary operation called multiplication with its own axioms.
A field is a commutative ring in which every nonzero element has a multiplicative inverse.
Homomorphisms are structure-preserving maps between algebraic structures that respect the group operation.
The kernel of a homomorphism consists of all elements that map to the identity element.
Galois theory establishes a deep correspondence between field extensions and groups of symmetries.
The symmetric group consists of all permutations of a finite set under function composition.
Quotient groups and cosets are fundamental concepts arising from normal subgroups of a group.
`,
	},
	{
		name: "startup.md",
		content: `
Our startup is building a developer productivity tool with a freemium pricing model.
The free tier includes core features while paid subscription plans are priced at twenty-nine dollars per month.
Our seed funding goal is five hundred thousand dollars from angel investors and early-stage funds.
Customer acquisition strategy combines content marketing with developer community outreach and referrals.
Product-market fit milestone requires five hundred paying customers within the first six months of launch.
The minimum viable product MVP is scheduled for launch in the third quarter of this year.
Revenue model depends on converting free-tier users to paid subscriptions through premium feature gating.
Break-even is projected at one thousand monthly active customers based on current cost structure.
Developer advocacy and open-source contributions drive organic inbound customer growth.
Pricing tiers are designed to minimise churn and maximise lifetime customer value and expansion revenue.
Our go-to-market strategy targets inbound marketing through documentation tutorials and referral programs.
`,
	},
	{
		name: "fitness.md",
		content: `
Progressive overload is the principle of gradually increasing training resistance to stimulate muscle hypertrophy.
Hypertrophy refers to the growth and enlargement of muscle cells resulting from resistance training stimulus.
Cardio exercise including running cycling and swimming improves cardiovascular endurance and heart health.
Protein intake of approximately one gram per kilogram of bodyweight supports muscle repair after strength training.
Recovery days between training sessions are essential to prevent overtraining and allow muscle protein synthesis.
Core exercises such as planks deadlifts and squats improve functional strength stability and injury prevention.
Compound movements like bench press overhead press and rows recruit multiple muscle groups simultaneously.
Periodisation of training volume and intensity optimises long-term strength gains and avoids adaptation plateaus.
Cardiovascular fitness reduces resting heart rate and improves oxygen delivery efficiency to working muscles.
Sleep and rest are critical for recovery hormone regulation and the muscle protein synthesis process.
Nutrition including sufficient carbohydrates fat and protein fuels performance and recovery for all fitness goals.
`,
	},
}

// — labelled questions ——————————————————————————————————————————————————————

type qaCase struct {
	question string
	source   string // expected document basename
}

var testCases = []qaCase{
	// Biology — 8 questions using domain-specific vocabulary
	{"what is basal ganglia responsible for in the brain", "biology.md"},
	{"how do neurons project to the cortex and thalamus", "biology.md"},
	{"what happens to dopaminergic neurons in Parkinson disease", "biology.md"},
	{"what role does substantia nigra play in dopamine release", "biology.md"},
	{"explain synaptic plasticity in neurons", "biology.md"},
	{"how does the hippocampus support memory consolidation", "biology.md"},
	{"what causes neuronal death in the striatum in Huntington disease", "biology.md"},
	{"how does dopamine modulate motor circuits in the brain", "biology.md"},

	// Algebra — 8 questions using domain-specific vocabulary
	{"what axioms define a group in abstract algebra", "algebra.md"},
	{"explain closure and associativity in group theory", "algebra.md"},
	{"what is the identity element in a group", "algebra.md"},
	{"how does a ring extend a group with multiplication", "algebra.md"},
	{"what is a homomorphism and how does it map groups", "algebra.md"},
	{"explain Galois theory and correspondence with field extensions", "algebra.md"},
	{"what is the symmetric group and how are permutations composed", "algebra.md"},
	{"how are quotient groups and cosets constructed from subgroups", "algebra.md"},

	// Startup — 7 questions using domain-specific vocabulary
	{"what is our freemium pricing model", "startup.md"},
	{"how much seed funding are we raising", "startup.md"},
	{"when is the MVP planned to launch", "startup.md"},
	{"how do we plan to acquire customers through content marketing", "startup.md"},
	{"what subscription price do we charge per month", "startup.md"},
	{"how many paying customers do we need for product market fit", "startup.md"},
	{"what is our projected break-even for monthly active customers", "startup.md"},

	// Fitness — 7 questions using domain-specific vocabulary
	{"what is progressive overload in strength training", "fitness.md"},
	{"what is muscle hypertrophy and how does resistance training cause it", "fitness.md"},
	{"how does cardio exercise improve cardiovascular endurance", "fitness.md"},
	{"how much protein should I eat for muscle repair after training", "fitness.md"},
	{"why are recovery days essential to prevent overtraining", "fitness.md"},
	{"what core exercises improve functional strength and stability", "fitness.md"},
	{"how does sleep affect muscle protein synthesis and recovery", "fitness.md"},
}

// — test ————————————————————————————————————————————————————————————————————

// TestRetrievalQuality indexes a four-domain corpus, embeds 30 labelled
// questions, and reports top-1 and top-3 accuracy. The test fails if
// top-1 accuracy falls below 80% or top-3 below 93%.
//
// With a TF embedder and clearly distinct domain vocabulary, a well-functioning
// pipeline should achieve ≥ 90% top-1. The thresholds here are deliberately
// conservative so that minor vocabulary changes do not cause flaky failures.
func TestRetrievalQuality(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()

	// Write corpus documents.
	corpusTexts := make([]string, len(corpus))
	for i, doc := range corpus {
		corpusTexts[i] = doc.content
		mustWriteFile(t, filepath.Join(folder, doc.name), doc.content)
	}

	// Build TF embedder from corpus vocabulary.
	emb := buildTFEmbedder(corpusTexts)

	// Index + embed pipeline.
	db, err := vectordb.Open(filepath.Join(dataDir, "vectors"))
	if err != nil {
		t.Fatalf("vectordb.Open: %v", err)
	}
	defer db.Close()

	result, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("indexer.Run: %v", err)
	}
	if result.Indexed != len(corpus) {
		t.Fatalf("expected %d files indexed, got %d", len(corpus), result.Indexed)
	}

	ck := chunker.New()
	for _, rec := range result.Records {
		ext, err := extractor.ForFile(rec.Path)
		if err != nil {
			t.Fatalf("extractor.ForFile(%s): %v", rec.Path, err)
		}
		text, err := ext.Extract(rec.Path)
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		for _, ch := range ck.Chunk(text, rec.Path) {
			vec, err := emb.Embed(ch.Text)
			if err != nil {
				t.Fatalf("Embed chunk: %v", err)
			}
			if _, err := db.Insert(vec, ch.Text, map[string]string{
				"file":     rec.Path,
				"chunk_id": ch.ID,
			}); err != nil {
				t.Fatalf("db.Insert: %v", err)
			}
		}
	}

	// Evaluate each test question.
	var top1, top3 int
	type miss struct {
		q, expected, got string
	}
	var misses []miss

	for _, tc := range testCases {
		queryVec, err := emb.Embed(tc.question)
		if err != nil {
			t.Fatalf("Embed question: %v", err)
		}

		results, err := db.Search(queryVec, 3)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}

		gotTop1 := ""
		if len(results) > 0 {
			gotTop1 = filepath.Base(results[0].Record.Metadata["file"])
		}

		if gotTop1 == tc.source {
			top1++
		} else {
			misses = append(misses, miss{tc.question, tc.source, gotTop1})
		}

		for _, r := range results {
			if filepath.Base(r.Record.Metadata["file"]) == tc.source {
				top3++
				break
			}
		}
	}

	total := len(testCases)
	top1Pct := 100 * float64(top1) / float64(total)
	top3Pct := 100 * float64(top3) / float64(total)

	t.Logf("--- RETRIEVAL QUALITY REPORT ---")
	t.Logf("Top-1 correct: %d/%d  (%.0f%%)", top1, total, top1Pct)
	t.Logf("Top-3 correct: %d/%d  (%.0f%%)", top3, total, top3Pct)
	if len(misses) > 0 {
		t.Logf("Misses (top-1):")
		for _, m := range misses {
			t.Logf("  q=%q  expected=%s  got=%s", m.q, m.expected, m.got)
		}
	}

	const minTop1 = 24 // 80 % of 30
	const minTop3 = 28 // 93 % of 30

	if top1 < minTop1 {
		t.Errorf("top-1 accuracy %d/%d below threshold %d/%d — retrieval quality regression",
			top1, total, minTop1, total)
	}
	if top3 < minTop3 {
		t.Errorf("top-3 accuracy %d/%d below threshold %d/%d — retrieval quality regression",
			top3, total, minTop3, total)
	}
}
