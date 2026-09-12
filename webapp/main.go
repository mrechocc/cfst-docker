package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

const (
	defaultDataDir = "/data"
	defaultListen  = "127.0.0.1:8088"
	stateFileName  = "state.json"
	maxHistory     = 30
	maxTableRows   = 100
)

type Config struct {
	IntervalHours    int     `json:"intervalHours"`
	Threads          int     `json:"threads"`
	Packets          int     `json:"packets"`
	Downloads        int     `json:"downloads"`
	DownloadSeconds  int     `json:"downloadSeconds"`
	LatencyLimitMS   int     `json:"latencyLimitMs"`
	LossLimit        float64 `json:"lossLimit"`
	FailureThreshold int     `json:"failureThreshold"`
	AutoPrune        bool    `json:"autoPrune"`
}

type Result struct {
	IP      string  `json:"ip"`
	Sent    int     `json:"sent"`
	Received int    `json:"received"`
	Loss    float64 `json:"loss"`
	Latency float64 `json:"latency"`
	Speed   float64 `json:"speed"`
	Colo    string  `json:"colo"`
}

type Candidate struct {
	IP                  string  `json:"ip"`
	ConsecutiveFailures int     `json:"consecutiveFailures"`
	LastSuccessAt       string  `json:"lastSuccessAt"`
	Latency             float64 `json:"latency"`
	Speed               float64 `json:"speed"`
	Colo                string  `json:"colo"`
}

type RunSummary struct {
	StartedAt     string  `json:"startedAt"`
	FinishedAt    string  `json:"finishedAt"`
	Source        string  `json:"source"`
	Tested        int     `json:"tested"`
	Reachable     int     `json:"reachable"`
	SpeedVerified int     `json:"speedVerified"`
	Pruned        int     `json:"pruned"`
	Best          *Result `json:"best,omitempty"`
	CSVFile       string  `json:"csvFile,omitempty"`
	Error         string  `json:"error,omitempty"`
}

type persistedState struct {
	Config      Config               `json:"config"`
	Candidates  map[string]Candidate `json:"candidates"`
	Runs        []RunSummary         `json:"runs"`
	LastResults []Result             `json:"lastResults"`
	LastCSVFile string                `json:"lastCsvFile"`
	LastLog     string                `json:"lastLog"`
}

type App struct {
	mu              sync.Mutex
	dataDir         string
	state           persistedState
	running         bool
	startedAt       string
	currentSource   string
	lastError       string
	nextRunAt       string
	scheduleChanged chan struct{}
}

type stateResponse struct {
	Config         Config       `json:"config"`
	Running        bool         `json:"running"`
	StartedAt      string       `json:"startedAt,omitempty"`
	CurrentSource  string       `json:"currentSource,omitempty"`
	NextRunAt      string       `json:"nextRunAt,omitempty"`
	LastError      string       `json:"lastError,omitempty"`
	CandidateCount int          `json:"candidateCount"`
	Candidates     []Candidate  `json:"candidates"`
	Results        []Result     `json:"results"`
	Runs           []RunSummary `json:"runs"`
	LastLog        string       `json:"lastLog"`
}

func defaultConfig() Config {
	return Config{
		IntervalHours:    6,
		Threads:          50,
		Packets:          4,
		Downloads:        20,
		DownloadSeconds:  5,
		LatencyLimitMS:   500,
		LossLimit:        0.10,
		FailureThreshold: 3,
		AutoPrune:        true,
	}
}

func main() {
	dataDir := os.Getenv("CFST_WEB_DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	app, err := newApp(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	go app.scheduleLoop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", app.handleState)
	mux.HandleFunc("PUT /api/config", app.handleConfig)
	mux.HandleFunc("POST /api/run", app.handleRun)
	mux.HandleFunc("POST /api/library/reset", app.handleLibraryReset)
	mux.HandleFunc("GET /api/download/current", app.handleDownload)

	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFiles)))

	listen := os.Getenv("CFST_WEB_ADDR")
	if listen == "" {
		listen = defaultListen
	}
	log.Printf("CFST web console listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, basicAuth(mux)))
}

