package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// paramFiles / paramStem / enclaveName.
// ---------------------------------------------------------------------------

func TestParamFiles(t *testing.T) {
	dir := t.TempDir()
	names := []string{"b.yaml", "a.yaml", "c.yaml"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x: 1\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	// non-yaml file: must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	// subdir (even one ending in .yaml) must be ignored / non-recursive.
	if err := os.Mkdir(filepath.Join(dir, "sub.yaml"), 0o755); err != nil {
		t.Fatalf("mkdir sub.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub.yaml", "nested.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatalf("write nested.yaml: %v", err)
	}

	got, err := paramFiles(dir)
	if err != nil {
		t.Fatalf("paramFiles error: %v", err)
	}
	wantAbs := func(n string) string {
		abs, err := filepath.Abs(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("filepath.Abs: %v", err)
		}
		return abs
	}
	want := []string{wantAbs("a.yaml"), wantAbs("b.yaml"), wantAbs("c.yaml")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paramFiles = %v, want %v", got, want)
	}
}

func TestParamFilesMissingDir(t *testing.T) {
	if _, err := paramFiles(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestParamStem(t *testing.T) {
	cases := map[string]string{
		"/a/b/lighthouse-teku.yaml": "lighthouse-teku",
		"grandine-vouch.yaml":       "grandine-vouch",
	}
	for in, want := range cases {
		if got := paramStem(in); got != want {
			t.Errorf("paramStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnclaveName(t *testing.T) {
	if got := enclaveName(3, "teku-prysm"); got != "c3-teku-prysm" {
		t.Errorf("enclaveName(3, teku-prysm) = %q, want c3-teku-prysm", got)
	}
	// Sanitization: uppercase and disallowed characters (e.g. from an
	// unexpectedly-named param file) must not leak into the enclave name.
	if got := enclaveName(0, "Teku_Prysm!"); got != "c0-teku-prysm-" {
		t.Errorf("enclaveName sanitization = %q, want c0-teku-prysm-", got)
	}
}

// ---------------------------------------------------------------------------
// Config loading.
// ---------------------------------------------------------------------------

func TestStateRoundTripAndAdvance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// missing file -> zero state, nil error
	s, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState(missing) error: %v", err)
	}
	if s != (state{}) {
		t.Errorf("loadState(missing) = %+v, want zero state", s)
	}

	// save/load round trip
	s2 := state{Cycle: 2, NextIndex: 35, CurrentEnclave: "c2-grandine-vouch"}
	if err := s2.save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}
	// no tmp file left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
	loaded, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState error: %v", err)
	}
	if loaded != s2 {
		t.Errorf("loadState() = %+v, want %+v", loaded, s2)
	}

	// advance: plain increment, no wrap (wrap is mainLoop's job now).
	adv := state{Cycle: 0, NextIndex: 34}
	adv.advance()
	if adv.Cycle != 0 || adv.NextIndex != 35 {
		t.Errorf("advance from 34: got (cycle=%d, idx=%d), want (0, 35)", adv.Cycle, adv.NextIndex)
	}
	adv.advance()
	if adv.Cycle != 0 || adv.NextIndex != 36 {
		t.Errorf("advance from 35: got (cycle=%d, idx=%d), want (0, 36) -- advance itself must not wrap", adv.Cycle, adv.NextIndex)
	}
}

// TestLoadStateRejectsNegativeValues guards against a corrupt/hand-edited/
// forward-incompatible state file crash-looping the process: a negative
// next_index or cycle must not propagate downstream -- loadState must
// discard it and hand back a clean zero state instead. There's no fixed
// upper bound anymore (mainLoop clamps next_index against the dynamic file
// count instead), so only negative values are rejected here.
func TestLoadStateRejectsNegativeValues(t *testing.T) {
	dir := t.TempDir()

	t.Run("next_index negative", func(t *testing.T) {
		path := filepath.Join(dir, "negative.json")
		if err := os.WriteFile(path, []byte(`{"cycle":0,"next_index":-1,"current_enclave":""}`), 0o644); err != nil {
			t.Fatalf("write state file: %v", err)
		}
		s, err := loadState(path)
		if err != nil {
			t.Fatalf("loadState error: %v", err)
		}
		if s != (state{}) {
			t.Errorf("loadState(next_index=-1) = %+v, want zero state", s)
		}
	})

	t.Run("cycle negative", func(t *testing.T) {
		path := filepath.Join(dir, "negative-cycle.json")
		if err := os.WriteFile(path, []byte(`{"cycle":-1,"next_index":0,"current_enclave":""}`), 0o644); err != nil {
			t.Fatalf("write state file: %v", err)
		}
		s, err := loadState(path)
		if err != nil {
			t.Fatalf("loadState error: %v", err)
		}
		if s != (state{}) {
			t.Errorf("loadState(cycle=-1) = %+v, want zero state", s)
		}
	})

	t.Run("large next_index is no longer rejected (dynamic file count)", func(t *testing.T) {
		// Unlike the old fixed-36-combo cycle, an out-of-range-looking
		// next_index isn't inherently invalid anymore -- mainLoop clamps it
		// against the current file count on each iteration. loadState must
		// pass it through unchanged as long as it's non-negative.
		path := filepath.Join(dir, "large.json")
		if err := os.WriteFile(path, []byte(`{"cycle":0,"next_index":9999,"current_enclave":""}`), 0o644); err != nil {
			t.Fatalf("write state file: %v", err)
		}
		s, err := loadState(path)
		if err != nil {
			t.Fatalf("loadState error: %v", err)
		}
		if s.NextIndex != 9999 {
			t.Errorf("loadState(next_index=9999).NextIndex = %d, want 9999 (unchanged)", s.NextIndex)
		}
	})
}

func TestComputeBackoff(t *testing.T) {
	cases := []struct {
		failures, base, cap, want int
	}{
		{0, 30, 900, 30},
		{1, 30, 900, 60},
		{2, 30, 900, 120},
		{20, 30, 900, 900},
		{-5, 30, 900, 30}, // negative consecutiveFailures treated as 0
		// Large/overflow-prone consecutiveFailures values (reachable across a
		// sustained multi-hour outage, since mainLoop never caps the
		// counter): "1 << uint(n)" would overflow int for n around the word
		// size, and base*that could wrap to <=0. Every one of these must
		// still land exactly at cap, never <=0 and never >cap.
		{59, 30, 900, 900},
		{1000, 30, 900, 900},
		{1 << 20, 30, 900, 900},
	}
	for _, c := range cases {
		got := computeBackoff(c.failures, c.base, c.cap)
		if got != c.want {
			t.Errorf("computeBackoff(%d,%d,%d) = %d, want %d", c.failures, c.base, c.cap, got, c.want)
		}
		if got <= 0 || got > c.cap {
			t.Errorf("computeBackoff(%d,%d,%d) = %d, out of range (0,%d]", c.failures, c.base, c.cap, got, c.cap)
		}
	}
}

func TestPromQLBuilders(t *testing.T) {
	q := promDutySuccess("kurtosis-teku-prysm", 4500)
	if !strings.Contains(q, "core_tracker_success_duties_total") {
		t.Errorf("promDutySuccess missing metric name: %s", q)
	}
	if !strings.Contains(q, `cluster_name="kurtosis-teku-prysm"`) {
		t.Errorf("promDutySuccess missing cluster_name selector: %s", q)
	}
	if !strings.Contains(q, "[4500s]") {
		t.Errorf("promDutySuccess missing window: %s", q)
	}
	if !strings.Contains(q, "by (duty, cluster_peer)") {
		t.Errorf("promDutySuccess missing group-by: %s", q)
	}

	if !strings.Contains(promDutyExpected("kurtosis-a-b", 60), "core_tracker_expect_duties_total") {
		t.Errorf("promDutyExpected missing metric name")
	}
	if !strings.Contains(promDVMemPeak("kurtosis-a-b", 5400), "process_resident_memory_bytes") {
		t.Errorf("promDVMemPeak missing metric name")
	}
	if !strings.Contains(promDVCPUPeak("kurtosis-a-b", 5400), "process_cpu_seconds_total") {
		t.Errorf("promDVCPUPeak missing metric name")
	}
	if !strings.Contains(promHealthFired("kurtosis-a-b", 5400), "max_over_time(app_health_checks") {
		t.Errorf("promHealthFired missing metric name")
	}
	if !strings.HasSuffix(strings.TrimSpace(promHealthFiringNow("kurtosis-a-b")), "== 1") {
		t.Errorf("promHealthFiringNow does not end with == 1")
	}
}

func s(labels map[string]string, value float64) sample {
	return sample{labels: labels, value: value}
}

func TestSelectWorstNode(t *testing.T) {
	t.Run("worst is min total success", func(t *testing.T) {
		expected := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 100),
			s(map[string]string{"duty": "aggregator", "cluster_peer": "0"}, 20),
			s(map[string]string{"duty": "aggregator", "cluster_peer": "1"}, 20),
		}
		success := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 90),
			s(map[string]string{"duty": "aggregator", "cluster_peer": "0"}, 20),
			s(map[string]string{"duty": "aggregator", "cluster_peer": "1"}, 15),
		}
		wn, ok := selectWorstNode(expected, success)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if wn.peer != "1" {
			t.Errorf("peer = %q, want 1", wn.peer)
		}
		byDuty := map[string]dutyResult{}
		for _, d := range wn.duties {
			byDuty[d.duty] = d
		}
		agg := byDuty["aggregator"]
		if agg.expected != 20 || agg.success != 15 {
			t.Errorf("aggregator = %+v, want expected=20 success=15", agg)
		}
		if pct := agg.pct(); pct < 74.99 || pct > 75.01 {
			t.Errorf("aggregator.pct() = %v, want ~75.0", pct)
		}
	})

	t.Run("missing success duty defaults to zero", func(t *testing.T) {
		expected := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 100),
			s(map[string]string{"duty": "proposer", "cluster_peer": "1"}, 5),
		}
		success := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 90),
		}
		wn, ok := selectWorstNode(expected, success)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if wn.peer != "1" {
			t.Errorf("peer = %q, want 1", wn.peer)
		}
		byDuty := map[string]dutyResult{}
		for _, d := range wn.duties {
			byDuty[d.duty] = d
		}
		prop := byDuty["proposer"]
		if prop.expected != 5 || prop.success != 0 {
			t.Errorf("proposer = %+v, want expected=5 success=0", prop)
		}
		if prop.pct() != 0 {
			t.Errorf("proposer.pct() = %v, want 0", prop.pct())
		}
	})

	t.Run("zero-expected (0/0) duties are dropped, genuine misses kept", func(t *testing.T) {
		expected := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "proposer", "cluster_peer": "0"}, 5), // genuine miss below
			s(map[string]string{"duty": "exit", "cluster_peer": "0"}, 0),     // idle 0/0
			s(map[string]string{"duty": "info_sync", "cluster_peer": "0"}, 0),
		}
		success := []sample{
			s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
			s(map[string]string{"duty": "exit", "cluster_peer": "0"}, 0),
			s(map[string]string{"duty": "info_sync", "cluster_peer": "0"}, 0),
		}
		wn, ok := selectWorstNode(expected, success)
		if !ok {
			t.Fatal("expected ok=true")
		}
		names := map[string]bool{}
		for _, d := range wn.duties {
			names[d.duty] = true
		}
		if !names["attester"] || !names["proposer"] {
			t.Errorf("expected>0 duties must be kept, got %+v", wn.duties)
		}
		if names["exit"] || names["info_sync"] {
			t.Errorf("0/0 duties must be dropped, got %+v", wn.duties)
		}
	})

	t.Run("empty samples -> ok false", func(t *testing.T) {
		_, ok := selectWorstNode(nil, nil)
		if ok {
			t.Error("expected ok=false for empty input")
		}
	})
}

