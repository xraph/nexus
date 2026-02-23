package provider_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/provider"
)

func TestNewHealthTracker(t *testing.T) {
	ht := provider.NewHealthTracker()
	if ht == nil {
		t.Fatal("NewHealthTracker() returned nil")
	}
}

func TestIsHealthy_NoData(t *testing.T) {
	ht := provider.NewHealthTracker()

	// With no recorded data, a provider should be assumed healthy.
	if !ht.IsHealthy("unknown-provider") {
		t.Error("IsHealthy() with no data should return true (assume healthy)")
	}
}

func TestStats_NoData(t *testing.T) {
	ht := provider.NewHealthTracker()

	stats := ht.Stats("nonexistent")
	if stats == nil {
		t.Fatal("Stats() for unknown provider should return non-nil HealthStats")
	}
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", stats.TotalRequests)
	}
	if stats.Successes != 0 {
		t.Errorf("Successes = %d, want 0", stats.Successes)
	}
	if stats.Failures != 0 {
		t.Errorf("Failures = %d, want 0", stats.Failures)
	}
	if stats.SuccessRate != 0 {
		t.Errorf("SuccessRate = %f, want 0", stats.SuccessRate)
	}
}

func TestRecordSuccess_IncrementsSuccesses(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("openai", 100*time.Millisecond)
	ht.RecordSuccess("openai", 200*time.Millisecond)
	ht.RecordSuccess("openai", 150*time.Millisecond)

	stats := ht.Stats("openai")
	if stats.Successes != 3 {
		t.Errorf("Successes = %d, want 3", stats.Successes)
	}
	if stats.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", stats.TotalRequests)
	}
	if stats.Failures != 0 {
		t.Errorf("Failures = %d, want 0", stats.Failures)
	}
}

func TestRecordSuccess_TracksLatency(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("openai", 100*time.Millisecond)
	ht.RecordSuccess("openai", 200*time.Millisecond)
	ht.RecordSuccess("openai", 300*time.Millisecond)

	stats := ht.Stats("openai")

	// Average latency: (100+200+300)/3 = 200ms
	expectedAvg := 200 * time.Millisecond
	if stats.AvgLatency != expectedAvg {
		t.Errorf("AvgLatency = %v, want %v", stats.AvgLatency, expectedAvg)
	}

	// P99 with 3 entries: index = int(3*0.99) = 2, which is the third element
	if stats.P99Latency == 0 {
		t.Error("P99Latency should not be zero after recording latencies")
	}
}

func TestRecordSuccess_UpdatesLastSuccess(t *testing.T) {
	ht := provider.NewHealthTracker()

	before := time.Now()
	ht.RecordSuccess("openai", 50*time.Millisecond)
	after := time.Now()

	stats := ht.Stats("openai")
	if stats.LastSuccess.Before(before) || stats.LastSuccess.After(after) {
		t.Errorf("LastSuccess = %v, expected between %v and %v", stats.LastSuccess, before, after)
	}
}

func TestRecordFailure_IncrementsFailures(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("openai", errors.New("timeout"))
	ht.RecordFailure("openai", errors.New("rate limited"))

	stats := ht.Stats("openai")
	if stats.Failures != 2 {
		t.Errorf("Failures = %d, want 2", stats.Failures)
	}
	if stats.TotalRequests != 2 {
		t.Errorf("TotalRequests = %d, want 2", stats.TotalRequests)
	}
	if stats.Successes != 0 {
		t.Errorf("Successes = %d, want 0", stats.Successes)
	}
}

func TestRecordFailure_TracksLastError(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("openai", errors.New("first error"))
	ht.RecordFailure("openai", errors.New("second error"))

	stats := ht.Stats("openai")
	if stats.LastError != "second error" {
		t.Errorf("LastError = %q, want %q", stats.LastError, "second error")
	}
}

func TestRecordFailure_NilError(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("openai", nil)

	stats := ht.Stats("openai")
	if stats.Failures != 1 {
		t.Errorf("Failures = %d, want 1", stats.Failures)
	}
	// LastError should remain empty when nil error is passed
	if stats.LastError != "" {
		t.Errorf("LastError = %q, want empty string", stats.LastError)
	}
}

func TestRecordFailure_UpdatesLastFailure(t *testing.T) {
	ht := provider.NewHealthTracker()

	before := time.Now()
	ht.RecordFailure("openai", errors.New("fail"))
	after := time.Now()

	stats := ht.Stats("openai")
	if stats.LastFailure.Before(before) || stats.LastFailure.After(after) {
		t.Errorf("LastFailure = %v, expected between %v and %v", stats.LastFailure, before, after)
	}
}

