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

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	yaml "gopkg.in/yaml.v3"
)

type ServerCfg struct {
	Addr         string
	ReadTimeout  string
	WriteTimeout string
	IdleTimeout  string
}

type RedisCfg struct {
	Addr     string
	Password string
	DB       int
}

type AnsibleCfg struct {
	PlaybookRoot string
	LogDir       string
	User         string
}

type ExecCfg struct {
	HostnameTimeout string
	PlaybookTimeout string
	OverallTimeout  string
	Env             []string
}

type Config struct {
	Server  ServerCfg
	Redis   RedisCfg
	Ansible AnsibleCfg
	Exec    ExecCfg
}

// 请求与校验
var (
	idRe       = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*-\d{3}$`)
	ipRe       = regexp.MustCompile(`^(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)$`)
)

type HostReq struct {
	ID       string `json:"ID"`
	Hostname string `json:"Hostname"`
	IP       string `json:"IP"`
}

type App struct {
	cfg Config
	rdb *redis.Client
}

// 主程序
func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	app := &App{cfg: cfg, rdb: rdb}

	// gin 初始化
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// v1 host API
	v1Host := r.Group("/v1/host")
	{
		v1Host.POST("/register", app.handleRegistry)
		v1Host.POST("/unregister", app.handleUnregistry)
	}

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      r,
		ReadTimeout:  mustDur(cfg.Server.ReadTimeout, 10*time.Second),
		WriteTimeout: mustDur(cfg.Server.WriteTimeout, 0),
		IdleTimeout:  mustDur(cfg.Server.IdleTimeout, 120*time.Second),
	}

	log.Printf("ansible-gateway listening on %s", cfg.Server.Addr)
	log.Fatal(server.ListenAndServe())
}

// 配置加载
func loadConfig(path string) (Config, error) {
	// 1. 读文件（只负责把 bytes 读出来）
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	// 2. YAML 解析（负责“看得懂”缩进、冒号、列表等语法）
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config yaml: %w", err)
	}

	return cfg, nil
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

func (a *App) handleRegistry(c *gin.Context) {
	// 只允许 POST（路由已经是 POST，这里再兜一层）
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	if err := validate(req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	// 计算 hostgroup（去掉最后的 -NNN）
	parts := strings.Split(req.Hostname, "-")
	hostgroup := strings.Join(parts[:len(parts)-1], "-")

	// 流式输出（避免一次性缓冲导致代理读超时）
	w := c.Writer
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ctx := c.Request.Context()
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
		http.Error(w, "redis error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !okSet {
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored != val {
			http.Error(
				w,
				fmt.Sprintf("[CONFLICT] already registered by %q, incoming=%q", stored, val),
				http.StatusConflict,
			)
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
		http.Error(w, "playbook select error: "+err.Error(), http.StatusNotFound)
		return
	}
	logf("[INFO] use playbook: %s", playbook)

	// 写 inventory 文件
	if err := os.MkdirAll(a.cfg.Ansible.LogDir, 0o755); err != nil {
		http.Error(w, "mkdir log_dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	invBase := fmt.Sprintf("%s__%s__%s.txt", req.ID, req.Hostname, req.IP)
	invPath := filepath.Join(a.cfg.Ansible.LogDir, invBase)
	if err := os.WriteFile(invPath, []byte("["+hostgroup+"]\n"+req.IP+"\n"), 0o644); err != nil {
		http.Error(w, "write inventory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logf("[INFO] inventory written: %s", invPath)

	// 步骤 1：设置主机名（通过 ansible 模块 shell）
	hostnameCmd := fmt.Sprintf(
		"ansible -u %s %s -i %s -m shell -a 'hostnamectl set-hostname %s'",
		a.cfg.Ansible.User, req.IP, invPath, req.Hostname,
	)
	if err := a.runAndStream(ctx, hostnameCmd, mustDur(a.cfg.Exec.HostnameTimeout, time.Minute), w, logf); err != nil {
		http.Error(w, "hostname step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 步骤 2：执行 playbook 并 tee 到日志
	logFile := strings.TrimSuffix(invPath, ".txt") +
		"__" + time.Now().Format("2006-01-02_15:04:05.000000") + ".log"

	playbookCmd := fmt.Sprintf(
		"cd %s && ansible-playbook %s -i %s -e hosts=%s 2>&1 | tee %s",
		a.cfg.Ansible.PlaybookRoot, playbook, invPath, hostgroup, logFile,
	)
	if err := a.runAndStream(ctx, playbookCmd, mustDur(a.cfg.Exec.PlaybookTimeout, 2*time.Hour), w, logf); err != nil {
		http.Error(w, "playbook step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logf("[INFO] initialize host done. log=%s", logFile)
}

// 解除注册：清理 Redis 键，便于后续重新注册（迁移/重装主机）
func (a *App) handleUnregistry(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	if req.Hostname == "" {
		c.String(http.StatusBadRequest, "missing hostname")
		return
	}

	lockKey := "LOCK__" + req.Hostname
	ctx := c.Request.Context()

	if req.ID != "" || req.IP != "" {
		incoming := strings.Trim(req.ID+"__"+req.IP, "_")
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored != incoming {
			c.String(http.StatusPreconditionFailed, "mismatch: stored=%q incoming=%q", stored, incoming)
			return
		}
	}

	_, _ = a.rdb.Del(ctx, lockKey).Result()
	c.JSON(http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": lockKey,
	})
}

// --- 业务辅助函数 ---

func validate(req HostReq) error {
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

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (a *App) runAndStream(
	ctx context.Context,
	shellCmd string,
	timeout time.Duration,
	w http.ResponseWriter,
	logf func(string, ...any),
) error {
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
