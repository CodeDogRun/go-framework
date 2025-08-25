package httpClient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/CodeDogRun/go-framework/logger"
	"resty.dev/v3"
)

const (
	T3s     = 3
	T5s     = 5
	T10s    = 10
	T15s    = 15
	T30s    = 30
	T60s    = 60
	T90s    = 90
	T120s   = 120
	T180s   = 180
	T300s   = 300
	T3600s  = 3600
	T7200s  = 7200
	T14400s = 14400
)

// Default 默认全局池：直接 httpClient.Default.C(httpClient.T30s) 取用
var Default *Pool

func init() {
	Default = NewPool(Options{
		MaxIdleConns:        400,
		MaxIdleConnsPerHost: 200,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		Expect100Timeout:    1 * time.Second,
		DialTimeout:         5 * time.Second,
		KeepAlive:           30 * time.Second,

		RetryCount:   3,
		RetryWaitMin: 800 * time.Millisecond,
		RetryWaitMax: 5 * time.Second,

		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36",
		TimeoutProfiles: []int{T3s, T5s, T10s, T15s, T30s, T60s, T90s, T120s, T180s, T300s, T3600s, T7200s, T14400s},
	})

	ctx, _ := context.WithCancel(context.Background())
	Default.StartAutoCloseIdle(ctx, 5*time.Second, func(oldHash, newHash string) {
		logger.Warning("网络发生变化: %s -> %s", oldHash[:8], newHash[:8])
	})
}

type Pool struct {
	clients   map[int]*resty.Client // key: 超时秒数
	transport *http.Transport       // 共享连接池
}

type Options struct {
	// 连接池
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	Expect100Timeout    time.Duration
	DialTimeout         time.Duration
	KeepAlive           time.Duration

	// 重试
	RetryCount   int
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration

	// TLS（仅测试环境建议使用）
	InsecureSkipVerify bool

	// 通用 Header/UA
	UserAgent    string
	CommonHeader map[string]string

	// 需要的超时档位（秒）
	TimeoutProfiles []int
}

// NewPool 初始化
func NewPool(opt Options) *Pool {
	if opt.MaxIdleConns == 0 {
		opt.MaxIdleConns = 200
	}
	if opt.MaxIdleConnsPerHost == 0 {
		opt.MaxIdleConnsPerHost = 100
	}
	if opt.IdleConnTimeout == 0 {
		opt.IdleConnTimeout = 90 * time.Second
	}
	if opt.TLSHandshakeTimeout == 0 {
		opt.TLSHandshakeTimeout = 10 * time.Second
	}
	if opt.Expect100Timeout == 0 {
		opt.Expect100Timeout = 1 * time.Second
	}
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	if opt.KeepAlive == 0 {
		opt.KeepAlive = 30 * time.Second
	}
	if opt.RetryCount == 0 {
		opt.RetryCount = 3
	}
	if opt.RetryWaitMin == 0 {
		opt.RetryWaitMin = 800 * time.Millisecond
	}
	if opt.RetryWaitMax == 0 {
		opt.RetryWaitMax = 5 * time.Second
	}
	if len(opt.TimeoutProfiles) == 0 {
		opt.TimeoutProfiles = []int{T3s, T5s, T10s, T15s, T30s, T60s, T90s, T120s, T180s, T300s, T3600s, T7200s, T14400s}
	}

	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          opt.MaxIdleConns,
		MaxIdleConnsPerHost:   opt.MaxIdleConnsPerHost,
		IdleConnTimeout:       opt.IdleConnTimeout,
		TLSHandshakeTimeout:   opt.TLSHandshakeTimeout,
		ExpectContinueTimeout: opt.Expect100Timeout,
		DialContext: (&net.Dialer{
			Timeout:   opt.DialTimeout,
			KeepAlive: opt.KeepAlive,
		}).DialContext,
	}

	clients := make(map[int]*resty.Client, len(opt.TimeoutProfiles))
	for _, secs := range opt.TimeoutProfiles {
		c := resty.New().
			SetTransport(tr).
			SetTimeout(time.Duration(secs) * time.Second).
			SetRetryCount(opt.RetryCount).
			SetRetryWaitTime(opt.RetryWaitMin).
			SetRetryMaxWaitTime(opt.RetryWaitMax)

		if opt.UserAgent != "" {
			c.SetHeader("User-Agent", opt.UserAgent)
		}
		for k, v := range opt.CommonHeader {
			c.SetHeader(k, v)
		}
		if opt.InsecureSkipVerify {
			c.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
		}
		clients[secs] = c
	}

	return &Pool{clients: clients, transport: tr}
}

// C 返回指定超时档位（秒）的 client，如 C(30)/C(120)
func (p *Pool) C(seconds int) *resty.Client {
	if c, ok := p.clients[seconds]; ok {
		return c
	}
	// 默认返回30s超时
	return p.clients[T30s]
}

// CloseIdle 在退出前或网络切换后清理空闲连接
func (p *Pool) CloseIdle() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}

// StartAutoCloseIdle 开启网络环境监控：当网卡/地址指纹变化时，自动调用 CloseIdle()
func (p *Pool) StartAutoCloseIdle(ctx context.Context, interval time.Duration, onChange func(string, string)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		prev := networkFingerprint()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := networkFingerprint()
				if cur != prev {
					// 指纹变更：主动清理空闲连接，避免复用到失效的socket
					p.CloseIdle()
					if onChange != nil {
						onChange(prev, cur)
					}
					prev = cur
				}
			}
		}
	}()
}

// networkFingerprint 采集“网络状态指纹”：UP 的接口名、IP 列表（v4/v6）做哈希。
// 注意：这是针对“本机网络变化”的近似判断，无法感知对端策略/代理等变化。
func networkFingerprint() string {
	ifis, err := net.Interfaces()
	if err != nil {
		sum := sha256.Sum256([]byte("iferror:" + err.Error()))
		return hex.EncodeToString(sum[:])
	}

	type row struct {
		Name  string
		Flags net.Flags
		IPs   []string
	}

	rows := make([]row, 0, len(ifis))
	for _, ifi := range ifis {
		// 只关心 UP 的接口，DOWN 的忽略
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		ips := make([]string, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, a.String())
		}
		sort.Strings(ips)
		rows = append(rows, row{
			Name:  ifi.Name,
			Flags: ifi.Flags,
			IPs:   ips,
		})
	}

	// 按接口名排序，保证稳定性
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	// 序列化为稳定字符串
	b := make([]byte, 0, 1024)
	for _, r := range rows {
		b = append(b, r.Name...)
		b = append(b, '|')
		b = append(b, r.Flags.String()...)
		b = append(b, '|')
		for _, ip := range r.IPs {
			b = append(b, ip...)
			b = append(b, ',')
		}
		b = append(b, '\n')
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