func TestSuccessRate_AllSuccesses(t *testing.T) {
	ht := provider.NewHealthTracker()

	for i := 0; i < 10; i++ {
		ht.RecordSuccess("openai", 50*time.Millisecond)
	}

	stats := ht.Stats("openai")
	if stats.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %f, want 1.0", stats.SuccessRate)
	}
}

func TestSuccessRate_AllFailures(t *testing.T) {
	ht := provider.NewHealthTracker()

	for i := 0; i < 10; i++ {
		ht.RecordFailure("openai", errors.New("fail"))
	}

	stats := ht.Stats("openai")
	if stats.SuccessRate != 0.0 {
		t.Errorf("SuccessRate = %f, want 0.0", stats.SuccessRate)
	}
}

func TestSuccessRate_MixedResults(t *testing.T) {
	ht := provider.NewHealthTracker()

	// 7 successes, 3 failures = 70% success rate
	for i := 0; i < 7; i++ {
		ht.RecordSuccess("openai", 50*time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		ht.RecordFailure("openai", errors.New("fail"))
	}

	stats := ht.Stats("openai")
	expectedRate := 0.7
	if stats.SuccessRate != expectedRate {
		t.Errorf("SuccessRate = %f, want %f", stats.SuccessRate, expectedRate)
	}
}

func TestIsHealthy_AboveThreshold(t *testing.T) {
	ht := provider.NewHealthTracker()

	// 6 successes, 4 failures = 60% success rate > 50%
	for i := 0; i < 6; i++ {
		ht.RecordSuccess("openai", 50*time.Millisecond)
	}
	for i := 0; i < 4; i++ {
		ht.RecordFailure("openai", errors.New("fail"))
	}

	if !ht.IsHealthy("openai") {
		t.Error("IsHealthy() should return true when success rate > 50%")
	}
}

func TestIsHealthy_BelowThreshold(t *testing.T) {
	ht := provider.NewHealthTracker()

	// 2 successes, 8 failures = 20% success rate < 50%
	for i := 0; i < 2; i++ {
		ht.RecordSuccess("openai", 50*time.Millisecond)
	}
	for i := 0; i < 8; i++ {
		ht.RecordFailure("openai", errors.New("fail"))
	}

	if ht.IsHealthy("openai") {
		t.Error("IsHealthy() should return false when success rate < 50%")
	}
}

func TestIsHealthy_ExactlyAtThreshold(t *testing.T) {
	ht := provider.NewHealthTracker()

	// 5 successes, 5 failures = 50% success rate - boundary case
	// Implementation uses > 0.5, so exactly 50% is NOT healthy
	for i := 0; i < 5; i++ {
		ht.RecordSuccess("openai", 50*time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		ht.RecordFailure("openai", errors.New("fail"))
	}

	if ht.IsHealthy("openai") {
		t.Error("IsHealthy() should return false when success rate = 50% (threshold is > 50%)")
	}
}

func TestIsHealthy_AllSuccesses(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("openai", 50*time.Millisecond)

	if !ht.IsHealthy("openai") {
		t.Error("IsHealthy() should return true when all requests succeed")
	}
}

func TestIsHealthy_AllFailures(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("openai", errors.New("fail"))

	if ht.IsHealthy("openai") {
		t.Error("IsHealthy() should return false when all requests fail")
	}
}

func TestAllStats_Empty(t *testing.T) {
	ht := provider.NewHealthTracker()

	all := ht.AllStats()
	if all == nil {
		t.Fatal("AllStats() returned nil")
	}
	if len(all) != 0 {
		t.Errorf("AllStats() on new tracker returned %d entries, want 0", len(all))
	}
}

func TestAllStats_MultipleProviders(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("openai", 100*time.Millisecond)
	ht.RecordSuccess("openai", 200*time.Millisecond)
	ht.RecordFailure("anthropic", errors.New("timeout"))
	ht.RecordSuccess("anthropic", 150*time.Millisecond)
	ht.RecordSuccess("ollama", 50*time.Millisecond)

	all := ht.AllStats()
	if len(all) != 3 {
		t.Fatalf("AllStats() returned %d entries, want 3", len(all))
	}

	// Verify openai stats
	openaiStats, ok := all["openai"]
	if !ok {
		t.Fatal("AllStats() missing openai")
	}
	if openaiStats.Successes != 2 {
		t.Errorf("openai Successes = %d, want 2", openaiStats.Successes)
	}
	if openaiStats.TotalRequests != 2 {
		t.Errorf("openai TotalRequests = %d, want 2", openaiStats.TotalRequests)
	}

	// Verify anthropic stats
	anthropicStats, ok := all["anthropic"]
	if !ok {
		t.Fatal("AllStats() missing anthropic")
	}
	if anthropicStats.Successes != 1 {
		t.Errorf("anthropic Successes = %d, want 1", anthropicStats.Successes)
	}
	if anthropicStats.Failures != 1 {
		t.Errorf("anthropic Failures = %d, want 1", anthropicStats.Failures)
	}

	// Verify ollama stats
	ollamaStats, ok := all["ollama"]
	if !ok {
		t.Fatal("AllStats() missing ollama")
	}
	if ollamaStats.TotalRequests != 1 {
		t.Errorf("ollama TotalRequests = %d, want 1", ollamaStats.TotalRequests)
	}
}

func TestStats_IndependentProviders(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("openai", 100*time.Millisecond)
	ht.RecordFailure("anthropic", errors.New("fail"))

	openaiStats := ht.Stats("openai")
	anthropicStats := ht.Stats("anthropic")

	if openaiStats.Successes != 1 || openaiStats.Failures != 0 {
		t.Error("openai stats should only reflect openai calls")
	}
	if anthropicStats.Successes != 0 || anthropicStats.Failures != 1 {
		t.Error("anthropic stats should only reflect anthropic calls")
	}
}

func TestStats_LatencyAverage(t *testing.T) {
	ht := provider.NewHealthTracker()

	latencies := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
	}
	for _, l := range latencies {
		ht.RecordSuccess("test", l)
	}

	stats := ht.Stats("test")
	// Average: (10+20+30+40)/4 = 25ms
	expectedAvg := 25 * time.Millisecond
	if stats.AvgLatency != expectedAvg {
		t.Errorf("AvgLatency = %v, want %v", stats.AvgLatency, expectedAvg)
	}
}

func TestStats_SingleLatency(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("test", 42*time.Millisecond)

	stats := ht.Stats("test")
	if stats.AvgLatency != 42*time.Millisecond {
		t.Errorf("AvgLatency = %v, want %v", stats.AvgLatency, 42*time.Millisecond)
	}
}

func TestRecordSuccess_And_Failure_Mixed(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("test", 100*time.Millisecond)
	ht.RecordFailure("test", errors.New("oops"))
	ht.RecordSuccess("test", 200*time.Millisecond)

	stats := ht.Stats("test")
	if stats.Successes != 2 {
		t.Errorf("Successes = %d, want 2", stats.Successes)
	}
	if stats.Failures != 1 {
		t.Errorf("Failures = %d, want 1", stats.Failures)
	}
	if stats.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", stats.TotalRequests)
	}

	// Latency should only account for successes (where latency was recorded)
	expectedAvg := 150 * time.Millisecond // (100+200)/2
	if stats.AvgLatency != expectedAvg {
		t.Errorf("AvgLatency = %v, want %v", stats.AvgLatency, expectedAvg)
	}
}