func TestMaxValue(t *testing.T) {
	v, ok := maxValue([]sample{
		s(map[string]string{"cluster_peer": "0"}, 3.0),
		s(map[string]string{"cluster_peer": "1"}, 7.5),
	})
	if !ok || v != 7.5 {
		t.Errorf("maxValue = (%v,%v), want (7.5,true)", v, ok)
	}
	_, ok = maxValue(nil)
	if ok {
		t.Error("maxValue(empty) ok = true, want false")
	}
}

func TestParseHealth(t *testing.T) {
	fired := []sample{
		s(map[string]string{"name": "high-mem", "severity": "warning"}, 1),
		s(map[string]string{"name": "peer-count", "severity": "error"}, 1),
	}
	now := []sample{
		s(map[string]string{"name": "peer-count", "severity": "error"}, 1),
	}
	checks := parseHealth(fired, now)
	if len(checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(checks))
	}
	byKey := map[[2]string]healthCheck{}
	for _, c := range checks {
		byKey[[2]string{c.name, c.severity}] = c
	}
	if byKey[[2]string{"high-mem", "warning"}].firingNow {
		t.Error("high-mem/warning should not be firing now")
	}
	if !byKey[[2]string{"peer-count", "error"}].firingNow {
		t.Error("peer-count/error should be firing now")
	}
}

func TestParseCPULineAndPercent(t *testing.T) {
	busy, total := parseCPULine("cpu  100 0 100 800 0 0 0 0 0 0\n")
	if busy != 200 || total != 1000 {
		t.Errorf("parseCPULine = (%v,%v), want (200,1000)", busy, total)
	}
	pct := cpuPercent([2]float64{300, 1000}, [2]float64{450, 1200})
	if pct != 75.0 {
		t.Errorf("cpuPercent = %v, want 75.0", pct)
	}
}

