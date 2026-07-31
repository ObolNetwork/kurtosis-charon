// Command dv-cycler cycles the ethereum-package devnet through every static
// args-file in network-params/ (one per CL x VC pairing behind the DV client),
// sampling metrics and posting a Slack report for each run. This file
// (main.go) is a script-like Go port of the original Python cycler package:
// no sub-packages, no interfaces, just plain data records and package-level
// functions/vars.
//
// Unlike the earlier code-generated-args-file design, the cycler no longer
// builds args-files itself: network-params/*.yaml are static, committed
// files with pins inlined and a literal $PROMETHEUS_REMOTE_WRITE_TOKEN
// placeholder. The cycler enumerates whatever *.yaml files exist in that
// directory on every loop iteration, runs each one in turn, and picks up
// newly added files automatically (no restart needed).
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Config loading: config, loadConfig.
//
// loadConfig is a no-argument `func loadConfig() (config, error)` that reads
// from env vars + flags rather than a config file -- a deliberate design
// choice for this port, not something derived from prior behavior.
// ---------------------------------------------------------------------------

type config struct {
	slackWebhookURL        string
	repoPath               string
	statePath              string
	paramsDir              string
	monitoringToken        string
	packageRef             string
	runMinutes             int
	warmupMinutes          int
	startupDeadlineMinutes int
	sampleIntervalS        int
	interRunBackoffS       int
	maxBackoffS            int
	logDir                 string
	slackBotToken          string
	slackChannelID         string
	resultsPath            string
	summaryMention         string
}

// dotEnvPath returns the .env file to load: $CYCLER_ENV_FILE if set, else
// ".env" in the current working directory (under systemd that's the unit's
// WorkingDirectory; under a manual `go run .` it's the module dir).
func dotEnvPath() string {
	if p := os.Getenv("CYCLER_ENV_FILE"); p != "" {
		return p
	}
	return ".env"
}

// loadDotEnv reads a simple KEY=VALUE .env file and sets any variable NOT
// already present in the environment, so real env vars (and --flags) still
// win over the file. A missing file is not an error. Blank lines and
// #-comments are skipped; a leading "export " and matching surrounding single
// or double quotes around the value are stripped.
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' ||
			val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); !ok {
			os.Setenv(key, val)
		}
	}
	return nil
}

// envInt reads an integer env var (CYCLER_<name>) into *dst if present and
// non-empty and parses cleanly; otherwise dst is left untouched.
func envInt(name string, dst *int) {
	v, ok := os.LookupEnv("CYCLER_" + name)
	if !ok || v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil {
		*dst = n
	}
}

// envStr reads a string env var (CYCLER_<name>) into *dst if present and
// non-empty.
func envStr(name string, dst *string) {
	if v, ok := os.LookupEnv("CYCLER_" + name); ok && v != "" {
		*dst = v
	}
}

// applyFlags does a minimal manual scan for --name=value flags matching our
// known config keys. It deliberately ignores anything it doesn't recognize
// (including `go test`'s own -test.* flags) rather than erroring, since
// os.Args under `go test` contains flags this program doesn't own.
func applyFlags(cfg *config, args []string) {
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		kv := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := kv[0], kv[1]
		switch key {
		case "slack-webhook-url":
			cfg.slackWebhookURL = val
		case "repo-path":
			cfg.repoPath = val
		case "state-path":
			cfg.statePath = val
		case "params-dir":
			cfg.paramsDir = val
		case "monitoring-token":
			cfg.monitoringToken = val
		case "package-ref":
			cfg.packageRef = val
		case "log-dir":
			cfg.logDir = val
		case "slack-bot-token":
			cfg.slackBotToken = val
		case "slack-channel-id":
			cfg.slackChannelID = val
		case "results-path":
			cfg.resultsPath = val
		case "summary-mention":
			cfg.summaryMention = val
		case "run-minutes":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.runMinutes = n
			}
		case "warmup-minutes":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.warmupMinutes = n
			}
		case "startup-deadline-minutes":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.startupDeadlineMinutes = n
			}
		case "sample-interval-s":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.sampleIntervalS = n
			}
		case "inter-run-backoff-s":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.interRunBackoffS = n
			}
		case "max-backoff-s":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.maxBackoffS = n
			}
		}
	}
}

func loadConfig() (config, error) {
	cfg := config{
		packageRef:             "github.com/ObolNetwork/ethereum-package@charon",
		runMinutes:             90,
		warmupMinutes:          15,
		startupDeadlineMinutes: 25,
		sampleIntervalS:        15,
		interRunBackoffS:       30,
		maxBackoffS:            900,
	}

	envStr("SLACK_WEBHOOK_URL", &cfg.slackWebhookURL)
	envStr("REPO_PATH", &cfg.repoPath)
	envStr("STATE_PATH", &cfg.statePath)
	envStr("PARAMS_DIR", &cfg.paramsDir)
	envStr("MONITORING_TOKEN", &cfg.monitoringToken)
	envStr("PACKAGE_REF", &cfg.packageRef)
	envStr("LOG_DIR", &cfg.logDir)
	envStr("SLACK_BOT_TOKEN", &cfg.slackBotToken)
	envStr("SLACK_CHANNEL_ID", &cfg.slackChannelID)
	envStr("RESULTS_PATH", &cfg.resultsPath)
	envStr("SUMMARY_MENTION", &cfg.summaryMention)
	envInt("RUN_MINUTES", &cfg.runMinutes)
	envInt("WARMUP_MINUTES", &cfg.warmupMinutes)
	envInt("STARTUP_DEADLINE_MINUTES", &cfg.startupDeadlineMinutes)
	envInt("SAMPLE_INTERVAL_S", &cfg.sampleIntervalS)
	envInt("INTER_RUN_BACKOFF_S", &cfg.interRunBackoffS)
	envInt("MAX_BACKOFF_S", &cfg.maxBackoffS)

	if len(os.Args) > 1 {
		applyFlags(&cfg, os.Args[1:])
	}

	// paramsDir's default is derived from repoPath, so it's computed after
	// both env vars and flags have had a chance to set repoPath (whichever
	// value wins there is the one the default is based on) -- but only if
	// paramsDir itself wasn't explicitly overridden.
	if cfg.paramsDir == "" && cfg.repoPath != "" {
		cfg.paramsDir = filepath.Join(cfg.repoPath, "dv-cycler", "network-params")
	}

	if cfg.logDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "."
		}
		cfg.logDir = filepath.Join(home, "dv-cycler-logs")
	}

	var missing []string
	if cfg.slackWebhookURL == "" {
		missing = append(missing, "slack_webhook_url (CYCLER_SLACK_WEBHOOK_URL / --slack-webhook-url)")
	}
	if cfg.repoPath == "" {
		missing = append(missing, "repo_path (CYCLER_REPO_PATH / --repo-path)")
	}
	if cfg.statePath == "" {
		missing = append(missing, "state_path (CYCLER_STATE_PATH / --state-path)")
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}

	// resultsPath defaults next to the state file (statePath is guaranteed set
	// by the required-config check above).
	if cfg.resultsPath == "" {
		cfg.resultsPath = filepath.Join(filepath.Dir(cfg.statePath), "cycler-results.json")
	}
	return cfg, nil
}

// ---------------------------------------------------------------------------
// Param files: paramFiles, paramStem.
// ---------------------------------------------------------------------------

