package mihomo

import (
	"bufio"
	"context"
	"errors"
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

const baseDir = "/root/android-data/"

// Task 单个 mihomo 实例结构
type Task struct {
	IP        string
	Name      string
	ProxyHost string
	ProxyPort int
	Username  string
	Password  string
	Filter    []string
	TunIndex  int
	Cmd       *exec.Cmd
	Ctx       context.Context
	Cancel    context.CancelFunc
	Logs      []string
	Done      chan struct{}
	LogLock   sync.Mutex
	logFile   *os.File
	Timeout   time.Duration
}

type Manager struct {
	tasks    map[string]*Task
	usedTuns map[int]bool
	lock     sync.Mutex
}

func NewManager() *Manager {
	return &Manager{
		tasks:    make(map[string]*Task),
		usedTuns: make(map[int]bool),
	}
}

// Start name - 容器ID
// IP - 容器内网IP
// host - 代理主机地址
// port - 代理端口
// user - 代理用户名
// pass - 代理密码
// timeoutSec - 自动关闭时间(秒)
func (m *Manager) Start(name, ip, host string, port int, user, pass string, filter []string, timeoutSec int64) error {
	if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
		logger.Error("[-] 无效的 IP 地址: %s（必须是合法 IPv4）", ip)
		return errors.New("无效的IP地址: " + ip)
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, exists := m.tasks[ip]; exists {
		logger.Info("[!] 任务 %s 已存在，跳过启动", ip)
		return errors.New("指定IP已存在运行中的代理")
	}

	tunIndex, err := m.allocateTun()
	if err != nil {
		logger.Error("[-] 分配 tun 失败: %v", err)
		return err
	}

	configDir := m.genConfigDir(name)
	os.MkdirAll(configDir, 0777)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, config.GetString("baseDir")+"/mihomo-"+runtime.GOARCH, "-d", configDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	logPath := filepath.Join(configDir, "log.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	task := &Task{
		IP:        ip,
		Name:      name,
		ProxyHost: host,
		ProxyPort: port,
		Username:  user,
		Password:  pass,
		Filter:    filter,
		TunIndex:  tunIndex,
		Ctx:       ctx,
		Cancel:    cancel,
		Cmd:       cmd,
		Done:      make(chan struct{}),
		logFile:   logFile,
		Timeout:   time.Duration(timeoutSec) * time.Second,
	}

	m.writeConfig(task)

	go task.collectLogs(stdout, logFile)
	go task.collectLogs(stderr, logFile)

	if err = cmd.Start(); err != nil {
		logger.Error("[-] 启动 mihomo 失败: %v", err)
		task.logFile.Close()
		close(task.Done)
		return err
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
		task.logFile.Close()
		close(task.Done)

		if err != nil {
			logger.Error("[-] mihomo %s 异常退出: %v", task.IP, err)
		} else {
			logger.Warning("[*] mihomo %s 正常退出", task.IP)
		}

	}(task)

	m.tasks[ip] = task

	if !m.waitForTun(task.TunIndex, time.Millisecond*5000) {
		logger.Error("[-] 等待 tun 设备 %v 超时，准备退出", task.TunIndex)
		m.Stop(task.IP)
		return errors.New("等待TUN网卡创建超时")
	}

	m.setupRoute(task)
	logger.Success("[+] 启动 mihomo [%s] 通过 %s:%d -> %v", ip, host, port, task.TunIndex)
	return nil
}

// Stop 停止任务
func (m *Manager) Stop(ip string) {
	m.lock.Lock()
	task, ok := m.tasks[ip]
	m.lock.Unlock()

	if !ok {
		logger.Error("[!] 未找到任务: %s", ip)
		return
	}

	logger.Warning("[x] 停止任务 %s...", ip)

	task.Cancel()
	if task.Cmd != nil && task.Cmd.Process != nil {
		if pgid, err := syscall.Getpgid(task.Cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		}
	}

	m.cleanupRoute(task)

	task.logFile.Close()
	<-task.Done

	m.lock.Lock()
	delete(m.tasks, ip)
	delete(m.usedTuns, task.TunIndex)
	m.lock.Unlock()
	logger.Info("[✔] 任务 %s 已停止并清理", ip)
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

func (m *Manager) allocateTun() (int, error) {
	for i := 0; i < 255; i++ {
		if !m.usedTuns[i] {
			m.usedTuns[i] = true
			return i, nil
		}
	}
	return -1, fmt.Errorf("暂无可用TUN网卡可分配")
}

// /root/android-data/aaa608ce34/system/proxy
func (m *Manager) genConfigDir(name string) string {
	return filepath.Join(baseDir, name, "system/proxy")
}

func (m *Manager) waitForTun(index int, timeout time.Duration) bool {
	start := time.Now()
	for {
		iface, err := net.InterfaceByName(fmt.Sprintf("tun%d", index))
		if err == nil && iface != nil {
			return true
		}
		if time.Since(start) > timeout {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (m *Manager) writeConfig(task *Task) {
	mark := 6000 + task.TunIndex
	device := fmt.Sprintf("tun%d", task.TunIndex)
	tableIndex := 2000 + task.TunIndex
	ruleIndex := 9000 + task.TunIndex
	dnsPort := 10000 + task.TunIndex
	var rules []string
	rules = append(rules, "  - DOMAIN-SUFFIX,localhost,DIRECT")
	rules = append(rules, "  - IP-CIDR,127.0.0.0/8,DIRECT")
	rules = append(rules, "  - IP-CIDR,10.0.0.0/8,DIRECT")
	rules = append(rules, "  - IP-CIDR,172.16.0.0/12,DIRECT")
	rules = append(rules, "  - IP-CIDR,192.168.0.0/16,DIRECT")
	rules = append(rules, "  - IP-CIDR,114.114.114.114/32,DIRECT")
	rules = append(rules, "  - IP-CIDR,1.1.1.1/32,DIRECT")
	rules = append(rules, "  - IP-CIDR,223.5.5.5/32,DIRECT")
	rules = append(rules, "  - IP-CIDR,223.6.6.6/32,DIRECT")
	rules = append(rules, "  - IP-CIDR,192.168.3.122/32,DIRECT")
	for i := range task.Filter {
		rules = append(rules, "  - IP-CIDR,"+task.Filter[i]+",DIRECT")
	}
	rules = append(rules, "  - MATCH,all")
	content := fmt.Sprintf(`
allow-lan: false
mode: rule
log-level: debug
ipv6: true
find-process-mode: off
routing-mark: %v
geodata-mode: false
geo-auto-update: false

tun:
  enable: true
  stack: system
  device: %v
  mtu: %v
  iproute2-table-index: %v
  iproute2-rule-index: %v
  auto-route: false
  auto-detect-interface: false
  strict-route: true
  dns-hijack: 
    - 0.0.0.0:%v

dns:
  cache-algorithm: arc
  enable: true
  prefer-h3: false
  listen: 0.0.0.0:%v
  enhanced-mode: fake-ip
  fake-ip-range: 198.%v.0.1/16
  fake-ip-filter-mode: blacklist
  default-nameserver:
    - 114.114.114.114
    - 8.8.8.8
    - system
  nameserver:
    - 114.114.114.114
    - 8.8.8.8
    - system

proxies:
  - name: s5-%v
    type: socks5
    server: %v
    port: %v
    username: "%v"
    password: "%v"
    skip-cert-verify: true
    udp: true

proxy-groups:
  - name: all
    type: select
    proxies:
      - s5-%v

rules:
%v
`, mark, device, 1480, tableIndex, ruleIndex, dnsPort, dnsPort, task.TunIndex, task.TunIndex, task.ProxyHost, task.ProxyPort, task.Username, task.Password, task.TunIndex, strings.Join(rules, "\n"))

	_ = os.WriteFile(filepath.Join(m.genConfigDir(task.Name), "config.yaml"), []byte(content), 0777)
}

func (m *Manager) setupRoute(task *Task) {
	table := fmt.Sprintf("%v", 1000+task.TunIndex)
	args := []string{"route", "replace", "default", "dev", fmt.Sprintf("tun%d", task.TunIndex), "table", table}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		logger.Error("添加策略路由失败[%v]: %v, 输出: %s", args, err, string(out))
	}

	args = []string{"rule", "add", "pref", "1", "from", task.IP, "lookup", table}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		logger.Error("添加策略路由失败[%v]: %v, 输出: %s", args, err, string(out))
	}

	logger.Info("[✔] 路由策略添加成功: %s", task.IP)
}

func (m *Manager) cleanupRoute(task *Task) {
	table := fmt.Sprintf("%v", 1000+task.TunIndex)
	args := []string{"rule", "del", "pref", "1", "from", task.IP, "lookup", table}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		logger.Error("路由策略清理失败[%v]: %v, 输出: %s", args, err, string(out))
	}

	args = []string{"route", "flush", "table", table}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		logger.Error("路由策略清理失败[%v]: %v, 输出: %s", args, err, string(out))
	}

	logger.Info("[x] 已清理策略路由: %s", task.IP)
}

// 日志收集函数
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
		_, _ = fmt.Fprintln(file, line)
	}
}