func TestParseMeminfo(t *testing.T) {
	used, total := parseMeminfo("MemTotal: 1000 kB\nMemAvailable: 250 kB\n")
	if total != 1000*1024 {
		t.Errorf("total = %v, want %v", total, 1000*1024)
	}
	if used != 750*1024 {
		t.Errorf("used = %v, want %v", used, 750*1024)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "# a comment\n\nexport DOTENV_TEST_A=hello\nDOTENV_TEST_B=\"quoted val\"\nDOTENV_TEST_C='x'\nno_equals_line\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// A is already set -> the file must NOT override it.
	t.Setenv("DOTENV_TEST_A", "preset")
	os.Unsetenv("DOTENV_TEST_B")
	os.Unsetenv("DOTENV_TEST_C")
	defer os.Unsetenv("DOTENV_TEST_B")
	defer os.Unsetenv("DOTENV_TEST_C")

	if err := loadDotEnv(p); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_A"); got != "preset" {
		t.Errorf("A = %q, want preset (pre-set env must win over .env)", got)
	}
	if got := os.Getenv("DOTENV_TEST_B"); got != "quoted val" {
		t.Errorf("B = %q, want %q (double-quotes stripped)", got, "quoted val")
	}
	if got := os.Getenv("DOTENV_TEST_C"); got != "x" {
		t.Errorf("C = %q, want x (single-quotes stripped)", got)
	}
	// A missing file is a no-op, not an error.
	if err := loadDotEnv(filepath.Join(dir, "does-not-exist.env")); err != nil {
		t.Errorf("missing file should be a no-op, got %v", err)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("required present -> defaults applied, paramsDir derived from repoPath", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "http://hook")
		t.Setenv("CYCLER_REPO_PATH", "/srv/kurtosis-charon")
		t.Setenv("CYCLER_STATE_PATH", "/var/lib/cycler/state.json")
		t.Setenv("CYCLER_PARAMS_DIR", "")
		t.Setenv("CYCLER_MONITORING_TOKEN", "")
		t.Setenv("CYCLER_PACKAGE_REF", "")
		t.Setenv("CYCLER_RUN_MINUTES", "")
		t.Setenv("CYCLER_STARTUP_DEADLINE_MINUTES", "")
		t.Setenv("CYCLER_SAMPLE_INTERVAL_S", "")
		t.Setenv("CYCLER_INTER_RUN_BACKOFF_S", "")
		t.Setenv("CYCLER_MAX_BACKOFF_S", "")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.slackWebhookURL != "http://hook" {
			t.Errorf("slackWebhookURL = %q", cfg.slackWebhookURL)
		}
		if cfg.repoPath != "/srv/kurtosis-charon" {
			t.Errorf("repoPath = %q", cfg.repoPath)
		}
		if cfg.statePath != "/var/lib/cycler/state.json" {
			t.Errorf("statePath = %q", cfg.statePath)
		}
		wantParamsDir := filepath.Join("/srv/kurtosis-charon", "dv-cycler", "network-params")
		if cfg.paramsDir != wantParamsDir {
			t.Errorf("paramsDir = %q, want %q", cfg.paramsDir, wantParamsDir)
		}
		if cfg.runMinutes != 90 {
			t.Errorf("runMinutes = %d, want 90", cfg.runMinutes)
		}
		if !strings.HasSuffix(cfg.packageRef, "ethereum-package@charon") {
			t.Errorf("packageRef = %q", cfg.packageRef)
		}
		if cfg.startupDeadlineMinutes != 25 || cfg.sampleIntervalS != 15 {
			t.Errorf("startupDeadlineMinutes/sampleIntervalS = %d/%d, want 25/15", cfg.startupDeadlineMinutes, cfg.sampleIntervalS)
		}
		if cfg.interRunBackoffS != 30 || cfg.maxBackoffS != 900 {
			t.Errorf("interRunBackoffS/maxBackoffS = %d/%d, want 30/900", cfg.interRunBackoffS, cfg.maxBackoffS)
		}
	})

	t.Run("logDir default + slack bot token/channel from env", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "s")
		t.Setenv("CYCLER_LOG_DIR", "")
		t.Setenv("CYCLER_SLACK_BOT_TOKEN", "xoxb-abc")
		t.Setenv("CYCLER_SLACK_CHANNEL_ID", "C123")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if !strings.HasSuffix(cfg.logDir, "dv-cycler-logs") {
			t.Errorf("logDir default = %q, want suffix dv-cycler-logs", cfg.logDir)
		}
		if cfg.slackBotToken != "xoxb-abc" || cfg.slackChannelID != "C123" {
			t.Errorf("bot token/channel = %q/%q", cfg.slackBotToken, cfg.slackChannelID)
		}
	})

	t.Run("resultsPath default (next to state) + summary mention", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "/var/lib/cyc/state.json")
		t.Setenv("CYCLER_RESULTS_PATH", "")
		t.Setenv("CYCLER_SUMMARY_MENTION", "<!subteam^S9>")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		want := filepath.Join("/var/lib/cyc", "cycler-results.json")
		if cfg.resultsPath != want {
			t.Errorf("resultsPath = %q, want %q", cfg.resultsPath, want)
		}
		if cfg.summaryMention != "<!subteam^S9>" {
			t.Errorf("summaryMention = %q", cfg.summaryMention)
		}
	})

	t.Run("CYCLER_LOG_DIR override wins over the default", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "s")
		t.Setenv("CYCLER_LOG_DIR", "/var/log/dvc")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.logDir != "/var/log/dvc" {
			t.Errorf("logDir = %q, want /var/log/dvc", cfg.logDir)
		}
	})

	t.Run("CYCLER_PARAMS_DIR override wins over the derived default", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("CYCLER_REPO_PATH", "/srv/kurtosis-charon")
		t.Setenv("CYCLER_STATE_PATH", "st")
		t.Setenv("CYCLER_PARAMS_DIR", "/custom/params")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.paramsDir != "/custom/params" {
			t.Errorf("paramsDir = %q, want /custom/params", cfg.paramsDir)
		}
	})

	t.Run("overrides applied", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "st")
		t.Setenv("CYCLER_RUN_MINUTES", "30")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.runMinutes != 30 {
			t.Errorf("runMinutes = %d, want 30", cfg.runMinutes)
		}
	})

	t.Run("missing required raises", func(t *testing.T) {
		t.Setenv("CYCLER_SLACK_WEBHOOK_URL", "")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "s")

		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error when slackWebhookURL missing")
		}
	})

	t.Run("bare env name is not picked up (CYCLER_-only)", func(t *testing.T) {
		// SLACK_WEBHOOK_URL (bare, no CYCLER_ prefix) must NOT satisfy the
		// required slack_webhook_url config -- only CYCLER_SLACK_WEBHOOK_URL
		// counts. Setting the bare name alongside the other two required
		// CYCLER_ vars should still error as missing.
		t.Setenv("SLACK_WEBHOOK_URL", "http://hook")
		t.Setenv("CYCLER_REPO_PATH", "r")
		t.Setenv("CYCLER_STATE_PATH", "s")

		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error: bare SLACK_WEBHOOK_URL (unprefixed) must not be read")
		}
		if !strings.Contains(err.Error(), "CYCLER_SLACK_WEBHOOK_URL") {
			t.Errorf("error = %v, want it to name CYCLER_SLACK_WEBHOOK_URL", err)
		}
	})
}

func TestBuildBlocksStatuses(t *testing.T) {
	base := func(status string) reportData {
		mem := 512.0 * 1024 * 1024
		cpu := 1.4
		return reportData{
			name:        "teku-prysm",
			clusterName: "kurtosis-teku-prysm",
			cycle:       3,
			status:      status,
			window:      "12:00-13:30 UTC",
			worst: &worstNode{peer: "1", duties: []dutyResult{
				{duty: "attester", expected: 780, success: 780},
				{duty: "aggregator", expected: 150, success: 130},
			}},
			dvMemBytes: &mem,
			dvCPU:      &cpu,
			host:       &hostStats{cpuAvg: 30, cpuPeak: 82, memAvg: 8e9, memPeak: 9e9, memTotal: 16e9},
			health:     []healthCheck{{name: "high-inclusion-delay", severity: "warning", firingNow: false}},
		}
	}

	t.Run("ok status renders duty ratios, worst peer, and cluster name", func(t *testing.T) {
		blocks := buildBlocks(base("ok"))
		dump := dumpBlocks(blocks)
		if !strings.Contains(dump, "780/780") {
			t.Errorf("missing 780/780 in %s", dump)
		}
		if !strings.Contains(dump, "130/150") {
			t.Errorf("missing 130/150 in %s", dump)
		}
		if !strings.Contains(dump, "86.6") {
			t.Errorf("missing 86.6x pct in %s", dump)
		}
		if !strings.Contains(strings.ToLower(dump), "node 1") {
			t.Errorf("missing worst node reference in %s", dump)
		}
		if strings.Contains(dump, "high-inclusion-delay") || strings.Contains(strings.ToLower(dump), "health check") {
			t.Errorf("health section should be omitted while reportHealthChecks is off: %s", dump)
		}
		if !strings.Contains(dump, "kurtosis-teku-prysm") {
			t.Errorf("missing discovered cluster name in %s", dump)
		}
		text := buildText(base("ok"))
		if !strings.Contains(text, "teku-prysm") || !strings.Contains(strings.ToLower(text), "cycle 3") {
			t.Errorf("buildText = %q", text)
		}
	})

	t.Run("health section renders when reportHealthChecks enabled", func(t *testing.T) {
		old := reportHealthChecks
		reportHealthChecks = true
		defer func() { reportHealthChecks = old }()
		dump := dumpBlocks(buildBlocks(base("ok")))
		if !strings.Contains(dump, "high-inclusion-delay") {
			t.Errorf("health check name missing when reportHealthChecks enabled: %s", dump)
		}
	})

	t.Run("degraded status", func(t *testing.T) {
		d := base("degraded")
		text := buildText(d)
		if !strings.Contains(text, "degraded") {
			t.Errorf("buildText missing degraded: %q", text)
		}
	})

	t.Run("failed status shows error and nil optionals don't panic", func(t *testing.T) {
		d := reportData{
			name:   "teku-prysm",
			cycle:  1,
			status: "failed",
			window: "-",
			errMsg: "launch failed: boom",
		}
		blocks := buildBlocks(d)
		dump := dumpBlocks(blocks)
		if !strings.Contains(strings.ToLower(dump), "failed") {
			t.Errorf("missing failed in %s", dump)
		}
		if !strings.Contains(dump, "boom") {
			t.Errorf("missing error message in %s", dump)
		}
		if !strings.Contains(dump, "n/a") {
			t.Errorf("missing n/a fallback for nil optionals in %s", dump)
		}
		if !strings.Contains(dump, "_no duty data_") {
			t.Errorf("missing no-duty-data fallback in %s", dump)
		}
	})
}

// dumpBlocks renders build blocks into a single string for substring assertions.
func dumpBlocks(blocks []map[string]any) string {
	b, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Sprint(blocks)
	}
	return string(b)
}