func newApp(dataDir string) (*App, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "results"), 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	a := &App{dataDir: dataDir, scheduleChanged: make(chan struct{}, 1)}
	a.state = persistedState{Config: defaultConfig(), Candidates: make(map[string]Candidate)}

	content, err := os.ReadFile(filepath.Join(dataDir, stateFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read state: %w", err)
		}
		return a, nil
	}
	if err := json.Unmarshal(content, &a.state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if a.state.Candidates == nil {
		a.state.Candidates = make(map[string]Candidate)
	}
	if err := validateConfig(a.state.Config); err != nil {
		a.state.Config = defaultConfig()
	}
	if len(a.state.Candidates) > 0 {
		if err := a.writeLibraryLocked(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *App) scheduleLoop() {
	for {
		a.mu.Lock()
		delay := time.Duration(a.state.Config.IntervalHours) * time.Hour
		a.nextRunAt = time.Now().Add(delay).Format(time.RFC3339)
		a.mu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			a.startRun()
		case <-a.scheduleChanged:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *App) startRun() bool {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return false
	}
	sourceFile, sourceName := a.sourceLocked()
	a.running = true
	a.startedAt = time.Now().Format(time.RFC3339)
	a.currentSource = sourceName
	a.lastError = ""
	a.mu.Unlock()

	go a.run(sourceFile, sourceName)
	return true
}

func (a *App) sourceLocked() (string, string) {
	if len(a.state.Candidates) == 0 {
		return "/app/ip.txt", "全量网段收集"
	}
	return filepath.Join(a.dataDir, "ip-library.txt"), "精简 IP 库复测"
}

func (a *App) run(sourceFile, sourceName string) {
	started := time.Now()
	stamp := started.Format("20060102-150405")
	csvFile := filepath.Join(a.dataDir, "results", "result-"+stamp+".csv")

	a.mu.Lock()
	config := a.state.Config
	a.mu.Unlock()

	args := []string{
		"-f", sourceFile,
		"-o", csvFile,
		"-n", strconv.Itoa(config.Threads),
		"-t", strconv.Itoa(config.Packets),
		"-dn", strconv.Itoa(config.Downloads),
		"-dt", strconv.Itoa(config.DownloadSeconds),
		"-tl", strconv.Itoa(config.LatencyLimitMS),
		"-tlr", strconv.FormatFloat(config.LossLimit, 'f', 2, 64),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/app/cfst", args...)
	cmd.Dir = "/app"
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()

	summary := RunSummary{
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: time.Now().Format(time.RFC3339),
		Source:     sourceName,
		CSVFile:    filepath.Base(csvFile),
	}
	if err != nil {
		summary.Error = err.Error()
		if ctx.Err() != nil {
			summary.Error = ctx.Err().Error()
		}
	}

	results, parseErr := readResults(csvFile)
	if parseErr != nil && summary.Error == "" {
		summary.Error = parseErr.Error()
	}
	if len(results) > 0 {
		summary.Tested = len(results)
		a.applyResults(sourceName, results, config, &summary)
	}

	a.mu.Lock()
	a.state.LastLog = tail(output.String(), 12000)
	if summary.Error != "" {
		a.lastError = summary.Error
	}
	a.state.LastCSVFile = summary.CSVFile
	a.state.Runs = append([]RunSummary{summary}, a.state.Runs...)
	if len(a.state.Runs) > maxHistory {
		a.state.Runs = a.state.Runs[:maxHistory]
	}
	if err := a.persistLocked(); err != nil {
		a.lastError = err.Error()
	}
	a.running = false
	a.currentSource = ""
	a.mu.Unlock()
}

func (a *App) applyResults(sourceName string, results []Result, config Config, summary *RunSummary) {
	sortResults(results)
	now := time.Now().Format(time.RFC3339)

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, r := range results {
		if isReachable(r, config.LossLimit) {
			summary.Reachable++
		}
		if r.Speed > 0 {
			summary.SpeedVerified++
		}
	}
	if len(results) > 0 && results[0].Speed > 0 {
		best := results[0]
		summary.Best = &best
	}

	if sourceName == "全量网段收集" {
		for _, r := range results {
			if !isReachable(r, config.LossLimit) {
				continue
			}
			a.state.Candidates[r.IP] = candidateFromResult(r, now, 0)
		}
	} else {
		byIP := make(map[string]Result, len(results))
		for _, r := range results {
			byIP[r.IP] = r
		}
		for ip, candidate := range a.state.Candidates {
			r, ok := byIP[ip]
			if ok && isReachable(r, config.LossLimit) {
				a.state.Candidates[ip] = candidateFromResult(r, now, 0)
				continue
			}
			candidate.ConsecutiveFailures++
			if config.AutoPrune && candidate.ConsecutiveFailures >= config.FailureThreshold {
				delete(a.state.Candidates, ip)
				summary.Pruned++
				continue
			}
			a.state.Candidates[ip] = candidate
		}
	}

	if err := a.writeLibraryLocked(); err != nil && summary.Error == "" {
		summary.Error = err.Error()
	}
	if len(results) > maxTableRows {
		a.state.LastResults = append([]Result(nil), results[:maxTableRows]...)
	} else {
		a.state.LastResults = append([]Result(nil), results...)
	}
}

func candidateFromResult(r Result, successAt string, failures int) Candidate {
	return Candidate{
		IP:                  r.IP,
		ConsecutiveFailures: failures,
		LastSuccessAt:       successAt,
		Latency:             r.Latency,
		Speed:               r.Speed,
		Colo:                r.Colo,
	}
}

func isReachable(r Result, lossLimit float64) bool {
	return r.Received > 0 && r.Loss <= lossLimit
}

func (a *App) writeLibraryLocked() error {
	ips := make([]string, 0, len(a.state.Candidates))
	for ip := range a.state.Candidates {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	content := strings.Join(ips, "\n")
	if content != "" {
		content += "\n"
	}
	return atomicWrite(filepath.Join(a.dataDir, "ip-library.txt"), []byte(content), 0o640)
}

func (a *App) persistLocked() error {
	content, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(a.dataDir, stateFileName), content, 0o640)
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readResults(path string) ([]Result, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open result CSV: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	if _, err := reader.Read(); err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	var results []Result
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row: %w", err)
		}
		if len(record) < 7 {
			continue
		}
		ip := strings.TrimSpace(record[0])
		if addr, err := netip.ParseAddr(ip); err != nil || !addr.Is4() {
			continue
		}
		result := Result{
			IP:       ip,
			Sent:     parseInt(record[1]),
			Received: parseInt(record[2]),
			Loss:     parseFloat(record[3]),
			Latency:  parseFloat(record[4]),
			Speed:    parseFloat(record[5]),
			Colo:     strings.TrimSpace(record[6]),
		}
		results = append(results, result)
	}
	return results, nil
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func sortResults(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Speed != results[j].Speed {
			return results[i].Speed > results[j].Speed
		}
		if results[i].Loss != results[j].Loss {
			return results[i].Loss < results[j].Loss
		}
		return results[i].Latency < results[j].Latency
	})
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	candidates := make([]Candidate, 0, len(a.state.Candidates))
	for _, candidate := range a.state.Candidates {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Speed != candidates[j].Speed {
			return candidates[i].Speed > candidates[j].Speed
		}
		return candidates[i].Latency < candidates[j].Latency
	})
	if len(candidates) > maxTableRows {
		candidates = candidates[:maxTableRows]
	}
	response := stateResponse{
		Config:         a.state.Config,
		Running:        a.running,
		StartedAt:      a.startedAt,
		CurrentSource:  a.currentSource,
		NextRunAt:      a.nextRunAt,
		LastError:      a.lastError,
		CandidateCount: len(a.state.Candidates),
		Candidates:     candidates,
		Results:        append([]Result(nil), a.state.LastResults...),
		Runs:           append([]RunSummary(nil), a.state.Runs...),
		LastLog:        a.state.LastLog,
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	var config Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, "配置格式无效")
		return
	}
	if err := validateConfig(config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.mu.Lock()
	a.state.Config = config
	err := a.persistLocked()
	a.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败")
		return
	}
	a.notifyScheduleChanged()
	writeJSON(w, http.StatusOK, map[string]string{"message": "配置已保存"})
}