// paramFiles returns the sorted (lexical) absolute paths of *.yaml files
// directly in dir (non-recursive) -- subdirectories and non-.yaml entries
// are ignored. Re-running this on every mainLoop iteration is what lets a
// newly dropped-in file get picked up without a restart.
func paramFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	paths := make([]string, 0, len(names))
	for _, n := range names {
		abs, err := filepath.Abs(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		paths = append(paths, abs)
	}
	return paths, nil
}

// paramStem returns path's filename without its .yaml extension, e.g.
// "/a/b/lighthouse-teku.yaml" -> "lighthouse-teku". This is the raw,
// human-readable name used for Slack reports and passed into enclaveName
// (which does its own separate sanitization for the enclave name itself).
func paramStem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".yaml")
}

// ---------------------------------------------------------------------------
// enclaveName.
// ---------------------------------------------------------------------------

// sanitizeEnclaveStem lowercases stem and replaces any character outside
// [a-z0-9-] with '-', since it feeds a Kurtosis enclave name (which has its
// own naming restrictions) -- unlike the human-readable report name, this
// value must be enclave-safe regardless of what the param file happens to be
// named.
func sanitizeEnclaveStem(stem string) string {
	stem = strings.ToLower(stem)
	var b strings.Builder
	for _, r := range stem {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func enclaveName(cycleNum int, stem string) string {
	return fmt.Sprintf("c%d-%s", cycleNum, sanitizeEnclaveStem(stem))
}

// ---------------------------------------------------------------------------
// Run state: state, loadState, save, advance.
// ---------------------------------------------------------------------------

type state struct {
	Cycle          int    `json:"cycle"`
	NextIndex      int    `json:"next_index"`
	CurrentEnclave string `json:"current_enclave"`
}

func loadState(path string) (state, error) {
	data, err := readFileFn(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state{}, nil
		}
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, err
	}
	// A corrupt/hand-edited/forward-incompatible state file with a negative
	// next_index or cycle would misbehave downstream. There's no fixed
	// upper bound to check anymore (the file count is dynamic, checked/
	// clamped in mainLoop instead), but negative values are never valid.
	if s.NextIndex < 0 || s.Cycle < 0 {
		fmt.Fprintf(os.Stderr,
			"dv-cycler: state file %s has a negative next_index=%d or cycle=%d; starting fresh\n",
			path, s.NextIndex, s.Cycle)
		return state{}, nil
	}
	return s, nil
}

func (s *state) save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// advance moves to the next index. Wrapping back to index 0 and bumping
// Cycle happens in mainLoop instead, since it depends on the current
// (dynamic) count of param files, which advance itself has no access to.
func (s *state) advance() {
	s.NextIndex++
}

// ---------------------------------------------------------------------------
// Inter-run backoff.
// ---------------------------------------------------------------------------

// computeBackoff returns min(cap, base*2**n). Unlike an arbitrary-precision
// integer, for which base*2**n could never overflow, Go's int can.
// consecutiveFailures grows unbounded across a sustained outage (mainLoop
// has no ceiling on it), so for large n "1 << uint(n)"
// eventually overflows and the multiply can wrap to a negative or zero
// result -- which would make sleepFn return instantly, hot-spinning the
// loop against git/kurtosis. Guard against that in two layers: short-circuit
// before the shift once n is large enough that the result must already
// exceed cap (base=30 reaches cap=900 by n=5, so n>=30 is a generous, safe
// threshold with headroom to spare before any overflow could occur), and
// double-check the computed value afterwards in case some other base/cap
// combination could still overflow.
func computeBackoff(consecutiveFailures, base, cap int) int {
	if consecutiveFailures < 0 {
		consecutiveFailures = 0
	}
	if consecutiveFailures >= 30 {
		return cap
	}
	v := base * (1 << uint(consecutiveFailures))
	if v <= 0 || v > cap {
		return cap
	}
	return v
}

// ---------------------------------------------------------------------------
// PromQL builders: promDutyExpected, promDutySuccess, promDVMemPeak,
// promDVCPUPeak, promHealthFired, promHealthFiringNow.
// ---------------------------------------------------------------------------

func promSelector(clusterName string) string {
	return fmt.Sprintf(`cluster_name="%s"`, clusterName)
}

func promDutyExpected(clusterName string, windowS int) string {
	return fmt.Sprintf("sum(increase(core_tracker_expect_duties_total{%s}[%ds])) by (duty, cluster_peer)",
		promSelector(clusterName), windowS)
}

func promDutySuccess(clusterName string, windowS int) string {
	return fmt.Sprintf("sum(increase(core_tracker_success_duties_total{%s}[%ds])) by (duty, cluster_peer)",
		promSelector(clusterName), windowS)
}

func promDVMemPeak(clusterName string, windowS int) string {
	return fmt.Sprintf("max(max_over_time(process_resident_memory_bytes{%s}[%ds])) by (cluster_peer)",
		promSelector(clusterName), windowS)
}

func promDVCPUPeak(clusterName string, windowS int) string {
	return fmt.Sprintf("max(max_over_time(rate(process_cpu_seconds_total{%s}[1m])[%ds:1m])) by (cluster_peer)",
		promSelector(clusterName), windowS)
}

func promHealthFired(clusterName string, windowS int) string {
	return fmt.Sprintf("max_over_time(app_health_checks{%s}[%ds]) > 0", promSelector(clusterName), windowS)
}

func promHealthFiringNow(clusterName string) string {
	return fmt.Sprintf("app_health_checks{%s} == 1", promSelector(clusterName))
}

// promClusterNameGroups is the PromQL used to discover the single charon
// cluster present in an enclave: app_version is emitted by every app
// (including the bootstrap lighthouse VCs), but grouping by cluster_name
// collapses that down to the distinct cluster_name label values seen.
const promClusterNameGroups = "group by (cluster_name) (app_version)"

// ---------------------------------------------------------------------------
// Metrics processing: sample, dutyResult, worstNode, healthCheck,
// selectWorstNode, maxValue, parseHealth.
// ---------------------------------------------------------------------------

type sample struct {
	labels map[string]string
	value  float64
}

type dutyResult struct {
	duty              string
	expected, success float64
}

func (d dutyResult) pct() float64 {
	if d.expected == 0 {
		return 0
	}
	return 100 * d.success / d.expected
}

type worstNode struct {
	peer   string
	duties []dutyResult
}

func selectWorstNode(expected, success []sample) (worstNode, bool) {
	peers := map[string]bool{}
	for _, sm := range expected {
		if p, ok := sm.labels["cluster_peer"]; ok && p != "" {
			peers[p] = true
		}
	}
	for _, sm := range success {
		if p, ok := sm.labels["cluster_peer"]; ok && p != "" {
			peers[p] = true
		}
	}
	if len(peers) == 0 {
		return worstNode{}, false
	}

	totalSuccess := map[string]float64{}
	for p := range peers {
		totalSuccess[p] = 0
	}
	for _, sm := range success {
		p, ok := sm.labels["cluster_peer"]
		if !ok || p == "" {
			continue
		}
		totalSuccess[p] += sm.value
	}

	var worst string
	first := true
	for p := range peers {
		if first || totalSuccess[p] < totalSuccess[worst] || (totalSuccess[p] == totalSuccess[worst] && p < worst) {
			worst = p
			first = false
		}
	}

	type kv struct{ expected, success float64 }
	dutiesMap := map[string]*kv{}
	for _, sm := range expected {
		p := sm.labels["cluster_peer"]
		d, ok := sm.labels["duty"]
		if p != worst || !ok || d == "" {
			continue
		}
		if _, exists := dutiesMap[d]; !exists {
			dutiesMap[d] = &kv{}
		}
		dutiesMap[d].expected = sm.value
	}
	for _, sm := range success {
		p := sm.labels["cluster_peer"]
		d, ok := sm.labels["duty"]
		if p != worst || !ok || d == "" {
			continue
		}
		if _, exists := dutiesMap[d]; !exists {
			dutiesMap[d] = &kv{}
		}
		dutiesMap[d].success = sm.value
	}

	dutyNames := make([]string, 0, len(dutiesMap))
	for d := range dutiesMap {
		dutyNames = append(dutyNames, d)
	}
	sort.Strings(dutyNames)

	results := make([]dutyResult, 0, len(dutyNames))
	for _, d := range dutyNames {
		v := dutiesMap[d]
		// Drop duties with no expected occurrences in the window (e.g. exit,
		// info_sync, signature, builder_*). Their 0/0 ratio is not a real
		// signal, and since pct() reports 0% for them, keeping them would both
		// clutter the report and wrongly trip the degraded check. A duty with
		// expected>0 but success=0 (a genuine miss) is kept.
		if v.expected == 0 {
			continue
		}
		results = append(results, dutyResult{duty: d, expected: v.expected, success: v.success})
	}

	return worstNode{peer: worst, duties: results}, true
}

func maxValue(samples []sample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	m := samples[0].value
	for _, sm := range samples[1:] {
		if sm.value > m {
			m = sm.value
		}
	}
	return m, true
}

type healthCheck struct {
	name, severity string
	firingNow      bool
}

func parseHealth(fired, firingNow []sample) []healthCheck {
	now := map[[2]string]bool{}
	for _, sm := range firingNow {
		now[[2]string{sm.labels["name"], sm.labels["severity"]}] = true
	}
	checks := make([]healthCheck, 0, len(fired))
	for _, sm := range fired {
		key := [2]string{sm.labels["name"], sm.labels["severity"]}
		checks = append(checks, healthCheck{name: key[0], severity: key[1], firingNow: now[key]})
	}
	return checks
}

// ---------------------------------------------------------------------------
// /proc parsing: parseCPULine, cpuPercent, parseMeminfo.
// ---------------------------------------------------------------------------

func parseCPULine(text string) (busy, total float64) {
	lines := strings.SplitN(text, "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) > 0 {
		fields = fields[1:] // drop the "cpu" label
	}
	nums := make([]float64, len(fields))
	for i, f := range fields {
		v, _ := strconv.ParseFloat(f, 64)
		nums[i] = v
	}
	for _, n := range nums {
		total += n
	}
	idle := 0.0
	if len(nums) > 3 {
		idle = nums[3]
	}
	if len(nums) > 4 {
		idle += nums[4]
	}
	busy = total - idle
	return busy, total
}

func cpuPercent(prev, cur [2]float64) float64 {
	busy := cur[0] - prev[0]
	total := cur[1] - prev[1]
	if total <= 0 {
		return 0
	}
	return 100 * busy / total
}

func parseMeminfo(text string) (used, total float64) {
	vals := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		rest := strings.TrimSpace(line[idx+1:])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		vals[key] = v * 1024 // kB -> bytes
	}
	total = vals["MemTotal"]
	avail := vals["MemAvailable"] // zero value if absent, matching Python's .get(...,0.0)
	used = total - avail
	return used, total
}