func TestPromQueryParsesAndErrors(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	httpGet = func(string) ([]byte, int, error) {
		return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"42.5"]}]}}`), 200, nil
	}
	samples, err := promQuery("http://x", "up")
	if err != nil {
		t.Fatalf("promQuery error: %v", err)
	}
	if len(samples) != 1 || samples[0].value != 42.5 {
		t.Fatalf("samples = %+v, want one sample value 42.5", samples)
	}
	if samples[0].labels["duty"] != "attester" || samples[0].labels["cluster_peer"] != "0" {
		t.Errorf("labels = %+v", samples[0].labels)
	}

	httpGet = func(string) ([]byte, int, error) {
		return []byte(`{"status":"error","errorType":"bad_data"}`), 200, nil
	}
	if _, err := promQuery("http://x", "up"); err == nil {
		t.Fatal("expected error when status != success")
	} else if !strings.Contains(err.Error(), "bad_data") {
		t.Errorf("error = %v, want mention of errorType bad_data", err)
	}
}

// ---------------------------------------------------------------------------
// discoverClusterName.
// ---------------------------------------------------------------------------

func TestDiscoverClusterName(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	t.Run("exactly one cluster_name -> returned", func(t *testing.T) {
		httpGet = func(string) ([]byte, int, error) {
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_name":"kurtosis-teku-prysm"},"value":[1,"1"]}]}}`), 200, nil
		}
		got, err := discoverClusterName("http://x")
		if err != nil {
			t.Fatalf("discoverClusterName error: %v", err)
		}
		if got != "kurtosis-teku-prysm" {
			t.Errorf("discoverClusterName = %q, want kurtosis-teku-prysm", got)
		}
	})

	t.Run("zero cluster_name values -> error", func(t *testing.T) {
		httpGet = func(string) ([]byte, int, error) {
			return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
		}
		if _, err := discoverClusterName("http://x"); err == nil {
			t.Fatal("expected error for zero cluster_name values")
		}
	})

	t.Run("two distinct cluster_name values -> error", func(t *testing.T) {
		httpGet = func(string) ([]byte, int, error) {
			return []byte(`{"status":"success","data":{"result":[
				{"metric":{"cluster_name":"kurtosis-a"},"value":[1,"1"]},
				{"metric":{"cluster_name":"kurtosis-b"},"value":[1,"1"]}
			]}}`), 200, nil
		}
		if _, err := discoverClusterName("http://x"); err == nil {
			t.Fatal("expected error for two distinct cluster_name values")
		}
	})

	t.Run("promQuery error propagates", func(t *testing.T) {
		httpGet = func(string) ([]byte, int, error) {
			return []byte(`{"status":"error","errorType":"timeout"}`), 200, nil
		}
		if _, err := discoverClusterName("http://x"); err == nil {
			t.Fatal("expected error propagated from promQuery")
		}
	})
}

func TestSlackPostPayloadAndNon200(t *testing.T) {
	old := httpPost
	defer func() { httpPost = old }()

	var capturedURL string
	var capturedBody []byte
	httpPost = func(u string, body []byte) (int, error) {
		capturedURL, capturedBody = u, body
		return 200, nil
	}
	if err := slackPost("http://hook", "hello", []map[string]any{{"type": "section"}}); err != nil {
		t.Fatalf("slackPost error: %v", err)
	}
	if capturedURL != "http://hook" {
		t.Errorf("posted url = %q, want http://hook", capturedURL)
	}
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal posted body: %v", err)
	}
	if payload["text"] != "hello" {
		t.Errorf("payload text = %v, want hello", payload["text"])
	}
	if _, ok := payload["blocks"]; !ok {
		t.Errorf("payload missing blocks key: %+v", payload)
	}

	httpPost = func(string, []byte) (int, error) { return 500, nil }
	if err := slackPost("http://hook", "hi", nil); err == nil {
		t.Error("expected error on non-200 status")
	}
}

func TestKurtosisRunAndRemove(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()

	var captured []string
	runCommand = func(name string, args ...string) (string, error) {
		captured = append([]string{name}, args...)
		return "", nil
	}
	if err := kurtosisRun("c1-teku-prysm", "pkg@ref", "/tmp/args.yaml"); err != nil {
		t.Fatalf("kurtosisRun error: %v", err)
	}
	want := []string{"kurtosis", "run", "--enclave", "c1-teku-prysm", "--image-download", "always", "pkg@ref", "--args-file", "/tmp/args.yaml"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("captured argv = %v, want %v", captured, want)
	}

	runCommand = func(string, ...string) (string, error) { return "boom", fmt.Errorf("exit status 1") }
	if err := kurtosisRun("e", "pkg", "f"); err == nil {
		t.Error("expected error from kurtosisRun on runCommand failure")
	}

	// kurtosisRemove must never panic, even if the fake errors.
	runCommand = func(string, ...string) (string, error) { return "", fmt.Errorf("boom") }
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("kurtosisRemove panicked: %v", r)
			}
		}()
		kurtosisRemove("c1-teku-prysm")
	}()
}