func TestRecordFailure_OverwritesLastError(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("test", errors.New("error-1"))
	stats1 := ht.Stats("test")
	if stats1.LastError != "error-1" {
		t.Errorf("LastError = %q, want %q", stats1.LastError, "error-1")
	}

	ht.RecordFailure("test", errors.New("error-2"))
	stats2 := ht.Stats("test")
	if stats2.LastError != "error-2" {
		t.Errorf("LastError = %q, want %q", stats2.LastError, "error-2")
	}
}

func TestAllStats_ReturnsCopy(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordSuccess("test", 100*time.Millisecond)

	all1 := ht.AllStats()

	// Record more data after getting stats
	ht.RecordSuccess("test", 200*time.Millisecond)

	all2 := ht.AllStats()

	// The first snapshot should not be affected by later recordings
	if all1["test"].TotalRequests != 1 {
		t.Errorf("first AllStats snapshot changed: TotalRequests = %d, want 1", all1["test"].TotalRequests)
	}
	if all2["test"].TotalRequests != 2 {
		t.Errorf("second AllStats should show 2, got %d", all2["test"].TotalRequests)
	}
}

func TestStats_ZeroLatencyOnNoData(t *testing.T) {
	ht := provider.NewHealthTracker()

	stats := ht.Stats("empty")
	if stats.AvgLatency != 0 {
		t.Errorf("AvgLatency = %v, want 0", stats.AvgLatency)
	}
	if stats.P99Latency != 0 {
		t.Errorf("P99Latency = %v, want 0", stats.P99Latency)
	}
}

func TestStats_FailuresOnlyNoLatency(t *testing.T) {
	ht := provider.NewHealthTracker()

	ht.RecordFailure("test", errors.New("fail"))
	ht.RecordFailure("test", errors.New("fail again"))

	stats := ht.Stats("test")
	if stats.AvgLatency != 0 {
		t.Errorf("AvgLatency = %v, want 0 (failures don't record latency)", stats.AvgLatency)
	}
	if stats.P99Latency != 0 {
		t.Errorf("P99Latency = %v, want 0", stats.P99Latency)
	}
}