func (a *App) handleRun(w http.ResponseWriter, r *http.Request) {
	if !a.startRun() {
		writeError(w, http.StatusConflict, "测速任务正在运行")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "测速任务已启动"})
}

func (a *App) handleLibraryReset(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		writeError(w, http.StatusConflict, "测速任务运行中，暂不能重置")
		return
	}
	a.state.Candidates = make(map[string]Candidate)
	a.state.LastResults = nil
	err := a.writeLibraryLocked()
	if err == nil {
		err = a.persistLocked()
	}
	a.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "重置候选库失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "候选库已清空，下次将执行全量网段收集"})
}

func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	fileName := a.state.LastCSVFile
	a.mu.Unlock()
	if fileName == "" || filepath.Base(fileName) != fileName {
		writeError(w, http.StatusNotFound, "暂无结果文件")
		return
	}
	path := filepath.Join(a.dataDir, "results", fileName)
	w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
	http.ServeFile(w, r, path)
}

func validateConfig(config Config) error {
	if config.IntervalHours < 1 || config.IntervalHours > 168 {
		return fmt.Errorf("运行间隔应为 1 到 168 小时")
	}
	if config.Threads < 1 || config.Threads > 200 {
		return fmt.Errorf("延迟线程应为 1 到 200")
	}
	if config.Packets < 1 || config.Packets > 10 {
		return fmt.Errorf("延迟测试次数应为 1 到 10")
	}
	if config.Downloads < 1 || config.Downloads > 50 {
		return fmt.Errorf("下载测速数量应为 1 到 50")
	}
	if config.DownloadSeconds < 1 || config.DownloadSeconds > 60 {
		return fmt.Errorf("下载测速时间应为 1 到 60 秒")
	}
	if config.LatencyLimitMS < 10 || config.LatencyLimitMS > 5000 {
		return fmt.Errorf("延迟上限应为 10 到 5000 毫秒")
	}
	if config.LossLimit < 0 || config.LossLimit > 1 {
		return fmt.Errorf("丢包率上限应为 0.00 到 1.00")
	}
	if config.FailureThreshold < 1 || config.FailureThreshold > 10 {
		return fmt.Errorf("失败删除阈值应为 1 到 10 次")
	}
	return nil
}

func (a *App) notifyScheduleChanged() {
	select {
	case a.scheduleChanged <- struct{}{}:
	default:
	}
}

func basicAuth(next http.Handler) http.Handler {
	user := os.Getenv("CFST_WEB_USER")
	password := os.Getenv("CFST_WEB_PASSWORD")
	if password == "" {
		log.Printf("warning: web authentication is disabled; keep CFST_WEB_ADDR on 127.0.0.1")
		return next
	}
	if user == "" {
		user = "admin"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedUser, providedPassword, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(providedUser), []byte(user)) == 1
		passwordOK := subtle.ConstantTimeCompare([]byte(providedPassword), []byte(password)) == 1
		if !ok || !userOK || !passwordOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="CFST console"`)
			writeError(w, http.StatusUnauthorized, "需要登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tail(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
