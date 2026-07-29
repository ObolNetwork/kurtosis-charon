// Command charon-cycler cycles the ethereum-package devnet through the
// CL x VC matrix behind Charon, sampling metrics and posting a Slack report
// for each run. This file (main.go) is a script-like Go port of the
// charon_cycler Python package: no sub-packages, no interfaces, just plain
// data records and package-level functions/vars.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// combos.py -> combo, cycle, enclaveName
// ---------------------------------------------------------------------------

type combo struct {
	cl, vc string
}

func (c combo) name() string {
	return c.cl + "-" + c.vc
}

func (c combo) clusterName() string {
	return "kurtosis-" + c.cl + "-" + c.vc
}

func enclaveName(cycleNum int, c combo) string {
	return fmt.Sprintf("c%d-%s-%s", cycleNum, c.cl, c.vc)
}

// clientsCL/clientsVC mirror charon_matrix.network_params.CLS/VCS. Order is
// fixed here (CL-major), not derived from any JSON.
var clientsCL = []string{"lighthouse", "lodestar", "nimbus", "teku", "prysm", "grandine"}
var clientsVC = []string{"lighthouse", "lodestar", "nimbus", "teku", "prysm", "vouch"}

// cycle is the fixed 36-entry CL-major combo matrix (CYCLE in combos.py).
var cycle []combo

func init() {
	for _, cl := range clientsCL {
		for _, vc := range clientsVC {
			cycle = append(cycle, combo{cl: cl, vc: vc})
		}
	}
}

// ---------------------------------------------------------------------------
// config.py -> config, loadConfig
//
// Deviation from the Python reference (documented in task-2-report.md): the
// committed Python's load_config(path) reads a YAML file. Task 2's brief
// mandates a no-argument `func loadConfig() (config, error)` reading from
// env vars + flags instead, so this is a deliberate redesign of the config
// source for the Go port, not a behavior port.
// ---------------------------------------------------------------------------

type config struct {
	slackWebhookURL, repoPath, statePath, monitoringToken, packageRef  string
	runMinutes, warmupMinutes, startupDeadlineMinutes, sampleIntervalS int
	interRunBackoffS, maxBackoffS                                      int
}

// envInt reads an integer env var into *dst if present and non-empty and
// parses cleanly; otherwise dst is left untouched.
func envInt(name string, dst *int) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil {
		*dst = n
	}
}

