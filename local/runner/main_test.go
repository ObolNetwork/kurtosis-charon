package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
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

	qf := promDutyFailed("kurtosis-a-b", 60)
	if !strings.Contains(qf, "core_tracker_failed_duties_total") {
		t.Errorf("promDutyFailed missing metric name: %s", qf)
	}
	if !strings.Contains(qf, "[60s]") || !strings.Contains(qf, "by (duty, cluster_peer)") {
		t.Errorf("promDutyFailed missing window or group-by: %s", qf)
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
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "http://hook")
		t.Setenv("RUNNER_REPO_PATH", "/srv/kurtosis-charon")
		t.Setenv("RUNNER_STATE_PATH", "/var/lib/runner/state.json")
		t.Setenv("RUNNER_PARAMS_DIR", "")
		t.Setenv("RUNNER_MONITORING_TOKEN", "")
		t.Setenv("RUNNER_PACKAGE_REF", "")
		t.Setenv("RUNNER_RUN_MINUTES", "")
		t.Setenv("RUNNER_STARTUP_DEADLINE_MINUTES", "")
		t.Setenv("RUNNER_SAMPLE_INTERVAL_S", "")
		t.Setenv("RUNNER_INTER_RUN_BACKOFF_S", "")
		t.Setenv("RUNNER_MAX_BACKOFF_S", "")

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
		if cfg.statePath != "/var/lib/runner/state.json" {
			t.Errorf("statePath = %q", cfg.statePath)
		}
		wantParamsDir := filepath.Join("/srv/kurtosis-charon", "deployments")
		if cfg.paramsDir != wantParamsDir {
			t.Errorf("paramsDir = %q, want %q", cfg.paramsDir, wantParamsDir)
		}
		if cfg.runMinutes != 90 {
			t.Errorf("runMinutes = %d, want 90", cfg.runMinutes)
		}
		if !strings.HasSuffix(cfg.packageRef, "ethereum-package@6.1.0-obol.3") {
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
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "s")
		t.Setenv("RUNNER_LOG_DIR", "")
		t.Setenv("RUNNER_SLACK_BOT_TOKEN", "xoxb-abc")
		t.Setenv("RUNNER_SLACK_CHANNEL_ID", "C123")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if !strings.HasSuffix(cfg.logDir, "runner-logs") {
			t.Errorf("logDir default = %q, want suffix runner-logs", cfg.logDir)
		}
		if cfg.slackBotToken != "xoxb-abc" || cfg.slackChannelID != "C123" {
			t.Errorf("bot token/channel = %q/%q", cfg.slackBotToken, cfg.slackChannelID)
		}
	})

	t.Run("resultsPath default (next to state) + summary mention", func(t *testing.T) {
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "/var/lib/cyc/state.json")
		t.Setenv("RUNNER_RESULTS_PATH", "")
		t.Setenv("RUNNER_SUMMARY_MENTION", "<!subteam^S9>")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		want := filepath.Join("/var/lib/cyc", "runner-results.json")
		if cfg.resultsPath != want {
			t.Errorf("resultsPath = %q, want %q", cfg.resultsPath, want)
		}
		if cfg.summaryMention != "<!subteam^S9>" {
			t.Errorf("summaryMention = %q", cfg.summaryMention)
		}
	})

	t.Run("RUNNER_LOG_DIR override wins over the default", func(t *testing.T) {
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "s")
		t.Setenv("RUNNER_LOG_DIR", "/var/log/dvc")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.logDir != "/var/log/dvc" {
			t.Errorf("logDir = %q, want /var/log/dvc", cfg.logDir)
		}
	})

	t.Run("RUNNER_PARAMS_DIR override wins over the derived default", func(t *testing.T) {
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("RUNNER_REPO_PATH", "/srv/kurtosis-charon")
		t.Setenv("RUNNER_STATE_PATH", "st")
		t.Setenv("RUNNER_PARAMS_DIR", "/custom/params")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.paramsDir != "/custom/params" {
			t.Errorf("paramsDir = %q, want /custom/params", cfg.paramsDir)
		}
	})

	t.Run("overrides applied", func(t *testing.T) {
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "h")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "st")
		t.Setenv("RUNNER_RUN_MINUTES", "30")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.runMinutes != 30 {
			t.Errorf("runMinutes = %d, want 30", cfg.runMinutes)
		}
	})

	t.Run("missing required raises", func(t *testing.T) {
		t.Setenv("RUNNER_SLACK_WEBHOOK_URL", "")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "s")

		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error when slackWebhookURL missing")
		}
	})

	t.Run("bare env name is not picked up (RUNNER_-only)", func(t *testing.T) {
		// SLACK_WEBHOOK_URL (bare, no RUNNER_ prefix) must NOT satisfy the
		// required slack_webhook_url config -- only RUNNER_SLACK_WEBHOOK_URL
		// counts. Setting the bare name alongside the other two required
		// RUNNER_ vars should still error as missing.
		t.Setenv("SLACK_WEBHOOK_URL", "http://hook")
		t.Setenv("RUNNER_REPO_PATH", "r")
		t.Setenv("RUNNER_STATE_PATH", "s")

		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error: bare SLACK_WEBHOOK_URL (unprefixed) must not be read")
		}
		if !strings.Contains(err.Error(), "RUNNER_SLACK_WEBHOOK_URL") {
			t.Errorf("error = %v, want it to name RUNNER_SLACK_WEBHOOK_URL", err)
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

	var capturedURL string
	httpGet = func(u string) ([]byte, int, error) {
		capturedURL = u
		return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"42.5"]}]}}`), 200, nil
	}
	samples, err := promQuery("http://x", "up", time.Time{})
	if err != nil {
		t.Fatalf("promQuery error: %v", err)
	}
	if len(samples) != 1 || samples[0].value != 42.5 {
		t.Fatalf("samples = %+v, want one sample value 42.5", samples)
	}
	if samples[0].labels["duty"] != "attester" || samples[0].labels["cluster_peer"] != "0" {
		t.Errorf("labels = %+v", samples[0].labels)
	}
	if u, err := url.Parse(capturedURL); err != nil || u.Query().Has("time") {
		t.Errorf("zero eval time must omit the time param, got %q", capturedURL)
	}

	// A non-zero eval time pins the query to that instant.
	eval := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	if _, err := promQuery("http://x", "up", eval); err != nil {
		t.Fatalf("promQuery error: %v", err)
	}
	u, err := url.Parse(capturedURL)
	if err != nil {
		t.Fatalf("parse captured url: %v", err)
	}
	if got := u.Query().Get("time"); got != strconv.FormatInt(eval.Unix(), 10) {
		t.Errorf("time param = %q, want %d", got, eval.Unix())
	}

	httpGet = func(string) ([]byte, int, error) {
		return []byte(`{"status":"error","errorType":"bad_data"}`), 200, nil
	}
	if _, err := promQuery("http://x", "up", time.Time{}); err == nil {
		t.Fatal("expected error when status != success")
	} else if !strings.Contains(err.Error(), "bad_data") {
		t.Errorf("error = %v, want mention of errorType bad_data", err)
	}
}

func TestAddSamples(t *testing.T) {
	success := []sample{
		s(map[string]string{"duty": "attester", "cluster_peer": "0"}, 100),
		s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 95),
		s(map[string]string{"duty": "proposer", "cluster_peer": "0"}, 10),
	}
	failed := []sample{
		s(map[string]string{"duty": "attester", "cluster_peer": "1"}, 5),
		s(map[string]string{"duty": "sync_message", "cluster_peer": "0"}, 3), // only in failed
	}
	got := addSamples(success, failed)

	byKey := map[string]float64{}
	for _, sm := range got {
		byKey[sm.labels["duty"]+"/"+sm.labels["cluster_peer"]] = sm.value
	}
	want := map[string]float64{
		"attester/0":     100, // no failures
		"attester/1":     100, // 95 + 5
		"proposer/0":     10,
		"sync_message/0": 3, // failed-only series still appears
	}
	if len(byKey) != len(want) {
		t.Fatalf("got %d series %v, want %d", len(byKey), byKey, len(want))
	}
	for k, v := range want {
		if byKey[k] != v {
			t.Errorf("%s = %v, want %v", k, byKey[k], v)
		}
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
// placeholder, mirroring the real deployments/*.yaml files) into dir and
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
		case strings.Contains(u, "core_tracker_failed_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"0"]}]}}`), 200, nil
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
		case strings.Contains(u, "core_tracker_failed_duties_total"):
			return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"0"]}]}}`), 200, nil
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
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, time.Time{}, nil, hostStats{})
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
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, time.Time{}, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("with health enabled, firing check gave status %q, want degraded", data.status)
	}
}

func TestRoundSamples(t *testing.T) {
	samples := []sample{
		{labels: map[string]string{"duty": "attester"}, value: 361.02},
		{labels: map[string]string{"duty": "proposer"}, value: 119.98},
		{labels: map[string]string{"duty": "sync_message"}, value: 420.0},
	}
	roundSamples(samples)
	if samples[0].value != 361 {
		t.Errorf("got %v, want 361", samples[0].value)
	}
	if samples[1].value != 120 {
		t.Errorf("got %v, want 120", samples[1].value)
	}
	if samples[2].value != 420 {
		t.Errorf("got %v, want 420", samples[2].value)
	}
}

func TestDegradedTolerance(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	mkResp := func(pct float64) func(string) ([]byte, int, error) {
		expected := 1000.0
		success := expected * pct / 100
		failed := expected - success
		return func(u string) ([]byte, int, error) {
			switch {
			case strings.Contains(u, "core_tracker_failed_duties_total"):
				return []byte(fmt.Sprintf(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"%v"]}]}}`, failed)), 200, nil
			case strings.Contains(u, "core_tracker_success_duties_total"):
				return []byte(fmt.Sprintf(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"%v"]}]}}`, success)), 200, nil
			default:
				return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
			}
		}
	}

	httpGet = mkResp(100)
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, time.Time{}, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "ok" {
		t.Errorf("status at 100%% pct = %q, want ok", data.status)
	}

	httpGet = mkResp(99.9)
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, time.Time{}, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("status at 99.9%% pct = %q, want degraded", data.status)
	}
}

// TestCollectReportEvalTime pins the shared evaluation timestamp: every
// query fired by collectReport carries the same time= parameter, so two
// still-moving counters can never disagree by a sampling race.
func TestCollectReportEvalTime(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	var urls []string
	httpGet = func(u string) ([]byte, int, error) {
		urls = append(urls, u)
		return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
	}

	end := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowS := 5400
	if _, err := collectReport("http://x", "a-b", "kurtosis-a-b", 1, windowS, end, nil, hostStats{}); err != nil {
		t.Fatalf("collectReport error: %v", err)
	}

	if len(urls) == 0 {
		t.Fatal("no queries fired")
	}
	for _, u := range urls {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse queried url %q: %v", u, err)
		}
		q := parsed.Query().Get("query")
		switch {
		case strings.Contains(q, "core_tracker_success_duties_total"),
			strings.Contains(q, "core_tracker_failed_duties_total"),
			strings.Contains(q, "process_resident_memory_bytes"),
			strings.Contains(q, "process_cpu_seconds_total"),
			strings.Contains(q, "max_over_time(app_health_checks"):
			// The CPU query is a subquery ("[5400s:1m]"), so match without
			// the closing bracket.
			if !strings.Contains(q, fmt.Sprintf("[%ds", windowS)) {
				t.Errorf("query not over the full [%ds] window: %s", windowS, q)
			}
		}
		if got := parsed.Query().Get("time"); got != strconv.FormatInt(end.Unix(), 10) {
			t.Errorf("query %q has time=%q, want %d", q, got, end.Unix())
		}
	}
}

// TestCollectReportFailedBasedScoring pins the success/(success+failed)
// scoring: a duty's expected total is derived from the two counters, so an
// in-flight duty (in neither counter yet) can never register as a miss.
func TestCollectReportFailedBasedScoring(t *testing.T) {
	old := httpGet
	defer func() { httpGet = old }()

	httpGet = func(u string) ([]byte, int, error) {
		switch {
		case strings.Contains(u, "core_tracker_success_duties_total"):
			return []byte(`{"status":"success","data":{"result":[
				{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"95"]},
				{"metric":{"cluster_peer":"0","duty":"proposer"},"value":[1,"10"]}
			]}}`), 200, nil
		case strings.Contains(u, "core_tracker_failed_duties_total"):
			return []byte(`{"status":"success","data":{"result":[
				{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"5"]},
				{"metric":{"cluster_peer":"0","duty":"proposer"},"value":[1,"0"]}
			]}}`), 200, nil
		default:
			return []byte(`{"status":"success","data":{"result":[]}}`), 200, nil
		}
	}

	data, err := collectReport("http://x", "a-b", "kurtosis-a-b", 1, 60, time.Time{}, nil, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("status = %q, want degraded (attester has failures)", data.status)
	}
	if data.worst == nil {
		t.Fatal("worst = nil, want populated")
	}
	byDuty := map[string]dutyResult{}
	for _, d := range data.worst.duties {
		byDuty[d.duty] = d
	}
	if att := byDuty["attester"]; att.expected != 100 || att.success != 95 {
		t.Errorf("attester = %d/%d, want 95/100", int(att.success), int(att.expected))
	}
	if prop := byDuty["proposer"]; prop.expected != 10 || prop.success != 10 || prop.pct() != 100 {
		t.Errorf("proposer = %+v, want a clean 10/10", prop)
	}

	// The warm-up grace subtracts from the failed counts: with all 5
	// attester failures graced, the run scores a clean 95/95.
	warmup := map[warmupKey]float64{{peer: "0", duty: "attester"}: 5}
	data, err = collectReport("http://x", "a-b", "kurtosis-a-b", 1, 60, time.Time{}, warmup, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "ok" {
		t.Errorf("status with graced failures = %q, want ok", data.status)
	}
	byDuty = map[string]dutyResult{}
	for _, d := range data.worst.duties {
		byDuty[d.duty] = d
	}
	if att := byDuty["attester"]; att.expected != 95 || att.success != 95 {
		t.Errorf("graced attester = %d/%d, want 95/95", int(att.success), int(att.expected))
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

	// docker ps -a can list several containers for one service (recreates);
	// duplicates must collapse to one target per service label, keeping the
	// FIRST occurrence (callers list running containers first, so the live
	// instance wins) even when a stopped duplicate sorts lexically earlier.
	_, dv, vcs = selectLogTargets([]string{
		"vc-3-geth-lodestar-charon-charon-0--zzz-running",
		"vc-3-geth-lodestar-charon-charon-0--aaa-stopped",
		"vc-3-geth-lodestar-charon-vc-0-nimbus--zzz-running",
		"vc-3-geth-lodestar-charon-vc-0-nimbus--aaa-stopped",
	})
	if len(dv) != 1 || !strings.HasSuffix(dv[0], "--zzz-running") {
		t.Errorf("dvNodes = %v, want the single first-listed (running) instance", dv)
	}
	if len(vcs) != 1 || !strings.HasSuffix(vcs[0], "--zzz-running") {
		t.Errorf("vcs = %v, want the single first-listed (running) instance", vcs)
	}
}

// TestLogTargetsPrefersRunning pins the running-first composition: logTargets
// lists running containers before the docker ps -a sweep, so a recreated
// service's live container is the one captured (the docker-logs fallback
// reads per-container, not per-service).
func TestLogTargetsPrefersRunning(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	runCommand = func(name string, args ...string) (string, error) {
		if name != "docker" || len(args) == 0 || args[0] != "ps" {
			return "", nil
		}
		for _, a := range args {
			if a == "-a" {
				return "vc-3-geth-teku-charon-charon-0--stopped\nvc-3-geth-teku-charon-charon-0--running\n", nil
			}
		}
		return "vc-3-geth-teku-charon-charon-0--running\n", nil
	}

	all, dvNodes, err := logTargets("c1-teku-nimbus")
	if err != nil {
		t.Fatalf("logTargets error: %v", err)
	}
	if len(all) != 1 || !strings.HasSuffix(all[0], "--running") {
		t.Errorf("all = %v, want just the running instance", all)
	}
	if len(dvNodes) != 1 || !strings.HasSuffix(dvNodes[0], "--running") {
		t.Errorf("dvNodes = %v, want just the running instance", dvNodes)
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
	archive := captureFailureLogs(config{logDir: logDir}, "c2-lodestar-nimbus", "lodestar-nimbus", 2, "")
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
	if dockerLogsCalled {
		t.Error("docker logs must not be used when kurtosis service logs succeeds")
	}
}

func TestSnapshotStartupLogs(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return "cl-3-teku-geth--a\nvc-3-geth-teku-charon-charon-0--b\nprometheus--c\n", nil
		}
		if name == "kurtosis" && len(args) >= 5 && args[0] == "service" && args[1] == "logs" {
			return "boot line for " + args[len(args)-1] + "\n", nil
		}
		return "", nil
	}

	dir := snapshotStartupLogs("c1-teku-nimbus")
	if dir == "" {
		t.Fatal("snapshot dir empty")
	}
	defer os.RemoveAll(dir)

	for _, want := range []string{"cl-3-teku-geth.log", "vc-3-geth-teku-charon-charon-0.log"} {
		b, err := os.ReadFile(filepath.Join(dir, want))
		if err != nil {
			t.Fatalf("snapshot missing %s: %v", want, err)
		}
		if !strings.HasPrefix(string(b), "boot line for ") {
			t.Errorf("%s content = %q", want, b)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "prometheus.log")); err == nil {
		t.Error("prometheus must not be snapshotted (only BN/Charon/VC)")
	}

	// No targets at all -> "" (best-effort no-op), no dir left behind.
	runCommand = func(name string, args ...string) (string, error) { return "", nil }
	if got := snapshotStartupLogs("c1-x"); got != "" {
		os.RemoveAll(got)
		t.Errorf("snapshot dir = %q, want empty when no targets found", got)
	}
}

// TestCaptureFailureLogsEarlySnapshot pins the boot-line recovery: every
// service's startup snapshot is archived alongside its end-of-run logs as
// <svc>.early.log (the end-of-run fetch can silently lose the start of the
// run to log rotation), and an absent or empty snapshot degrades to the
// end-of-run logs alone.
func TestCaptureFailureLogsEarlySnapshot(t *testing.T) {
	oldRun, oldNow := runCommand, nowFn
	defer func() { runCommand, nowFn = oldRun, oldNow }()
	nowFn = func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }

	const bootLine = `07:00:01.000 INFO app-start Lock file loaded {"peer_name": "calm-shape"}`

	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return "vc-3-geth-teku-charon-charon-0--a\n", nil
		}
		if name == "kurtosis" && len(args) >= 5 && args[0] == "service" && args[1] == "logs" {
			return "07:05:00.000 INFO wrapped: boot lines gone\n", nil
		}
		return "", nil
	}

	t.Run("snapshot present -> archived as early.log", func(t *testing.T) {
		dir := t.TempDir()
		early := bootLine + "\n07:00:02.000 INFO more boot\n"
		if err := os.WriteFile(filepath.Join(dir, "vc-3-geth-teku-charon-charon-0.log"), []byte(early), 0o644); err != nil {
			t.Fatal(err)
		}
		archive := captureFailureLogs(config{logDir: t.TempDir()}, "c1-teku-nimbus", "teku-nimbus", 1, dir)
		if archive == "" {
			t.Fatal("archive path empty")
		}
		got := readTarGz(t, archive)
		earlyGot, ok := got["vc-3-geth-teku-charon-charon-0.early.log"]
		if !ok {
			t.Fatalf("missing early log; archive has %v", keysOf(got))
		}
		if !strings.Contains(earlyGot, bootLine) {
			t.Errorf("early log content = %q, want the boot line", earlyGot)
		}
	})

	t.Run("no snapshot dir -> no early file", func(t *testing.T) {
		archive := captureFailureLogs(config{logDir: t.TempDir()}, "c1-teku-nimbus", "teku-nimbus", 1, "")
		if archive == "" {
			t.Fatal("archive path empty")
		}
		got := readTarGz(t, archive)
		if _, bad := got["vc-3-geth-teku-charon-charon-0.early.log"]; bad {
			t.Errorf("early file must not appear without a snapshot; has %v", keysOf(got))
		}
	})

	t.Run("empty snapshot file -> no early file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vc-3-geth-teku-charon-charon-0.log"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		archive := captureFailureLogs(config{logDir: t.TempDir()}, "c1-teku-nimbus", "teku-nimbus", 1, dir)
		if archive == "" {
			t.Fatal("archive path empty")
		}
		got := readTarGz(t, archive)
		if _, bad := got["vc-3-geth-teku-charon-charon-0.early.log"]; bad {
			t.Errorf("empty snapshot must not be archived; has %v", keysOf(got))
		}
	})
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
		logArchivePath: "/home/u/runner-logs/cycle1-x-ts.tar.gz",
	}
	dump := dumpBlocks(buildBlocks(d))
	if !strings.Contains(dump, "cycle1-x-ts.tar.gz") {
		t.Errorf("logs section missing archive path: %s", dump)
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

func TestSubtractWarmup(t *testing.T) {
	failed := []sample{
		{labels: map[string]string{"cluster_peer": "cute-child", "duty": "attester"}, value: 12},
		{labels: map[string]string{"cluster_peer": "cute-child", "duty": "aggregator"}, value: 5},
		{labels: map[string]string{"cluster_peer": "bold-storm", "duty": "attester"}, value: 7},
		{labels: map[string]string{"cluster_peer": "bold-storm", "duty": "aggregator"}, value: 3},
	}

	warmup := map[warmupKey]float64{
		{peer: "cute-child", duty: "attester"}:   10,
		{peer: "cute-child", duty: "aggregator"}: 5,
		{peer: "bold-storm", duty: "attester"}:   7,
		{peer: "bold-storm", duty: "aggregator"}: 9, // more graced than counted: clamps to 0
	}
	got := subtractWarmup(failed, warmup)

	for i, want := range []float64{2, 0, 0, 0} {
		if got[i].value != want {
			t.Errorf("sample %d (%s/%s): got %v, want %v",
				i, got[i].labels["cluster_peer"], got[i].labels["duty"], got[i].value, want)
		}
	}

	// nil map should be a no-op.
	unchanged := subtractWarmup(failed, nil)
	for i, s := range unchanged {
		if s.value != failed[i].value {
			t.Errorf("nil map: sample %d changed from %v to %v", i, failed[i].value, s.value)
		}
	}
}

func TestCountWarmupFailuresLogParsing(t *testing.T) {
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

	got := countWarmupFailures("test-enclave", "")
	if got[warmupKey{peer: "cute-child", duty: "aggregator"}] != 3 {
		t.Errorf("aggregator = %v, want 3 (slots 3, 31, 63)", got[warmupKey{peer: "cute-child", duty: "aggregator"}])
	}
	if got[warmupKey{peer: "cute-child", duty: "attester"}] != 1 {
		t.Errorf("attester = %v, want 1 (slot 15)", got[warmupKey{peer: "cute-child", duty: "attester"}])
	}
	if got[warmupKey{peer: "cute-child", duty: "proposer"}] != 1 {
		t.Errorf("proposer = %v, want 1 (slot 33)", got[warmupKey{peer: "cute-child", duty: "proposer"}])
	}
	if len(got) != 3 {
		t.Errorf("got %d keys, want 3 (slots 64+ excluded)", len(got))
	}
}

// TestCountWarmupFailuresSplitRecords covers charon records that docker split
// across several lines. An attester "Duty failed" carries its per-validator
// failure array twice, which pushes the trailing `"duty"` field onto its own
// line; scanning raw lines finds "Duty failed" with no duty field and silently
// drops the record, leaving a warmup failure reported as a real one.
func TestCountWarmupFailuresSplitRecords(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	// Mirrors a real cycle-19 log: the aggregator record fits on one line,
	// the attester record is split, and the kurtosis "[service] " prefix is
	// repeated on every physical line.
	const p = `[vc-3-geth-lodestar-charon-charon-0] `
	charonLogs := strings.Join([]string{
		p + `05:29:58.000 INFO app-start Lock file loaded {"peer_name": "alert-word", "peer_index": 0}`,
		p + `05:44:33.149 WARN tracker Duty failed: fetch aggregator data: 404 {"reason_code": "bug_fetch_error", "duty": "2/aggregator"}`,
		p + `05:44:33.435 WARN tracker Duty failed: beacon api submit_attestations: POST failed with status 400: {"code":400,"failures":[{"index":0,"message":"PublishError.NoPeersSubscribedToTopic"}]} {"step": "bcast",`,
		p + `"reason_code": "broadcast_bn_error", "duty": "2/attester"}`,
		p + `	app/eth2wrap/eth2wrap.go:323 .wrapError`,
		p + `05:44:45.072 WARN tracker Duty failed: beacon api submit_attestations: POST failed with status 400: {"code":400,"failures":[{"index":0,"message":"PublishError.NoPeersSubscribedToTopic"}]} {"step": "bcast",`,
		p + `"reason_code": "broadcast_bn_error", "duty": "3/attester"}`,
		// A split record beyond the warmup window must still be excluded.
		p + `06:10:00.000 WARN tracker Duty failed: beacon api submit_attestations: POST failed with status 400: {"step": "bcast",`,
		p + `"reason_code": "broadcast_bn_error", "duty": "200/attester"}`,
	}, "\n")

	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return "vc-3-geth-lodestar-charon-charon-0--abc123\n", nil
		}
		if name == "kurtosis" && len(args) > 0 && args[0] == "service" {
			return charonLogs, nil
		}
		return "", nil
	}

	got := countWarmupFailures("test-enclave", "")
	if v := got[warmupKey{peer: "alert-word", duty: "attester"}]; v != 2 {
		t.Errorf("attester = %v, want 2 (split records at slots 2 and 3)", v)
	}
	if v := got[warmupKey{peer: "alert-word", duty: "aggregator"}]; v != 1 {
		t.Errorf("aggregator = %v, want 1 (slot 2)", v)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2 (slot 200 excluded)", len(got))
	}
}

// TestCountWarmupFailuresStartupSnapshot pins the two robustness upgrades:
// the peer name resolves from the startup snapshot when the boot lines have
// rotated out of the end-of-run logs, and a warm-up duty the tracker counted
// as failed without emitting a "Duty failed" line is still graced via its
// component-level "Permanent failure" record (deduped by (slot, duty)
// against any tracker line for the same instance).
func TestCountWarmupFailuresStartupSnapshot(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	// End-of-run logs: truncated (no "Lock file loaded"), one fetcher
	// "Permanent failure" for slot 1 with no matching "Duty failed", and a
	// slot-2 instance reported by BOTH patterns (must count once).
	finalLogs := strings.Join([]string{
		`00:05:00.000 ERRO fetcher Permanent failure calling fetcher/fetch: fetch aggregator data: 404 {"duty": "1/aggregator"}`,
		`00:05:12.000 ERRO fetcher Permanent failure calling fetcher/fetch: fetch aggregator data: 404 {"duty": "2/aggregator"}`,
		`00:13:59.000 WARN tracker Duty failed {"duty": "2/aggregator", "step": "fetcher", "reason_code": "bug_fetch_error"}`,
	}, "\n")

	runCommand = func(name string, args ...string) (string, error) {
		if name == "docker" && len(args) > 0 && args[0] == "ps" {
			return "vc-3-geth-lighthouse-charon-charon-3--abc\n", nil
		}
		if name == "kurtosis" && len(args) > 0 && args[0] == "service" {
			return finalLogs, nil
		}
		return "", nil
	}

	t.Run("without snapshot: grace skipped (no peer name)", func(t *testing.T) {
		if got := countWarmupFailures("test-enclave", ""); len(got) != 0 {
			t.Errorf("got %v, want empty map when peer name is unavailable", got)
		}
	})

	t.Run("with snapshot: peer resolved, permanent failures graced, deduped", func(t *testing.T) {
		dir := t.TempDir()
		boot := `00:00:01.000 INFO app-start Lock file loaded {"peer_name": "vivacious-country", "peer_index": 3}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "vc-3-geth-lighthouse-charon-charon-3.log"), []byte(boot), 0o644); err != nil {
			t.Fatal(err)
		}

		got := countWarmupFailures("test-enclave", dir)
		if v := got[warmupKey{peer: "vivacious-country", duty: "aggregator"}]; v != 2 {
			t.Errorf("aggregator = %v, want 2 (slot 1 via Permanent failure + slot 2 counted once)", v)
		}
		if len(got) != 1 {
			t.Errorf("got %d keys %v, want 1", len(got), got)
		}
	})
}