func TestPrometheusBaseURLParse(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()

	runCommand = func(string, ...string) (string, error) { return "http://127.0.0.1:53455\n", nil }
	if got := prometheusBaseURL("e"); got != "http://127.0.0.1:53455" {
		t.Errorf("prometheusBaseURL = %q, want http://127.0.0.1:53455", got)
	}

	runCommand = func(string, ...string) (string, error) { return "", fmt.Errorf("rc!=0") }
	if got := prometheusBaseURL("e"); got != "" {
		t.Errorf("prometheusBaseURL on runner error = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// runOne, via a temp param-files dir and fakes.
// ---------------------------------------------------------------------------

// writeParamFile writes a minimal static args-file (with the token
// placeholder, mirroring the real network-params/*.yaml files) into dir and
// returns its path.
func writeParamFile(t *testing.T, dir, name string) string {
	t.Helper()
	content := "prometheus_params:\n  remote_write_token: \"$PROMETHEUS_REMOTE_WRITE_TOKEN\"\ncharon_node_count: 4\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestTokenSubstitution(t *testing.T) {
	dir := t.TempDir()
	path := writeParamFile(t, dir, "teku-prysm.yaml")
	raw, err := readFileFn(path)
	if err != nil {
		t.Fatalf("readFileFn: %v", err)
	}
	got := strings.ReplaceAll(string(raw), "$PROMETHEUS_REMOTE_WRITE_TOKEN", "SECRET123")
	if strings.Contains(got, "$PROMETHEUS_REMOTE_WRITE_TOKEN") {
		t.Errorf("token placeholder not substituted: %s", got)
	}
	if !strings.Contains(got, "SECRET123") {
		t.Errorf("token not present in output: %s", got)
	}
}

func TestRunOnePreLaunchFailurePostsFailed(t *testing.T) {
	oldRun, oldPost := runCommand, httpPost
	defer func() { runCommand, httpPost = oldRun, oldPost }()

	runCommand = func(name string, args ...string) (string, error) {
		if name == "git" {
			return "", fmt.Errorf("network unreachable")
		}
		return "", nil
	}
	postCount := 0
	httpPost = func(string, []byte) (int, error) {
		postCount++
		return 200, nil
	}

	dir := t.TempDir()
	paramFile := writeParamFile(t, dir, "teku-prysm.yaml")

	cfg := config{
		slackWebhookURL: "http://hook", repoPath: "/nonexistent", packageRef: "pkg",
		runMinutes: 1, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}
	data := runOne(cfg, paramFile, "teku-prysm", 1)
	if data.status != "failed" {
		t.Errorf("status = %q, want failed", data.status)
	}
	if postCount != 1 {
		t.Errorf("slackPost called %d times, want 1", postCount)
	}
}

func TestRunOneLaunchFailureTearsDown(t *testing.T) {
	oldRun, oldPost := runCommand, httpPost
	defer func() { runCommand, httpPost = oldRun, oldPost }()

	dir := t.TempDir()
	paramFile := writeParamFile(t, dir, "teku-prysm.yaml")

	removeCalls := 0
	runCommand = func(name string, args ...string) (string, error) {
		if name != "kurtosis" {
			return "", nil // git pull etc.
		}
		switch {
		case len(args) > 0 && args[0] == "run":
			// kurtosis run fails but (as in production) may have left containers.
			return "a service did not become available", fmt.Errorf("exit status 1")
		case len(args) > 0 && args[0] == "enclave":
			removeCalls++
			return "", nil
		}
		return "", nil
	}
	postCount := 0
	httpPost = func(string, []byte) (int, error) { postCount++; return 200, nil }

	cfg := config{
		slackWebhookURL: "http://hook", repoPath: dir, packageRef: "pkg",
		runMinutes: 1, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}
	data := runOne(cfg, paramFile, "teku-prysm", 1)
	if data.status != "failed" {
		t.Errorf("status = %q, want failed", data.status)
	}
	if postCount != 1 {
		t.Errorf("slackPost called %d times, want 1", postCount)
	}
	// A failed kurtosis run must still tear the enclave down: pre-clear at the top
	// of runOne + the explicit teardown in the launch-failure branch = 2. Before
	// the fix this was 1 (the enclave leaked), so this guards the regression.
	if removeCalls != 2 {
		t.Errorf("kurtosisRemove called %d times, want 2 (pre-clear + launch-failure teardown)", removeCalls)
	}
}

func TestRunOneMidRunFailureTearsDownAndPosts(t *testing.T) {
	oldRun, oldPost, oldGet, oldSleep := runCommand, httpPost, httpGet, sleepFn
	defer func() { runCommand, httpPost, httpGet, sleepFn = oldRun, oldPost, oldGet, oldSleep }()
	sleepFn = func(time.Duration) {}

	dir := t.TempDir()
	paramFile := writeParamFile(t, dir, "teku-prysm.yaml")

	removeCalls := 0
	runCommand = func(name string, args ...string) (string, error) {
		if name != "kurtosis" {
			return "", nil // git pull, etc.
		}
		switch {
		case len(args) > 0 && args[0] == "port":
			return "http://127.0.0.1:9999\n", nil
		case len(args) > 0 && args[0] == "enclave":
			removeCalls++
			return "", nil
		}
		return "", nil // "kurtosis run"
	}
	httpGet = func(u string) ([]byte, int, error) {
		if strings.Contains(u, "core_scheduler_validators_active") {
			return []byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1,"1"]}]}}`), 200, nil
		}
		// Everything queried after health (cluster discovery, duties, etc) fails.
		return []byte(`{"status":"error","errorType":"timeout"}`), 200, nil
	}
	postCount := 0
	httpPost = func(string, []byte) (int, error) { postCount++; return 200, nil }

	cfg := config{
		slackWebhookURL: "http://hook", repoPath: dir, packageRef: "pkg",
		runMinutes: 1, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}
	data := runOne(cfg, paramFile, "teku-prysm", 1)
	if data.status != "failed" {
		t.Errorf("status = %q, want failed", data.status)
	}
	if postCount != 1 {
		t.Errorf("slackPost called %d times, want 1", postCount)
	}
	if data.name != "teku-prysm" {
		t.Errorf("name = %q, want teku-prysm", data.name)
	}
	// Exactly 2 kurtosis-enclave-rm calls are guaranteed on this path: the
	// guarded pre-clear at the top of runOne, and the deferred teardown
	// after a successful kurtosisRun. A regression that drops either one
	// should fail this count.
	if removeCalls != 2 {
		t.Errorf("kurtosisRemove (via runCommand enclave rm) called %d times, want 2 (pre-clear + deferred teardown)", removeCalls)
	}
}

// TestRunOneRecoversFromPanicAfterLaunch exercises runOne's top-level
// recover(): a fake (httpGet, called via promQuery during collectReport,
// well after a successful kurtosisRun) panics instead of erroring. runOne
// must not let that panic escape -- it must produce a "failed" reportData,
// post it exactly once, and still tear down the enclave (pre-clear +
// deferred teardown), exactly as if collectReport had returned an error.
func TestRunOneRecoversFromPanicAfterLaunch(t *testing.T) {
	oldRun, oldPost, oldGet, oldSleep := runCommand, httpPost, httpGet, sleepFn
	defer func() { runCommand, httpPost, httpGet, sleepFn = oldRun, oldPost, oldGet, oldSleep }()
	sleepFn = func(time.Duration) {}

	dir := t.TempDir()
	paramFile := writeParamFile(t, dir, "teku-prysm.yaml")

	removeCalls := 0
	runCommand = func(name string, args ...string) (string, error) {
		if name != "kurtosis" {
			return "", nil // git pull, etc.
		}
		switch {
		case len(args) > 0 && args[0] == "port":
			return "http://127.0.0.1:9999\n", nil
		case len(args) > 0 && args[0] == "enclave":
			removeCalls++
			return "", nil
		}
		return "", nil // "kurtosis run"
	}
	httpGet = func(u string) ([]byte, int, error) {
		if strings.Contains(u, "core_scheduler_validators_active") {
			// Let waitHealthy succeed so we get past launch into the
			// post-launch phase, then panic on the very next query
			// (discoverClusterName's).
			return []byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1,"1"]}]}}`), 200, nil
		}
		panic("simulated metrics client panic")
	}
	postCount := 0
	httpPost = func(string, []byte) (int, error) { postCount++; return 200, nil }

	cfg := config{
		slackWebhookURL: "http://hook", repoPath: dir, packageRef: "pkg",
		runMinutes: 1, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}

	data := runOne(cfg, paramFile, "teku-prysm", 1) // must not panic out of this call
	if data.status != "failed" {
		t.Errorf("status = %q, want failed", data.status)
	}
	if !strings.Contains(data.errMsg, "simulated metrics client panic") {
		t.Errorf("errMsg = %q, want it to mention the panic value", data.errMsg)
	}
	if postCount != 1 {
		t.Errorf("slackPost called %d times, want 1", postCount)
	}
	// 3 calls on this path: the guarded pre-clear, the plain `defer
	// kurtosisRemove(enclave)` registered right after the successful
	// kurtosisRun, and the recover handler's own guarded kurtosisRemove
	// (which exists to cover panics that happen *before* that plain defer
	// is registered, e.g. during pre-launch -- it's redundant-but-harmless,
	// idempotent/best-effort, when a panic happens after launch instead).
	if removeCalls != 3 {
		t.Errorf("kurtosisRemove called %d times, want 3 (pre-clear + deferred teardown + recover handler)", removeCalls)
	}
}

func TestRunOneHappyPathOK(t *testing.T) {
	oldRun, oldPost, oldGet, oldSleep, oldNow := runCommand, httpPost, httpGet, sleepFn, nowFn
	defer func() { runCommand, httpPost, httpGet, sleepFn, nowFn = oldRun, oldPost, oldGet, oldSleep, oldNow }()

	dir := t.TempDir()
	paramFile := writeParamFile(t, dir, "teku-prysm.yaml")

	runCommand = func(name string, args ...string) (string, error) {
		if name == "kurtosis" && len(args) > 0 && args[0] == "port" {
			return "http://127.0.0.1:9999\n", nil
		}
		return "", nil
	}
	httpGet = func(u string) ([]byte, int, error) {
		switch {
		case strings.Contains(u, "core_scheduler_validators_active"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1,"1"]}]}}`), 200, nil
		case strings.Contains(u, "cluster_name") && strings.Contains(u, "app_version"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_name":"kurtosis-teku-prysm"},"value":[1,"1"]}]}}`), 200, nil
		case strings.Contains(u, "core_tracker_expect_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"780"]}]}}`), 200, nil
		case strings.Contains(u, "core_tracker_success_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"780"]}]}}`), 200, nil
		case strings.Contains(u, "process_resident_memory_bytes"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0"},"value":[1,"1.2e8"]}]}}`), 200, nil
		case strings.Contains(u, "process_cpu_seconds_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0"},"value":[1,"0.5"]}]}}`), 200, nil
		case strings.Contains(u, "app_health_checks"):
			return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
		}
		return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
	}
	sleepFn = func(time.Duration) {}
	fixedNow := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return fixedNow }
	httpPost = func(string, []byte) (int, error) { return 200, nil }

	cfg := config{
		slackWebhookURL: "http://hook", repoPath: dir, packageRef: "pkg",
		runMinutes: 2, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}
	data := runOne(cfg, paramFile, "teku-prysm", 1)
	if data.status != "ok" {
		t.Fatalf("status = %q, want ok (errMsg=%q)", data.status, data.errMsg)
	}
	if data.clusterName != "kurtosis-teku-prysm" {
		t.Errorf("clusterName = %q, want kurtosis-teku-prysm", data.clusterName)
	}
	windowS := cfg.runMinutes * 60
	wantWindow := fmtWindow(fixedNow.Add(-time.Duration(windowS)*time.Second), fixedNow)
	if data.window != wantWindow {
		t.Errorf("window = %q, want %q", data.window, wantWindow)
	}
}

func TestHealthCheckStatusGatedByToggle(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()
	httpGet = func(u string) ([]byte, int, error) {
		switch {
		case strings.Contains(u, "core_tracker_expect_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"1000"]}]}}`), 200, nil
		case strings.Contains(u, "core_tracker_success_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"1000"]}]}}`), 200, nil
		case strings.Contains(u, "app_health_checks"):
			// A check that is both fired and firing now.
			return []byte(`{"status":"success","data":{"result":[{"metric":{"name":"peer-count","severity":"error"},"value":[1,"1"]}]}}`), 200, nil
		default:
			return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
		}
	}

	// Default (disabled): a firing health check must NOT downgrade an otherwise
	// healthy run.
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "ok" {
		t.Errorf("with health disabled, firing check gave status %q, want ok", data.status)
	}

	// Enabled: the same firing check downgrades to degraded.
	oldFlag := reportHealthChecks
	reportHealthChecks = true
	defer func() { reportHealthChecks = oldFlag }()
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("with health enabled, firing check gave status %q, want degraded", data.status)
	}
}