func envStr(name string, dst *string) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
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
		case "monitoring-token":
			cfg.monitoringToken = val
		case "package-ref":
			cfg.packageRef = val
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
	envStr("MONITORING_TOKEN", &cfg.monitoringToken)
	envStr("PACKAGE_REF", &cfg.packageRef)
	envInt("RUN_MINUTES", &cfg.runMinutes)
	envInt("WARMUP_MINUTES", &cfg.warmupMinutes)
	envInt("STARTUP_DEADLINE_MINUTES", &cfg.startupDeadlineMinutes)
	envInt("SAMPLE_INTERVAL_S", &cfg.sampleIntervalS)
	envInt("INTER_RUN_BACKOFF_S", &cfg.interRunBackoffS)
	envInt("MAX_BACKOFF_S", &cfg.maxBackoffS)

	if len(os.Args) > 1 {
		applyFlags(&cfg, os.Args[1:])
	}

	var missing []string
	if cfg.slackWebhookURL == "" {
		missing = append(missing, "slack_webhook_url (SLACK_WEBHOOK_URL / --slack-webhook-url)")
	}
	if cfg.repoPath == "" {
		missing = append(missing, "repo_path (REPO_PATH / --repo-path)")
	}
	if cfg.statePath == "" {
		missing = append(missing, "state_path (STATE_PATH / --state-path)")
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// ---------------------------------------------------------------------------
// state.py -> state, loadState, save, advance
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

func (s *state) advance() {
	s.NextIndex++
	if s.NextIndex >= len(cycle) {
		s.NextIndex = 0
		s.Cycle++
	}
}

// ---------------------------------------------------------------------------
// selection.py -> readOverride, selectNextCombo
// ---------------------------------------------------------------------------

// readOverride is a var (not a plain func) so tests can substitute an
// override without a runtime dependency-injection layer. Its real behavior
// is inert: it always returns nil until an override.json reader is built.
var readOverride = func() *combo { return nil }

func selectNextCombo(s state) (combo, string) {
	if ov := readOverride(); ov != nil {
		return *ov, "override"
	}
	return cycle[s.NextIndex], "cycle"
}

// ---------------------------------------------------------------------------
// cycler.py -> compute_backoff
// ---------------------------------------------------------------------------

func computeBackoff(consecutiveFailures, base, cap int) int {
	v := base * (1 << uint(consecutiveFailures))
	if v > cap {
		return cap
	}
	return v
}

// ---------------------------------------------------------------------------
// params.py + network_params.py -> images, loadImages, buildArgsFile
// ---------------------------------------------------------------------------

type images struct {
	Charon      string            `json:"charon"`
	EL          string            `json:"el"`
	BootstrapCL string            `json:"bootstrap_cl"`
	CL          map[string]string `json:"cl"`
	VC          map[string]string `json:"vc"`
}

func loadImages(repoPath string) (images, error) {
	data, err := readFileFn(filepath.Join(repoPath, "images.json"))
	if err != nil {
		return images{}, err
	}
	var im images
	if err := json.Unmarshal(data, &im); err != nil {
		return images{}, err
	}
	return im, nil
}

// validatorKeysMnemonic mirrors network_params.VALIDATOR_KEYS_MNEMONIC.
const validatorKeysMnemonic = "giant issue aisle success illegal bike spike question tent bar rely arctic " +
	"volcano long crawl hungry vocal artwork sniff fantasy very lucky have athlete"

// buildArgsFile reproduces network_params.build_network_params's YAML
// exactly, with the $PROMETHEUS_REMOTE_WRITE_TOKEN placeholder substituted.
func buildArgsFile(im images, c combo, token string, charonNodeCount int) string {
	nimbusEnv := ""
	if c.vc == "nimbus" {
		nimbusEnv = "    vc_extra_env_vars:\n      CHARON_FEATURE_SET_ENABLE: json_requests\n"
	}
	raw := fmt.Sprintf(`participants:
  - el_type: geth
    el_image: %s
    cl_type: lighthouse
    cl_image: %s
    use_separate_vc: true
    vc_type: lighthouse
    vc_image: %s
    count: 2
    supernode: true

  - el_type: geth
    el_image: %s
    cl_type: %s
    cl_image: %s
    supernode: true
    use_separate_vc: true
    vc_type: charon
    vc_image: %s
    charon_node_count: %d
    charon_params:
      charon_vc: %s
      charon_vc_image: %s
%s    count: 1
network_params:
  network: kurtosis
  network_id: "3151908"
  deposit_contract_address: "0x4242424242424242424242424242424242424242"
  seconds_per_slot: 12
  num_validator_keys_per_node: 128
  preregistered_validator_keys_mnemonic: "%s"
  shard_committee_period: 1
  prefunded_accounts: '{"0xb9e79D19f651a941757b35830232E7EFC77E1c79": {"balance": "100000ETH"}}'
wait_for_finalization: false
global_log_level: info
parallel_keystore_generation: false
mev_type: flashbots
mev_params:
  mev_builder_subsidy: 1
prometheus_params:
  storage_tsdb_retention_time: 3h
  remote_write_url: "https://vm.monitoring.gcp.obol.tech/write"
  remote_write_token: "$PROMETHEUS_REMOTE_WRITE_TOKEN"
  remote_write_relabel_configs:
    - SourceLabels: ["job"]
      Regex: ".*charon.*"
      Action: keep
    - SourceLabels: ["client_name"]
      Regex: "charon"
      TargetLabel: job
      Replacement: charon
      Action: replace
additional_services:
  - spamoor
  - prometheus
`, im.EL, im.BootstrapCL, im.BootstrapCL, im.EL, c.cl, im.CL[c.cl], im.Charon, charonNodeCount,
		c.vc, im.VC[c.vc], nimbusEnv, validatorKeysMnemonic)

	return strings.ReplaceAll(raw, "$PROMETHEUS_REMOTE_WRITE_TOKEN", token)
}

// ---------------------------------------------------------------------------
// promql.py -> promDutyExpected, promDutySuccess, promCharonMemPeak,
// promCharonCPUPeak, promHealthFired, promHealthFiringNow
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

func promCharonMemPeak(clusterName string, windowS int) string {
	return fmt.Sprintf("max(max_over_time(process_resident_memory_bytes{%s}[%ds])) by (cluster_peer)",
		promSelector(clusterName), windowS)
}

func promCharonCPUPeak(clusterName string, windowS int) string {
	return fmt.Sprintf("max(max_over_time(rate(process_cpu_seconds_total{%s}[1m])[%ds:1m])) by (cluster_peer)",
		promSelector(clusterName), windowS)
}

func promHealthFired(clusterName string, windowS int) string {
	return fmt.Sprintf("max_over_time(app_health_checks{%s}[%ds]) > 0", promSelector(clusterName), windowS)
}

func promHealthFiringNow(clusterName string) string {
	return fmt.Sprintf("app_health_checks{%s} == 1", promSelector(clusterName))
}

// ---------------------------------------------------------------------------
// metrics.py -> sample, dutyResult, worstNode, healthCheck, selectWorstNode,
// maxValue, parseHealth
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
// host_sampler.py -> parseCPULine, cpuPercent, parseMeminfo
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
// report.py -> hostStats, reportData, buildText, buildBlocks
// ---------------------------------------------------------------------------

type hostStats struct {
	cpuAvg, cpuPeak, memAvg, memPeak, memTotal float64
}

type reportData struct {
	combo          combo
	cycle          int
	status         string
	clImage        string
	vcImage        string
	charonImage    string
	window         string
	worst          *worstNode
	charonMemBytes *float64
	charonCPU      *float64
	host           *hostStats
	health         []healthCheck
	errMsg         string
}

var statusEmoji = map[string]string{"ok": "✅", "degraded": "⚠️", "failed": "❌"}

func buildText(d reportData) string {
	e := statusEmoji[d.status]
	return fmt.Sprintf("%s %s → charon → %s · cycle %d · %s", e, d.combo.cl, d.combo.vc, d.cycle, d.status)
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

func buildBlocks(d reportData) []map[string]any {
	e := statusEmoji[d.status]
	header := fmt.Sprintf("%s %s → charon → %s", e, d.combo.cl, d.combo.vc)
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": header}},
		{"type": "context", "elements": []map[string]any{{
			"type": "mrkdwn",
			"text": fmt.Sprintf("cycle %d · %s · status *%s*", d.cycle, d.window, d.status),
		}}},
		{"type": "section", "fields": []map[string]any{
			{"type": "mrkdwn", "text": "*CL:* " + d.clImage},
			{"type": "mrkdwn", "text": "*VC:* " + d.vcImage},
			{"type": "mrkdwn", "text": "*Charon:* " + d.charonImage},
		}},
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
	if d.charonCPU != nil {
		cpuStr = fmt.Sprintf("%.2f cores", *d.charonCPU)
	}
	hostCPUStr := "n/a"
	hostMemStr := "n/a"
	if d.host != nil {
		hostCPUStr = fmt.Sprintf("%.0f%% avg / %.0f%% peak", d.host.cpuAvg, d.host.cpuPeak)
		hostMemStr = fmt.Sprintf("%s avg / %s peak of %s", gbf(d.host.memAvg), gbf(d.host.memPeak), gbf(d.host.memTotal))
	}
	res := fmt.Sprintf("*Charon (worst node):* mem %s, cpu %s\n*Host:* cpu %s, mem %s",
		gb(d.charonMemBytes), cpuStr, hostCPUStr, hostMemStr)
	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": res},
	})

	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": healthMD(d.health)},
	})

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

	nowFn      = time.Now
	sleepFn    = time.Sleep
	readFileFn = os.ReadFile
)
