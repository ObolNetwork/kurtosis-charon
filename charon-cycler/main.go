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

// ---------------------------------------------------------------------------
// metrics.py -> PrometheusClient.query -> promQuery
// ---------------------------------------------------------------------------

// promQuery GETs Prometheus's instant-query endpoint and parses the result
// into samples, mirroring PrometheusClient.query. It returns an error
// (including errorType/error from the response body) whenever the JSON
// "status" field isn't "success".
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
		if len(item.Value) < 2 {
			continue
		}
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
// slack.py -> slackPost
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
// kurtosis.py -> kurtosisRun, kurtosisRemove, prometheusBaseURL, gitPull
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
// host_sampler.py -> Sampler -> sampleHost
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
// cycler.py -> wait_healthy -> waitHealthy
// ---------------------------------------------------------------------------

// waitHealthy polls core_scheduler_validators_active>0 until deadlineS
// elapses, sleeping sampleIntervalS-independent 15s steps between polls
// (matching _default_deps.wait_healthy). A promQuery error ends the wait
// early (returns false) rather than retrying -- this is a deliberate change
// from Python's behavior of letting the exception propagate out of
// wait_healthy and fail the whole run, since this Go signature returns a
// bool rather than (bool, error); the net effect on run_one is the same
// (the run is treated as unhealthy/failed).
func waitHealthy(baseURL, clusterName string, deadlineS int) bool {
	promQL := fmt.Sprintf(`core_scheduler_validators_active{cluster_name="%s"}`, clusterName)
	waited := 0
	for waited < deadlineS {
		samples, err := promQuery(baseURL, promQL)
		if err != nil {
			return false
		}
		for _, sm := range samples {
			if sm.value > 0 {
				return true
			}
		}
		sleepFn(15 * time.Second)
		waited += 15
	}
	return false
}

// ---------------------------------------------------------------------------
// cycler.py -> collect_report -> collectReport
// ---------------------------------------------------------------------------

// degradedPctThreshold mirrors cycler.py's DEGRADED_PCT_THRESHOLD: below
// this per-duty success pct on the worst node, or with any health check
// firing now, a run's status is downgraded from "ok" to "degraded".
const degradedPctThreshold = 99.5

