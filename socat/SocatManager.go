package socat

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/CodeDogRun/go-framework/logger"

	"github.com/google/uuid"
)

type Task struct {
	ID         string
	LocalPort  int
	RemoteIP   string
	RemotePort int

	Cancel    context.CancelFunc
	Done      chan struct{}
	Timeout   time.Duration
	StartedAt time.Time

	ActiveConns map[string]net.Conn
	ConnLock    sync.Mutex
}

type TaskInfo struct {
	ID         string
	LocalPort  int
	RemoteIP   string
	RemotePort int
	Running    bool
	TimeoutSec int64
}

type Manager struct {
	tasks map[string]*Task
	lock  sync.Mutex
}

func NewManager() *Manager {
	return &Manager{
		tasks: make(map[string]*Task),
	}
}

func (m *Manager) Start(id string, localPort int, remoteIP string, remotePort int, timeoutSec int64) {
	m.lock.Lock()
	if _, exists := m.tasks[id]; exists {
		m.lock.Unlock()
		logger.Warning("[!] 任务 %s 已存在", id)
		return
	}
	m.lock.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	task := &Task{
		ID:          id,
		LocalPort:   localPort,
		RemoteIP:    remoteIP,
		RemotePort:  remotePort,
		Cancel:      cancel,
		Done:        done,
		Timeout:     time.Duration(timeoutSec) * time.Second,
		StartedAt:   time.Now(),
		ActiveConns: make(map[string]net.Conn),
	}

	go func() {
		defer close(done)
		err := m.serve(ctx, task)
		if err != nil {
			logger.Error("[-] 任务 %s 监听失败: %v", id, err)
		}
	}()

	// 自动停止定时器
	if task.Timeout > 0 {
		go func(id string, d time.Duration) {
			time.Sleep(d)
			m.lock.Lock()
			_, stillRunning := m.tasks[id]
			m.lock.Unlock()
			if stillRunning {
				logger.Warning("[⏰] 任务 %s 已运行 %v，自动停止", id, d)
				m.Stop(id)
			}
		}(id, task.Timeout)
	}

	m.lock.Lock()
	m.tasks[id] = task
	m.lock.Unlock()

	logger.Success("[+] 启动任务 %s：本地 %d → %s:%d（%ds 超时）", id, localPort, remoteIP, remotePort, timeoutSec)
}

func (m *Manager) serve(ctx context.Context, task *Task) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", task.LocalPort))
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("[-] 接受连接失败: %v", err)
			continue
		}
		go m.handleConnection(ctx, clientConn, task)
	}
}

func (m *Manager) handleConnection(ctx context.Context, clientConn net.Conn, task *Task) {
	connID := uuid.New().String()

	task.ConnLock.Lock()
	task.ActiveConns[connID] = clientConn
	task.ConnLock.Unlock()

	defer func() {
		clientConn.Close()
		task.ConnLock.Lock()
		delete(task.ActiveConns, connID)
		task.ConnLock.Unlock()
	}()

	serverConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", task.RemoteIP, task.RemotePort))
	if err != nil {
		logger.Error("[-] 无法连接目标 %s:%d: %v", task.RemoteIP, task.RemotePort, err)
		return
	}
	defer serverConn.Close()

	go io.Copy(serverConn, clientConn)
	io.Copy(clientConn, serverConn)
}

func (m *Manager) Stop(id string) {
	m.lock.Lock()
	task, ok := m.tasks[id]
	m.lock.Unlock()

	if !ok {
		logger.Error("[!] 未找到任务: %s", id)
		return
	}

	logger.Warning("[x] 正在停止任务 %s", id)

	// 主动关闭所有活跃连接
	task.ConnLock.Lock()
	for cid, conn := range task.ActiveConns {
		_ = conn.Close()
		logger.Info("[x] 关闭连接 %s", cid)
	}
	task.ActiveConns = make(map[string]net.Conn)
	task.ConnLock.Unlock()

	task.Cancel()
	<-task.Done

	m.lock.Lock()
	delete(m.tasks, id)
	m.lock.Unlock()

	logger.Info("[✔] 任务 %s 已停止", id)
}

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

func (m *Manager) List() []TaskInfo {
	m.lock.Lock()
	defer m.lock.Unlock()

	list := make([]TaskInfo, 0, len(m.tasks))
	for _, t := range m.tasks {
		list = append(list, TaskInfo{
			ID:         t.ID,
			LocalPort:  t.LocalPort,
			RemoteIP:   t.RemoteIP,
			RemotePort: t.RemotePort,
			Running:    true,
			TimeoutSec: int64(t.Timeout.Seconds()),
		})
	}
	return list
}
