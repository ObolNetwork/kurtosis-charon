package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		t.Setenv("CYCLER_WARMUP_MINUTES", "")
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
		if cfg.runMinutes != 90 || cfg.warmupMinutes != 15 {
			t.Errorf("runMinutes/warmupMinutes = %d/%d, want 90/15", cfg.runMinutes, cfg.warmupMinutes)
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
	want := []string{"kurtosis", "run", "--enclave", "c1-teku-prysm", "pkg@ref", "--args-file", "/tmp/args.yaml"}
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
		runMinutes: 1, warmupMinutes: 0, startupDeadlineMinutes: 1, sampleIntervalS: 1,
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
		runMinutes: 1, warmupMinutes: 0, startupDeadlineMinutes: 1, sampleIntervalS: 1,
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
		runMinutes: 1, warmupMinutes: 0, startupDeadlineMinutes: 1, sampleIntervalS: 1,
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
		runMinutes: 1, warmupMinutes: 0, startupDeadlineMinutes: 1, sampleIntervalS: 1,
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
		runMinutes: 2, warmupMinutes: 1, startupDeadlineMinutes: 1, sampleIntervalS: 1,
	}
	data := runOne(cfg, paramFile, "teku-prysm", 1)
	if data.status != "ok" {
		t.Fatalf("status = %q, want ok (errMsg=%q)", data.status, data.errMsg)
	}
	if data.clusterName != "kurtosis-teku-prysm" {
		t.Errorf("clusterName = %q, want kurtosis-teku-prysm", data.clusterName)
	}
	windowS := cfg.runMinutes*60 - cfg.warmupMinutes*60
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
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, hostStats{})
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
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, hostStats{})
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
	data, err := collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "ok" {
		t.Errorf("status at 99.9%% pct = %q, want ok", data.status)
	}

	httpGet = mkResp(95)
	data, err = collectReport("http://x", "teku-prysm", "kurtosis-teku-prysm", 1, 60, hostStats{})
	if err != nil {
		t.Fatalf("collectReport error: %v", err)
	}
	if data.status != "degraded" {
		t.Errorf("status at 95%% pct = %q, want degraded", data.status)
	}
}
