package mihomo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CodeDogRun/go-framework/config"
	"github.com/CodeDogRun/go-framework/logger"
)

// Task 单个 mihomo 实例结构
type Task struct {
	IP          string
	TableName   string
	ProxyHost   string
	ProxyPort   int
	Username    string
	Password    string
	TunIndex    int
	TunName     string
	Mtu         int
	Address     string
	Gateway     string
	Cmd         *exec.Cmd
	Ctx         context.Context
	Cancel      context.CancelFunc
	Logs        []string
	Done        chan struct{}
	LogLock     sync.Mutex
	logFilePath string
	Timeout     time.Duration
}

type TaskInfo struct {
	IP         string
	ProxyHost  string
	ProxyPort  int
	TunName    string
	Running    bool
	LogFile    string
	TimeoutSec int64
}

type Manager struct {
	tasks    map[string]*Task
	usedTuns map[string]bool
	lock     sync.Mutex
}

func NewManager() *Manager {
	return &Manager{
		tasks:    make(map[string]*Task),
		usedTuns: make(map[string]bool),
	}
}

func (m *Manager) Start(ip, host string, port int, user, pass string, mtu int, timeoutSec int64) {
	if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
		logger.Error("[-] 无效的 IP 地址: %s（必须是合法 IPv4）", ip)
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, exists := m.tasks[ip]; exists {
		logger.Info("[!] 任务 %s 已存在，跳过启动", ip)
		return
	}

	tunIndex, tun, addr, gw, err := m.allocateTun()
	if err != nil {
		logger.Error("[-] 分配 tun 失败: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, config.GetString("baseDir")+"/mihomo-"+runtime.GOARCH, "-d", m.genConfigDir(ip))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	logPath := filepath.Join("logs", fmt.Sprintf("mihomo_%s.log", strings.ReplaceAll(ip, ".", "-")))
	_ = os.MkdirAll("logs", 0755)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	task := &Task{
		IP:          ip,
		TableName:   "t" + strings.ReplaceAll(ip, ".", "_"),
		ProxyHost:   host,
		ProxyPort:   port,
		Username:    user,
		Password:    pass,
		TunIndex:    tunIndex,
		TunName:     tun,
		Mtu:         mtu,
		Address:     addr,
		Gateway:     gw,
		Ctx:         ctx,
		Cancel:      cancel,
		Cmd:         cmd,
		Done:        make(chan struct{}),
		logFilePath: logPath,
		Timeout:     time.Duration(timeoutSec) * time.Second,
	}

	m.writeConfig(task)

	go task.collectLogs(stdout, logFile)
	go task.collectLogs(stderr, logFile)

	if err = cmd.Start(); err != nil {
		logger.Error("[-] 启动 mihomo 失败: %v", err)
		close(task.Done)
		return
	}

	// 自动停止计时器
	if task.Timeout > 0 {
		go func(ip string, d time.Duration) {
			time.Sleep(d)
			logger.Warning("[⏰] 任务 %s 到达指定运行时间 %v，自动停止", ip, d)
			m.Stop(ip)
		}(ip, task.Timeout)
	}

	go func(task *Task) {
		err = cmd.Wait()
		close(task.Done)

		if err != nil {
			logger.Error("[-] mihomo %s 异常退出: %v", task.IP, err)
		} else {
			logger.Warning("[*] mihomo %s 正常退出", task.IP)
		}

	}(task)

	m.tasks[ip] = task

	// 新增：等待 tunX 被系统识别成功（限时 5 秒）
	if !m.waitForTun(task.TunName, time.Millisecond*5000) {
		logger.Error("[-] 等待 tun 设备 %s 超时，准备退出", task.TunName)
		m.Stop(task.IP)
		return
	}

	m.setupRoute(task)
	logger.Success("[+] 启动 mihomo [%s] 通过 %s:%d -> %s", ip, host, port, tun)
}

// Stop 停止任务
func (m *Manager) Stop(id string) {
	m.lock.Lock()
	task, ok := m.tasks[id]
	m.lock.Unlock()

	if !ok {
		logger.Error("[!] 未找到任务: %s", id)
		return
	}

	logger.Warning("[x] 停止任务 %s...", id)

	task.Cancel()
	if task.Cmd != nil && task.Cmd.Process != nil {
		if pgid, err := syscall.Getpgid(task.Cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		}
	}

	// 1. 清理路由策略
	m.cleanupRoute(task)

	// 2. 删除 mihomo 配置文件目录
	configDir := m.genConfigDir(task.IP)
	if err := os.RemoveAll(configDir); err != nil {
		logger.Warning("[-] 删除配置目录失败: %s -> %v", configDir, err)
	} else {
		logger.Info("[✔] 配置目录已清理: %s", configDir)
	}

	<-task.Done

	m.lock.Lock()
	delete(m.tasks, id)
	delete(m.usedTuns, task.TunName)
	m.lock.Unlock()
	logger.Info("[✔] 任务 %s 已停止并清理", id)
}

func (m *Manager) List() []TaskInfo {
	m.lock.Lock()
	list := make([]TaskInfo, 0, len(m.tasks))
	for _, t := range m.tasks {
		list = append(list, TaskInfo{
			IP:         t.IP,
			ProxyHost:  t.ProxyHost,
			ProxyPort:  t.ProxyPort,
			TunName:    t.TunName,
			Running:    true,
			LogFile:    t.logFilePath,
			TimeoutSec: int64(t.Timeout.Seconds()),
		})
	}
	m.lock.Unlock()
	return list
}

// StopAll 停止所有任务
func (m *Manager) StopAll() {
	m.lock.Lock()
	ids := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		ids = append(ids, id)
	}
	m.lock.Unlock()

	for _, id := range ids {
		m.Stop(id)
	}
}

func (m *Manager) allocateTun() (int, string, string, string, error) {
	for i := 0; i < 255; i++ {
		tun := fmt.Sprintf("tun%d", i)
		if !m.usedTuns[tun] {
			addr := fmt.Sprintf("10.10.%d.2/30", i)
			gw := fmt.Sprintf("10.10.%d.1", i)
			m.usedTuns[tun] = true
			return i, tun, addr, gw, nil
		}
	}
	return -1, "", "", "", fmt.Errorf("无可用 tun")
}

func (m *Manager) genConfigDir(ip string) string {
	return filepath.Join("/root/android-data", strings.ReplaceAll(ip, ".", "-"))
}

func (m *Manager) waitForTun(name string, timeout time.Duration) bool {
	start := time.Now()
	for {
		iface, err := net.InterfaceByName(name)
		if err == nil && iface != nil {
			return true
		}
		if time.Since(start) > timeout {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m *Manager) writeConfig(task *Task) {
	configDir := m.genConfigDir(task.IP)
	_ = os.RemoveAll(configDir)
	_ = os.MkdirAll(configDir, 0755)

	path := filepath.Join(m.genConfigDir(task.IP), "config.yaml")

	mixedPort := 7890 + task.TunIndex
	dnsPort := 10053 + task.TunIndex

	content := fmt.Sprintf(`
mode: rule
log-level: debug
allow-lan: false
find-process-mode: off
ipv6: true
authentication:
  - "%v:%v"

tun:
  enable: true
  stack: system
  auto-route: false
  auto-detect-interface: false
  device: %v
  mtu: %v
  dns-hijack: 
    - 0.0.0.0:%v

dns:
  cache-algorithm: arc
  enable: true
  prefer-h3: false
  listen: 0.0.0.0:%v
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.%v.1/16
  fake-ip-filter-mode: blacklist
  default-nameserver:
    - 114.114.114.114
    - 8.8.8.8
    - tls://1.12.12.12:853
    - tls://223.5.5.5:853
    - system
  nameserver:
    - 114.114.114.114
    - 8.8.8.8
    - tls://1.12.12.12:853
    - tls://223.5.5.5:853
    - system

proxies:
  - name: %v
    type: socks5
    server: %v
    port: %v
    username: %v
    password: %v
    skip-cert-verify: true
    udp: true

proxy-groups:
  - name: all
    type: select
    proxies:
      - %v

rules:
  - IP-CIDR,192.168.0.0/16,DIRECT
  - IP-CIDR,172.16.0.0/12,DIRECT
  - IP-CIDR,10.0.0.0/8,DIRECT
  - MATCH,all

`, task.Username, task.Password, task.TunName, task.Mtu, dnsPort, dnsPort, task.TunIndex, task.TableName, task.ProxyHost, task.ProxyPort, task.Username, task.Password, task.TableName)

	logger.Info("写入代理配置信息: mixedPort[%v], TunName[%v], Mtu[%v], Address[%v], Gateway[%v], dnsPort[%v], proxyName[%v], ProxyHost[%v], ProxyPort[%v], Username[%v], Password[%v]", mixedPort, task.TunName, task.Mtu, task.Address, task.Gateway, dnsPort, task.TableName, task.ProxyHost, task.ProxyPort, task.Username, task.Password)

	_ = os.WriteFile(path, []byte(content), 0644)
}

func (m *Manager) setupRoute(task *Task) {
	tableLine := fmt.Sprintf("%v %s", 100+task.TunIndex, task.TableName)
	data, err := os.ReadFile("/etc/iproute2/rt_tables")
	if err == nil && !strings.Contains(string(data), tableLine) {
		f, err := os.OpenFile("/etc/iproute2/rt_tables", os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			defer f.Close()
			_, _ = f.WriteString(tableLine + "\n")
			logger.Info("[+] 已添加路由表定义: %s", tableLine)
		} else {
			logger.Error("[-] 写入 rt_tables 失败: %v", err)
		}
	}

	logger.Info("添加路由策略: %v, %v, %v", task.IP, task.TunName, task.TableName)

	if out, err := exec.Command("ip", "rule", "add", "from", task.IP, "lookup", task.TableName).CombinedOutput(); err != nil {
		logger.Error("添加策略路由失败-1: %v, 输出: %s", err, string(out))
	}
	if out, err := exec.Command("ip", "route", "add", "default", "dev", task.TunName, "table", task.TableName).CombinedOutput(); err != nil {
		logger.Error("添加策略路由失败-2: %v, 输出: %s", err, string(out))
	}

	if out, err := exec.Command("ip", "route", "flush", "cache").CombinedOutput(); err != nil {
		logger.Error("刷新路由表: %v, 输出: %s", err, string(out))
	}
}

func (m *Manager) cleanupRoute(task *Task) {
	// 删除规则和路由表项
	if out, err := exec.Command("ip", "rule", "del", "from", task.IP, "lookup", task.TableName).CombinedOutput(); err != nil {
		logger.Error("清理策略路由失败: %v, 输出: %s", err, string(out))
	}
	if out, err := exec.Command("ip", "route", "flush", "table", task.TableName).CombinedOutput(); err != nil {
		logger.Error("清理策略路由失败: %v, 输出: %s", err, string(out))
	}

	logPath := filepath.Join("logs", fmt.Sprintf("mihomo_%s.log", strings.ReplaceAll(task.IP, ".", "-")))
	logger.Warning("清理代理日志: %v", logPath)
	_ = os.Remove(logPath)

	// 打开文件
	file, err := os.Open("/etc/iproute2/rt_tables")
	if err != nil {
		return
	}
	defer file.Close()

	// 创建 Scanner 按行读取
	scanner := bufio.NewScanner(file)
	content := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, task.TableName) {
			content += line + "\n"
		}
	}

	_ = os.WriteFile("/etc/iproute2/rt_tables", []byte(content), 0644)

	if out, err := exec.Command("ip", "route", "flush", "cache").CombinedOutput(); err != nil {
		logger.Error("刷新路由表: %v, 输出: %s", err, string(out))
	}

	logger.Info("[x] 已清理策略路由: %s -> %s", task.IP, task.TableName)
}

// 日志收集函数（带文件写入 + 内存限制）
func (task *Task) collectLogs(pipe io.ReadCloser, file *os.File) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()

		// 限制内存日志
		task.LogLock.Lock()
		if len(task.Logs) >= 1000 {
			task.Logs = task.Logs[1:]
		}
		task.Logs = append(task.Logs, line)
		task.LogLock.Unlock()

		// 写入文件
		fmt.Fprintln(file, line)
	}
}