// ---------------------------------------------------------------------------
// Slack report building: hostStats, reportData, buildText, buildBlocks.
// ---------------------------------------------------------------------------

type hostStats struct {
	cpuAvg, cpuPeak, memAvg, memPeak, memTotal float64
}

type reportData struct {
	name        string
	clusterName string
	cycle       int
	status      string
	window      string
	worst       *worstNode
	dvMemBytes  *float64
	dvCPU       *float64
	host        *hostStats
	health      []healthCheck
	errMsg      string

	logArchivePath string // local path to the captured-logs tarball (failing runs)
	logExcerpt     string // short excerpt (Charon error lines) for the Slack message
}

var statusEmoji = map[string]string{"ok": "✅", "degraded": "⚠️", "failed": "❌"}

func buildText(d reportData) string {
	e := statusEmoji[d.status]
	return fmt.Sprintf("%s %s · cycle %d · %s", e, d.name, d.cycle, d.status)
}

func gbf(x float64) string {
	return fmt.Sprintf("%.2f GB", x/1e9)
}

func gb(x *float64) string {
	if x == nil {
		return "n/a"
	}
	return gbf(*x)
}

func dutiesMD(wn *worstNode) string {
	if wn == nil || len(wn.duties) == 0 {
		return "_no duty data_"
	}
	lines := []string{fmt.Sprintf("*Duties (worst node %s):*", wn.peer)}
	for _, d := range wn.duties {
		lines = append(lines, fmt.Sprintf("• %s: %d/%d — %.2f%%", d.duty, int(d.success), int(d.expected), d.pct()))
	}
	return strings.Join(lines, "\n")
}

func healthMD(health []healthCheck) string {
	if len(health) == 0 {
		return "*Health checks:* none fired ✅"
	}
	lines := []string{"*Health checks fired:*"}
	for _, h := range health {
		mark := "✔ cleared"
		if h.firingNow {
			mark = "✖ still firing"
		}
		lines = append(lines, fmt.Sprintf("• %s (%s) — %s", h.name, h.severity, mark))
	}
	return strings.Join(lines, "\n")
}

// reportHealthChecks gates whether charon app_health_checks are surfaced at
// all: when false (current default), the Slack report omits the health-check
// section and a firing check does NOT downgrade a run to "degraded" (so status
// reflects duty ratios only). The checks are still queried and populated on
// reportData, so re-enabling is just flipping this to true. Disabled for now
// because the health checks are noisy in these matrix runs.
var reportHealthChecks = false

func buildBlocks(d reportData) []map[string]any {
	e := statusEmoji[d.status]
	header := fmt.Sprintf("%s %s", e, d.name)

	contextText := fmt.Sprintf("cycle %d · %s · status *%s*", d.cycle, d.window, d.status)
	if d.clusterName != "" {
		contextText = fmt.Sprintf("cluster `%s` · cycle %d · %s · status *%s*", d.clusterName, d.cycle, d.window, d.status)
	}

	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": header}},
		{"type": "context", "elements": []map[string]any{{
			"type": "mrkdwn",
			"text": contextText,
		}}},
	}

	if d.errMsg != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": ":x: *Error:* " + d.errMsg},
		})
	}

	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": dutiesMD(d.worst)},
	})

	cpuStr := "n/a"
	if d.dvCPU != nil {
		cpuStr = fmt.Sprintf("%.2f cores", *d.dvCPU)
	}
	hostCPUStr := "n/a"
	hostMemStr := "n/a"
	if d.host != nil {
		hostCPUStr = fmt.Sprintf("%.0f%% avg / %.0f%% peak", d.host.cpuAvg, d.host.cpuPeak)
		hostMemStr = fmt.Sprintf("%s avg / %s peak of %s", gbf(d.host.memAvg), gbf(d.host.memPeak), gbf(d.host.memTotal))
	}
	res := fmt.Sprintf("*DV (worst node):* mem %s, cpu %s\n*Host:* cpu %s, mem %s",
		gb(d.dvMemBytes), cpuStr, hostCPUStr, hostMemStr)
	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": res},
	})

	if reportHealthChecks {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": healthMD(d.health)},
		})
	}

	if d.logArchivePath != "" {
		txt := fmt.Sprintf("*Logs:* `%s`", d.logArchivePath)
		if d.logExcerpt != "" {
			txt += "\n```" + d.logExcerpt + "```"
		}
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": txt},
		})
	}

	return blocks
}

