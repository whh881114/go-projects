package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type ServerCfg struct{ Addr, ReadTimeout, WriteTimeout, IdleTimeout string }

type RedisCfg struct {
	Addr, Password string
	DB             int
}

type AnsibleCfg struct{ PlaybookRoot, LogDir, User string }

type ExecCfg struct {
	HostnameTimeout, PlaybookTimeout, OverallTimeout string
	Env                                              []string
}

type Config struct {
	Server  ServerCfg
	Redis   RedisCfg
	Ansible AnsibleCfg
	Exec    ExecCfg
}

// --- 请求与校验 ---
var (
	idRe       = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*-\d{3}$`)
	ipRe       = regexp.MustCompile(`^(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)$`)
)

type InitReq struct{ ID, Hostname, IP string }

type App struct {
	cfg Config
	rdb *redis.Client
}

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	app := &App{cfg: cfg, rdb: rdb}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/v1/host/register", app.handleRegistry)
	mux.HandleFunc("/v1/host/unregister", app.handleUnregistry)

	server := &http.Server{Addr: cfg.Server.Addr, Handler: mux, ReadTimeout: mustDur(cfg.Server.ReadTimeout, 10*time.Second), WriteTimeout: mustDur(cfg.Server.WriteTimeout, 0), IdleTimeout: mustDur(cfg.Server.IdleTimeout, 120*time.Second)}
	log.Printf("listening on %s", cfg.Server.Addr)
	log.Fatal(server.ListenAndServe())
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	// 允许 JSON 或 YAML（极简解析：优先 JSON，失败再 YAML）
	var c Config
	if json.Unmarshal(b, &c) == nil {
		return c, nil
	}
	// 轻量 YAML 解析：支持 "k: v" 格式与简单嵌套（避免引第三方依赖，便于单文件复制）
	return parseNaiveYAML(b)
}

// --- 极简 YAML 解析器（够用就好，结构固定） ---
func parseNaiveYAML(b []byte) (Config, error) {
	type section map[string]string
	m := map[string]section{}
	var cur string
	for _, ln := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(ln)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.Contains(line, " ") {
			cur = strings.TrimSuffix(line, ":")
			m[cur] = section{}
			continue
		}
		if cur == "" {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		m[cur][k] = v
	}
	var c Config
	c.Server = ServerCfg{Addr: m["server"]["addr"], ReadTimeout: m["server"]["read_timeout"], WriteTimeout: m["server"]["write_timeout"], IdleTimeout: m["server"]["idle_timeout"]}
	c.Redis = RedisCfg{Addr: m["redis"]["addr"], Password: m["redis"]["password"], DB: atoiDefault(m["redis"]["db"], 0)}
	c.Ansible = AnsibleCfg{PlaybookRoot: m["ansible"]["playbook_root"], LogDir: m["ansible"]["log_dir"], User: m["ansible"]["user"]}
	c.Exec = ExecCfg{HostnameTimeout: m["exec"]["hostname_timeout"], PlaybookTimeout: m["exec"]["playbook_timeout"], OverallTimeout: m["exec"]["overall_timeout"], Env: nil}
	return c, nil
}

func atoiDefault(s string, d int) int {
	if s == "" {
		return d
	}
	v, err := fmt.Sscanf(s, "%d", &d)
	_ = v
	if err != nil {
		return d
	}
	return d
}
func mustDur(s string, d time.Duration) time.Duration {
	if s == "" || s == "0" {
		return d
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return d
	}
	return v
}

// --- 处理器 ---
func (a *App) handleRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req InitReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if err := validate(req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 计算 hostgroup（去掉最后的 -NNN）
	parts := strings.Split(req.Hostname, "-")
	hostgroup := strings.Join(parts[:len(parts)-1], "-")

	// 流式输出（避免一次性缓冲导致代理读超时）
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}

	ctx := r.Context()
	if ov := a.cfg.Exec.OverallTimeout; ov != "" && ov != "0s" {
		d, _ := time.ParseDuration(ov)
		if d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(w, time.Now().Format("2006-01-02_15:04:05.000000 ")+" "+format+"\n", args...)
		flusher.Flush()
	}

	// Redis 锁
	lockKey := "LOCK__" + req.Hostname
	val := req.ID + "__" + req.IP
	logf("[INFO] trying to register: %s", lockKey)
	okSet, err := a.rdb.HSetNX(ctx, lockKey, "id__ip", val).Result()
	if err != nil {
		http.Error(w, "redis error: "+err.Error(), 500)
		return
	}
	if !okSet {
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored != val {
			http.Error(w, fmt.Sprintf("[CONFLICT] already registered by %q, incoming=%q", stored, val), 409)
			return
		}
		logf("[WARN] already registered (idempotent)")
	} else {
		logf("[INFO] registered")
	}

	// 选 playbook
	playbook, warn, err := selectPlaybook(a.cfg.Ansible.PlaybookRoot, hostgroup)
	if warn != "" {
		logf("[WARN] %s", warn)
	}
	if err != nil {
		http.Error(w, "playbook select error: "+err.Error(), 404)
		return
	}
	logf("[INFO] use playbook: %s", playbook)

	// 写 inventory 文件
	if err := os.MkdirAll(a.cfg.Ansible.LogDir, 0o755); err != nil {
		http.Error(w, "mkdir log_dir: "+err.Error(), 500)
		return
	}
	invBase := fmt.Sprintf("%s__%s__%s.txt", req.ID, req.Hostname, req.IP)
	invPath := filepath.Join(a.cfg.Ansible.LogDir, invBase)
	if err := os.WriteFile(invPath, []byte("["+hostgroup+"]\n"+req.IP+"\n"), 0o644); err != nil {
		http.Error(w, "write inventory: "+err.Error(), 500)
		return
	}
	logf("[INFO] inventory written: %s", invPath)

	// 步骤 1：设置主机名（通过 ansible 模块 shell）
	hostnameCmd := fmt.Sprintf("ansible -u %s %s -i %s -m shell -a 'hostnamectl set-hostname %s'", a.cfg.Ansible.User, req.IP, invPath, req.Hostname)
	if err := a.runAndStream(ctx, hostnameCmd, mustDur(a.cfg.Exec.HostnameTimeout, time.Minute), w, logf); err != nil {
		http.Error(w, "hostname step failed: "+err.Error(), 500)
		return
	}

	// 步骤 2：执行 playbook 并 tee 到日志
	logFile := strings.TrimSuffix(invPath, ".txt") + "__" + time.Now().Format("2006-01-02_15:04:05.000000") + ".log"
	playbookCmd := fmt.Sprintf("cd %s && ansible-playbook %s -i %s -e hosts=%s 2>&1 | tee %s", a.cfg.Ansible.PlaybookRoot, playbook, invPath, hostgroup, logFile)
	if err := a.runAndStream(ctx, playbookCmd, mustDur(a.cfg.Exec.PlaybookTimeout, 2*time.Hour), w, logf); err != nil {
		http.Error(w, "playbook step failed: "+err.Error(), 500)
		return
	}
	logf("[INFO] initialize host done. log=%s", logFile)
}

// 解除注册：清理 Redis 键，便于后续重新注册（迁移/重装主机）
func (a *App) handleUnregistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req InitReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if req.Hostname == "" {
		http.Error(w, "missing hostname", 400)
		return
	}
	lockKey := "LOCK__" + req.Hostname
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	if req.ID != "" || req.IP != "" {
		incoming := strings.Trim(req.ID+"__"+req.IP, "_")
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored != incoming {
			http.Error(w, fmt.Sprintf("mismatch: stored=%q incoming=%q", stored, incoming), 412)
			return
		}
	}
	_, _ = a.rdb.Del(ctx, lockKey).Result()
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "deleted": lockKey})
}

func validate(req InitReq) error {
	if req.ID == "" || req.Hostname == "" || req.IP == "" {
		return errors.New("missing id/hostname/ip")
	}
	if !idRe.MatchString(req.ID) {
		return fmt.Errorf("invalid id: %s", req.ID)
	}
	if !hostnameRe.MatchString(req.Hostname) {
		return fmt.Errorf("invalid hostname: %s", req.Hostname)
	}
	if !ipRe.MatchString(req.IP) {
		return fmt.Errorf("invalid ip: %s", req.IP)
	}
	return nil
}

func selectPlaybook(root, hostgroup string) (playbook, warn string, err error) {
	def := filepath.Join(root, "default.yml")
	hg := filepath.Join(root, hostgroup+".yml")
	defExists := fileExists(def)
	hgExists := fileExists(hg)
	switch {
	case defExists && !hgExists:
		return def, fmt.Sprintf("hostgroup playbook missing: %s; fallback to default", hg), nil
	case !defExists && hgExists:
		return hg, "", nil
	case defExists && hgExists:
		return hg, "both default and hostgroup exist; prefer hostgroup", nil
	default:
		return "", "", fmt.Errorf("neither playbook exists: %s, %s", def, hg)
	}
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func (a *App) runAndStream(ctx context.Context, shellCmd string, timeout time.Duration, w http.ResponseWriter, logf func(string, ...any)) error {
	logf("[INFO] run: %s", shellCmd)
	c := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		c, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(c, "/bin/bash", "-lc", shellCmd)
	// 传递额外环境变量
	if len(a.cfg.Exec.Env) > 0 {
		cmd.Env = append(os.Environ(), a.cfg.Exec.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// 合并两路输出并保持行级刷新
	merge := func(r io.Reader, prefix string) error {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for s.Scan() {
			fmt.Fprintf(w, "%s %s\n", prefix, s.Text())
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		return s.Err()
	}
	// 并发读取 stdout/stderr
	done := make(chan error, 2)
	go func() { done <- merge(stdout, "[OUT]") }()
	go func() { done <- merge(stderr, "[ERR]") }()
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			logf("[WARN] stream: %v", e)
		}
	}

	err = cmd.Wait()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("command timeout")
		}
		return err
	}
	return nil
}