// collectReport queries Prometheus for duty/mem/cpu/health data over the
// scored window and assembles a reportData, applying the ok/degraded
// classification (never "failed" -- a query error is returned to the
// caller, which builds the failed report). window/status are always
// computed here as "ok" or "degraded"; runOne fills in the human-readable
// window label afterwards since it's derived from wall-clock time, not from
// anything collectReport queries.
func collectReport(baseURL string, c combo, cycle, windowS int, host hostStats, im images) (reportData, error) {
	cn := c.clusterName()

	expected, err := promQuery(baseURL, promDutyExpected(cn, windowS))
	if err != nil {
		return reportData{}, err
	}
	success, err := promQuery(baseURL, promDutySuccess(cn, windowS))
	if err != nil {
		return reportData{}, err
	}
	worst, ok := selectWorstNode(expected, success)
	var worstPtr *worstNode
	if ok {
		worstPtr = &worst
	}

	memSamples, err := promQuery(baseURL, promCharonMemPeak(cn, windowS))
	if err != nil {
		return reportData{}, err
	}
	var memPtr *float64
	if v, ok := maxValue(memSamples); ok {
		memPtr = &v
	}

	cpuSamples, err := promQuery(baseURL, promCharonCPUPeak(cn, windowS))
	if err != nil {
		return reportData{}, err
	}
	var cpuPtr *float64
	if v, ok := maxValue(cpuSamples); ok {
		cpuPtr = &v
	}

	fired, err := promQuery(baseURL, promHealthFired(cn, windowS))
	if err != nil {
		return reportData{}, err
	}
	firingNow, err := promQuery(baseURL, promHealthFiringNow(cn))
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
	if !degraded {
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

	clImage := im.CL[c.cl]
	if clImage == "" {
		clImage = c.cl
	}
	vcImage := im.VC[c.vc]
	if vcImage == "" {
		vcImage = c.vc
	}

	h := host
	return reportData{
		combo:          c,
		cycle:          cycle,
		status:         status,
		clImage:        clImage,
		vcImage:        vcImage,
		charonImage:    im.Charon,
		worst:          worstPtr,
		charonMemBytes: memPtr,
		charonCPU:      cpuPtr,
		host:           &h,
		health:         health,
	}, nil
}

// ---------------------------------------------------------------------------
// cycler.py -> run_one -> runOne (+ helpers _failed_report, _post_best_effort)
// ---------------------------------------------------------------------------

// charonNodeCount mirrors params.py's write_args_file default.
const charonNodeCount = 4

func fmtWindow(start, end time.Time) string {
	return fmt.Sprintf("%s-%s UTC", start.UTC().Format("15:04"), end.UTC().Format("15:04"))
}

// failedReport mirrors cycler.py's _failed_report. Since the Go port loads
// CL/VC image pins from the repo's images.json (rather than a static
// charon_matrix import), a failure that happens before those pins are
// available falls back to the raw client names, matching Python's
// CL_IMAGES.get(combo.cl, combo.cl) default.
func failedReport(c combo, cycle int, errMsg string) reportData {
	return reportData{
		combo:   c,
		cycle:   cycle,
		status:  "failed",
		clImage: c.cl,
		vcImage: c.vc,
		window:  "-",
		errMsg:  errMsg,
	}
}

// postBestEffort mirrors _post_best_effort: Slack failures (including a
// panic from a misbehaving fake) must never break runOne.
func postBestEffort(cfg config, d reportData) {
	defer func() { _ = recover() }()
	_ = slackPost(cfg.slackWebhookURL, buildText(d), buildBlocks(d))
}

// runWindow performs the post-launch phase of runOne: wait for health, run
// the sampler across the wait window, then collect the report. The sampler
// is always started right before, and stopped right after, the wait loop --
// there is no early return in between, so teardown is unconditional in the
// only case where it was started.
func runWindow(cfg config, c combo, cycle int, enclave string, im images) reportData {
	baseURL := prometheusBaseURL(enclave)
	if baseURL == "" || !waitHealthy(baseURL, c.clusterName(), cfg.startupDeadlineMinutes*60) {
		return failedReport(c, cycle, "cluster did not become healthy before deadline")
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

	data, err := collectReport(baseURL, c, cycle, windowS, host, im)
	if err != nil {
		return failedReport(c, cycle, err.Error())
	}
	data.window = windowLabel
	return data
}

// runOne mirrors cycler.py's run_one exactly: a guarded pre-clear, then
// pre-launch (git pull + build/write args file) failures produce a failed
// report and return before anything was launched; a launch failure tears
// down and returns; after a successful launch, teardown is guaranteed via
// defer, and any failure from there on (unhealthy startup, sampling,
// metrics query, report assembly) still produces a failed report. The
// top-level recover is an extra safety net beyond the Python reference (it
// has no direct analogue) so that even an unexpected panic from a fake or a
// bug never escapes to kill the caller's loop.
func runOne(cfg config, c combo, cycle int) (result reportData) {
	enclave := enclaveName(cycle, c)

	defer func() {
		if r := recover(); r != nil {
			result = failedReport(c, cycle, fmt.Sprintf("panic: %v", r))
			postBestEffort(cfg, result)
			kurtosisRemove(enclave)
		}
	}()

	kurtosisRemove(enclave) // idempotent: clear any stale enclave from a previous run

	if err := gitPull(cfg.repoPath); err != nil {
		data := failedReport(c, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		kurtosisRemove(enclave)
		return data
	}

	im, err := loadImages(cfg.repoPath)
	if err != nil {
		data := failedReport(c, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		kurtosisRemove(enclave)
		return data
	}

	argsYAML := buildArgsFile(im, c, cfg.monitoringToken, charonNodeCount)
	argsPath := filepath.Join(os.TempDir(), "network_params.yaml")
	if err := os.WriteFile(argsPath, []byte(argsYAML), 0o644); err != nil {
		data := failedReport(c, cycle, fmt.Sprintf("pre-launch failed: %v", err))
		postBestEffort(cfg, data)
		kurtosisRemove(enclave)
		return data
	}

	if err := kurtosisRun(enclave, cfg.packageRef, argsPath); err != nil {
		data := failedReport(c, cycle, fmt.Sprintf("launch failed: %v", err))
		postBestEffort(cfg, data)
		kurtosisRemove(enclave)
		return data
	}
	defer kurtosisRemove(enclave) // guaranteed teardown after a successful launch

	data := runWindow(cfg, c, cycle, enclave, im)
	postBestEffort(cfg, data)
	return data
}

// ---------------------------------------------------------------------------
// cycler.py -> main, _default_deps -> mainLoop, main
// ---------------------------------------------------------------------------

// mainLoop mirrors cycler.py's main(): resume from a possibly-interrupted
// run, then loop forever selecting the next combo, running it, and backing
// off according to consecutive failures. State-save errors are logged and
// otherwise ignored (best-effort) -- unlike the Python reference, which
// would let an unhandled exception from state.save crash the process --
// since this whole task's mandate is that the loop must never die.
func mainLoop(cfg config) {
	st, err := loadState(cfg.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charon-cycler: failed to load state (starting fresh): %v\n", err)
		st = state{}
	}

	saveState := func() {
		if err := st.save(cfg.statePath); err != nil {
			fmt.Fprintf(os.Stderr, "charon-cycler: failed to save state: %v\n", err)
		}
	}

	if st.CurrentEnclave != "" {
		kurtosisRemove(st.CurrentEnclave) // best-effort: clear an enclave left over from an interrupted run
		st.CurrentEnclave = ""
		saveState()
	}

	consecutiveFailures := 0
	for {
		c, origin := selectNextCombo(st)
		st.CurrentEnclave = enclaveName(st.Cycle, c)
		saveState()

		data := runOne(cfg, c, st.Cycle)

		st.CurrentEnclave = ""
		saveState()
		if origin == "cycle" {
			st.advance()
			saveState()
		}

		if data.status == "failed" {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}
		sleepFn(time.Duration(computeBackoff(consecutiveFailures, cfg.interRunBackoffS, cfg.maxBackoffS)) * time.Second)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "charon-cycler:", err)
		os.Exit(1)
	}
	mainLoop(cfg)
}
