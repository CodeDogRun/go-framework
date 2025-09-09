package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/CodeDogRun/go-framework/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormLogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"gorm.io/plugin/dbresolver"
)

var (
	models         = make([]any, 0)
	modelsWithName = make(map[string]any)
	keys           = make([]string, 0)
	wrappers       = make(map[string]*gorm.DB)
)

func Register(value any) {
	models = append(models, value)
}

func RegisterWithName(name string, value any) {
	modelsWithName[name] = value
}

func Init() {
	for key := range config.GetStringMap("database") {
		dsn := fmt.Sprintf(
			"%v:%v@tcp(%v)/%v?charset=%v&parseTime=True&loc=Local",
			config.GetString("database."+key+".username"),
			config.GetString("database."+key+".password"),
			config.GetString("database."+key+".addr"),
			config.GetString("database."+key+".name"),
			config.GetString("database."+key+".charset"),
		)

		conn, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
			PrepareStmt:                              true,
			DisableForeignKeyConstraintWhenMigrating: true,
			NamingStrategy: schema.NamingStrategy{
				SingularTable: true,
			},
			Logger: gormLogger.New(
				log.New(os.Stdout, "", log.Lshortfile|log.LstdFlags),
				gormLogger.Config{
					SlowThreshold:             time.Duration(config.GetInt("database."+key+".slow_threshold")) * time.Millisecond,
					LogLevel:                  gormLogger.Warn,
					IgnoreRecordNotFoundError: true,
					Colorful:                  true,
				},
			),
		})

		if err != nil {
			panic(fmt.Sprintf("数据库连接失败: %v", err))
		}

		err = conn.Use(
			dbresolver.Register(dbresolver.Config{}).
				SetConnMaxIdleTime(time.Hour).
				SetConnMaxLifetime(24 * time.Hour).
				SetMaxIdleConns(100).
				SetMaxOpenConns(200),
		)
		if err != nil {
			panic(fmt.Sprintf("数据库连接池设置失败: %v", err))
			return
		}

		if len(models) > 0 && config.GetBool("database."+key+".auto_migrate") {
			err = conn.Set("gorm:table_options", "ENGINE=InnoDB ROW_FORMAT=DYNAMIC").AutoMigrate(models...)
			if err != nil {
				panic(fmt.Sprintf("数据表检测失败: %v", err))
			}
		}
		if len(modelsWithName) > 0 && config.GetBool("database."+key+".auto_migrate") {
			for name, value := range modelsWithName {
				err = conn.Set("gorm:table_options", "ENGINE=InnoDB ROW_FORMAT=DYNAMIC").Table(name).AutoMigrate(value)
				if err != nil {
					panic(fmt.Sprintf("数据表检测失败: %v", err))
				}
			}
		}

		keys = append(keys, key)
		wrappers[key] = conn
	}

}

func Connect(node ...string) *gorm.DB {
	if len(keys) == 0 {
		panic("尚未创建数据库连接, 请检查配置文件！")
	}

	key := keys[0]
	if len(node) > 0 && len(node[0]) > 0 {
		key = node[0]
	}

	client, ok := wrappers[key]
	if !ok {
		panic("不存在的数据库连接: " + key)
	}

	return client
}

// Generics
// 调用方法: user, err := db.Generics[model.UserMain]().Where("user_id = ?", 10854693428006912).Take(ctx)
func Generics[T any](args ...any) gorm.Interface[T] {
	if args == nil || len(args) == 0 {
		return gorm.G[T](Connect())
	} else {
		node, opts := "", make([]clause.Expression, 0)
		for i := range args {
			switch args[i].(type) {
			case string:
				node = args[i].(string)
			case clause.Expression:
				opts = append(opts, args[i].(clause.Expression))
			case []clause.Expression:
				opts = append(opts, args[i].([]clause.Expression)...)
			}
		}
		return gorm.G[T](Connect(node), opts...)
	}
}

func Paginate(page int, pageSize int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 20
		} else if pageSize > 100 {
			pageSize = 100
		}
		offset := (page - 1) * pageSize
		return db.Offset(offset).Limit(pageSize)
	}
}