func TestJoinLogRecords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "continuation lines are merged",
			in: "12:00:00.000 INFO a start\n" +
				"12:00:01.000 WARN b head {\"x\":\n" +
				"1}\n" +
				"12:00:02.000 INFO c done",
			want: []string{
				`12:00:00.000 INFO a start`,
				`12:00:01.000 WARN b head {"x":1}`,
				`12:00:02.000 INFO c done`,
			},
		},
		{
			name: "kurtosis service prefix is recognised",
			in:   "[svc-0] 12:00:00.000 INFO a head\n[svc-0] tail\n",
			want: []string{`[svc-0] 12:00:00.000 INFO a head[svc-0] tail`},
		},
		{
			name: "preamble before the first record is dropped",
			in:   "garbage banner\n12:00:00.000 INFO a x",
			want: []string{`12:00:00.000 INFO a x`},
		},
		{
			name: "unrecognised format falls back to raw lines",
			in:   "no timestamps here\nsecond line",
			want: []string{"no timestamps here", "second line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinLogRecords(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d records %q, want %d %q", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("record %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGitHead(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	var captured []string
	runCommand = func(name string, args ...string) (string, error) {
		captured = append([]string{name}, args...)
		return "abc123def\n", nil
	}
	if got := gitHead("/repo"); got != "abc123def" {
		t.Errorf("gitHead = %q, want trimmed abc123def", got)
	}
	want := []string{"git", "-C", "/repo", "rev-parse", "HEAD"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("command = %v, want %v", captured, want)
	}

	runCommand = func(string, ...string) (string, error) { return "", fmt.Errorf("boom") }
	if got := gitHead("/repo"); got != "" {
		t.Errorf("gitHead on error = %q, want empty", got)
	}
}

func TestRunnerSourceChanged(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()

	mkDiff := func(out string, err error) func(string, ...string) (string, error) {
		return func(name string, args ...string) (string, error) {
			if len(args) > 0 && args[len(args)-1] != "local/runner/" {
				t.Errorf("diff must be scoped to local/runner/, got %v", args)
			}
			return out, err
		}
	}

	runCommand = mkDiff("local/runner/main.go\n", nil)
	if !runnerSourceChanged("/repo", "aaa", "bbb") {
		t.Error("changed runner file must report true")
	}

	runCommand = mkDiff("\n", nil)
	if runnerSourceChanged("/repo", "aaa", "bbb") {
		t.Error("no changed runner files must report false")
	}

	runCommand = mkDiff("", fmt.Errorf("boom"))
	if runnerSourceChanged("/repo", "aaa", "bbb") {
		t.Error("git error must report false (best-effort)")
	}

	// Same or unknown heads never restart, without even invoking git.
	runCommand = func(string, ...string) (string, error) { t.Fatal("must not call git"); return "", nil }
	if runnerSourceChanged("/repo", "aaa", "aaa") || runnerSourceChanged("/repo", "", "bbb") || runnerSourceChanged("/repo", "aaa", "") {
		t.Error("same/empty heads must report false")
	}
}

func TestRestartSelf(t *testing.T) {
	oldExec, oldRun := execFn, runCommand
	defer func() { execFn, runCommand = oldExec, oldRun }()

	var built []string
	runCommand = func(name string, args ...string) (string, error) {
		built = append([]string{name}, args...)
		return "", nil
	}

	var gotArgv0 string
	var gotArgs []string
	execFn = func(argv0 string, argv []string, env []string) error {
		gotArgv0 = argv0
		gotArgs = argv
		return nil
	}

	if err := restartSelf(); err != nil {
		t.Fatalf("restartSelf error: %v", err)
	}

	// The updated source is built to a staged binary first (a flat exec
	// chain: re-execing `go run .` would leak one waiting supervisor per
	// update), in a fresh private temp dir, keeping the basename the
	// start/stop/status.sh pgrep pattern matches.
	if len(built) != 5 || built[1] != "build" || built[2] != "-o" || built[4] != "." {
		t.Fatalf("build command = %v, want <go> build -o <staged> .", built)
	}
	if !strings.HasSuffix(built[0], "/go") && built[0] != "go" {
		t.Errorf("toolchain = %q, want a go binary path", built[0])
	}
	staged := built[3]
	if filepath.Base(staged) != stagedRunnerName {
		t.Errorf("staged basename = %q, want %q", filepath.Base(staged), stagedRunnerName)
	}
	dir := filepath.Dir(staged)
	if !strings.HasPrefix(filepath.Base(dir), stagedRunnerName+"-") {
		t.Errorf("staged dir = %q, want a private per-restart temp dir", dir)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("staged dir perms/err = %v/%v, want 0700", info, err)
	}
	defer os.RemoveAll(dir)

	// The staged binary is exec'd with the original flags preserved.
	if gotArgv0 != staged {
		t.Errorf("argv0 = %q, want %q", gotArgv0, staged)
	}
	want := append([]string{staged}, os.Args[1:]...)
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv = %v, want %v (flags preserved)", gotArgs, want)
	}

	// A failed build must leave the current process running.
	runCommand = func(string, ...string) (string, error) { return "compile error", fmt.Errorf("exit 1") }
	if err := restartSelf(); err == nil {
		t.Error("restartSelf must surface build failures")
	}
}

// TestMaybeRestart pins the mainLoop integration branch: a runner-source
// change triggers the re-exec, an unchanged checkout does not, and an exec
// failure returns normally so the loop continues with the current build.
func TestMaybeRestart(t *testing.T) {
	oldExec, oldRun := execFn, runCommand
	defer func() { execFn, runCommand = oldExec, oldRun }()

	mkGit := func(head, diff string) func(string, ...string) (string, error) {
		return func(name string, args ...string) (string, error) {
			switch {
			case len(args) > 0 && args[len(args)-1] == "HEAD":
				return head + "\n", nil
			case len(args) > 1 && args[1] == "build":
				return "", nil
			default: // git diff
				return diff, nil
			}
		}
	}

	execs := 0
	execFn = func(string, []string, []string) error { execs++; return nil }

	runCommand = mkGit("newhead", "local/runner/main.go\n")
	maybeRestart("/repo", "oldhead")
	if execs != 1 {
		t.Errorf("changed source: execs = %d, want 1", execs)
	}

	runCommand = mkGit("oldhead", "")
	maybeRestart("/repo", "oldhead")
	if execs != 1 {
		t.Errorf("unchanged source: execs = %d, want still 1", execs)
	}

	// Exec failure must not panic or stop the caller.
	execFn = func(string, []string, []string) error { return fmt.Errorf("exec blocked") }
	runCommand = mkGit("newhead", "local/runner/main.go\n")
	maybeRestart("/repo", "oldhead")
}

func TestGoToolchain(t *testing.T) {
	// In any environment able to run this test, at least one candidate (the
	// building toolchain via GOROOT) must resolve.
	goBin, err := goToolchain()
	if err != nil {
		t.Fatalf("goToolchain error: %v", err)
	}
	if !strings.HasSuffix(goBin, "go") {
		t.Errorf("goToolchain = %q, want a go binary path", goBin)
	}
}

func TestPruneStagedRunners(t *testing.T) {
	stale1, err := os.MkdirTemp("", stagedRunnerName+"-*")
	if err != nil {
		t.Fatal(err)
	}
	stale2, err := os.MkdirTemp("", stagedRunnerName+"-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale1, stagedRunnerName), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated, err := os.MkdirTemp("", "unrelated-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(unrelated)

	pruneStagedRunners()

	for _, dir := range []string{stale1, stale2} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			os.RemoveAll(dir)
			t.Errorf("stale staging dir %s must be pruned", dir)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated temp dir must be untouched: %v", err)
	}
}

// TestPostBestEffortRetries pins the report-post retry: a transient failure
// (e.g. a resolver blip) is retried once after a delay, and a failure on
// both attempts is logged but never fatal.
func TestPostBestEffortRetries(t *testing.T) {
	oldPost, oldSleep := httpPost, sleepFn
	defer func() { httpPost, sleepFn = oldPost, oldSleep }()

	var slept []time.Duration
	sleepFn = func(d time.Duration) { slept = append(slept, d) }

	t.Run("first attempt succeeds: no retry", func(t *testing.T) {
		slept = nil
		posts := 0
		httpPost = func(string, []byte) (int, error) { posts++; return 200, nil }
		postBestEffort(config{slackWebhookURL: "http://hook"}, reportData{name: "a-b"})
		if posts != 1 || len(slept) != 0 {
			t.Errorf("posts=%d slept=%v, want 1 post and no sleep", posts, slept)
		}
	})

	t.Run("transient failure: retried once and delivered", func(t *testing.T) {
		slept = nil
		posts := 0
		httpPost = func(string, []byte) (int, error) {
			posts++
			if posts == 1 {
				return 0, fmt.Errorf("dial tcp: lookup slack.com: server misbehaving")
			}
			return 200, nil
		}
		postBestEffort(config{slackWebhookURL: "http://hook"}, reportData{name: "a-b"})
		if posts != 2 {
			t.Errorf("posts = %d, want 2 (one retry)", posts)
		}
		if len(slept) != 1 {
			t.Errorf("slept = %v, want one backoff before the retry", slept)
		}
	})

	t.Run("both attempts fail: logged, not fatal", func(t *testing.T) {
		slept = nil
		posts := 0
		httpPost = func(string, []byte) (int, error) { posts++; return 0, fmt.Errorf("still down") }
		postBestEffort(config{slackWebhookURL: "http://hook"}, reportData{name: "a-b"}) // must not panic
		if posts != 2 {
			t.Errorf("posts = %d, want exactly 2 attempts", posts)
		}
	})
}

// TestPostBestEffortPendingQueue pins the ordered pending queue: reports
// that fail both attempts are persisted, later reports queue behind them
// (never posted ahead), and the first healthy post flushes the backlog in
// run order before the fresh report.
func TestPostBestEffortPendingQueue(t *testing.T) {
	oldPost, oldSleep := httpPost, sleepFn
	defer func() { httpPost, sleepFn = oldPost, oldSleep }()
	sleepFn = func(time.Duration) {}

	dir := t.TempDir()
	cfg := config{slackWebhookURL: "http://hook", statePath: filepath.Join(dir, "state.json")}
	queueFile := filepath.Join(dir, "runner-pending-posts.json")

	readQueue := func(t *testing.T) []pendingPost {
		t.Helper()
		posts, err := loadPendingPosts(queueFile)
		if err != nil {
			t.Fatalf("load queue: %v", err)
		}
		return posts
	}

	// Outage: report A fails both attempts and is queued.
	posts := 0
	httpPost = func(string, []byte) (int, error) { posts++; return 0, fmt.Errorf("dns down") }
	postBestEffort(cfg, reportData{name: "aaa-combo"})
	if posts != 2 {
		t.Errorf("posts = %d, want 2 attempts for the fresh report", posts)
	}
	if q := readQueue(t); len(q) != 1 || q[0].Name != "aaa-combo" {
		t.Fatalf("queue = %+v, want [aaa-combo]", q)
	}

	// Still down: report B queues BEHIND A after one flush attempt; B itself
	// is not attempted (order preservation).
	posts = 0
	postBestEffort(cfg, reportData{name: "bbb-combo"})
	if posts != 1 {
		t.Errorf("posts = %d, want exactly 1 (flush attempt for A only)", posts)
	}
	if q := readQueue(t); len(q) != 2 || q[0].Name != "aaa-combo" || q[1].Name != "bbb-combo" {
		t.Fatalf("queue = %+v, want [aaa-combo bbb-combo]", q)
	}

	// Recovery: report C's post first flushes A then B, then posts C.
	var sent []string
	httpPost = func(u string, body []byte) (int, error) {
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(body, &payload)
		sent = append(sent, payload.Text)
		return 200, nil
	}
	postBestEffort(cfg, reportData{name: "ccc-combo"})
	if len(sent) != 3 || !strings.Contains(sent[0], "aaa-combo") || !strings.Contains(sent[1], "bbb-combo") || !strings.Contains(sent[2], "ccc-combo") {
		t.Fatalf("sent order = %v, want backlog A, B then fresh C", sent)
	}
	if q := readQueue(t); len(q) != 0 {
		t.Errorf("queue after recovery = %+v, want empty", q)
	}
}

// TestPendingPostsCap pins the queue bound: the oldest entries are dropped
// beyond maxPendingPosts so a permanently broken webhook cannot grow the
// file forever.
func TestPendingPostsCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "q.json")

	var posts []pendingPost
	for i := range maxPendingPosts + 5 {
		posts = append(posts, pendingPost{Name: fmt.Sprintf("combo-%03d", i)})
	}
	if err := savePendingPosts(path, posts); err != nil {
		t.Fatal(err)
	}
	got, err := loadPendingPosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxPendingPosts {
		t.Fatalf("len = %d, want capped at %d", len(got), maxPendingPosts)
	}
	if got[0].Name != "combo-005" || got[len(got)-1].Name != fmt.Sprintf("combo-%03d", maxPendingPosts+4) {
		t.Errorf("cap must drop the OLDEST entries, got first=%s last=%s", got[0].Name, got[len(got)-1].Name)
	}
}

