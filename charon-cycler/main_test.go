package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCycleIs36CLMajor(t *testing.T) {
	if len(cycle) != 36 {
		t.Fatalf("len(cycle) = %d, want 36", len(cycle))
	}
	if cycle[0] != (combo{"lighthouse", "lighthouse"}) {
		t.Errorf("cycle[0] = %+v, want lighthouse/lighthouse", cycle[0])
	}
	if cycle[6] != (combo{"lodestar", "lighthouse"}) {
		t.Errorf("cycle[6] = %+v, want lodestar/lighthouse (CL-major rollover)", cycle[6])
	}
	if cycle[35] != (combo{"grandine", "vouch"}) {
		t.Errorf("cycle[35] = %+v, want grandine/vouch", cycle[35])
	}
}

func TestNamesAndEnclave(t *testing.T) {
	c := combo{cl: "teku", vc: "prysm"}
	if got := c.name(); got != "teku-prysm" {
		t.Errorf("name() = %q, want teku-prysm", got)
	}
	if got := c.clusterName(); got != "kurtosis-teku-prysm" {
		t.Errorf("clusterName() = %q, want kurtosis-teku-prysm", got)
	}
	if got := enclaveName(3, c); got != "c3-teku-prysm" {
		t.Errorf("enclaveName(3, c) = %q, want c3-teku-prysm", got)
	}
}

func TestStateRoundTripAndAdvanceWrap(t *testing.T) {
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

	// advance: 34 -> 35 (no wrap)
	adv := state{Cycle: 0, NextIndex: 34}
	adv.advance()
	if adv.Cycle != 0 || adv.NextIndex != 35 {
		t.Errorf("advance from 34: got (cycle=%d, idx=%d), want (0, 35)", adv.Cycle, adv.NextIndex)
	}
	// advance: 35 -> wrap to (cycle+1, idx 0)
	adv.advance()
	if adv.Cycle != 1 || adv.NextIndex != 0 {
		t.Errorf("advance from 35: got (cycle=%d, idx=%d), want (1, 0)", adv.Cycle, adv.NextIndex)
	}
}