// ---------------------------------------------------------------------------
// I/O func-var seams (real, minimal bodies; fully exercised in Task 3).
// ---------------------------------------------------------------------------

var (
	runCommand = func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).CombinedOutput()
		return string(out), err
	}

	httpGet = func(url string) ([]byte, int, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, err
		}
		return body, resp.StatusCode, nil
	}

	httpPost = func(url string, body []byte) (int, error) {
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}

	// httpDo is a general request seam (method + headers + body -> body +
	// status), used by the Slack file-upload flow which needs Bearer auth,
	// varied content types, and the response body.
	httpDo = func(method, reqURL string, headers map[string]string, body []byte) ([]byte, int, error) {
		req, err := http.NewRequest(method, reqURL, bytes.NewReader(body))
		if err != nil {
			return nil, 0, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		rb, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, err
		}
		return rb, resp.StatusCode, nil
	}

	nowFn      = time.Now
	sleepFn    = time.Sleep
	readFileFn = os.ReadFile
)

// ---------------------------------------------------------------------------
// promQuery: query the in-enclave Prometheus.
// ---------------------------------------------------------------------------

// promQuery GETs Prometheus's instant-query endpoint and parses the result
// into samples. It returns an error (including errorType/error from the
// response body) whenever the JSON "status" field isn't "success".
func promQuery(baseURL, promQL string) ([]sample, error) {
	u := strings.TrimRight(baseURL, "/") + "/api/v1/query?query=" + url.QueryEscape(promQL)
	body, _, err := httpGet(u)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
		Data      struct {
			Result []struct {
				Metric map[string]string  `json:"metric"`
				Value  [2]json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("prometheus query: invalid JSON response: %w", err)
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: errorType=%q error=%q", payload.ErrorType, payload.Error)
	}

	samples := make([]sample, 0, len(payload.Data.Result))
	for _, item := range payload.Data.Result {
		var valStr string
		if err := json.Unmarshal(item.Value[1], &valStr); err != nil {
			return nil, fmt.Errorf("prometheus query: invalid sample value: %w", err)
		}
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, fmt.Errorf("prometheus query: invalid sample value %q: %w", valStr, err)
		}
		samples = append(samples, sample{labels: item.Metric, value: v})
	}
	return samples, nil
}

// ---------------------------------------------------------------------------
// discoverClusterName: identify the single charon cluster in an enclave.
// ---------------------------------------------------------------------------

// discoverClusterName queries the enclave Prometheus for the distinct
// cluster_name label values present (via app_version, grouped by
// cluster_name) and returns the one charon cluster_name found. There is
// exactly one charon cluster per enclave -- the bootstrap lighthouse VCs are
// not charon and don't count as a second cluster here since they still share
// no distinguishing extra cluster_name of their own in this query -- so
// zero or more than one distinct value found is an error.
func discoverClusterName(baseURL string) (string, error) {
	samples, err := promQuery(baseURL, promClusterNameGroups)
	if err != nil {
		return "", err
	}
	names := map[string]bool{}
	for _, sm := range samples {
		if cn, ok := sm.labels["cluster_name"]; ok && cn != "" {
			names[cn] = true
		}
	}
	if len(names) != 1 {
		list := make([]string, 0, len(names))
		for n := range names {
			list = append(list, n)
		}
		sort.Strings(list)
		return "", fmt.Errorf("discoverClusterName: expected exactly one cluster_name, found %d: %v", len(names), list)
	}
	for n := range names {
		return n, nil
	}
	panic("unreachable") // len(names) == 1 guarantees a single iteration above
}

// ---------------------------------------------------------------------------
// Slack webhook posting: slackPost.
// ---------------------------------------------------------------------------

