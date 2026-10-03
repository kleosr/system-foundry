package search

import (
	"context"
	"fmt"
	zipfrand "math/rand"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"systemsfoundry/platform"
)

const benchVocabulary = 20000

type corpus struct {
	zipf *zipfrand.Zipf
}

func newCorpus(seed int64) corpus {
	source := zipfrand.New(zipfrand.NewSource(seed))
	return corpus{zipf: zipfrand.NewZipf(source, 1.1, 1, benchVocabulary-1)}
}

func (c corpus) text(words int) string {
	picked := make([]string, words)
	for i := range picked {
		picked[i] = "w" + strconv.FormatUint(c.zipf.Uint64(), 10)
	}
	return strings.Join(picked, " ")
}

func midFrequencyQuery(rng *rand.Rand) string {
	return "w" + strconv.Itoa(50+rng.IntN(2000)) + " w" + strconv.Itoa(50+rng.IntN(2000))
}

func TestPostgresHybridLatencyReport(t *testing.T) {
	docs, _ := strconv.Atoi(os.Getenv("FOUNDRY_BENCH_DOCS"))
	if docs < 1 {
		t.Skip("FOUNDRY_BENCH_DOCS is not set")
	}
	repo, _ := postgresRepo(t)
	actor := owner(freshTenant(t, "bench"))
	ctx := context.Background()
	start := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			words := newCorpus(seed)
			for id := range jobs {
				title, body := words.text(4), words.text(60)
				if err := repo.Save(ctx, actor, Analyze("d"+strconv.Itoa(id), title, body), platform.HashEmbed(title+" "+body)); err != nil {
					t.Error(err)
					return
				}
			}
		}(int64(worker))
	}
	for id := 0; id < docs; id++ {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	seeded := time.Since(start)
	rng := rand.New(rand.NewPCG(99, 1))
	latencies := make([]time.Duration, 0, 200)
	for i := 0; i < 200; i++ {
		query := midFrequencyQuery(rng)
		began := time.Now()
		if _, _, err := repo.Rank(ctx, actor, Tokens(query), platform.HashEmbed(query), rankDepth); err != nil {
			t.Fatal(err)
		}
		latencies = append(latencies, time.Since(began))
	}
	slices.Sort(latencies)
	report := fmt.Sprintf("docs=%d seed=%s p50=%s p95=%s p99=%s max=%s", docs, seeded.Round(time.Millisecond),
		latencies[100].Round(10*time.Microsecond), latencies[190].Round(10*time.Microsecond),
		latencies[198].Round(10*time.Microsecond), latencies[199].Round(10*time.Microsecond))
	t.Log(report)
}
