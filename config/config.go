package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/CodeDogRun/go-framework/logger"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const (
	machineIdBits = int64(5)                                 //机器id位数
	maxMachineId  = int64(-1) ^ (int64(-1) << machineIdBits) //最大机器id

	sequenceBits = int64(12)                               //序列id位数
	sequenceMask = int64(-1) ^ (int64(-1) << sequenceBits) //最大序列id

	machineIdShift     = sequenceBits
	timestampLeftShift = machineIdBits + sequenceBits

	epoch = int64(1672502401000) //初始毫秒,时间是: 2023-07-05 11:44:18
)

var (
	env       *viper.Viper
	snowFlake *flake
)

type flake struct {
	sync.Mutex
	lastStamp  int64
	machineId  int64 //机器id,0~31
	sequenceID int64
}

func Init(path string) {
	env = viper.New()
	env.SetConfigFile(path)

	if err := env.ReadInConfig(); err != nil {
		panic(fmt.Sprintf("环境配置加载失败: %v", err))
	}

	env.WatchConfig()
	env.OnConfigChange(func(e fsnotify.Event) {
		logger.Warning("配置发生变化: %v", e.Name)
	})

	executable, _ := os.Executable()
	baseDir, _ := os.Getwd()
	env.Set("executable", executable)
	env.Set("baseDir", baseDir)

	//初始化雪花算法
	snowFlake = &flake{
		lastStamp:  0,
		sequenceID: 0,
		machineId:  env.GetInt64("app.machine_id"),
	}
}

func Raw() *viper.Viper {
	return env
}

func Set(key string, value any) {
	env.Set(key, value)
}

func Get(key string) any {
	return env.Get(key)
}

func GetString(key string) string {
	return env.GetString(key)
}

func GetInt64(key string) int64 {
	return env.GetInt64(key)
}

func GetInt(key string) int {
	return env.GetInt(key)
}

func GetBool(key string) bool {
	return env.GetBool(key)
}

func GetFloat64(key string) float64 {
	return env.GetFloat64(key)
}

func GetIntSlice(key string) []int {
	return env.GetIntSlice(key)
}

func GetStringSlice(key string) []string {
	return env.GetStringSlice(key)
}

func GetStringMap(key string) map[string]any {
	return env.GetStringMap(key)
}

func GetStringMapStringSlice(key string) map[string][]string {
	return env.GetStringMapStringSlice(key)
}

func GetTime(key string) time.Time {
	return env.GetTime(key)
}

func GetDuration(key string) time.Duration {
	return env.GetDuration(key)
}

func GetSnowId() uint64 {
	return uint64(snowFlake.nextID())
}

func GetSnowIdString() string {
	return strconv.FormatInt(snowFlake.nextID(), 10)
}

func (w *flake) nextID() int64 {
	w.Lock()
	defer w.Unlock()

	mill := time.Now().UnixMilli()

	if mill == w.lastStamp {
		w.sequenceID = (w.sequenceID + 1) & sequenceMask
		//当一个毫秒内分配的id数>4096个时，只能等待到下一毫秒去分配。
		if w.sequenceID == 0 {
			for mill > w.lastStamp {
				mill = time.Now().UnixMilli()
			}
		}
	} else {
		w.sequenceID = 0
	}

	w.lastStamp = mill

	return (w.lastStamp-epoch)<<timestampLeftShift | w.machineId<<machineIdShift | w.sequenceID
}

func TestSnowFlake(count int) {
	startTime := time.Now()
	ch := make(chan uint64, count)
	var wg sync.WaitGroup

	wg.Add(count)
	defer close(ch)
	//并发 count个goroutine 进行 snowFlake ID 生成
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			id := GetSnowId()
			ch <- id
		}()
	}
	wg.Wait()
	m, repeatCount := make(map[uint64]int), 0
	for i := 0; i < count; i++ {
		id := <-ch
		// 如果 map 中存在为 id 的 key, 说明生成的 snowflake ID 有重复
		_, ok := m[id]
		if ok {
			repeatCount++
			continue
		}
		// 将 id 作为 key 存入 map
		m[id] = i
	}
	// 成功生成 snowflake ID
	if repeatCount > 0 {
		log.Printf("累计生成 %v 个数据ID, %v个重复数据, 耗时: %v", len(m), repeatCount, time.Since(startTime))
	} else {
		log.Printf("累计生成 %v 个数据ID, 无重复数据! 耗时: %v", len(m), time.Since(startTime))
	}
}