func TestDegradedTolerance(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	mkResp := func(pct float64) func(string) ([]byte, int, error) {
		expected := 1000.0
		success := expected * pct / 100
		return func(u string) ([]byte, int, error) {
			switch {
			case strings.Contains(u, "core_tracker_expect_duties_total"):
				return []byte(fmt.Sprintf(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"%v"]}]}}`, expected)), 200, nil
			case strings.Contains(u, "core_tracker_success_duties_total"):
				return []byte(fmt.Sprintf(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"%v"]}]}}`, success)), 200, nil
			default:
				return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
			}
		}
	}

	httpGet = mkResp(99.9)
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "ok" {
		t.Errorf("status at 99.9%% pct = %q, want ok", data.status)
	}

	httpGet = mkResp(95)
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("status at 95%% pct = %q, want degraded", data.status)
	}
}

// ---------------------------------------------------------------------------
// Failure log capture + Slack upload.
// ---------------------------------------------------------------------------

func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}

func keysOf(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestSelectLogTargets(t *testing.T) {
	// Models a lodestar-nimbus enclave: a fixed lighthouse bootstrap (cl-1/cl-2)
	// plus the DV participant (node index 3) whose CL is lodestar and whose 4
	// Charon nodes each run a Nimbus VC. The BN must be the DV's cl-3-lodestar,
	// NOT the lexically-first cl-1-lighthouse bootstrap.
	containers := []string{
		"cl-1-lighthouse-geth--a",
		"cl-2-lighthouse-geth--b",
		"cl-3-lodestar-geth--c",
		"el-1-geth-lighthouse--d",
		"vc-1-geth-lighthouse--e", // bootstrap VC: not a DV VC
		"vc-3-geth-lodestar-charon-charon-0--f",
		"vc-3-geth-lodestar-charon-charon-1--g",
		"vc-3-geth-lodestar-charon-charon-relay-2--h",      // helper: excluded
		"vc-3-geth-lodestar-charon-charon-split-keys-2--i", // helper: excluded
		"vc-3-geth-lodestar-charon-vc-0-nimbus--j",
		"vc-3-geth-lodestar-charon-vc-1-nimbus--k",
		"prometheus--l",
	}
	bn, dv, vcs := selectLogTargets(containers)
	if bn != "cl-3-lodestar-geth--c" {
		t.Errorf("bn = %q, want the DV's cl-3-lodestar (not the lighthouse bootstrap)", bn)
	}
	if strings.Contains(bn, "lighthouse") {
		t.Errorf("bn = %q must not be the lighthouse bootstrap node", bn)
	}
	if len(dv) != 2 || !strings.Contains(dv[0], "charon-charon-0") || !strings.Contains(dv[1], "charon-charon-1") {
		t.Errorf("dvNodes = %v, want exactly the 2 numbered charon nodes (relay/split-keys excluded)", dv)
	}
	if len(vcs) != 2 || !strings.Contains(vcs[0], "-vc-0-") || !strings.Contains(vcs[1], "-vc-1-") {
		t.Errorf("vcs = %v, want the two charon-vc clients", vcs)
	}
}

func TestCaptureFailureLogs(t *testing.T) {
	oldRun, oldNow := runCommand, nowFn
	defer func() { runCommand, nowFn = oldRun, oldNow }()
	nowFn = func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) }

	psOut := strings.Join([]string{
		"cl-1-lighthouse-geth--a", // bootstrap CL: must NOT be captured
		"cl-3-lodestar-geth--b",   // DV CL: the one to capture
		"el-3-geth-lodestar--c",
		"vc-3-geth-lodestar-charon-charon-0--d",
		"vc-3-geth-lodestar-charon-vc-0-nimbus--e",
		"prometheus--f",
	}, "\n")
	dockerLogsCalled := false
	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return psOut + "\n", nil
		}
		// Logs must come from the kurtosis log aggregator (complete history),
		// not docker's local ring buffer.
		if name == "kurtosis" && len(args) >= 5 && args[0] == "service" && args[1] == "logs" && args[2] == "-a" {
			if args[3] != "c2-lodestar-nimbus" {
				return "", fmt.Errorf("unexpected enclave %q", args[3])
			}
			svc := args[len(args)-1]
			if strings.Contains(svc, "--") {
				return "", fmt.Errorf("service name %q must not contain the docker container hash suffix", svc)
			}
			if strings.Contains(svc, "charon-charon-0") {
				return "INFO all good\nERRO something bad happened\n", nil
			}
			return "log for " + svc + "\n", nil
		}
		if name == "docker" && len(args) > 0 && args[0] == "logs" {
			dockerLogsCalled = true
			return "docker ring buffer content\n", nil
		}
		return "", nil
	}

	logDir := t.TempDir()
	archive, excerpt := captureFailureLogs(config{logDir: logDir}, "c2-lodestar-nimbus", "lodestar-nimbus", 2)
	if archive == "" {
		t.Fatal("archive path empty")
	}
	if filepath.Dir(archive) != logDir {
		t.Errorf("archive %q not under logDir %q", archive, logDir)
	}
	if base := filepath.Base(archive); base != "cycle2-lodestar-nimbus-20260731-120000.tar.gz" {
		t.Errorf("archive name = %q", base)
	}

	got := readTarGz(t, archive)
	for _, want := range []string{
		"cl-3-lodestar-geth.log", // the DV's CL, not the bootstrap
		"vc-3-geth-lodestar-charon-charon-0.log",
		"vc-3-geth-lodestar-charon-vc-0-nimbus.log",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("archive missing %s; has %v", want, keysOf(got))
		}
	}
	if _, bad := got["cl-1-lighthouse-geth.log"]; bad {
		t.Error("bootstrap lighthouse CL must NOT be captured for a lodestar combo")
	}
	if _, bad := got["el-3-geth-lodestar.log"]; bad {
		t.Error("EL log should NOT be captured (only BN/Charon/VC)")
	}
	if _, bad := got["prometheus.log"]; bad {
		t.Error("prometheus log should NOT be captured")
	}
	if !strings.Contains(got["vc-3-geth-lodestar-charon-charon-0.log"], "something bad happened") {
		t.Error("charon-0 log content missing from archive")
	}
	if !strings.Contains(excerpt, "something bad happened") {
		t.Errorf("excerpt missing the error line: %q", excerpt)
	}
	if dockerLogsCalled {
		t.Error("docker logs must not be used when kurtosis service logs succeeds")
	}
}

func TestFetchServiceLogsFallback(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	// Kurtosis aggregator unavailable -> fall back to docker logs.
	runCommand = func(name string, args ...string) (string, error) {
		if name == "kurtosis" {
			return "", fmt.Errorf("engine unreachable")
		}
		if name == "docker" && args[0] == "logs" && args[1] == "vc-3-x-charon-charon-0--abc" {
			return "ring buffer logs\n", nil
		}
		return "", nil
	}
	if got := fetchServiceLogs("c1-x", "vc-3-x-charon-charon-0--abc"); got != "ring buffer logs\n" {
		t.Errorf("fallback logs = %q, want docker ring buffer content", got)
	}

	// Kurtosis succeeding but empty also falls back (a service kurtosis lost).
	runCommand = func(name string, args ...string) (string, error) {
		if name == "kurtosis" {
			return "  \n", nil
		}
		return "docker content\n", nil
	}
	if got := fetchServiceLogs("c1-x", "svc--abc"); got != "docker content\n" {
		t.Errorf("empty-kurtosis fallback = %q, want docker content", got)
	}
}