func TestSelectNextCombo(t *testing.T) {
	old := readOverride
	defer func() { readOverride = old }()

	t.Run("no override selects from cycle", func(t *testing.T) {
		readOverride = func() *combo { return nil }
		s := state{NextIndex: 6}
		got, origin := selectNextCombo(s)
		if origin != "cycle" {
			t.Errorf("origin = %q, want cycle", origin)
		}
		if got != (combo{"lodestar", "lighthouse"}) {
			t.Errorf("combo = %+v, want lodestar/lighthouse", got)
		}
	})

	t.Run("override takes priority without advancing", func(t *testing.T) {
		readOverride = func() *combo { return &combo{cl: "prysm", vc: "teku"} }
		s := state{NextIndex: 6}
		got, origin := selectNextCombo(s)
		if origin != "override" {
			t.Errorf("origin = %q, want override", origin)
		}
		if got != (combo{"prysm", "teku"}) {
			t.Errorf("combo = %+v, want prysm/teku", got)
		}
		if s.NextIndex != 6 {
			t.Errorf("state.NextIndex mutated to %d, want unchanged 6", s.NextIndex)
		}
	})

	t.Run("default readOverride is inert", func(t *testing.T) {
		readOverride = old
		if got := readOverride(); got != nil {
			t.Errorf("default readOverride() = %+v, want nil", got)
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
	}
	for _, c := range cases {
		if got := computeBackoff(c.failures, c.base, c.cap); got != c.want {
			t.Errorf("computeBackoff(%d,%d,%d) = %d, want %d", c.failures, c.base, c.cap, got, c.want)
		}
	}
}

func testImages() images {
	return images{
		Charon:      "obolnetwork/charon:next",
		EL:          "ethereum/client-go:v1.17.4",
		BootstrapCL: "sigp/lighthouse:v8.2.1",
		CL: map[string]string{
			"lighthouse": "sigp/lighthouse:v8.2.1",
			"lodestar":   "chainsafe/lodestar:v1.45.0",
			"nimbus":     "statusim/nimbus-eth2:multiarch-v26.7.0",
			"teku":       "consensys/teku:26.7.1",
			"prysm":      "gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8",
			"grandine":   "sifrai/grandine:2.0.5",
		},
		VC: map[string]string{
			"lighthouse": "sigp/lighthouse:v8.2.1",
			"lodestar":   "chainsafe/lodestar:v1.45.0",
			"nimbus":     "statusim/nimbus-validator-client:multiarch-v26.7.0",
			"teku":       "consensys/teku:26.7.1",
			"prysm":      "gcr.io/prysmaticlabs/prysm/validator:v7.1.8",
			"vouch":      "attestant/vouch:1.13.1",
		},
	}
}

func TestBuildArgsFile(t *testing.T) {
	im := testImages()

	t.Run("four nodes and token substituted", func(t *testing.T) {
		y := buildArgsFile(im, combo{cl: "lighthouse", vc: "teku"}, "SECRET123", 4)
		if !strings.Contains(y, "charon_node_count: 4") {
			t.Errorf("missing charon_node_count: 4")
		}
		if strings.Contains(y, "$PROMETHEUS_REMOTE_WRITE_TOKEN") {
			t.Errorf("token placeholder not substituted")
		}
		if !strings.Contains(y, "SECRET123") {
			t.Errorf("token not present in output")
		}
	})

	t.Run("nimbus vc gets json_requests", func(t *testing.T) {
		y := buildArgsFile(im, combo{cl: "teku", vc: "nimbus"}, "tok", 4)
		if !strings.Contains(y, "vc_extra_env_vars:") || !strings.Contains(y, "CHARON_FEATURE_SET_ENABLE: json_requests") {
			t.Errorf("missing nimbus vc_extra_env_vars block, got:\n%s", y)
		}
	})

	t.Run("non-nimbus vc has no json_requests", func(t *testing.T) {
		y := buildArgsFile(im, combo{cl: "teku", vc: "prysm"}, "tok", 4)
		if strings.Contains(y, "json_requests") {
			t.Errorf("unexpected vc_extra_env_vars for non-nimbus vc")
		}
	})

	t.Run("teku pin present and charon_vc set", func(t *testing.T) {
		y := buildArgsFile(im, combo{cl: "prysm", vc: "vouch"}, "tok", 4)
		if !strings.Contains(y, im.CL["prysm"]) {
			t.Errorf("missing prysm CL pin")
		}
		if !strings.Contains(y, "charon_vc: vouch") {
			t.Errorf("missing charon_vc: vouch")
		}
		y2 := buildArgsFile(im, combo{cl: "teku", vc: "prysm"}, "tok", 4)
		if !strings.Contains(y2, im.CL["teku"]) {
			t.Errorf("missing teku CL pin: %s", im.CL["teku"])
		}
	})

	t.Run("storage_tsdb_retention_time kept at 3h", func(t *testing.T) {
		y := buildArgsFile(im, combo{cl: "lighthouse", vc: "lighthouse"}, "tok", 4)
		if !strings.Contains(y, "storage_tsdb_retention_time: 3h") {
			t.Errorf("missing storage_tsdb_retention_time: 3h")
		}
	})
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
	if !strings.Contains(promCharonMemPeak("kurtosis-a-b", 5400), "process_resident_memory_bytes") {
		t.Errorf("promCharonMemPeak missing metric name")
	}
	if !strings.Contains(promCharonCPUPeak("kurtosis-a-b", 5400), "process_cpu_seconds_total") {
		t.Errorf("promCharonCPUPeak missing metric name")
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

func TestLoadConfig(t *testing.T) {
	t.Run("required present -> defaults applied", func(t *testing.T) {
		t.Setenv("SLACK_WEBHOOK_URL", "http://hook")
		t.Setenv("REPO_PATH", "/srv/kurtosis-charon")
		t.Setenv("STATE_PATH", "/var/lib/cycler/state.json")
		t.Setenv("MONITORING_TOKEN", "")
		t.Setenv("PACKAGE_REF", "")
		t.Setenv("RUN_MINUTES", "")
		t.Setenv("WARMUP_MINUTES", "")
		t.Setenv("STARTUP_DEADLINE_MINUTES", "")
		t.Setenv("SAMPLE_INTERVAL_S", "")
		t.Setenv("INTER_RUN_BACKOFF_S", "")
		t.Setenv("MAX_BACKOFF_S", "")

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

	t.Run("overrides applied", func(t *testing.T) {
		t.Setenv("SLACK_WEBHOOK_URL", "h")
		t.Setenv("REPO_PATH", "r")
		t.Setenv("STATE_PATH", "st")
		t.Setenv("RUN_MINUTES", "30")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig error: %v", err)
		}
		if cfg.runMinutes != 30 {
			t.Errorf("runMinutes = %d, want 30", cfg.runMinutes)
		}
	})

	t.Run("missing required raises", func(t *testing.T) {
		t.Setenv("SLACK_WEBHOOK_URL", "")
		t.Setenv("REPO_PATH", "r")
		t.Setenv("STATE_PATH", "s")

		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error when slackWebhookURL missing")
		}
	})
}

func TestBuildBlocksStatuses(t *testing.T) {
	base := func(status string) reportData {
		mem := 512.0 * 1024 * 1024
		cpu := 1.4
		return reportData{
			combo:       combo{cl: "teku", vc: "prysm"},
			cycle:       3,
			status:      status,
			clImage:     "consensys/teku:26.7.1",
			vcImage:     "gcr.io/.../validator:v7.1.8",
			charonImage: "obolnetwork/charon:next",
			window:      "12:00-13:30 UTC",
			worst: &worstNode{peer: "1", duties: []dutyResult{
				{duty: "attester", expected: 780, success: 780},
				{duty: "aggregator", expected: 150, success: 130},
			}},
			charonMemBytes: &mem,
			charonCPU:      &cpu,
			host:           &hostStats{cpuAvg: 30, cpuPeak: 82, memAvg: 8e9, memPeak: 9e9, memTotal: 16e9},
			health:         []healthCheck{{name: "high-inclusion-delay", severity: "warning", firingNow: false}},
		}
	}

	t.Run("ok status renders duty ratios and worst peer", func(t *testing.T) {
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
		if !strings.Contains(dump, "high-inclusion-delay") {
			t.Errorf("missing health check name in %s", dump)
		}
		text := buildText(base("ok"))
		if !strings.Contains(text, "teku") || !strings.Contains(text, "prysm") || !strings.Contains(strings.ToLower(text), "cycle 3") {
			t.Errorf("buildText = %q", text)
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
			combo:       combo{cl: "teku", vc: "prysm"},
			cycle:       1,
			status:      "failed",
			clImage:     "cl-image",
			vcImage:     "vc-image",
			charonImage: "charon-image",
			window:      "-",
			errMsg:      "launch failed: boom",
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

func TestLoadImages(t *testing.T) {
	dir := t.TempDir()
	content := `{
		"charon": "obolnetwork/charon:next",
		"el": "ethereum/client-go:v1.17.4",
		"bootstrap_cl": "sigp/lighthouse:v8.2.1",
		"cl": {"teku": "consensys/teku:26.7.1"},
		"vc": {"vouch": "attestant/vouch:1.13.1"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "images.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write images.json: %v", err)
	}
	im, err := loadImages(dir)
	if err != nil {
		t.Fatalf("loadImages error: %v", err)
	}
	if im.Charon != "obolnetwork/charon:next" {
		t.Errorf("Charon = %q", im.Charon)
	}
	if im.EL != "ethereum/client-go:v1.17.4" {
		t.Errorf("EL = %q", im.EL)
	}
	if im.BootstrapCL != "sigp/lighthouse:v8.2.1" {
		t.Errorf("BootstrapCL = %q", im.BootstrapCL)
	}
	if im.CL["teku"] != "consensys/teku:26.7.1" {
		t.Errorf("CL[teku] = %q", im.CL["teku"])
	}
	if im.VC["vouch"] != "attestant/vouch:1.13.1" {
		t.Errorf("VC[vouch] = %q", im.VC["vouch"])
	}
}