func slackPost(webhookURL, text string, blocks []map[string]any) error {
	payload := map[string]any{"text": text, "blocks": blocks}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	status, err := httpPost(webhookURL, data)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("slack webhook returned HTTP %d", status)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kurtosis/git shell-outs: kurtosisRun, kurtosisRemove, prometheusBaseURL,
// gitPull.
// ---------------------------------------------------------------------------

// kurtosisRun launches an enclave via `kurtosis run`. It returns an error on
// non-zero exit (runCommand's error already reflects that, as it does for
// os/exec.Cmd.CombinedOutput).
func kurtosisRun(enclave, pkg, argsFile string) error {
	out, err := runCommand("kurtosis", "run", "--enclave", enclave, pkg, "--args-file", argsFile)
	if err != nil {
		return fmt.Errorf("kurtosis run failed for %s: %w (output: %s)", enclave, err, strings.TrimSpace(out))
	}
	return nil
}

// kurtosisRemove tears down an enclave via `kurtosis enclave rm -f`. It is
// best-effort/idempotent -- it never returns an error and, via the recover,
// never panics -- so callers can call it freely as a guarded pre-clear or a
// guaranteed-teardown step.
func kurtosisRemove(enclave string) {
	defer func() { _ = recover() }()
	_, _ = runCommand("kurtosis", "enclave", "rm", "-f", enclave)
}

// prometheusBaseURL resolves the in-enclave Prometheus URL via
// `kurtosis port print`. It returns "" on any error or empty output.
func prometheusBaseURL(enclave string) string {
	out, err := runCommand("kurtosis", "port", "print", enclave, "prometheus", "http")
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// gitPull runs `git -C repoPath pull --ff-only`. It returns an error on
// non-zero exit.
func gitPull(repoPath string) error {
	out, err := runCommand("git", "-C", repoPath, "pull", "--ff-only")
	if err != nil {
		return fmt.Errorf("git pull failed in %s: %w (output: %s)", repoPath, err, strings.TrimSpace(out))
	}
	return nil
}

// ---------------------------------------------------------------------------
// sampleHost: background /proc sampler.
//
// sampleHost is a plain function (not an interface/struct) run as a
// goroutine: it samples /proc/stat and /proc/meminfo via readFileFn every
// intervalS seconds until stopCh is closed/signaled, then returns the
// summary. Read errors (e.g. non-Linux hosts, or in tests) are tolerated by
// skipping that sample, matching the "best-effort" spirit of the rest of
// this port; an all-error run simply yields all-zero stats.
// ---------------------------------------------------------------------------

func floatsAvg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func floatsMax(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func sampleHost(stopCh <-chan struct{}, intervalS int) hostStats {
	if intervalS < 1 {
		intervalS = 1
	}
	interval := time.Duration(intervalS) * time.Second

	var cpuSamples, memSamples []float64
	var memTotal float64
	var prevCPU [2]float64
	havePrevCPU := false

	sampleOnce := func() {
		if statText, err := readFileFn("/proc/stat"); err == nil {
			cur := [2]float64{}
			cur[0], cur[1] = parseCPULine(string(statText))
			if havePrevCPU {
				cpuSamples = append(cpuSamples, cpuPercent(prevCPU, cur))
			}
			prevCPU = cur
			havePrevCPU = true
		}
		if memText, err := readFileFn("/proc/meminfo"); err == nil {
			used, total := parseMeminfo(string(memText))
			memSamples = append(memSamples, used)
			memTotal = total
		}
	}

	sampleOnce() // prime the CPU baseline, matching Sampler._loop's initial call

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-stopCh:
			break loop
		case <-ticker.C:
			sampleOnce()
		}
	}

	if len(cpuSamples) == 0 {
		cpuSamples = []float64{0}
	}
	if len(memSamples) == 0 {
		memSamples = []float64{0}
	}
	return hostStats{
		cpuAvg:   floatsAvg(cpuSamples),
		cpuPeak:  floatsMax(cpuSamples),
		memAvg:   floatsAvg(memSamples),
		memPeak:  floatsMax(memSamples),
		memTotal: memTotal,
	}
}

// ---------------------------------------------------------------------------
// waitHealthy: poll until the cluster is healthy or the deadline expires.
// ---------------------------------------------------------------------------

// waitHealthy polls core_scheduler_validators_active>0 (cluster-agnostic --
// we don't yet know cluster_name at this point in runOne) until deadlineS
// elapses, sleeping 15s between polls regardless of sampleIntervalS. A
// promQuery error ends the wait early (returns false) rather than retrying,
// since this function's bool-only signature has no way to distinguish "not
// yet healthy" from "query failed" -- either way, runOne treats the run as
// unhealthy/failed.
func waitHealthy(baseURL string, deadlineS int) bool {
	const promQL = "core_scheduler_validators_active > 0"
	waited := 0
	for waited < deadlineS {
		samples, err := promQuery(baseURL, promQL)
		if err != nil {
			return false
		}
		if len(samples) > 0 {
			return true
		}
		sleepFn(15 * time.Second)
		waited += 15
	}
	return false
}

// ---------------------------------------------------------------------------
// collectReport: assemble the post-run report.
// ---------------------------------------------------------------------------

// degradedPctThreshold: below this per-duty success pct on the worst node,
// or with any health check firing now, a run's status is downgraded from
// "ok" to "degraded".
const degradedPctThreshold = 99.5

// collectReport queries Prometheus for duty/mem/cpu/health data over the
// scored window and assembles a reportData, applying the ok/degraded
// classification (never "failed" -- a query error is returned to the
// caller, which builds the failed report). window is always filled in by
// the caller afterwards, since it's derived from wall-clock time, not from
// anything collectReport queries.
func collectReport(baseURL, name, clusterName string, cycle, windowS int, host hostStats) (reportData, error) {
	expected, err := promQuery(baseURL, promDutyExpected(clusterName, windowS))
	if err != nil {
		return reportData{}, err
	}
	success, err := promQuery(baseURL, promDutySuccess(clusterName, windowS))
	if err != nil {
		return reportData{}, err
	}
	worst, ok := selectWorstNode(expected, success)
	var worstPtr *worstNode
	if ok {
		worstPtr = &worst
	}

	memSamples, err := promQuery(baseURL, promDVMemPeak(clusterName, windowS))
	if err != nil {
		return reportData{}, err
	}
	var memPtr *float64
	if v, ok := maxValue(memSamples); ok {
		memPtr = &v
	}

	cpuSamples, err := promQuery(baseURL, promDVCPUPeak(clusterName, windowS))
	if err != nil {
		return reportData{}, err
	}
	var cpuPtr *float64
	if v, ok := maxValue(cpuSamples); ok {
		cpuPtr = &v
	}

	fired, err := promQuery(baseURL, promHealthFired(clusterName, windowS))
	if err != nil {
		return reportData{}, err
	}
	firingNow, err := promQuery(baseURL, promHealthFiringNow(clusterName))
	if err != nil {
		return reportData{}, err
	}
	health := parseHealth(fired, firingNow)

	degraded := false
	if worstPtr != nil {
		for _, d := range worstPtr.duties {
			if d.pct() < degradedPctThreshold {
				degraded = true
				break
			}
		}
	}
	if reportHealthChecks && !degraded {
		for _, h := range health {
			if h.firingNow {
				degraded = true
				break
			}
		}
	}
	status := "ok"
	if degraded {
		status = "degraded"
	}

	h := host
	return reportData{
		name:        name,
		clusterName: clusterName,
		cycle:       cycle,
		status:      status,
		worst:       worstPtr,
		dvMemBytes:  memPtr,
		dvCPU:       cpuPtr,
		host:        &h,
		health:      health,
	}, nil
}

// ---------------------------------------------------------------------------
// runOne: run one param file end to end (with failedReport/postBestEffort
// helpers).
// ---------------------------------------------------------------------------

func fmtWindow(start, end time.Time) string {
	return fmt.Sprintf("%s-%s UTC", start.UTC().Format("15:04"), end.UTC().Format("15:04"))
}

// failedReport builds a failed-status report for name/cycle. There are no
// image pins to carry through anymore (those used to live in a separate
// pins file the cycler no longer reads -- pins are now inline in each
// static network-params file), so this is now a plain constructor.
func failedReport(name string, cycle int, errMsg string) reportData {
	return reportData{
		name:   name,
		cycle:  cycle,
		status: "failed",
		window: "-",
		errMsg: errMsg,
	}
}

// postBestEffort posts the run's Slack report on a best-effort basis: Slack
// failures (including a panic from a misbehaving fake) must never break
// runOne.
func postBestEffort(cfg config, d reportData) {
	defer func() { _ = recover() }()
	_ = slackPost(cfg.slackWebhookURL, buildText(d), buildBlocks(d))
}

// ---------------------------------------------------------------------------
// Failure log capture + Slack upload: on a non-ok run, dump the logs of one
// beacon node, all Charon nodes, and all DV validator clients to a gzipped
// tarball under cfg.logDir, and (if a bot token is configured) upload it to
// Slack. Everything here is best-effort: it must never break runOne.
// ---------------------------------------------------------------------------

// serviceLabel strips kurtosis's "--<hash>" suffix from a container name,
// leaving the readable service name (used for log file names).
func serviceLabel(container string) string {
	if i := strings.LastIndex(container, "--"); i > 0 {
		return container[:i]
	}
	return container
}

// selectLogTargets picks, from a list of container names, the log targets for
// a failing run: one beacon node (the lexically-first "cl-*"), all Charon
// nodes ("*-charon-charon-*"), and all DV validator clients ("*-charon-vc-*").
func selectLogTargets(containers []string) (bn string, dvNodes, vcs []string) {
	sorted := append([]string(nil), containers...)
	sort.Strings(sorted)
	for _, c := range sorted {
		switch {
		case strings.Contains(c, "-charon-charon-"):
			dvNodes = append(dvNodes, c)
		case strings.Contains(c, "-charon-vc-"):
			vcs = append(vcs, c)
		case bn == "" && strings.HasPrefix(serviceLabel(c), "cl-"):
			bn = c
		}
	}
	return bn, dvNodes, vcs
}

// captureFailureLogs dumps the targeted logs for a failing run into a gzipped
// tarball under cfg.logDir and returns its path plus a short excerpt (error
// lines from a Charon node) for the Slack message. Best-effort: on any problem
// it returns whatever it managed (possibly ""), never panicking. Assumes a
// single enclave is running (the cycler tears down between runs), so it scopes
// targets by service-name pattern rather than by enclave label.
func captureFailureLogs(cfg config, name string, cycle int) (archivePath, excerpt string) {
	defer func() { _ = recover() }()

	out, err := runCommand("docker", "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: log capture: docker ps failed: %v\n", err)
		return "", ""
	}
	var containers []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			containers = append(containers, ln)
		}
	}
	bn, dvNodes, vcs := selectLogTargets(containers)

	var targets []string
	if bn != "" {
		targets = append(targets, bn)
	}
	targets = append(targets, dvNodes...)
	targets = append(targets, vcs...)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "dv-cycler: log capture: no BN/Charon/VC containers found")
		return "", ""
	}

	staging, err := os.MkdirTemp("", "dv-cycler-logs-*")
	if err != nil {
		return "", ""
	}
	defer os.RemoveAll(staging)

	for _, c := range targets {
		logs, _ := runCommand("docker", "logs", c) // capture whatever exists
		_ = os.WriteFile(filepath.Join(staging, serviceLabel(c)+".log"), []byte(logs), 0o644)
	}

	if err := os.MkdirAll(cfg.logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: log capture: mkdir %s failed: %v\n", cfg.logDir, err)
		return "", ""
	}
	ts := nowFn().UTC().Format("20060102-150405")
	archivePath = filepath.Join(cfg.logDir, fmt.Sprintf("cycle%d-%s-%s.tar.gz", cycle, name, ts))
	if err := makeTarGz(staging, archivePath); err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: log capture: archive failed: %v\n", err)
		return "", ""
	}

	return archivePath, extractExcerpt(staging, dvNodes)
}