// TestPendingQueueCrashAndCorruption pins two delivery-robustness rules:
// each successful flush is persisted immediately (a crash mid-flush must
// not re-send already-delivered reports), and a queued entry with malformed
// blocks degrades to a text-only post instead of stalling the queue.
func TestPendingQueueCrashAndCorruption(t *testing.T) {
	oldPost, oldSleep := httpPost, sleepFn
	defer func() { httpPost, sleepFn = oldPost, oldSleep }()
	sleepFn = func(time.Duration) {}

	t.Run("crash mid-flush does not resend delivered reports", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config{slackWebhookURL: "http://hook", statePath: filepath.Join(dir, "state.json")}
		queueFile := filepath.Join(dir, "runner-pending-posts.json")
		if err := savePendingPosts(queueFile, []pendingPost{{Name: "aaa"}, {Name: "bbb"}}); err != nil {
			t.Fatal(err)
		}

		posts := 0
		httpPost = func(string, []byte) (int, error) {
			posts++
			if posts == 2 {
				panic("simulated crash after first delivery")
			}
			return 200, nil
		}
		postBestEffort(cfg, reportData{name: "ccc"}) // recover() must swallow the panic

		got, err := loadPendingPosts(queueFile)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "bbb" {
			t.Fatalf("queue after crash = %+v, want only [bbb]: aaa was delivered and must not be re-sent", got)
		}
	})

	t.Run("malformed blocks degrade to text-only, queue keeps draining", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config{slackWebhookURL: "http://hook", statePath: filepath.Join(dir, "state.json")}
		queueFile := filepath.Join(dir, "runner-pending-posts.json")
		// Valid JSON of the wrong shape: whole-file invalidity is already
		// handled by the queue-unreadable path; per-entry corruption
		// surfaces as blocks that don't decode into []map[string]any.
		bad := []pendingPost{{Name: "aaa", Text: "aaa report", Blocks: json.RawMessage(`"not-an-array"`)}}
		if err := savePendingPosts(queueFile, bad); err != nil {
			t.Fatal(err)
		}

		var sent []string
		httpPost = func(u string, body []byte) (int, error) {
			var payload struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(body, &payload)
			sent = append(sent, payload.Text)
			return 200, nil
		}
		postBestEffort(cfg, reportData{name: "bbb"})

		if len(sent) != 2 || sent[0] != "aaa report" {
			t.Fatalf("sent = %v, want the malformed entry delivered text-only, then the fresh report", sent)
		}
		if got, _ := loadPendingPosts(queueFile); len(got) != 0 {
			t.Errorf("queue = %+v, want empty", got)
		}
	})
}