func TestUploadLogsToSlack(t *testing.T) {
	oldDo := httpDo
	defer func() { httpDo = oldDo }()

	// Unconfigured (no token/channel) is a silent no-op: (false, nil).
	if up, err := uploadLogsToSlack(config{}, "/does/not/matter", "c"); err != nil || up {
		t.Errorf("unconfigured upload should be (false,nil), got up=%v err=%v", up, err)
	}

	dir := t.TempDir()
	arch := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(arch, []byte("PAYLOAD"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	httpDo = func(method, reqURL string, headers map[string]string, body []byte) ([]byte, int, error) {
		calls = append(calls, reqURL)
		switch {
		case strings.Contains(reqURL, "getUploadURLExternal"):
			if headers["Authorization"] != "Bearer xoxb-tok" {
				t.Errorf("step1 missing bearer auth: %v", headers)
			}
			if !strings.Contains(string(body), "filename=a.tar.gz") || !strings.Contains(string(body), "length=7") {
				t.Errorf("step1 body = %q", body)
			}
			return []byte(`{"ok":true,"upload_url":"https://files.slack/upload/xyz","file_id":"F1"}`), 200, nil
		case strings.Contains(reqURL, "files.slack/upload"):
			if !strings.Contains(string(body), "PAYLOAD") {
				t.Error("upload POST body missing the file bytes")
			}
			return []byte("OK"), 200, nil
		case strings.Contains(reqURL, "completeUploadExternal"):
			if !strings.Contains(string(body), `"channel_id":"C42"`) {
				t.Errorf("complete body missing channel: %q", body)
			}
			if !strings.Contains(string(body), `"F1"`) {
				t.Errorf("complete body missing file id: %q", body)
			}
			return []byte(`{"ok":true}`), 200, nil
		}
		return []byte(`{"ok":false,"error":"unexpected url"}`), 200, nil
	}

	cfg := config{slackBotToken: "xoxb-tok", slackChannelID: "C42"}
	up, err := uploadLogsToSlack(cfg, arch, "logs for x")
	if err != nil {
		t.Fatalf("uploadLogsToSlack error: %v", err)
	}
	if !up {
		t.Error("expected uploaded=true on a successful upload")
	}
	if len(calls) != 3 {
		t.Errorf("expected 3 HTTP calls (reserve/upload/complete), got %d: %v", len(calls), calls)
	}
}

func TestUploadLogsBestEffortDeletesOnSuccess(t *testing.T) {
	oldDo := httpDo
	defer func() { httpDo = oldDo }()
	httpDo = func(method, reqURL string, headers map[string]string, body []byte) ([]byte, int, error) {
		switch {
		case strings.Contains(reqURL, "getUploadURLExternal"):
			return []byte(`{"ok":true,"upload_url":"https://x/up","file_id":"F1"}`), 200, nil
		case strings.Contains(reqURL, "/up"):
			return []byte("OK"), 200, nil
		case strings.Contains(reqURL, "completeUploadExternal"):
			return []byte(`{"ok":true}`), 200, nil
		}
		return []byte(`{"ok":false}`), 200, nil
	}
	dir := t.TempDir()
	arch := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(arch, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploadLogsBestEffort(config{slackBotToken: "t", slackChannelID: "C"},
		reportData{name: "x", cycle: 1, status: "failed", logArchivePath: arch})
	if _, err := os.Stat(arch); !os.IsNotExist(err) {
		t.Errorf("archive should be deleted after a successful upload; stat err = %v", err)
	}
}

func TestUploadLogsBestEffortKeepsWhenNotUploaded(t *testing.T) {
	oldDo := httpDo
	defer func() { httpDo = oldDo }()
	dir := t.TempDir()

	// Upload not configured -> keep the local archive.
	a1 := filepath.Join(dir, "k1.tar.gz")
	if err := os.WriteFile(a1, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploadLogsBestEffort(config{}, reportData{logArchivePath: a1})
	if _, err := os.Stat(a1); err != nil {
		t.Errorf("unconfigured: archive should be kept, got %v", err)
	}

	// Configured but the upload fails -> keep the local archive.
	httpDo = func(method, reqURL string, headers map[string]string, body []byte) ([]byte, int, error) {
		return []byte(`{"ok":false,"error":"boom"}`), 200, nil
	}
	a2 := filepath.Join(dir, "k2.tar.gz")
	if err := os.WriteFile(a2, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploadLogsBestEffort(config{slackBotToken: "t", slackChannelID: "C"}, reportData{logArchivePath: a2})
	if _, err := os.Stat(a2); err != nil {
		t.Errorf("failed upload: archive should be kept, got %v", err)
	}
}

func TestBuildBlocksLogsSection(t *testing.T) {
	d := reportData{
		name: "x", cycle: 1, status: "failed", window: "-",
		logArchivePath: "/home/u/dv-cycler-logs/cycle1-x-ts.tar.gz",
		logExcerpt:     "charon-0:\nERRO boom",
	}
	dump := dumpBlocks(buildBlocks(d))
	if !strings.Contains(dump, "cycle1-x-ts.tar.gz") {
		t.Errorf("logs section missing archive path: %s", dump)
	}
	if !strings.Contains(dump, "ERRO boom") {
		t.Errorf("logs section missing excerpt: %s", dump)
	}
	if strings.Contains(dumpBlocks(buildBlocks(reportData{name: "x", status: "ok", window: "-"})), "*Logs:*") {
		t.Error("logs section should be absent when no archive was captured")
	}
}

// ---------------------------------------------------------------------------
// DV results matrix.
// ---------------------------------------------------------------------------

func TestParsePins(t *testing.T) {
	yaml := `participants:
  - el_type: geth
    cl_type: lighthouse
    cl_image: sigp/lighthouse:v8.2.1
    vc_type: lighthouse
    vc_image: sigp/lighthouse:v8.2.1
    count: 2
  - el_type: geth
    cl_type: lodestar
    cl_image: "chainsafe/lodestar:v1.43.1"
    vc_type: charon
    charon_params:
      charon_vc: nimbus
      charon_vc_image: statusim/nimbus-validator-client:multiarch-v26.7.0
`
	p := parsePins(yaml)
	if p.cl != "chainsafe/lodestar:v1.43.1" {
		t.Errorf("cl = %q, want the DV (last) cl_image, quotes stripped", p.cl)
	}
	if p.vc != "statusim/nimbus-validator-client:multiarch-v26.7.0" {
		t.Errorf("vc = %q, want the charon_vc_image", p.vc)
	}
}

func TestPendingCombos(t *testing.T) {
	files := []string{"/p/grandine-lodestar.yaml", "/p/lodestar-nimbus.yaml", "/p/teku-prysm.yaml"}
	curPins := map[string]pins{
		"grandine-lodestar": {cl: "grandine:2.0.5", vc: "lodestar:v1.43.1"},
		"lodestar-nimbus":   {cl: "lodestar:v1.43.1", vc: "nimbus:v26.7.0"},
		"teku-prysm":        {cl: "teku:26.7.1", vc: "prysm:v7.1.8"},
	}
	m := matrixStore{Results: map[string]comboResult{
		// VC pin changed (lodestar bumped v1.43.0 -> v1.43.1) => pending
		"grandine-lodestar": {Status: "ok", CL: "grandine:2.0.5", VC: "lodestar:v1.43.0"},
		// matches current pins => valid
		"lodestar-nimbus": {Status: "ok", CL: "lodestar:v1.43.1", VC: "nimbus:v26.7.0"},
		// teku-prysm: no result at all => pending
	}}
	got := pendingCombos(m, files, curPins)
	want := []string{"grandine-lodestar", "teku-prysm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pending = %v, want %v (changed VC + never-run, sorted)", got, want)
	}
}

func TestParseCharonCommit(t *testing.T) {
	cases := map[string]string{
		"v1.11.0-dev [git_commit_hash=bc5674a,git_commit_time=2026-08-03T11:24:23Z]": "bc5674a",
		"v1.2.3 [git_commit_hash=abc1234]":                                           "abc1234",
		"no hash here":                                                               "",
	}
	for in, want := range cases {
		if got := parseCharonCommit(in); got != want {
			t.Errorf("parseCharonCommit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageTag(t *testing.T) {
	cases := map[string]string{
		"chainsafe/lodestar:v1.43.1":                         "v1.43.1",
		"statusim/nimbus-validator-client:multiarch-v26.7.0": "multiarch-v26.7.0",
		"":      "N/A",
		"notag": "notag",
	}
	for in, want := range cases {
		if got := imageTag(in); got != want {
			t.Errorf("imageTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestComboResultFrom(t *testing.T) {
	mem, cpu := 1.5e9, 1.4
	d := reportData{
		status: "degraded",
		worst: &worstNode{peer: "1", duties: []dutyResult{
			{duty: "attester", expected: 100, success: 90},
			{duty: "proposer", expected: 4, success: 4},
		}},
		dvMemBytes: &mem, dvCPU: &cpu,
		host:          &hostStats{cpuPeak: 82, memPeak: 9e9},
		charonVersion: "bc5674a",
	}
	r := comboResultFrom(d)
	if r.Status != "degraded" {
		t.Errorf("status = %q", r.Status)
	}
	if r.DutyPct == nil || *r.DutyPct < 90.3 || *r.DutyPct > 90.4 { // 94/104 = 90.38%
		t.Errorf("dutyPct = %v, want ~90.38", r.DutyPct)
	}
	if r.CharonMem == nil || *r.CharonMem != mem || r.CharonCPU == nil || *r.CharonCPU != cpu {
		t.Errorf("charon mem/cpu = %v/%v", r.CharonMem, r.CharonCPU)
	}
	if r.HostCPU == nil || *r.HostCPU != 82 || r.HostMem == nil || *r.HostMem != 9e9 {
		t.Errorf("host cpu/mem = %v/%v", r.HostCPU, r.HostMem)
	}
	if r.Charon != "bc5674a" {
		t.Errorf("charon commit = %q, want bc5674a", r.Charon)
	}

	f := comboResultFrom(reportData{status: "failed"})
	if f.Status != "failed" || f.DutyPct != nil || f.CharonMem != nil || f.HostCPU != nil || f.Charon != "" {
		t.Errorf("failed run should have nil metrics + empty charon: %+v", f)
	}
}

func TestBuildMatrixBlocks(t *testing.T) {
	combos := []string{"grandine-nimbus", "lodestar-nimbus", "teku-teku"}
	mem, cpu, hp, hm, dp := 1.2e9, 1.1, 55.0, 8e9, 99.9
	m := matrixStore{Results: map[string]comboResult{
		"grandine-nimbus": {Status: "ok", DutyPct: &dp, CharonMem: &mem, CharonCPU: &cpu, HostCPU: &hp, HostMem: &hm,
			CL: "sifrai/grandine:2.0.5", VC: "statusim/nimbus-validator-client:multiarch-v26.7.0", Charon: "bc5674a"},
		"lodestar-nimbus": {Status: "failed", CL: "chainsafe/lodestar:v1.43.1", VC: "statusim/nimbus:v26.7.0"},
		// teku-teku absent -> an N/A row
	}}
	text, blocks := buildMatrixBlocks(m, combos, "<!subteam^S1|proto>", "2026-08-03 15:40 UTC")
	if !strings.Contains(text, "3 combos") {
		t.Errorf("fallback text = %q", text)
	}
	dump := dumpBlocks(blocks)
	// dumpBlocks JSON-encodes, so "<"/">" render as </>; match the
	// un-bracketed core of the mention.
	for _, want := range []string{
		"grandine-nimbus", "lodestar-nimbus", "teku-teku", // every combo present
		"2.0.5", "multiarch-v26.7.0", "bc5674a", "v1.43.1", // version columns (tags)
		"99.9%", "1.20GB", // metrics for the filled row
		"N/A",                          // the unrun teku-teku row
		"subteam^S1|proto",             // mention prepended
		"updated 2026-08-03 15:40 UTC", // timestamp
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("matrix dump missing %q\n%s", want, dump)
		}
	}
}

func TestLoadMatrixRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.json")

	m, err := loadMatrix(path)
	if err != nil {
		t.Fatalf("loadMatrix(missing): %v", err)
	}
	if m.Results == nil {
		t.Error("Results map should be initialized for a missing file")
	}

	dp := 88.0
	m.Posted = true
	m.Results["lodestar-nimbus"] = comboResult{
		Status: "ok", DutyPct: &dp,
		CL: "chainsafe/lodestar:v1.43.1", VC: "statusim/nimbus:v26.7.0", Charon: "bc5674a",
	}
	if err := m.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loadMatrix(path)
	if err != nil {
		t.Fatalf("loadMatrix: %v", err)
	}
	if !loaded.Posted {
		t.Errorf("loaded.Posted = false, want true")
	}
	r, ok := loaded.Results["lodestar-nimbus"]
	if !ok || r.Status != "ok" || r.CL != "chainsafe/lodestar:v1.43.1" || r.Charon != "bc5674a" || r.DutyPct == nil || *r.DutyPct != 88.0 {
		t.Errorf("loaded result = %+v", loaded.Results)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestSubtractEpoch0(t *testing.T) {
	expected := []sample{
		{labels: map[string]string{"cluster_peer": "cute-child", "duty": "attester"}, value: 100},
		{labels: map[string]string{"cluster_peer": "cute-child", "duty": "aggregator"}, value: 50},
		{labels: map[string]string{"cluster_peer": "bold-storm", "duty": "attester"}, value: 100},
		{labels: map[string]string{"cluster_peer": "bold-storm", "duty": "aggregator"}, value: 50},
	}

	epoch0 := map[epoch0Key]float64{
		{peer: "cute-child", duty: "attester"}:   10,
		{peer: "cute-child", duty: "aggregator"}: 5,
		{peer: "bold-storm", duty: "attester"}:   7,
		{peer: "bold-storm", duty: "aggregator"}: 3,
	}
	got := subtractEpoch0(expected, epoch0)

	if got[0].value != 90 {
		t.Errorf("attester cute-child: got %v, want 90", got[0].value)
	}
	if got[1].value != 45 {
		t.Errorf("aggregator cute-child: got %v, want 45", got[1].value)
	}
	if got[2].value != 93 {
		t.Errorf("attester bold-storm: got %v, want 93", got[2].value)
	}
	if got[3].value != 47 {
		t.Errorf("aggregator bold-storm: got %v, want 47", got[3].value)
	}

	// nil map should be a no-op.
	unchanged := subtractEpoch0(expected, nil)
	for i, s := range unchanged {
		if s.value != expected[i].value {
			t.Errorf("nil map: sample %d changed from %v to %v", i, expected[i].value, s.value)
		}
	}
}

func TestCountEpoch0FailuresLogParsing(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	charonLogs := strings.Join([]string{
		`14:05:00.000 INFO app-start Lock file loaded {"peer_name": "cute-child", "peer_index": 0, "cluster_name": "kurtosis-test"}`,
		`14:05:32.123 WARN tracker Duty failed {"duty": "3/aggregator", "step": "fetcher", "reason": "insufficient_peer_signatures"}`,
		`14:05:32.200 WARN tracker Duty failed {"duty": "15/attester", "step": "consensus", "reason": "no_consensus"}`,
		`14:05:33.000 WARN tracker Duty failed {"duty": "31/aggregator", "step": "fetcher", "reason": "insufficient_peer_signatures"}`,
		`14:06:00.000 WARN tracker Duty failed {"duty": "33/proposer", "step": "fetcher", "reason": "not_included_onchain"}`,
		`14:06:05.000 WARN tracker Duty failed {"duty": "63/aggregator", "step": "fetcher", "reason": "insufficient_peer_signatures"}`,
		`14:06:10.000 WARN tracker Duty failed {"duty": "64/aggregator", "step": "fetcher", "reason": "insufficient_peer_signatures"}`,
		`14:06:15.000 WARN tracker Duty failed {"duty": "100/attester", "step": "consensus", "reason": "no_consensus"}`,
		`14:06:20.000 INFO tracker All peers participated in duty {"duty": "10/attester"}`,
	}, "\n")

	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return "vc-3-teku-prysm-charon-charon-0--abc123\n", nil
		}
		if name == "kurtosis" && len(args) > 0 && args[0] == "service" {
			return charonLogs, nil
		}
		return "", nil
	}

	got := countEpoch0Failures("test-enclave")
	if got[epoch0Key{peer: "cute-child", duty: "aggregator"}] != 3 {
		t.Errorf("aggregator = %v, want 3 (slots 3, 31, 63)", got[epoch0Key{peer: "cute-child", duty: "aggregator"}])
	}
	if got[epoch0Key{peer: "cute-child", duty: "attester"}] != 1 {
		t.Errorf("attester = %v, want 1 (slot 15)", got[epoch0Key{peer: "cute-child", duty: "attester"}])
	}
	if got[epoch0Key{peer: "cute-child", duty: "proposer"}] != 1 {
		t.Errorf("proposer = %v, want 1 (slot 33)", got[epoch0Key{peer: "cute-child", duty: "proposer"}])
	}
	if len(got) != 3 {
		t.Errorf("got %d keys, want 3 (slots 64+ excluded)", len(got))
	}
}