// extractExcerpt reads the first Charon node's captured log and returns up to
// ~25 recent noteworthy lines (error/warn/fatal/panic/doppelganger), capped in
// length, for inlining into the Slack message.
func extractExcerpt(staging string, dvNodes []string) string {
	if len(dvNodes) == 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(staging, serviceLabel(dvNodes[0])+".log"))
	if err != nil {
		return ""
	}
	var hits []string
	for _, ln := range strings.Split(string(data), "\n") {
		low := strings.ToLower(ln)
		if strings.Contains(low, "error") || strings.Contains(low, "erro ") ||
			strings.Contains(low, "warn") || strings.Contains(low, "fatal") ||
			strings.Contains(low, "panic") || strings.Contains(low, "doppelganger") {
			hits = append(hits, ln)
		}
	}
	if len(hits) == 0 {
		return ""
	}
	if len(hits) > 25 {
		hits = hits[len(hits)-25:]
	}
	out := strings.Join(hits, "\n")
	const maxLen = 1500
	if len(out) > maxLen {
		out = out[len(out)-maxLen:]
	}
	return serviceLabel(dvNodes[0]) + ":\n" + out
}

// makeTarGz writes a gzipped tarball of every regular file directly in srcDir
// to destPath.
func makeTarGz(srcDir, destPath string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.Name(), Mode: 0o644, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// uploadLogsBestEffort uploads the failure-log archive to Slack (if a bot
// token + channel are configured) and, on a successful upload, deletes the
// local archive so logDir doesn't grow -- Slack becomes the durable store. If
// upload isn't configured or fails, the local copy is kept (it's the only
// copy). Best-effort: never breaks runOne.
func uploadLogsBestEffort(cfg config, d reportData) {
	defer func() { _ = recover() }()
	comment := fmt.Sprintf("Logs for %s (cycle %d, %s)", d.name, d.cycle, d.status)
	uploaded, err := uploadLogsToSlack(cfg, d.logArchivePath, comment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: slack log upload failed (keeping local %s): %v\n", d.logArchivePath, err)
		return
	}
	if uploaded {
		if rmErr := os.Remove(d.logArchivePath); rmErr != nil {
			fmt.Fprintf(os.Stderr, "dv-cycler: could not delete uploaded archive %s: %v\n", d.logArchivePath, rmErr)
		}
	}
	// not uploaded (upload not configured): keep the local archive.
}

// uploadLogsToSlack uploads archivePath to Slack via the external-upload flow
// (files.getUploadURLExternal -> POST to the returned upload_url ->
// files.completeUploadExternal) and shares it into cfg.slackChannelID with an
// initial comment. It returns (true, nil) only when the file was actually
// uploaded and shared; (false, nil) when upload isn't configured (bot token or
// channel unset); and (false, err) on any failure.
func uploadLogsToSlack(cfg config, archivePath, comment string) (bool, error) {
	if cfg.slackBotToken == "" || cfg.slackChannelID == "" {
		return false, nil
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return false, err
	}
	filename := filepath.Base(archivePath)
	bearer := "Bearer " + cfg.slackBotToken

	// 1. Reserve an upload URL.
	form := url.Values{"filename": {filename}, "length": {strconv.Itoa(len(data))}}.Encode()
	body, _, err := httpDo("POST", "https://slack.com/api/files.getUploadURLExternal",
		map[string]string{"Authorization": bearer, "Content-Type": "application/x-www-form-urlencoded"},
		[]byte(form))
	if err != nil {
		return false, err
	}
	var got struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		return false, err
	}
	if !got.OK {
		return false, fmt.Errorf("files.getUploadURLExternal: %s", got.Error)
	}

	// 2. Upload the bytes to the reserved URL (multipart form field "file").
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return false, err
	}
	if _, err := part.Write(data); err != nil {
		return false, err
	}
	if err := mw.Close(); err != nil {
		return false, err
	}
	_, status, err := httpDo("POST", got.UploadURL,
		map[string]string{"Content-Type": mw.FormDataContentType()}, buf.Bytes())
	if err != nil {
		return false, err
	}
	if status != 200 {
		return false, fmt.Errorf("upload POST returned HTTP %d", status)
	}

	// 3. Complete the upload and share it into the channel.
	cbody, err := json.Marshal(map[string]any{
		"files":           []map[string]string{{"id": got.FileID, "title": filename}},
		"channel_id":      cfg.slackChannelID,
		"initial_comment": comment,
	})
	if err != nil {
		return false, err
	}
	rb, _, err := httpDo("POST", "https://slack.com/api/files.completeUploadExternal",
		map[string]string{"Authorization": bearer, "Content-Type": "application/json"}, cbody)
	if err != nil {
		return false, err
	}
	var done struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rb, &done); err != nil {
		return false, err
	}
	if !done.OK {
		return false, fmt.Errorf("files.completeUploadExternal: %s", done.Error)
	}
	return true, nil
}

// writeTempArgsFile writes yaml to a fresh temp file and returns its path.
func writeTempArgsFile(yaml string) (path string, err error) {
	f, err := os.CreateTemp("", "dv-cycler-args-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(yaml); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// runWindow performs the post-launch phase of runOne: resolve the
// Prometheus URL, wait for health, discover the cluster_name, run the
// sampler across the wait window, then collect the report. The sampler is
// always started right before, and stopped right after, the wait loop --
// there is no early return in between, so teardown of the sampler goroutine
// is unconditional in the only case where it was started.
func runWindow(cfg config, name string, cycle int, enclave string) reportData {
	baseURL := prometheusBaseURL(enclave)
	if baseURL == "" {
		return failedReport(name, cycle, "could not resolve prometheus base URL")
	}

	if !waitHealthy(baseURL, cfg.startupDeadlineMinutes*60) {
		return failedReport(name, cycle, "cluster did not become healthy before deadline")
	}

	clusterName, err := discoverClusterName(baseURL)
	if err != nil {
		return failedReport(name, cycle, fmt.Sprintf("cluster name discovery failed: %v", err))
	}

	stopCh := make(chan struct{})
	sampleDone := make(chan hostStats, 1)
	go func() { sampleDone <- sampleHost(stopCh, cfg.sampleIntervalS) }()

	totalS := cfg.runMinutes * 60
	for elapsed := 0; elapsed < totalS; {
		step := cfg.sampleIntervalS
		if step > totalS-elapsed {
			step = totalS - elapsed
		}
		if step < 1 {
			step = 1
		}
		sleepFn(time.Duration(step) * time.Second)
		elapsed += step
	}

	end := nowFn()
	windowS := cfg.runMinutes*60 - cfg.warmupMinutes*60
	if windowS < 1 {
		windowS = 1
	}
	windowLabel := fmtWindow(end.Add(-time.Duration(windowS)*time.Second), end)

	close(stopCh)
	host := <-sampleDone

	data, err := collectReport(baseURL, name, clusterName, cycle, windowS, host)
	if err != nil {
		return failedReport(name, cycle, err.Error())
	}
	data.window = windowLabel
	return data
}

// runOne executes one param file: a guarded pre-clear, then pre-launch (git
// pull + read/substitute/write the args file) failures produce a failed
// report and return before anything was launched; a launch failure produces
// a failed report and returns; after a successful launch, teardown is
// guaranteed via defer, and any failure from there on (resolving
// Prometheus, unhealthy startup, cluster discovery, sampling, metrics
// query, report assembly) still produces a failed report. The top-level
// recover is an extra safety net so that even an unexpected panic from a
// fake or a bug never escapes to kill the caller's loop.
func runOne(cfg config, paramFile, name string, cycle int) (result reportData) {
	enclave := enclaveName(cycle, name)

	defer func() {
		if r := recover(); r != nil {
			result = failedReport(name, cycle, fmt.Sprintf("panic: %v", r))
			postBestEffort(cfg, result)
			kurtosisRemove(enclave)
		}
	}()

	kurtosisRemove(enclave) // idempotent: clear any stale enclave from a previous run

	if err := gitPull(cfg.repoPath); err != nil {
		data := failedReport(name, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		return data
	}

	raw, err := readFileFn(paramFile)
	if err != nil {
		data := failedReport(name, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		return data
	}
	argsYAML := strings.ReplaceAll(string(raw), "$PROMETHEUS_REMOTE_WRITE_TOKEN", cfg.monitoringToken)

	tmpArgsPath, err := writeTempArgsFile(argsYAML)
	if err != nil {
		data := failedReport(name, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		return data
	}
	defer os.Remove(tmpArgsPath)

	if err := kurtosisRun(enclave, cfg.packageRef, tmpArgsPath); err != nil {
		// kurtosis run can exit non-zero while still leaving the enclave and its
		// containers behind (e.g. a service readiness check timing out under
		// load). Tear it down so a failed launch doesn't leak an enclave and
		// compound resource pressure on the next combo's run.
		kurtosisRemove(enclave)
		data := failedReport(name, cycle, fmt.Sprintf("launch failed: %v", err))
		postBestEffort(cfg, data)
		return data
	}
	defer kurtosisRemove(enclave) // guaranteed teardown after a successful launch

	data := runWindow(cfg, name, cycle, enclave)
	if data.status != "ok" {
		// Capture BN/Charon/VC logs while the enclave is still up (teardown is
		// deferred), for post-mortem of a failing/degraded combo.
		data.logArchivePath, data.logExcerpt = captureFailureLogs(cfg, name, cycle)
	}
	postBestEffort(cfg, data)
	if data.logArchivePath != "" {
		uploadLogsBestEffort(cfg, data)
	}
	return data
}

// ---------------------------------------------------------------------------
// Per-commit results summary: accumulate each combo's headline metrics keyed
// by the repo commit (the version set), and post a Slack table once every
// combo has run under a commit -- or when a new commit supersedes it (partial,
// with N/A for combos not yet run under it). Additive to the per-run reports.
// ---------------------------------------------------------------------------

type comboResult struct {
	Status    string   `json:"status"`
	DutyPct   *float64 `json:"duty_pct,omitempty"`
	CharonMem *float64 `json:"charon_mem_bytes,omitempty"`
	CharonCPU *float64 `json:"charon_cpu,omitempty"`
	HostCPU   *float64 `json:"host_cpu_peak,omitempty"`
	HostMem   *float64 `json:"host_mem_peak_bytes,omitempty"`
}

type resultsStore struct {
	Commit  string                 `json:"commit"`  // the commit currently accumulating
	Posted  bool                   `json:"posted"`  // whether Commit's table has been posted
	Results map[string]comboResult `json:"results"` // combo name -> latest result for Commit
}

// summaryToPost is a table the loop should post: the results for one commit,
// flagged as a completion (all combos ran) or a supersede (partial).
type summaryToPost struct {
	Commit     string
	Results    map[string]comboResult
	Complete   bool
	Superseded bool
}

func loadResults(path string) (resultsStore, error) {
	data, err := readFileFn(path)
	if err != nil {
		if os.IsNotExist(err) {
			return resultsStore{Results: map[string]comboResult{}}, nil
		}
		return resultsStore{}, err
	}
	var s resultsStore
	if err := json.Unmarshal(data, &s); err != nil {
		return resultsStore{}, err
	}
	if s.Results == nil {
		s.Results = map[string]comboResult{}
	}
	return s, nil
}

func (s *resultsStore) save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// worstDutyPct is the worst node's overall duty success rate (successes /
// expected, summed across its duties, which already exclude 0/0 idle duties).
func worstDutyPct(d reportData) *float64 {
	if d.worst == nil || len(d.worst.duties) == 0 {
		return nil
	}
	var se, ss float64
	for _, du := range d.worst.duties {
		se += du.expected
		ss += du.success
	}
	if se == 0 {
		return nil
	}
	p := 100 * ss / se
	return &p
}

func comboResultFrom(d reportData) comboResult {
	r := comboResult{Status: d.status, DutyPct: worstDutyPct(d), CharonMem: d.dvMemBytes, CharonCPU: d.dvCPU}
	if d.host != nil {
		cp, mp := d.host.cpuPeak, d.host.memPeak
		r.HostCPU, r.HostMem = &cp, &mp
	}
	return r
}

func cloneResults(m map[string]comboResult) map[string]comboResult {
	out := make(map[string]comboResult, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func allCombosPresent(allCombos []string, results map[string]comboResult) bool {
	if len(allCombos) == 0 {
		return false
	}
	for _, c := range allCombos {
		if _, ok := results[c]; !ok {
			return false
		}
	}
	return true
}

// ingest records combo's result under commit. If the commit changed it rotates
// (returning a supersede summary for the previous, unposted commit), and once
// every combo in allCombos has run under the current commit it returns a
// completion summary. Each commit is posted at most once.
func (s *resultsStore) ingest(commit, combo string, res comboResult, allCombos []string) []summaryToPost {
	if s.Results == nil {
		s.Results = map[string]comboResult{}
	}
	var posts []summaryToPost
	if s.Commit != commit {
		if s.Commit != "" && !s.Posted && len(s.Results) > 0 {
			posts = append(posts, summaryToPost{Commit: s.Commit, Results: cloneResults(s.Results), Superseded: true})
		}
		s.Commit = commit
		s.Results = map[string]comboResult{}
		s.Posted = false
	}
	s.Results[combo] = res
	if !s.Posted && allCombosPresent(allCombos, s.Results) {
		posts = append(posts, summaryToPost{Commit: s.Commit, Results: cloneResults(s.Results), Complete: true})
		s.Posted = true
	}
	return posts
}

// comboNames returns the sorted combo stems for the given param file paths.
func comboNames(files []string) []string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, paramStem(f))
	}
	sort.Strings(names)
	return names
}

// repoCommit returns the short HEAD commit of repoPath (the version set), or ""
// if it can't be resolved.
func repoCommit(repoPath string) string {
	out, err := runCommand("git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// fmtF formats an optional metric with the given printf verb, or "N/A" if nil.
func fmtF(p *float64, format string) string {
	if p == nil {
		return "N/A"
	}
	return fmt.Sprintf(format, *p)
}

// fmtGB formats an optional byte count as gigabytes, or "N/A" if nil.
func fmtGB(p *float64) string {
	if p == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2fGB", *p/1e9)
}

// summaryRows renders the fixed-width header line and one row per combo in
// allCombos (N/A across the board for combos with no result), aligned so the
// table lines up in a Slack monospace block.
func summaryRows(results map[string]comboResult, allCombos []string) (header string, rows []string) {
	cw := len("combo")
	for _, c := range allCombos {
		if len(c) > cw {
			cw = len(c)
		}
	}
	row := func(combo, status, duty, cmem, ccpu, hcpu, hmem string) string {
		return fmt.Sprintf("%-*s  %-9s  %7s  %9s  %8s  %8s  %9s",
			cw, combo, status, duty, cmem, ccpu, hcpu, hmem)
	}
	header = row("combo", "status", "duty%", "chn-mem", "chn-cpu", "host-cpu", "host-mem")
	for _, c := range allCombos {
		r, ok := results[c]
		if !ok {
			rows = append(rows, row(c, "N/A", "N/A", "N/A", "N/A", "N/A", "N/A"))
			continue
		}
		rows = append(rows, row(c, r.Status,
			fmtF(r.DutyPct, "%.1f%%"), fmtGB(r.CharonMem), fmtF(r.CharonCPU, "%.2f"),
			fmtF(r.HostCPU, "%.0f%%"), fmtGB(r.HostMem)))
	}
	return header, rows
}

// buildSummaryBlocks renders the Slack fallback text and Block Kit blocks for a
// results summary: a lead line (optionally prefixed with a mention to ping),
// a run-count context line, and the table split across monospace code blocks
// to stay under Slack's per-block character limit.
func buildSummaryBlocks(sum summaryToPost, allCombos []string, mention string) (string, []map[string]any) {
	ran := 0
	for _, c := range allCombos {
		if _, ok := sum.Results[c]; ok {
			ran++
		}
	}
	kind := "in progress"
	switch {
	case sum.Complete:
		kind = "complete"
	case sum.Superseded:
		kind = "partial (superseded by a newer commit)"
	}
	headline := fmt.Sprintf("*DV matrix results* — commit `%s` — %s", sum.Commit, kind)
	fallback := fmt.Sprintf("DV matrix results %s (%d/%d combos, %s)", sum.Commit, ran, len(allCombos), kind)

	// Only ping the mention on a complete table (all combos ran). Partial /
	// superseded tables (common during active development, since the repo
	// commit changes on nearly every run) post without a ping to avoid noise.
	lead := headline
	if mention != "" && sum.Complete {
		lead = mention + " " + headline
	}
	blocks := []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": lead}},
		{"type": "context", "elements": []map[string]any{
			{"type": "mrkdwn", "text": fmt.Sprintf("%d/%d combos run", ran, len(allCombos))},
		}},
	}

	header, rows := summaryRows(sum.Results, allCombos)
	const perBlock = 18 // keeps each code block well under Slack's 3000-char limit
	for i := 0; i < len(rows); i += perBlock {
		j := i + perBlock
		if j > len(rows) {
			j = len(rows)
		}
		table := header + "\n" + strings.Join(rows[i:j], "\n")
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": "```\n" + table + "\n```"},
		})
	}
	return fallback, blocks
}

// postSummaryBestEffort posts one results summary via the webhook. Best-effort:
// never breaks the loop.
func postSummaryBestEffort(cfg config, sum summaryToPost, allCombos []string) {
	defer func() { _ = recover() }()
	text, blocks := buildSummaryBlocks(sum, allCombos, cfg.summaryMention)
	if err := slackPost(cfg.slackWebhookURL, text, blocks); err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: summary post failed: %v\n", err)
	}
}

// recordResultAndMaybePost records this run's headline metrics into the
// per-commit results store and posts a summary table when a commit completes
// (or is superseded). Best-effort: never breaks the loop.
func recordResultAndMaybePost(cfg config, combo string, d reportData, files []string) {
	defer func() { _ = recover() }()
	if cfg.resultsPath == "" {
		return
	}
	commit := repoCommit(cfg.repoPath)
	if commit == "" {
		fmt.Fprintln(os.Stderr, "dv-cycler: results: could not resolve repo commit; skipping summary")
		return
	}
	store, err := loadResults(cfg.resultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: results: load failed (starting fresh): %v\n", err)
		store = resultsStore{Results: map[string]comboResult{}}
	}
	allCombos := comboNames(files)
	posts := store.ingest(commit, combo, comboResultFrom(d), allCombos)
	if err := store.save(cfg.resultsPath); err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: results: save failed: %v\n", err)
	}
	for _, p := range posts {
		postSummaryBestEffort(cfg, p, allCombos)
	}
}

// ---------------------------------------------------------------------------
// The 24/7 driver loop: mainLoop, main.
// ---------------------------------------------------------------------------

// mainLoop drives the 24/7 loop: resume from a possibly-interrupted run,
// then loop forever re-enumerating network-params/*.yaml, running the next
// one, and backing off according to consecutive failures. Re-enumerating on
// every iteration (rather than once at startup) is what makes a newly
// dropped-in file get picked up without restarting the service.
// State-save errors are logged and otherwise ignored (best-effort), since
// this whole task's mandate is that the loop must never die.
func mainLoop(cfg config) {
	st, err := loadState(cfg.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dv-cycler: failed to load state (starting fresh): %v\n", err)
		st = state{}
	}

	saveState := func() {
		if err := st.save(cfg.statePath); err != nil {
			fmt.Fprintf(os.Stderr, "dv-cycler: failed to save state: %v\n", err)
		}
	}

	if st.CurrentEnclave != "" {
		kurtosisRemove(st.CurrentEnclave) // best-effort: clear an enclave left over from an interrupted run
		st.CurrentEnclave = ""
		saveState()
	}

	consecutiveFailures := 0
	for {
		files, err := paramFiles(cfg.paramsDir)
		if err != nil || len(files) == 0 {
			fmt.Fprintf(os.Stderr, "dv-cycler: no param files found in %s (err=%v); backing off\n", cfg.paramsDir, err)
			sleepFn(time.Duration(computeBackoff(consecutiveFailures, cfg.interRunBackoffS, cfg.maxBackoffS)) * time.Second)
			continue
		}

		if st.NextIndex >= len(files) {
			st.NextIndex = 0
			st.Cycle++
		}

		f := files[st.NextIndex]
		name := paramStem(f)

		st.CurrentEnclave = enclaveName(st.Cycle, name)
		saveState()

		data := runOne(cfg, f, name, st.Cycle)

		st.CurrentEnclave = ""
		st.advance()
		saveState()

		recordResultAndMaybePost(cfg, name, data, files)

		if data.status == "failed" {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}
		sleepFn(time.Duration(computeBackoff(consecutiveFailures, cfg.interRunBackoffS, cfg.maxBackoffS)) * time.Second)
	}
}

func main() {
	if err := loadDotEnv(dotEnvPath()); err != nil {
		fmt.Fprintln(os.Stderr, "dv-cycler: failed to read .env:", err)
		os.Exit(1)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dv-cycler:", err)
		os.Exit(1)
	}
	mainLoop(cfg)
}
